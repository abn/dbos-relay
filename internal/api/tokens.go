package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/store"
	storegen "github.com/abn/relay/internal/store/gen"
)

// ListTokens lists all API key tokens for an organisation.
func (s *Server) ListTokens(ctx context.Context, request gen.ListTokensRequestObject) (gen.ListTokensResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.ListTokensdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", fmt.Sprintf("organisation %q not found", orgName)),
			}, nil
		}
		return gen.ListTokensdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "database store is unavailable"),
		}, nil
	}

	keys, err := s.store.ListAPIKeys(ctx, org.ID)
	if err != nil {
		return gen.ListTokensdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	resp := make([]gen.Token, 0, len(keys))
	for _, k := range keys {
		appNames := k.ApplicationNames
		if appNames == nil {
			appNames = []string{}
		}
		perms := k.Permissions
		if perms == nil {
			perms = []string{}
		}
		resp = append(resp, gen.Token{
			TokenName:   k.Name,
			AppIds:      appNames,
			Permissions: perms,
			CreatedAt:   k.CreatedAt.Time,
		})
	}

	return gen.ListTokens200JSONResponse(resp), nil
}

// CreateToken mints a new API key and stores its record.
func (s *Server) CreateToken(ctx context.Context, request gen.CreateTokenRequestObject) (gen.CreateTokenResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
			return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", fmt.Sprintf("organisation %q not found", orgName)),
			}, nil
		}
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "database store is unavailable"),
		}, nil
	}

	plain, rec, err := auth.Mint()
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	callerIdentity, _ := auth.IdentityFromContext(ctx)
	isCallerAdmin := false
	if callerIdentity != nil {
		isCallerAdmin = callerIdentity.IsAdmin || callerIdentity.Role == auth.RoleAdmin
	}

	appNames := []string{}
	permissions := []string{}
	if request.Body != nil {
		if request.Body.AppNames != nil {
			appNames = *request.Body.AppNames
		}
		if request.Body.Permissions != nil {
			permissions = *request.Body.Permissions
		}
	}

	if callerIdentity != nil && !isCallerAdmin {
		// Non-admin caller must have token.write to mint tokens
		if !auth.HasPermission(callerIdentity.Permissions, auth.PermTokenWrite) {
			s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
			return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusForbidden,
				Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Missing required permission: token.write"),
			}, nil
		}

		// App-scoped caller cannot mint unscoped tokens or tokens outside their allowed apps
		if len(callerIdentity.ApplicationNames) > 0 {
			if len(appNames) == 0 {
				s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
				return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
					StatusCode: http.StatusForbidden,
					Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "App-scoped caller cannot mint unscoped tokens"),
				}, nil
			}
			for _, app := range appNames {
				found := false
				for _, allowed := range callerIdentity.ApplicationNames {
					if app == allowed {
						found = true
						break
					}
				}
				if !found {
					s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
					return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
						StatusCode: http.StatusForbidden,
						Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", fmt.Sprintf("Cannot grant app %q outside caller scope", app)),
					}, nil
				}
			}
		}

		// Non-admin caller cannot grant permissions outside caller's own permissions
		if len(permissions) == 0 {
			permissions = append([]string{}, callerIdentity.Permissions...)
		} else {
			for _, perm := range permissions {
				if !auth.HasPermission(callerIdentity.Permissions, perm) {
					s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
					return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
						StatusCode: http.StatusForbidden,
						Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", fmt.Sprintf("Cannot grant permission %q outside caller scope", perm)),
					}, nil
				}
			}
		}
	} else {
		// Admin caller: default to catalog permissions if none requested
		if len(permissions) == 0 {
			permissions = auth.CatalogPermissions()
		}
	}

	_, err = s.store.CreateAPIKey(ctx, storegen.CreateAPIKeyParams{
		OrganisationID:   org.ID,
		Name:             request.TokenName,
		Lookup:           rec.Lookup,
		KeyHash:          rec.Hash,
		ApplicationNames: appNames,
		Permissions:      permissions,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		if store.IsUniqueViolation(err) {
			return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusConflict,
				Body:       MakeErrorModel(http.StatusConflict, "Conflict", fmt.Sprintf("an active token named %q already exists", request.TokenName)),
			}, nil
		}
		return gen.CreateTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpTokenCreate, auditStatusSuccess, string(gen.AuditTargetTypeToken), request.TokenName, map[string]any{"permissions": permissions, "applications": appNames})
	return gen.CreateToken201JSONResponse{
		Token:     plain,
		TokenName: request.TokenName,
	}, nil
}

// DeleteToken revokes an existing API key token by name.
func (s *Server) DeleteToken(ctx context.Context, request gen.DeleteTokenRequestObject) (gen.DeleteTokenResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.auditOperation(ctx, request.OrgName, "", auditOpTokenRevoke, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
			return gen.DeleteTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", fmt.Sprintf("organisation %q not found", orgName)),
			}, nil
		}
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenRevoke, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.DeleteTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "database store is unavailable"),
		}, nil
	}

	key, err := s.store.GetAPIKeyByName(ctx, storegen.GetAPIKeyByNameParams{
		OrganisationID: org.ID,
		Name:           request.TokenName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.auditOperation(ctx, request.OrgName, "", auditOpTokenRevoke, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
			return gen.DeleteTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Token not found", "Token not found"),
			}, nil
		}
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenRevoke, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.DeleteTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	_, err = s.store.RevokeAPIKey(ctx, storegen.RevokeAPIKeyParams{
		ID:             key.ID,
		OrganisationID: org.ID,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenRevoke, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.DeleteTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpTokenRevoke, auditStatusSuccess, string(gen.AuditTargetTypeToken), request.TokenName, nil)
	return gen.DeleteToken204Response{}, nil
}

// UpdateToken renames an existing API key. The key secret is unchanged.
// Provenance: Conductor OpenAPI PATCH /v2/orgs/{orgName}/tokens/{tokenName} (operation: updateToken), vendored from https://cloud.dbos.dev/conductor/v2/openapi.json, fetched 2026-09-30.
func (s *Server) UpdateToken(ctx context.Context, request gen.UpdateTokenRequestObject) (gen.UpdateTokenResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	if request.Body == nil || request.Body.NewName == nil || *request.Body.NewName == "" {
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.UpdateTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "newName is required"),
		}, nil
	}
	newName := *request.Body.NewName

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
			return gen.UpdateTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", fmt.Sprintf("organisation %q not found", orgName)),
			}, nil
		}
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.UpdateTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "database store is unavailable"),
		}, nil
	}

	key, err := s.store.GetAPIKeyByName(ctx, storegen.GetAPIKeyByNameParams{
		OrganisationID: org.ID,
		Name:           request.TokenName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
			return gen.UpdateTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Token not found", "Token not found"),
			}, nil
		}
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.UpdateTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	_, err = s.store.RenameAPIKey(ctx, storegen.RenameAPIKeyParams{
		ID:             key.ID,
		OrganisationID: org.ID,
		Name:           newName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || store.IsUniqueViolation(err) {
			s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
			return gen.UpdateTokendefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusConflict,
				Body:       MakeErrorModel(http.StatusConflict, "Conflict", fmt.Sprintf("an active token named %q already exists", newName)),
			}, nil
		}
		s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusFailure, string(gen.AuditTargetTypeToken), request.TokenName, nil)
		return gen.UpdateTokendefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpTokenUpdate, auditStatusSuccess, string(gen.AuditTargetTypeToken), newName, map[string]any{"previous_name": request.TokenName})
	return gen.UpdateToken204Response{}, nil
}
