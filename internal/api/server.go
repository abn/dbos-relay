package api

import (
	"context"
	"log/slog"
	"sync"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/router"
	storegen "github.com/abn/relay/internal/store/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

// StoreReader provides database access for the API server.
type StoreReader interface {
	GetOrganisationByName(ctx context.Context, name string) (storegen.Organisation, error)
	GetApplicationByName(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error)
	ListApplicationsByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Application, error)
	UpsertApplication(ctx context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error)
	UpdateApplicationSettings(ctx context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error)
	DeleteApplication(ctx context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error)
	ListExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]storegen.Executor, error)
	ListAPIKeys(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ApiKey, error)
	GetAPIKeyByLookup(ctx context.Context, lookup string) (storegen.ApiKey, error)
	CreateAPIKey(ctx context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error)
	RevokeAPIKey(ctx context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error)
	UpsertOrganisation(ctx context.Context, name string) (storegen.Organisation, error)
	CreateAlertingRule(ctx context.Context, arg storegen.CreateAlertingRuleParams) (storegen.AlertingRule, error)
	GetAlertingRule(ctx context.Context, arg storegen.GetAlertingRuleParams) (storegen.AlertingRule, error)
	ListAlertingRulesByApplication(ctx context.Context, applicationID pgtype.UUID) ([]storegen.AlertingRule, error)
	DeleteAlertingRule(ctx context.Context, arg storegen.DeleteAlertingRuleParams) (int64, error)
	GetAutoscalingPolicy(ctx context.Context, applicationID pgtype.UUID) (storegen.AutoscalingPolicy, error)
	UpsertAutoscalingPolicy(ctx context.Context, arg storegen.UpsertAutoscalingPolicyParams) (storegen.AutoscalingPolicy, error)
	DeleteAutoscalingPolicy(ctx context.Context, applicationID pgtype.UUID) (int64, error)

	// Identity store queries
	CreateUser(ctx context.Context, arg storegen.CreateUserParams) (storegen.User, error)
	UpsertUser(ctx context.Context, arg storegen.UpsertUserParams) (storegen.User, error)
	GetUserBySubject(ctx context.Context, subject string) (storegen.User, error)
	GetUserByUsername(ctx context.Context, username string) (storegen.User, error)
	GetUserByID(ctx context.Context, id pgtype.UUID) (storegen.User, error)
	ListMembersByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ListMembersByOrganisationRow, error)
	GetMember(ctx context.Context, arg storegen.GetMemberParams) (storegen.GetMemberRow, error)
	UpsertMemberRole(ctx context.Context, arg storegen.UpsertMemberRoleParams) (storegen.OrganisationMember, error)
	RemoveMember(ctx context.Context, arg storegen.RemoveMemberParams) (storegen.OrganisationMember, error)
	GetUserPrimaryOrganisation(ctx context.Context, userID pgtype.UUID) (storegen.GetUserPrimaryOrganisationRow, error)
	ListRoles(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Role, error)
	GetRole(ctx context.Context, arg storegen.GetRoleParams) (storegen.Role, error)
	CreateRole(ctx context.Context, arg storegen.CreateRoleParams) (storegen.Role, error)
	DeleteRole(ctx context.Context, arg storegen.DeleteRoleParams) (storegen.Role, error)
	ListDomainClaims(ctx context.Context, organisationID pgtype.UUID) ([]storegen.DomainClaim, error)
	GetDomainClaim(ctx context.Context, domain string) (storegen.DomainClaim, error)
	CreateDomainClaim(ctx context.Context, arg storegen.CreateDomainClaimParams) (storegen.DomainClaim, error)
	DeleteDomainClaim(ctx context.Context, arg storegen.DeleteDomainClaimParams) (storegen.DomainClaim, error)
	CreateAuditLog(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error)
	TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error
	ListAuditLogs(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error)
	ListAllOrganisations(ctx context.Context) ([]storegen.Organisation, error)
	UpdateOrganisation(ctx context.Context, arg storegen.UpdateOrganisationParams) (storegen.Organisation, error)
	DeleteExpiredAuditLogs(ctx context.Context, arg storegen.DeleteExpiredAuditLogsParams) (int64, error)
}

// Server implements gen.StrictServerInterface.
type Server struct {
	router      router.Router
	store       StoreReader
	logger      *slog.Logger
	authEnabled bool
	validator   any
	orgSecrets  sync.Map
	broadcaster *EventBroadcaster
}

// NewServer creates a new API server instance.
func NewServer(r router.Router, s StoreReader, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		router:      r,
		store:       s,
		logger:      logger,
		broadcaster: NewEventBroadcaster(),
	}
}

// Broadcaster returns the server's event broadcaster.
func (s *Server) Broadcaster() *EventBroadcaster {
	return s.broadcaster
}

// PublishEvent distributes a streaming event to subscribers.
func (s *Server) PublishEvent(evt StreamEvent) {
	if s.broadcaster != nil {
		s.broadcaster.Publish(evt)
	}
}

// WithAuth configures authentication for the server.
func (s *Server) WithAuth(enabled bool, validator any) *Server {
	s.authEnabled = enabled
	s.validator = validator
	return s
}

var _ gen.StrictServerInterface = (*Server)(nil)
