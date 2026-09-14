package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/problem"
	storegen "github.com/abn/relay/internal/store/gen"
)

// AuthMiddleware creates an HTTP middleware that extracts and validates bearer tokens,
// and enforces tenant and application scoping based on the request path.
func AuthMiddleware(server *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if (server == nil || !server.authEnabled) && r.URL.Path != "/v1/metrics" {
				// No-auth mode: implicit local identity
				ctx := auth.WithIdentity(r.Context(), &auth.UserIdentity{
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
					orgSlug := slugOrgName(user.Username)
					org, err := server.store.UpsertOrganisation(r.Context(), orgSlug)
					if err == nil {
						_, _ = server.store.UpsertMemberRole(r.Context(), storegen.UpsertMemberRoleParams{
							OrganisationID: org.ID,
							UserID:         user.ID,
							RoleName:       auth.RoleAdmin,
						})
						orgName = org.Name
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
			isJoin := r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/join")
			if targetOrgName == "" && r.URL.Path == "/v2/users/me" {
				// Allow /v2/users/me
			} else if targetOrgName != "" {
				// We have a target organization, let's verify access
				org, err := server.store.GetOrganisationByName(r.Context(), targetOrgName)
				if err != nil {
					if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "not found") {
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Not Found", Status: http.StatusNotFound, Detail: "Organisation not found"})
					} else {
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Internal Error", Status: http.StatusInternalServerError, Detail: err.Error()})
					}
					return
				}

				if isJoin {
					identity.OrgName = org.Name
				} else if identity.IsAPIKey {
					// API Key org check
					if org.ID != identity.OrgID {
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "API key does not belong to this organisation"})
						return
					}
					identity.OrgName = org.Name

					// App-scoped keys cannot access org-level routes (routes where appName is empty)
					if len(identity.ApplicationNames) > 0 && targetAppName == "" {
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
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: err.Error()})
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
								problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "API key does not have access to this application"})
								return
							}
						}
					}

					// Check permissions
					reqPerm := ""
					if r.Method == http.MethodGet {
						reqPerm = auth.PermApplicationRead
					} else {
						reqPerm = auth.PermApplicationWrite
					}

					hasPerm := auth.HasPermission(identity.Permissions, reqPerm)
					if identity.IsAPIKey && len(identity.Permissions) == 0 {
						hasPerm = true
					}
					if !hasPerm && !identity.IsAdmin && identity.Role != auth.RoleAdmin {
						problem.Write(w, &problem.Problem{Type: "about:blank", Title: "Forbidden", Status: http.StatusForbidden, Detail: "Missing required permission: " + reqPerm})
						return
					}
				}
			} else {
				// No org in path, might be like /v2/users/me, let's ensure primary org logic for OIDC users
				if !identity.IsAPIKey {
					user, err := server.store.GetUserBySubject(r.Context(), identity.Subject)
					if err == nil {
						primaryOrg, err := server.store.GetUserPrimaryOrganisation(r.Context(), user.ID)
						if err != nil || primaryOrg.Name == "" {
							orgSlug := slugOrgName(user.Username)
							org, err := server.store.UpsertOrganisation(r.Context(), orgSlug)
							if err == nil {
								_, _ = server.store.UpsertMemberRole(r.Context(), storegen.UpsertMemberRoleParams{
									OrganisationID: org.ID,
									UserID:         user.ID,
									RoleName:       auth.RoleAdmin,
								})
								identity.OrgName = org.Name
								identity.Role = auth.RoleAdmin
							}
						} else {
							identity.OrgName = primaryOrg.Name
							identity.Role = primaryOrg.RoleName
						}
					}
				}
			}

			ctx := auth.WithIdentity(r.Context(), identity)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
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
