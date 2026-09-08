package api

import (
	"context"
	"net/http"

	"github.com/abn/relay/internal/api/gen"
)

func oidcNotAvailableError() gen.ErrorModel {
	return MakeErrorModel(http.StatusNotFound, "Not Found", "Endpoint requires OAuth and is not available in no-auth mode")
}

// GetOrg stub returns 404 in no-auth mode.
func (s *Server) GetOrg(ctx context.Context, request gen.GetOrgRequestObject) (gen.GetOrgResponseObject, error) {
	return gen.GetOrgdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// UpdateOrg stub returns 404 in no-auth mode.
func (s *Server) UpdateOrg(ctx context.Context, request gen.UpdateOrgRequestObject) (gen.UpdateOrgResponseObject, error) {
	return gen.UpdateOrgdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// JoinOrg stub returns 404 in no-auth mode.
func (s *Server) JoinOrg(ctx context.Context, request gen.JoinOrgRequestObject) (gen.JoinOrgResponseObject, error) {
	return gen.JoinOrgdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// GenerateSecret stub returns 404 in no-auth mode.
func (s *Server) GenerateSecret(ctx context.Context, request gen.GenerateSecretRequestObject) (gen.GenerateSecretResponseObject, error) {
	return gen.GenerateSecretdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// ListMembers stub returns 404 in no-auth mode.
func (s *Server) ListMembers(ctx context.Context, request gen.ListMembersRequestObject) (gen.ListMembersResponseObject, error) {
	return gen.ListMembersdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// RemoveMember stub returns 404 in no-auth mode.
func (s *Server) RemoveMember(ctx context.Context, request gen.RemoveMemberRequestObject) (gen.RemoveMemberResponseObject, error) {
	return gen.RemoveMemberdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// GrantRole stub returns 404 in no-auth mode.
func (s *Server) GrantRole(ctx context.Context, request gen.GrantRoleRequestObject) (gen.GrantRoleResponseObject, error) {
	return gen.GrantRoledefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// ListRoles stub returns 404 in no-auth mode.
func (s *Server) ListRoles(ctx context.Context, request gen.ListRolesRequestObject) (gen.ListRolesResponseObject, error) {
	return gen.ListRolesdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// CreateRole stub returns 404 in no-auth mode.
func (s *Server) CreateRole(ctx context.Context, request gen.CreateRoleRequestObject) (gen.CreateRoleResponseObject, error) {
	return gen.CreateRoledefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// DeleteRole stub returns 404 in no-auth mode.
func (s *Server) DeleteRole(ctx context.Context, request gen.DeleteRoleRequestObject) (gen.DeleteRoleResponseObject, error) {
	return gen.DeleteRoledefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// ListDomainClaims stub returns 404 in no-auth mode.
func (s *Server) ListDomainClaims(ctx context.Context, request gen.ListDomainClaimsRequestObject) (gen.ListDomainClaimsResponseObject, error) {
	return gen.ListDomainClaimsdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// RequestDomainClaim stub returns 404 in no-auth mode.
func (s *Server) RequestDomainClaim(ctx context.Context, request gen.RequestDomainClaimRequestObject) (gen.RequestDomainClaimResponseObject, error) {
	return gen.RequestDomainClaimdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// ReleaseDomainClaim stub returns 404 in no-auth mode.
func (s *Server) ReleaseDomainClaim(ctx context.Context, request gen.ReleaseDomainClaimRequestObject) (gen.ReleaseDomainClaimResponseObject, error) {
	return gen.ReleaseDomainClaimdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// ListAuditLogs stub returns 404 in no-auth mode.
func (s *Server) ListAuditLogs(ctx context.Context, request gen.ListAuditLogsRequestObject) (gen.ListAuditLogsResponseObject, error) {
	return gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// RegisterUser stub returns 404 in no-auth mode.
func (s *Server) RegisterUser(ctx context.Context, request gen.RegisterUserRequestObject) (gen.RegisterUserResponseObject, error) {
	return gen.RegisterUserdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}

// GetCurrentUser stub returns 404 in no-auth mode.
func (s *Server) GetCurrentUser(ctx context.Context, request gen.GetCurrentUserRequestObject) (gen.GetCurrentUserResponseObject, error) {
	return gen.GetCurrentUserdefaultApplicationProblemPlusJSONResponse{
		StatusCode: http.StatusNotFound,
		Body:       oidcNotAvailableError(),
	}, nil
}
