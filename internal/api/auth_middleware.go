package api

import (
	"net/http"
	"strings"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/problem"
	storegen "github.com/abn/relay/internal/store/gen"
)

// AuthMiddleware creates an HTTP middleware that extracts and validates bearer tokens.
func AuthMiddleware(server *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if server == nil || !server.authEnabled {
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

			// Public unauthenticated routes
			path := r.URL.Path
			if path == "/healthz" || path == "/openapi.json" || path == "/openapi-3.0.json" ||
				path == "/openapi.yaml" || path == "/docs" || strings.HasPrefix(path, "/schemas/") {
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

			// API Key path (starts with dbos_)
			if strings.HasPrefix(token, auth.KeyPrefix) {
				lookup := auth.Lookup(token)
				keyRec, err := server.store.GetAPIKeyByLookup(r.Context(), lookup)
				if err != nil || !auth.Verify(token, keyRec.KeyHash) {
					problem.Write(w, &problem.Problem{
						Type:   "about:blank",
						Title:  "Unauthorized",
						Status: http.StatusUnauthorized,
						Detail: "Invalid API key",
					})
					return
				}

				ctx := auth.WithIdentity(r.Context(), &auth.UserIdentity{
					Subject:  keyRec.Lookup,
					Username: keyRec.Name,
					IsAdmin:  true,
					IsAPIKey: true,
					Token:    token,
				})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

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
					Detail: err.Error(),
				})
				return
			}

			// Auto-register user if not present
			username := claims.PreferredUsername
			if username == "" && claims.Email != "" {
				username = strings.Split(claims.Email, "@")[0]
			}
			if username == "" {
				username = claims.Subject
			}

			user, err := server.store.UpsertUser(r.Context(), storegen.UpsertUserParams{
				Subject:  claims.Subject,
				Username: username,
				Email:    claims.Email,
				IsAdmin:  false,
			})
			if err != nil {
				user, _ = server.store.GetUserBySubject(r.Context(), claims.Subject)
			}

			// Check domain claims for auto-membership
			if claims.Email != "" && strings.Contains(claims.Email, "@") {
				domain := strings.Split(claims.Email, "@")[1]
				if dc, err := server.store.GetDomainClaim(r.Context(), domain); err == nil {
					_, _ = server.store.UpsertMemberRole(r.Context(), storegen.UpsertMemberRoleParams{
						OrganisationID: dc.OrganisationID,
						UserID:         user.ID,
						RoleName:       auth.RoleViewer,
					})
				}
			}

			// Ensure user has at least one organization
			primaryOrg, err := server.store.GetUserPrimaryOrganisation(r.Context(), user.ID)
			orgName := primaryOrg.Name
			roleName := primaryOrg.RoleName
			if err != nil || orgName == "" {
				org, err := server.store.UpsertOrganisation(r.Context(), user.Username)
				if err == nil {
					_, _ = server.store.UpsertMemberRole(r.Context(), storegen.UpsertMemberRoleParams{
						OrganisationID: org.ID,
						UserID:         user.ID,
						RoleName:       auth.RoleAdmin,
					})
					orgName = org.Name
					roleName = auth.RoleAdmin
				}
			}

			ctx := auth.WithIdentity(r.Context(), &auth.UserIdentity{
				Subject:  claims.Subject,
				Username: user.Username,
				Email:    user.Email,
				IsAdmin:  user.IsAdmin,
				OrgName:  orgName,
				Role:     roleName,
				Token:    token,
			})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
