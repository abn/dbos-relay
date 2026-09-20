package api

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	storegen "github.com/abn/relay/internal/store/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

type orgSecretEntry struct {
	secret    string
	expiresAt time.Time
}

var joinSecretTTL = 1 * time.Hour

func isGlobalRole(name string) bool {
	return name == auth.RoleAdmin || name == auth.RoleOperator || name == auth.RoleViewer
}

func isAdmin(ctx context.Context) bool {
	identity, ok := auth.IdentityFromContext(ctx)
	if !ok || identity == nil {
		return false
	}
	if identity.IsAdmin {
		return true
	}
	return identity.Role == auth.RoleAdmin
}

func (s *Server) handleGetCurrentUser(ctx context.Context, _ gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	identity, ok := auth.IdentityFromContext(ctx)
	if !ok || identity == nil {
		return gen.GetCurrentUserdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       MakeErrorModel(http.StatusUnauthorized, "Unauthorized", "Not authenticated"),
		}, nil
	}

	if identity.IsAPIKey {
		roleName := auth.RoleViewer
		if identity.Role != "" {
			roleName = identity.Role
		} else if identity.IsAdmin {
			roleName = auth.RoleAdmin
		}
		perms := identity.Permissions
		if len(perms) == 0 {
			perms = auth.CatalogPermissions()
		}

		orgName := identity.OrgName
		if orgName == "" {
			if orgGetter, ok := s.store.(interface {
				GetOrganisationByID(ctx context.Context, id pgtype.UUID) (storegen.Organisation, error)
			}); ok {
				org, err := orgGetter.GetOrganisationByID(ctx, identity.OrgID)
				if err == nil {
					orgName = org.Name
				} else {
					orgName = "local"
				}
			} else {
				orgName = "local"
			}
		}

		profile := gen.UserProfile{
			Name:             identity.Username,
			Email:            "",
			OrgName:          orgName,
			OrgId:            formatUUID(identity.OrgID),
			SubscriptionPlan: "self-hosted",
			CreatedAt:        time.Now(),
			Role: &gen.RoleOutput{
				Name:        roleName,
				IsGlobal:    isGlobalRole(roleName),
				Permissions: perms,
			},
		}
		return gen.GetCurrentUser200JSONResponse(profile), nil
	}

	user, err := s.store.GetUserByUsername(ctx, identity.Username)
	if err != nil {
		user, err = s.store.GetUserBySubject(ctx, identity.Subject)
		if err != nil {
			return gen.GetCurrentUserdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Not Found", "User not found"),
			}, nil
		}
	}

	primaryOrg, err := s.store.GetUserPrimaryOrganisation(ctx, user.ID)
	orgName := primaryOrg.Name
	roleName := primaryOrg.RoleName
	if err != nil || orgName == "" {
		orgName = identity.OrgName
		roleName = identity.Role
	}
	if orgName == "" {
		orgName = "local"
	}
	if roleName == "" {
		roleName = auth.RoleViewer
	}

	profile := gen.UserProfile{
		Name:             user.Username,
		Email:            user.Email,
		OrgName:          orgName,
		OrgId:            formatUUID(primaryOrg.ID),
		SubscriptionPlan: "self-hosted",
		CreatedAt:        user.CreatedAt.Time,
		Role: &gen.RoleOutput{
			Name:     roleName,
			IsGlobal: isGlobalRole(roleName),
			Permissions: func() []string {
				role, err := s.store.GetRole(ctx, storegen.GetRoleParams{
					OrganisationID: primaryOrg.ID,
					Name:           roleName,
				})
				if err == nil {
					return role.Permissions
				}
				return []string{}
			}(),
		},
	}
	if user.IsAdmin {
		admin := true
		profile.IsDbosAdmin = &admin
	}

	return gen.GetCurrentUser200JSONResponse(profile), nil
}

func (s *Server) handleRegisterUser(ctx context.Context, request gen.RegisterUserRequestObject) (gen.RegisterUserResponseObject, error) {
	identity, ok := auth.IdentityFromContext(ctx)
	if !ok || identity == nil {
		return gen.RegisterUserdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusUnauthorized,
			Body:       MakeErrorModel(http.StatusUnauthorized, "Unauthorized", "Not authenticated"),
		}, nil
	}

	if request.Body == nil || request.Body.Name == "" {
		return gen.RegisterUserdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Username is required"),
		}, nil
	}

	// An API key cannot register arbitrary users unless it is an admin
	if identity.IsAPIKey && !isAdmin(ctx) {
		return gen.RegisterUserdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Admin permission required to register users with API key"),
		}, nil
	}

	subject := request.Body.Name
	email := ""
	if !isAdmin(ctx) {
		if identity.Subject != "" {
			subject = identity.Subject
		}
		if identity.Email != "" {
			email = identity.Email
		}
	}

	user, err := s.store.UpsertUser(ctx, storegen.UpsertUserParams{
		Subject:  subject,
		Username: request.Body.Name,
		Email:    email,
		IsAdmin:  false,
	})
	if err != nil {
		return gen.RegisterUserdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", err.Error()),
		}, nil
	}

	return gen.RegisterUser201JSONResponse{
		Id: formatUUID(user.ID),
	}, nil
}

func (s *Server) handleGetOrg(ctx context.Context, request gen.GetOrgRequestObject) (gen.GetOrgResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.GetOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	retention := org.AuditLogRetentionDays
	if retention < MinAuditLogRetentionDays || retention > MaxAuditLogRetentionDays {
		retention = DefaultAuditLogRetentionDays
	}
	return gen.GetOrg200JSONResponse{
		Id:                    formatUUID(org.ID),
		Name:                  org.Name,
		CreatedAt:             org.CreatedAt.Time,
		SubscriptionPlan:      "self-hosted",
		AuditLogRetentionDays: retention,
	}, nil
}

func (s *Server) handleUpdateOrg(ctx context.Context, request gen.UpdateOrgRequestObject) (gen.UpdateOrgResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.UpdateOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	fail := func(target, detail string, code int, title string) gen.UpdateOrgResponseObject {
		s.auditOperation(ctx, request.OrgName, "", auditOpOrgUpdate, auditStatusFailure, string(gen.AuditTargetTypeOrganization), target, nil)
		return gen.UpdateOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: code,
			Body:       MakeErrorModel(code, title, detail),
		}
	}

	if !isAdmin(ctx) {
		return fail(request.OrgName, "Admin role required", http.StatusForbidden, "Forbidden"), nil
	}

	var newName *string
	var retentionDays *int32
	details := make(map[string]any)
	if request.Body != nil {
		if request.Body.NewName != nil && *request.Body.NewName != "" && *request.Body.NewName != org.Name {
			if err := validateOrgName(*request.Body.NewName); err != nil {
				return fail(request.OrgName, err.Error(), http.StatusUnprocessableEntity, "Validation Error"), nil
			}
			if _, err := s.store.GetOrganisationByName(ctx, *request.Body.NewName); err == nil {
				return fail(request.OrgName, fmt.Sprintf("organisation %q already exists", *request.Body.NewName), http.StatusConflict, "Conflict"), nil
			}
			newName = request.Body.NewName
			details["new_name"] = *newName
		}
		if request.Body.AuditLogRetentionDays != nil {
			days := *request.Body.AuditLogRetentionDays
			if days < MinAuditLogRetentionDays || days > MaxAuditLogRetentionDays {
				return fail(request.OrgName, fmt.Sprintf("audit log retention must be between %d and %d days", MinAuditLogRetentionDays, MaxAuditLogRetentionDays), http.StatusUnprocessableEntity, "Validation Error"), nil
			}
			retentionDays = &days
			details["audit_log_retention_days"] = days
		}
	}

	if newName == nil && retentionDays == nil {
		return gen.UpdateOrg204Response{}, nil
	}

	updated, err := s.store.UpdateOrganisation(ctx, storegen.UpdateOrganisationParams{
		ID:                    org.ID,
		Name:                  newName,
		AuditLogRetentionDays: retentionDays,
	})
	if err != nil {
		return fail(request.OrgName, err.Error(), http.StatusInternalServerError, "Internal Server Error"), nil
	}

	s.auditOperation(ctx, updated.Name, "", auditOpOrgUpdate, auditStatusSuccess, string(gen.AuditTargetTypeOrganization), updated.Name, details)
	return gen.UpdateOrg204Response{}, nil
}

func (s *Server) handleJoinOrg(ctx context.Context, request gen.JoinOrgRequestObject) (gen.JoinOrgResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	identity, _ := auth.IdentityFromContext(ctx)
	username := "user"
	if identity != nil && identity.Username != "" {
		username = identity.Username
	}

	if request.Body == nil || request.Body.Secret == "" {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusFailure, string(gen.AuditTargetTypeUser), username, nil)
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Secret is required"),
		}, nil
	}

	val, ok := s.orgSecrets.Load(org.Name)
	if !ok {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusFailure, string(gen.AuditTargetTypeUser), username, nil)
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Invalid join secret"),
		}, nil
	}

	entry, ok := val.(orgSecretEntry)
	if !ok || subtle.ConstantTimeCompare([]byte(entry.secret), []byte(request.Body.Secret)) != 1 {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusFailure, string(gen.AuditTargetTypeUser), username, nil)
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Invalid join secret"),
		}, nil
	}

	if time.Now().After(entry.expiresAt) {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusFailure, string(gen.AuditTargetTypeUser), username, nil)
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Join secret has expired"),
		}, nil
	}

	// Check if already an admin in this org - do not downgrade
	existingMember, err := s.store.GetMember(ctx, storegen.GetMemberParams{
		OrganisationID: org.ID,
		Username:       username,
	})
	if err == nil && existingMember.RoleName == auth.RoleAdmin {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusSuccess, string(gen.AuditTargetTypeUser), username, nil)
		return gen.JoinOrg204Response{}, nil
	}

	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusFailure, string(gen.AuditTargetTypeUser), username, nil)
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", "User not found"),
		}, nil
	}

	role := auth.RoleViewer

	_, err = s.store.UpsertMemberRole(ctx, storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       role,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusFailure, string(gen.AuditTargetTypeUser), username, nil)
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpUserJoin, auditStatusSuccess, string(gen.AuditTargetTypeUser), username, nil)
	return gen.JoinOrg204Response{}, nil
}

func (s *Server) handleGenerateSecret(ctx context.Context, request gen.GenerateSecretRequestObject) (gen.GenerateSecretResponseObject, error) {
	if !requireOrgWrite(ctx) {
		s.auditOperation(ctx, request.OrgName, "", auditOpSecretGenerate, auditStatusFailure, string(gen.AuditTargetTypeOrganization), request.OrgName, nil)
		return gen.GenerateSecretdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Missing required permission: organization.write"),
		}, nil
	}

	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpSecretGenerate, auditStatusFailure, string(gen.AuditTargetTypeOrganization), request.OrgName, nil)
		return gen.GenerateSecretdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpSecretGenerate, auditStatusFailure, string(gen.AuditTargetTypeOrganization), request.OrgName, nil)
		return gen.GenerateSecretdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "Generating secret"),
		}, nil
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)

	s.orgSecrets.Store(org.Name, orgSecretEntry{
		secret:    secret,
		expiresAt: time.Now().Add(joinSecretTTL),
	})

	// The secret itself never lands in the audit log, only its issuance.
	s.auditOperation(ctx, request.OrgName, "", auditOpSecretGenerate, auditStatusSuccess, string(gen.AuditTargetTypeOrganization), request.OrgName, nil)
	return gen.GenerateSecret201JSONResponse{
		Secret: secret,
	}, nil
}

func (s *Server) handleListMembers(ctx context.Context, request gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.ListMembersdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	members, err := s.store.ListMembersByOrganisation(ctx, org.ID)
	if err != nil {
		return gen.ListMembersdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	userMap := make(map[string]gen.RoleOutput)

	// Fetch all roles for the org to map permissions
	roles, _ := s.store.ListRoles(ctx, org.ID)
	rolePerms := make(map[string][]string)
	for _, r := range roles {
		rolePerms[r.Name] = r.Permissions
	}

	for _, m := range members {
		perms := rolePerms[m.RoleName]
		if perms == nil {
			perms = []string{}
		}
		userMap[m.Username] = gen.RoleOutput{
			Name:        m.RoleName,
			IsGlobal:    isGlobalRole(m.RoleName),
			Permissions: perms,
		}
	}

	return gen.ListMembers200JSONResponse{
		OrgName: request.OrgName,
		Users:   userMap,
	}, nil
}

func (s *Server) handleRemoveMember(ctx context.Context, request gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	if !isAdmin(ctx) {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserRemove, auditStatusFailure, string(gen.AuditTargetTypeUser), request.Username, nil)
		return gen.RemoveMemberdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Admin role required"),
		}, nil
	}

	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.RemoveMemberdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	user, err := s.store.GetUserByUsername(ctx, request.Username)
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserRemove, auditStatusFailure, string(gen.AuditTargetTypeUser), request.Username, nil)
		return gen.RemoveMemberdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("user %q not found", request.Username)),
		}, nil
	}

	_, err = s.store.RemoveMember(ctx, storegen.RemoveMemberParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpUserRemove, auditStatusFailure, string(gen.AuditTargetTypeUser), request.Username, nil)
		return gen.RemoveMemberdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpUserRemove, auditStatusSuccess, string(gen.AuditTargetTypeUser), user.Username, nil)

	return gen.RemoveMember200JSONResponse{
		Id:    formatUUID(user.ID),
		Name:  user.Username,
		OrgId: formatUUID(org.ID),
		Email: user.Email,
	}, nil
}

func (s *Server) handleGrantRole(ctx context.Context, request gen.GrantRoleRequestObject) (gen.GrantRoleResponseObject, error) {
	if !isAdmin(ctx) {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleGrant, auditStatusFailure, string(gen.AuditTargetTypeUser), request.Username, map[string]any{"role_name": request.RoleName})
		return gen.GrantRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Admin role required"),
		}, nil
	}

	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.GrantRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	user, err := s.store.GetUserByUsername(ctx, request.Username)
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleGrant, auditStatusFailure, string(gen.AuditTargetTypeUser), request.Username, map[string]any{"role_name": request.RoleName})
		return gen.GrantRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("user %q not found", request.Username)),
		}, nil
	}

	_, err = s.store.UpsertMemberRole(ctx, storegen.UpsertMemberRoleParams{
		OrganisationID: org.ID,
		UserID:         user.ID,
		RoleName:       request.RoleName,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleGrant, auditStatusFailure, string(gen.AuditTargetTypeUser), request.Username, map[string]any{"role_name": request.RoleName})
		return gen.GrantRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpRoleGrant, auditStatusSuccess, string(gen.AuditTargetTypeUser), user.Username, map[string]any{"role_name": request.RoleName})

	return gen.GrantRole204Response{}, nil
}

func (s *Server) handleListRoles(ctx context.Context, request gen.ListRolesRequestObject) (gen.ListRolesResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.ListRolesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	roles, err := s.store.ListRoles(ctx, org.ID)
	if err != nil {
		return gen.ListRolesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	out := make([]gen.RoleOutput, 0, len(roles))
	for _, r := range roles {
		out = append(out, gen.RoleOutput{
			Name:        r.Name,
			IsGlobal:    r.IsGlobal,
			Permissions: r.Permissions,
		})
	}

	return gen.ListRoles200JSONResponse(out), nil
}

func (s *Server) handleCreateRole(ctx context.Context, request gen.CreateRoleRequestObject) (gen.CreateRoleResponseObject, error) {
	if !isAdmin(ctx) {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleCreate, auditStatusFailure, string(gen.AuditTargetTypeRole), roleNameForAudit(request.Body), nil)
		return gen.CreateRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Admin role required"),
		}, nil
	}

	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.CreateRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	if request.Body == nil || request.Body.Name == "" {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleCreate, auditStatusFailure, string(gen.AuditTargetTypeRole), roleNameForAudit(request.Body), nil)
		return gen.CreateRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Role name is required"),
		}, nil
	}

	roleName := request.Body.Name
	if len(roleName) < 3 || len(roleName) > 30 {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleCreate, auditStatusFailure, string(gen.AuditTargetTypeRole), roleName, nil)
		return gen.CreateRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Role name must be between 3 and 30 characters"),
		}, nil
	}

	var perms []string
	if request.Body.Permissions != nil {
		perms = *request.Body.Permissions
	}

	r, err := s.store.CreateRole(ctx, storegen.CreateRoleParams{
		OrganisationID: org.ID,
		Name:           roleName,
		Permissions:    perms,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleCreate, auditStatusFailure, string(gen.AuditTargetTypeRole), roleName, map[string]any{"permissions": perms})
		return gen.CreateRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpRoleCreate, auditStatusSuccess, string(gen.AuditTargetTypeRole), r.Name, map[string]any{"permissions": r.Permissions})

	loc := fmt.Sprintf("/v2/orgs/%s/roles/%s", request.OrgName, r.Name)
	return gen.CreateRole201JSONResponse{
		Body: gen.CreateRoleOutputBody{
			Name:        r.Name,
			Permissions: r.Permissions,
		},
		Headers: gen.CreateRole201ResponseHeaders{
			Location: &loc,
		},
	}, nil
}

func (s *Server) handleDeleteRole(ctx context.Context, request gen.DeleteRoleRequestObject) (gen.DeleteRoleResponseObject, error) {
	if !isAdmin(ctx) {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleDelete, auditStatusFailure, string(gen.AuditTargetTypeRole), request.RoleName, nil)
		return gen.DeleteRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Admin role required"),
		}, nil
	}

	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.DeleteRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	if isGlobalRole(request.RoleName) {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleDelete, auditStatusFailure, string(gen.AuditTargetTypeRole), request.RoleName, nil)
		return gen.DeleteRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Cannot delete built-in global role"),
		}, nil
	}

	_, err = s.store.DeleteRole(ctx, storegen.DeleteRoleParams{
		OrganisationID: org.ID,
		Name:           request.RoleName,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpRoleDelete, auditStatusFailure, string(gen.AuditTargetTypeRole), request.RoleName, nil)
		return gen.DeleteRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpRoleDelete, auditStatusSuccess, string(gen.AuditTargetTypeRole), request.RoleName, nil)

	return gen.DeleteRole204Response{}, nil
}

func (s *Server) handleListDomainClaims(ctx context.Context, request gen.ListDomainClaimsRequestObject) (gen.ListDomainClaimsResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.ListDomainClaimsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	claims, err := s.store.ListDomainClaims(ctx, org.ID)
	if err != nil {
		return gen.ListDomainClaimsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	out := make([]gen.DomainClaim, 0, len(claims))
	for _, c := range claims {
		out = append(out, gen.DomainClaim{
			Domain:      c.Domain,
			Id:          formatUUID(c.ID),
			OrgName:     request.OrgName,
			Status:      gen.Approved,
			RequestedAt: c.CreatedAt.Time,
			RequestedBy: "admin",
		})
	}

	return gen.ListDomainClaims200JSONResponse{
		Claims: out,
	}, nil
}

func (s *Server) handleRequestDomainClaim(ctx context.Context, request gen.RequestDomainClaimRequestObject) (gen.RequestDomainClaimResponseObject, error) {
	if !isAdmin(ctx) {
		s.auditOperation(ctx, request.OrgName, "", auditOpDomainClaimCreate, auditStatusFailure, string(gen.AuditTargetTypeDomainClaim), domainForAudit(request.Body), nil)
		return gen.RequestDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Admin role required"),
		}, nil
	}

	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.RequestDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	if request.Body == nil || request.Body.Domain == "" {
		s.auditOperation(ctx, request.OrgName, "", auditOpDomainClaimCreate, auditStatusFailure, string(gen.AuditTargetTypeDomainClaim), domainForAudit(request.Body), nil)
		return gen.RequestDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Domain is required"),
		}, nil
	}

	dc, err := s.store.CreateDomainClaim(ctx, storegen.CreateDomainClaimParams{
		OrganisationID: org.ID,
		Domain:         request.Body.Domain,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpDomainClaimCreate, auditStatusFailure, string(gen.AuditTargetTypeDomainClaim), request.Body.Domain, nil)
		return gen.RequestDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", err.Error()),
		}, nil
	}

	identity, _ := auth.IdentityFromContext(ctx)
	username := "user"
	if identity != nil && identity.Username != "" {
		username = identity.Username
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpDomainClaimCreate, auditStatusSuccess, string(gen.AuditTargetTypeDomainClaim), dc.Domain, nil)

	return gen.RequestDomainClaim201JSONResponse{
		Domain:      dc.Domain,
		Id:          formatUUID(dc.ID),
		OrgName:     request.OrgName,
		Status:      gen.Approved,
		RequestedAt: dc.CreatedAt.Time,
		RequestedBy: username,
	}, nil
}

func (s *Server) handleReleaseDomainClaim(ctx context.Context, request gen.ReleaseDomainClaimRequestObject) (gen.ReleaseDomainClaimResponseObject, error) {
	if !isAdmin(ctx) {
		s.auditOperation(ctx, request.OrgName, "", auditOpDomainClaimDelete, auditStatusFailure, string(gen.AuditTargetTypeDomainClaim), request.Domain, nil)
		return gen.ReleaseDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusForbidden,
			Body:       MakeErrorModel(http.StatusForbidden, "Forbidden", "Admin role required"),
		}, nil
	}

	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.ReleaseDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	_, err = s.store.DeleteDomainClaim(ctx, storegen.DeleteDomainClaimParams{
		OrganisationID: org.ID,
		Domain:         request.Domain,
	})
	if err != nil {
		s.auditOperation(ctx, request.OrgName, "", auditOpDomainClaimDelete, auditStatusFailure, string(gen.AuditTargetTypeDomainClaim), request.Domain, nil)
		return gen.ReleaseDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, "", auditOpDomainClaimDelete, auditStatusSuccess, string(gen.AuditTargetTypeDomainClaim), request.Domain, nil)

	return gen.ReleaseDomainClaim204Response{}, nil
}

func (s *Server) handleListAuditLogs(ctx context.Context, request gen.ListAuditLogsRequestObject) (gen.ListAuditLogsResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	if request.Params.Limit != nil && *request.Params.Limit < 0 {
		return gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "limit must be non-negative"),
		}, nil
	}
	if request.Params.Offset != nil && *request.Params.Offset < 0 {
		return gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "offset must be non-negative"),
		}, nil
	}

	limit := int64(100)
	if request.Params.Limit != nil {
		raw := *request.Params.Limit
		if raw > 1000 {
			return gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "limit must not exceed 1000"),
			}, nil
		} else if raw > 0 {
			limit = raw
		}
	}
	offset := int64(0)
	if request.Params.Offset != nil && *request.Params.Offset > 0 {
		offset = *request.Params.Offset
	}

	var startTime, endTime pgtype.Timestamptz
	if request.Params.StartTime != nil {
		startTime = pgtype.Timestamptz{Time: *request.Params.StartTime, Valid: true}
	}
	if request.Params.EndTime != nil {
		endTime = pgtype.Timestamptz{Time: *request.Params.EndTime, Valid: true}
	}

	logs, err := s.store.ListAuditLogs(ctx, storegen.ListAuditLogsParams{
		OrganisationID: org.ID,
		StartTime:      startTime,
		EndTime:        endTime,
		Operation:      request.Params.Operation,
		Subject:        request.Params.Subject,
		Target:         request.Params.Target,
		Limit:          limit,
		Offset:         offset,
	})
	if err != nil {
		return gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	out := make([]gen.AuditLogEntry, 0, len(logs))
	for _, l := range logs {
		var details map[string]interface{}
		if len(l.Details) > 0 {
			_ = json.Unmarshal(l.Details, &details)
		}
		if details == nil {
			details = make(map[string]interface{})
		}
		status := gen.Success
		if sVal, ok := details["status"].(string); ok && sVal == "failure" {
			status = gen.Failure
		}
		sourceIp, _ := details["source_ip"].(string)
		if sourceIp == "" {
			sourceIp, _ = details["ip_address"].(string)
		}

		subjectType := gen.AuditSubjectTypeUser
		if sVal, ok := details["subject_type"].(string); ok && sVal == string(gen.AuditSubjectTypeApiKey) {
			subjectType = gen.AuditSubjectTypeApiKey
		}
		subjectID, _ := details["subject_id"].(string)
		if subjectID == "" {
			subjectID = formatUUID(l.UserID)
		}
		display, _ := details["subject_display"].(string)
		if display == "" {
			display = l.Username
		}

		var target *gen.AuditTarget
		if tType, ok := details["target_type"].(string); ok && tType != "" {
			tID, _ := details["target_id"].(string)
			target = &gen.AuditTarget{
				Id:   tID,
				Type: gen.AuditTargetType(tType),
			}
		}

		out = append(out, gen.AuditLogEntry{
			Id:        formatUUID(l.ID),
			Operation: l.Action,
			EmitTime:  l.CreatedAt.Time,
			Status:    status,
			SourceIp:  sourceIp,
			Details:   &details,
			Subject: gen.AuditSubject{
				Id:      subjectID,
				Display: display,
				Type:    subjectType,
			},
			Target: target,
		})
	}

	return gen.ListAuditLogs200JSONResponse(out), nil
}
