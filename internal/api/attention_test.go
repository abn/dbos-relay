package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/protocol"
	storegen "github.com/abn/relay/internal/store/gen"
)

type mockAttentionRouter struct {
	onDispatch func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)
}

func (m *mockAttentionRouter) Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
	if m.onDispatch != nil {
		return m.onDispatch(ctx, orgName, appName, msg)
	}
	return nil, nil
}

type mockAttentionStore struct {
	org        storegen.Organisation
	app        storegen.Application
	executors  []storegen.Executor
	dispatches []storegen.RecoveryDispatch
}

func (m *mockAttentionStore) GetOrganisationByName(ctx context.Context, name string) (storegen.Organisation, error) {
	return m.org, nil
}

func (m *mockAttentionStore) ListAllOrganisations(_ context.Context) ([]storegen.Organisation, error) {
	return []storegen.Organisation{m.org}, nil
}

func (m *mockAttentionStore) UpdateOrganisation(_ context.Context, arg storegen.UpdateOrganisationParams) (storegen.Organisation, error) {
	if arg.Name != nil {
		m.org.Name = *arg.Name
	}
	if arg.AuditLogRetentionDays != nil {
		m.org.AuditLogRetentionDays = *arg.AuditLogRetentionDays
	}
	return m.org, nil
}

func (m *mockAttentionStore) DeleteExpiredAuditLogs(_ context.Context, _ storegen.DeleteExpiredAuditLogsParams) (int64, error) {
	return 0, nil
}

func (m *mockAttentionStore) GetOrganisationByID(ctx context.Context, id pgtype.UUID) (storegen.Organisation, error) {
	return m.org, nil
}

func (m *mockAttentionStore) GetApplicationByName(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
	return m.app, nil
}

func (m *mockAttentionStore) ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]storegen.Executor, error) {
	return m.executors, nil
}

func (m *mockAttentionStore) ListRecentRecoveryDispatches(ctx context.Context, arg storegen.ListRecentRecoveryDispatchesParams) ([]storegen.RecoveryDispatch, error) {
	return m.dispatches, nil
}

// Stub unused StoreReader methods
func (m *mockAttentionStore) ListApplicationsByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Application, error) {
	return nil, nil
}
func (m *mockAttentionStore) UpsertApplication(ctx context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error) {
	return storegen.Application{}, nil
}
func (m *mockAttentionStore) UpdateApplicationSettings(ctx context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error) {
	return storegen.Application{}, nil
}
func (m *mockAttentionStore) DeleteApplication(ctx context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error) {
	return storegen.Application{}, nil
}
func (m *mockAttentionStore) ListAPIKeys(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ApiKey, error) {
	return nil, nil
}
func (m *mockAttentionStore) GetAPIKeyByLookup(ctx context.Context, lookup string) (storegen.ApiKey, error) {
	return storegen.ApiKey{}, nil
}
func (m *mockAttentionStore) CreateAPIKey(ctx context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error) {
	return storegen.ApiKey{}, nil
}
func (m *mockAttentionStore) RevokeAPIKey(ctx context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error) {
	return storegen.ApiKey{}, nil
}
func (m *mockAttentionStore) UpsertOrganisation(ctx context.Context, name string) (storegen.Organisation, error) {
	return storegen.Organisation{}, nil
}
func (m *mockAttentionStore) CreateAlertingRule(ctx context.Context, arg storegen.CreateAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, nil
}
func (m *mockAttentionStore) GetAlertingRule(ctx context.Context, arg storegen.GetAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, nil
}
func (m *mockAttentionStore) ListAlertingRulesByApplication(ctx context.Context, applicationID pgtype.UUID) ([]storegen.AlertingRule, error) {
	return nil, nil
}
func (m *mockAttentionStore) DeleteAlertingRule(ctx context.Context, arg storegen.DeleteAlertingRuleParams) (int64, error) {
	return 0, nil
}
func (m *mockAttentionStore) CreateUser(ctx context.Context, arg storegen.CreateUserParams) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockAttentionStore) UpsertUser(ctx context.Context, arg storegen.UpsertUserParams) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockAttentionStore) GetUserBySubject(ctx context.Context, subject string) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockAttentionStore) GetUserByUsername(ctx context.Context, username string) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockAttentionStore) GetUserByID(ctx context.Context, id pgtype.UUID) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockAttentionStore) ListMembersByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ListMembersByOrganisationRow, error) {
	return nil, nil
}
func (m *mockAttentionStore) GetMember(ctx context.Context, arg storegen.GetMemberParams) (storegen.GetMemberRow, error) {
	return storegen.GetMemberRow{}, nil
}
func (m *mockAttentionStore) UpsertMemberRole(ctx context.Context, arg storegen.UpsertMemberRoleParams) (storegen.OrganisationMember, error) {
	return storegen.OrganisationMember{}, nil
}
func (m *mockAttentionStore) RemoveMember(ctx context.Context, arg storegen.RemoveMemberParams) (storegen.OrganisationMember, error) {
	return storegen.OrganisationMember{}, nil
}
func (m *mockAttentionStore) GetUserPrimaryOrganisation(ctx context.Context, userID pgtype.UUID) (storegen.GetUserPrimaryOrganisationRow, error) {
	return storegen.GetUserPrimaryOrganisationRow{}, nil
}
func (m *mockAttentionStore) ListRoles(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Role, error) {
	return nil, nil
}
func (m *mockAttentionStore) GetRole(ctx context.Context, arg storegen.GetRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (m *mockAttentionStore) CreateRole(ctx context.Context, arg storegen.CreateRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (m *mockAttentionStore) DeleteRole(ctx context.Context, arg storegen.DeleteRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (m *mockAttentionStore) ListDomainClaims(ctx context.Context, organisationID pgtype.UUID) ([]storegen.DomainClaim, error) {
	return nil, nil
}
func (m *mockAttentionStore) GetDomainClaim(ctx context.Context, domain string) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (m *mockAttentionStore) CreateDomainClaim(ctx context.Context, arg storegen.CreateDomainClaimParams) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (m *mockAttentionStore) DeleteDomainClaim(ctx context.Context, arg storegen.DeleteDomainClaimParams) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (m *mockAttentionStore) CreateAuditLog(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
	return storegen.AuditLog{}, nil
}
func (m *mockAttentionStore) ListAuditLogs(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) {
	return nil, nil
}

func TestGetNeedsAttention(t *testing.T) {
	appID := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
	orgID := pgtype.UUID{Bytes: [16]byte{4, 5, 6}, Valid: true}

	store := &mockAttentionStore{
		org: storegen.Organisation{ID: orgID, Name: "default"},
		app: storegen.Application{ID: appID, Name: "checkout", OrganisationID: orgID},
		executors: []storegen.Executor{
			{ExecutorID: "exec-1", Status: "connected", ApplicationVersion: "v2.0"},
		},
		dispatches: []storegen.RecoveryDispatch{
			{DeadExecutorID: "exec-flapping", DispatchedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}},
			{DeadExecutorID: "exec-flapping", DispatchedAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(-5 * time.Minute), Valid: true}},
		},
	}

	stuckTime := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	versionOld := "v1.0"
	statusError := "ERROR"
	statusPending := "PENDING"

	router := &mockAttentionRouter{
		onDispatch: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
			req, ok := msg.(*protocol.ListWorkflowsRequest)
			if !ok {
				return nil, nil
			}

			// If query for ERROR / FAILED
			if len(req.Body.Status) > 0 && req.Body.Status[0] == "ERROR" {
				return &protocol.ListWorkflowsResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows},
					Output: []protocol.ListWorkflowsResponseBody{
						{WorkflowUUID: "wf-failed-1", Status: &statusError},
					},
				}, nil
			}

			// If query for PENDING
			if len(req.Body.Status) > 0 && req.Body.Status[0] == "PENDING" {
				return &protocol.ListWorkflowsResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows},
					Output: []protocol.ListWorkflowsResponseBody{
						{
							WorkflowUUID:       "wf-stuck-orphaned-1",
							Status:             &statusPending,
							CreatedAt:          &stuckTime,
							ApplicationVersion: &versionOld, // v1.0 has no connected executor!
						},
					},
				}, nil
			}

			// If query for ENQUEUED
			if len(req.Body.Status) > 0 && req.Body.Status[0] == "ENQUEUED" {
				forkedFrom := "wf-parent-1"
				statusEnqueued := "ENQUEUED"
				return &protocol.ListWorkflowsResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows},
					Output: []protocol.ListWorkflowsResponseBody{
						{
							WorkflowUUID:       "wf-stranded-fork-1",
							Status:             &statusEnqueued,
							ForkedFrom:         &forkedFrom,
							ApplicationVersion: &versionOld,
						},
					},
				}, nil
			}

			return &protocol.ListWorkflowsResponse{
				Envelope: protocol.Envelope{Type: protocol.MessageTypeListWorkflows},
			}, nil
		},
	}

	server := api.NewServer(router, store, nil)
	report, code, errModel := server.GetNeedsAttention(context.Background(), "default", "checkout", 15*time.Minute)

	if code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d (err: %v)", code, errModel.Detail)
	}

	if len(report.FailedWorkflows) != 1 {
		t.Errorf("expected 1 failed workflow, got %d", len(report.FailedWorkflows))
	}
	if len(report.OrphanedWorkflows) != 1 {
		t.Errorf("expected 1 orphaned workflow, got %d", len(report.OrphanedWorkflows))
	}
	if len(report.StuckWorkflows) != 1 {
		t.Errorf("expected 1 stuck workflow, got %d", len(report.StuckWorkflows))
	}
	if len(report.StrandedForks) != 1 {
		t.Errorf("expected 1 stranded fork, got %d", len(report.StrandedForks))
	}
	if len(report.FlappingExecutors) != 1 {
		t.Errorf("expected 1 flapping executor, got %d", len(report.FlappingExecutors))
	}
	if report.FlappingExecutors[0].RecoveryCount != 2 {
		t.Errorf("expected flapping recovery count 2, got %d", report.FlappingExecutors[0].RecoveryCount)
	}
	if report.TotalNeedsAttention != 5 {
		t.Errorf("expected total 5 needs-attention items, got %d", report.TotalNeedsAttention)
	}
}

func (m *mockAttentionStore) TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error {
	return nil
}
