package api

import (
	"context"
	"net/http"

	"github.com/abn/relay/internal/api/gen"
)

func oidcNotAvailableError() gen.ErrorModel {
	return MakeErrorModel(http.StatusNotFound, "Not Found", "Endpoint requires OAuth and is not available in no-auth mode")
}

// GetOrg returns 404 in no-auth mode or calls handleGetOrg in auth mode.
func (s *Server) GetOrg(ctx context.Context, request gen.GetOrgRequestObject) (gen.GetOrgResponseObject, error) {
	if !s.authEnabled {
		return gen.GetOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleGetOrg(ctx, request)
}

// UpdateOrg returns 404 in no-auth mode or calls handleUpdateOrg in auth mode.
func (s *Server) UpdateOrg(ctx context.Context, request gen.UpdateOrgRequestObject) (gen.UpdateOrgResponseObject, error) {
	if !s.authEnabled {
		return gen.UpdateOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleUpdateOrg(ctx, request)
}

// JoinOrg returns 404 in no-auth mode or calls handleJoinOrg in auth mode.
func (s *Server) JoinOrg(ctx context.Context, request gen.JoinOrgRequestObject) (gen.JoinOrgResponseObject, error) {
	if !s.authEnabled {
		return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleJoinOrg(ctx, request)
}

// GenerateSecret returns 404 in no-auth mode or calls handleGenerateSecret in auth mode.
func (s *Server) GenerateSecret(ctx context.Context, request gen.GenerateSecretRequestObject) (gen.GenerateSecretResponseObject, error) {
	if !s.authEnabled {
		return gen.GenerateSecretdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleGenerateSecret(ctx, request)
}

// ListMembers returns 404 in no-auth mode or calls handleListMembers in auth mode.
func (s *Server) ListMembers(ctx context.Context, request gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	if !s.authEnabled {
		return gen.ListMembersdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleListMembers(ctx, request)
}

// RemoveMember returns 404 in no-auth mode or calls handleRemoveMember in auth mode.
func (s *Server) RemoveMember(ctx context.Context, request gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	if !s.authEnabled {
		return gen.RemoveMemberdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleRemoveMember(ctx, request)
}

// GrantRole returns 404 in no-auth mode or calls handleGrantRole in auth mode.
func (s *Server) GrantRole(ctx context.Context, request gen.GrantRoleRequestObject) (gen.GrantRoleResponseObject, error) {
	if !s.authEnabled {
		return gen.GrantRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleGrantRole(ctx, request)
}

// ListRoles returns 404 in no-auth mode or calls handleListRoles in auth mode.
func (s *Server) ListRoles(ctx context.Context, request gen.ListRolesRequestObject) (gen.ListRolesResponseObject, error) {
	if !s.authEnabled {
		return gen.ListRolesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleListRoles(ctx, request)
}

// CreateRole returns 404 in no-auth mode or calls handleCreateRole in auth mode.
func (s *Server) CreateRole(ctx context.Context, request gen.CreateRoleRequestObject) (gen.CreateRoleResponseObject, error) {
	if !s.authEnabled {
		return gen.CreateRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleCreateRole(ctx, request)
}

// DeleteRole returns 404 in no-auth mode or calls handleDeleteRole in auth mode.
func (s *Server) DeleteRole(ctx context.Context, request gen.DeleteRoleRequestObject) (gen.DeleteRoleResponseObject, error) {
	if !s.authEnabled {
		return gen.DeleteRoledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleDeleteRole(ctx, request)
}

// ListDomainClaims returns 404 in no-auth mode or calls handleListDomainClaims in auth mode.
func (s *Server) ListDomainClaims(ctx context.Context, request gen.ListDomainClaimsRequestObject) (gen.ListDomainClaimsResponseObject, error) {
	if !s.authEnabled {
		return gen.ListDomainClaimsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleListDomainClaims(ctx, request)
}

// RequestDomainClaim returns 404 in no-auth mode or calls handleRequestDomainClaim in auth mode.
func (s *Server) RequestDomainClaim(ctx context.Context, request gen.RequestDomainClaimRequestObject) (gen.RequestDomainClaimResponseObject, error) {
	if !s.authEnabled {
		return gen.RequestDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleRequestDomainClaim(ctx, request)
}

// ReleaseDomainClaim returns 404 in no-auth mode or calls handleReleaseDomainClaim in auth mode.
func (s *Server) ReleaseDomainClaim(ctx context.Context, request gen.ReleaseDomainClaimRequestObject) (gen.ReleaseDomainClaimResponseObject, error) {
	if !s.authEnabled {
		return gen.ReleaseDomainClaimdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleReleaseDomainClaim(ctx, request)
}

// ListAuditLogs returns 404 in no-auth mode or calls handleListAuditLogs in auth mode.
func (s *Server) ListAuditLogs(ctx context.Context, request gen.ListAuditLogsRequestObject) (gen.ListAuditLogsResponseObject, error) {
	if !s.authEnabled {
		return gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleListAuditLogs(ctx, request)
}

// RegisterUser returns 404 in no-auth mode or calls handleRegisterUser in auth mode.
func (s *Server) RegisterUser(ctx context.Context, request gen.RegisterUserRequestObject) (gen.RegisterUserResponseObject, error) {
	if !s.authEnabled {
		return gen.RegisterUserdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleRegisterUser(ctx, request)
}

// GetCurrentUser returns 404 in no-auth mode or calls handleGetCurrentUser in auth mode.
func (s *Server) GetCurrentUser(ctx context.Context, request gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	if !s.authEnabled {
		return gen.GetCurrentUserdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       oidcNotAvailableError(),
		}, nil
	}
	return s.handleGetCurrentUser(ctx, request)
}
