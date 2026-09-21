package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/problem"
	storegen "github.com/abn/relay/internal/store/gen"
)

// hasMetricsSuffix reports whether path is a per-app metrics route of the
// form .../apps/{app}/metrics, tolerating a trailing slash.
func hasMetricsSuffix(path string) bool {
	trimmed := strings.TrimSuffix(path, "/")
	segs := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segs) < 6 || segs[0] != "v2" || segs[1] != "orgs" {
		return false
	}
	// Expect v2/orgs/{org}/apps/{app}/metrics exactly.
	return segs[3] == "apps" && segs[5] == "metrics" && len(segs) == 6
}

// orgLevelRequiredPerm maps an organization-level route (no app name) to
// the permission it exercises, or "" when the route needs no permission
// check here. Application registry routes keep application.read/write,
// token routes keep token.read/write, and the permissions catalog stays
// open (auth-only); join is genuinely open subject to its secret while
// secrets POST is restricted at the handler (handleGenerateSecret);
// everything else under an org requires the organization permissions.
func orgLevelRequiredPerm(method, path string) string {
	trimmed := strings.TrimSuffix(path, "/")
	segs := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(segs) < 3 || segs[0] != "v2" || segs[1] != "orgs" {
		return ""
	}
	rest := segs[2:]
	if len(rest) == 1 {
		if method == http.MethodGet {
			return auth.PermOrgRead
		}
		return auth.PermOrgWrite
	}
	switch rest[1] {
	case "apps":
		if method == http.MethodGet {
			return auth.PermApplicationRead
		}
		return auth.PermApplicationWrite
	case "tokens":
		if method == http.MethodGet {
			return auth.PermTokenRead
		}
		return auth.PermTokenWrite
	case "permissions":
		// Open catalog: any authenticated caller may list grantable
		// permissions.
		return ""
	case "join":
		// Genuinely open subject to the join secret; API keys are
		// rejected explicitly in the middleware.
		return ""
	case "secrets":
		// Restricted at the handler by requireOrgWrite
		// (handleGenerateSecret); no middleware permission check.
		return ""
	case "audit-logs", "members", "roles", "domain-claims":
		if method == http.MethodGet {
			return auth.PermOrgRead
		}
		return auth.PermOrgWrite
	default:
		if method == http.MethodGet {
			return auth.PermOrgRead
		}
		return auth.PermOrgWrite
	}
}

// AuthMiddleware creates an HTTP middleware that extracts and validates bearer tokens,
// and enforces tenant and application scoping based on the request path.
func AuthMiddleware(server *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (server == nil || !server.authEnabled) && r.URL.Path != "/v1/metrics" {
				// No-auth mode: implicit local identity
				ctx := StashSourceIP(r.Context(), getRealIPFromHeaders(r.Header.Get("X-Forwarded-For"), r.RemoteAddr))
				ctx = auth.WithIdentity(ctx, &auth.UserIdentity{
					Subject:  "local",
					Username: "local",
					Email:    "local@local",
					IsAdmin:  true,
					OrgName:  "local",
					Role:     auth.RoleAdmin,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			path := r.URL.Path
			if path == "/healthz" || path == "/openapi.json" || path == "/openapi-3.0.json" ||
				path == "/openapi.yaml" || strings.HasPrefix(path, "/docs") || strings.HasPrefix(path, "/schemas/") {
				next.ServeHTTP(w, r)
				return
			}

			authHeader := r.Header.Get("Authorization")
			if authHeader == "" && r.URL.Query().Get("token") != "" {
				authHeader = "Bearer " + r.URL.Query().Get("token")
			}
			if authHeader == "" {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  "Unauthorized",
					Status: http.StatusUnauthorized,
					Detail: "Authorization header required",
				})
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				problem.Write(w, &problem.Problem{
					Type:   "about:blank",
					Title:  "Unauthorized",
					Status: http.StatusUnauthorized,
					Detail: "Bearer token format required",
				})
				return
			}

			token := parts[1]
			var identity *auth.UserIdentity

			// API Key path (starts with dbos_)
			if strings.HasPrefix(token, auth.KeyPrefix) {
				lookup := auth.Lookup(token)
				keyRec, err := server.store.GetAPIKeyByLookup(r.Context(), lookup)
				if err != nil || !auth.AuthenticateKey(token, keyRec.KeyHash) {
					problem.Write(w, &problem.Problem{
						Type:   "about:blank",
						Title:  "Unauthorized",
						Status: http.StatusUnauthorized,
						Detail: "Invalid API key",
					})
					return
				}

				_ = server.store.TouchAPIKeyLastUsed(r.Context(), keyRec.ID)

				orgName := ""
				if orgGetter, ok := server.store.(interface {
					GetOrganisationByID(ctx context.Context, id pgtype.UUID) (storegen.Organisation, error)
				}); ok {
					if org, err := orgGetter.GetOrganisationByID(r.Context(), keyRec.OrganisationID); err == nil {
						orgName = org.Name
					}
				}
				identity = &auth.UserIdentity{
					Subject:          keyRec.Lookup,
					Username:         keyRec.Name,
					IsAdmin:          false,
					IsAPIKey:         true,
					Token:            token,
					OrgID:            keyRec.OrganisationID,
					OrgName:          orgName,
					ApplicationNames: keyRec.ApplicationNames,
					Permissions:      keyRec.Permissions,
				}
			} else {
				// OIDC JWT path
				val, ok := server.validator.(auth.Validator)
				if !ok || val == nil {
					problem.Write(w, &problem.Problem{
						Type:   "about:blank",
						Title:  "Unauthorized",
						Status: http.StatusUnauthorized,
						Detail: "OIDC validator not configured",
					})
					return
				}

				claims, err := val.Validate(r.Context(), token)
				if err != nil {
					problem.Write(w, &problem.Problem{
						Type:   "about:blank",
						Title:  "Unauthorized",
						Status: http.StatusUnauthorized,
						Detail: "invalid token",
					})
					return
				}

				username := claims.PreferredUsername
				if username == "" && claims.Email != "" {
					username = strings.Split(claims.Email, "@")[0]
				}
				if username == "" {
					username = claims.Subject
				}

				_, err = server.store.GetUserBySubject(r.Context(), claims.Subject)
				isNewUser := err != nil

				user, err := server.store.UpsertUser(r.Context(), storegen.UpsertUserParams{
					Subject:  claims.Subject,
					Username: username,
					Email:    claims.Email,
					IsAdmin:  false,
				})
				if err != nil {
					user, _ = server.store.GetUserBySubject(r.Context(), claims.Subject)
				}

				if isNewUser && claims.Email != "" && strings.Contains(claims.Email, "@") {
					domain := strings.Split(claims.Email, "@")[1]
					if dc, err := server.store.GetDomainClaim(r.Context(), domain); err == nil {
						_, _ = server.store.UpsertMemberRole(r.Context(), storegen.UpsertMemberRoleParams{
							OrganisationID: dc.OrganisationID,
							UserID:         user.ID,
							RoleName:       auth.RoleViewer,
						})
					}
				}

				var orgName, roleName string
				primaryOrg, err := server.store.GetUserPrimaryOrganisation(r.Context(), user.ID)
				if err != nil || primaryOrg.Name == "" {
					pOrgName, err := resolvePersonalOrg(r.Context(), server.store, user)
					if err == nil {
						orgName = pOrgName
						roleName = auth.RoleAdmin
					}
				} else {
					orgName = primaryOrg.Name
					roleName = primaryOrg.RoleName
				}

				identity = &auth.UserIdentity{
					Subject:  claims.Subject,
					Username: user.Username,
					Email:    user.Email,
					IsAdmin:  user.IsAdmin,
					OrgName:  orgName,
					Role:     roleName,
					Token:    token,
				}
			}

			// Perform authorization checks based on path
			targetOrgName := r.PathValue("orgName")
			targetAppName := r.PathValue("appName")
			isJoin := false
			if r.Method == http.MethodPost {
				if r.Pattern != "" {
					isJoin = r.Pattern == "POST /v2/orgs/{orgName}/join" || strings.HasSuffix(r.Pattern, "/v2/orgs/{orgName}/join")
				} else {
					isJoin = r.URL.Path == "/v2/orgs/"+targetOrgName+"/join" || strings.HasSuffix(r.URL.Path, "/v2/orgs/"+targetOrgName+"/join")
				}
			}
			if targetOrgName == "" && r.URL.Path == "/v2/users/me" {
				// Allow /v2/users/me
			} else if targetOrgName != "" {
				// We have a target organization, let's verify access
				org, err := server.store.GetOrganisationByName(r.Context(), targetOrgName)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "not found") {
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Not Found", Status: http.StatusNotFound, Detail: "Organisation not found"})
					} else {
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Internal Error", Status: http.StatusInternalServerError, Detail: "Failed to resolve organisation"})
					}
					return
				}

				if isJoin {
					if identity.IsAPIKey {
						recordMiddlewareDenial(r, server, identity)
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "API keys cannot join organisations"})
						return
					}
					identity.OrgName = org.Name
				} else if identity.IsAPIKey {
					// API Key org check
					if org.ID != identity.OrgID {
						recordMiddlewareDenial(r, server, identity)
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "API key does not belong to this organisation"})
						return
					}
					identity.OrgName = org.Name

					// App-scoped keys cannot access org-level routes (routes where appName is empty)
					if len(identity.ApplicationNames) > 0 && targetAppName == "" {
						recordMiddlewareDenial(r, server, identity)
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "API key is scoped to specific applications and cannot access organization-level resources"})
						return
					}
				} else {
					// OIDC user org check
					member, err := server.store.GetMember(r.Context(), storegen.GetMemberParams{
						OrganisationID: org.ID,
						Username:       identity.Username,
					})
					if err != nil {
						recordMiddlewareDenial(r, server, identity)
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "User is not a member of this organisation"})
						return
					}

					identity.OrgName = org.Name
					identity.OrgID = org.ID
					identity.Role = member.RoleName

					// Fetch permissions from role
					role, err := server.store.GetRole(r.Context(), storegen.GetRoleParams{
						OrganisationID: org.ID,
						Name:           member.RoleName,
					})
					if err == nil {
						identity.Permissions = role.Permissions
					} else {
						identity.Permissions = []string{}
					}
				}

				if !isJoin {
					// Check app scoping
					if targetAppName != "" && identity.IsAPIKey {
						if len(identity.ApplicationNames) > 0 {
							allowed := false
							for _, appName := range identity.ApplicationNames {
								if appName == targetAppName {
									allowed = true
									break
								}
							}
							if !allowed {
								recordMiddlewareDenial(r, server, identity)
								problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "API key does not have access to this application"})
								return
							}
						}
					}

					if targetAppName == "" && !identity.IsAdmin && identity.Role != auth.RoleAdmin {
						// Organization-level routes require the permission
						// the route exercises. Application and token routes
						// keep their own permissions; org management,
						// membership, roles, and audit logs require the
						// organization permissions. This applies to API keys
						// as well as OIDC users. The bootstrap admin role
						// always passes.
						required := orgLevelRequiredPerm(r.Method, r.URL.Path)
						if required != "" && !auth.HasPermission(identity.Permissions, required) {
							recordMiddlewareDenial(r, server, identity)
							problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "Missing required permission: " + required})
							return
						}
						// Gate 1 is the verdict for org-level routes; the
						// app-permission check below applies to app-scoped
						// routes only.
					} else if targetAppName != "" {
						// Check permissions
						reqPerm := ""
						if r.Method == http.MethodGet {
							reqPerm = auth.PermApplicationRead
						} else {
							reqPerm = auth.PermApplicationWrite
						}

						// Metric reads accept metric.read or application.read,
						// matching the scrape endpoint.
						metricsPass := false
						if r.Method == http.MethodGet && hasMetricsSuffix(r.URL.Path) {
							reqPerm = auth.PermMetricRead
							metricsPass = auth.HasPermission(identity.Permissions, auth.PermMetricRead) || auth.HasPermission(identity.Permissions, auth.PermApplicationRead)
						}

						hasPerm := metricsPass || auth.HasPermission(identity.Permissions, reqPerm)
						// The role-name bypass below is deliberate: the admin
						// role is the bootstrap superuser and always passes,
						// while everyone else is judged on permissions.
						if !hasPerm && !identity.IsAdmin && identity.Role != auth.RoleAdmin {
							recordMiddlewareDenial(r, server, identity)
							problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "Missing required permission: " + reqPerm})
							return
						}
					}
				}
			} else {
				// Global endpoints require admin privileges. This branch
				// stays unaudited: the paths carry no org scope to record
				// under and never map to a taxonomy operation.
				if r.URL.Path != "/v2/users/me" && r.URL.Path != "/v1/metrics" && !identity.IsAdmin {
					problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "Global endpoints require admin privileges"})
					return
				}

				// No org in path, might be like /v2/users/me, let's ensure primary org logic for OIDC users
				if !identity.IsAPIKey {
					user, err := server.store.GetUserBySubject(r.Context(), identity.Subject)
					if err == nil {
						primaryOrg, err := server.store.GetUserPrimaryOrganisation(r.Context(), user.ID)
						if err != nil || primaryOrg.Name == "" {
							pOrgName, err := resolvePersonalOrg(r.Context(), server.store, user)
							if err == nil {
								identity.OrgName = pOrgName
								identity.Role = auth.RoleAdmin
							}
						} else {
							identity.OrgName = primaryOrg.Name
							identity.Role = primaryOrg.RoleName
						}
					}
				}
			}

			ctx := StashSourceIP(r.Context(), getRealIPFromHeaders(r.Header.Get("X-Forwarded-For"), r.RemoteAddr))
			ctx = auth.WithIdentity(ctx, identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func resolvePersonalOrg(ctx context.Context, store StoreReader, user storegen.User) (string, error) {
	baseSlug := slugOrgName(user.Username)
	org, err := store.GetOrganisationByName(ctx, baseSlug)
	if err == nil {
		member, mErr := store.GetMember(ctx, storegen.GetMemberParams{
			OrganisationID: org.ID,
			Username:       user.Username,
		})
		if mErr == nil && member.UserID == user.ID {
			return org.Name, nil
		}
		// Colliding organisation name owned by someone else. Disambiguate with user ID hash suffix.
		hashSuffix := fmt.Sprintf("%x", sha256.Sum256([]byte(user.Username+":"+user.ID.String())))[:8]
		disambiguated := baseSlug
		if len(disambiguated) > 21 {
			disambiguated = disambiguated[:21]
		}
		disambiguated = disambiguated + "_" + hashSuffix
		newOrg, err := store.UpsertOrganisation(ctx, disambiguated)
		if err != nil {
			return "", err
		}
		_, _ = store.UpsertMemberRole(ctx, storegen.UpsertMemberRoleParams{
			OrganisationID: newOrg.ID,
			UserID:         user.ID,
			RoleName:       auth.RoleAdmin,
		})
		return newOrg.Name, nil
	}

	newOrg, err := store.UpsertOrganisation(ctx, baseSlug)
	if err != nil {
		return "", err
	}
	_, _ = store.UpsertMemberRole(ctx, storegen.UpsertMemberRoleParams{
		OrganisationID: newOrg.ID,
		UserID:         user.ID,
		RoleName:       auth.RoleAdmin,
	})
	return newOrg.Name, nil
}

func slugOrgName(s string) string {
	s = strings.ToLower(s)
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			sb.WriteRune(r)
		} else if r == '.' || r == '-' {
			sb.WriteRune('_')
		}
	}
	res := sb.String()
	for len(res) < 3 {
		res += "_"
	}
	if len(res) > 30 {
		res = res[:30]
	}
	return res
}
