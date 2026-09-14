package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api"
	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	storegen "github.com/abn/relay/internal/store/gen"
)

type mockStoreReader struct {
	getOrgByNameFunc       func(ctx context.Context, name string) (storegen.Organisation, error)
	getAppByNameFunc       func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error)
	listAppsByOrgFunc      func(ctx context.Context, orgID pgtype.UUID) ([]storegen.Application, error)
	upsertAppFunc          func(ctx context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error)
	updateAppSettingsFunc  func(ctx context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error)
	deleteAppFunc          func(ctx context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error)
	listExecutorsByAppFunc func(ctx context.Context, appID pgtype.UUID) ([]storegen.Executor, error)
	listAPIKeysFunc        func(ctx context.Context, orgID pgtype.UUID) ([]storegen.ApiKey, error)
	createAPIKeyFunc       func(ctx context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error)
	revokeAPIKeyFunc       func(ctx context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error)
	upsertOrgFunc          func(ctx context.Context, name string) (storegen.Organisation, error)
	listAlertRulesFunc     func(ctx context.Context, applicationID pgtype.UUID) ([]storegen.AlertingRule, error)
}

func (m *mockStoreReader) GetOrganisationByName(ctx context.Context, name string) (storegen.Organisation, error) {
	if m.getOrgByNameFunc != nil {
		return m.getOrgByNameFunc(ctx, name)
	}
	return storegen.Organisation{}, errors.New("unexpected GetOrganisationByName")
}

func (m *mockStoreReader) GetOrganisationByID(ctx context.Context, id pgtype.UUID) (storegen.Organisation, error) {
	return storegen.Organisation{ID: id, Name: "mock-org"}, nil
}

func (m *mockStoreReader) GetApplicationByName(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
	if m.getAppByNameFunc != nil {
		return m.getAppByNameFunc(ctx, arg)
	}
	return storegen.Application{}, errors.New("unexpected GetApplicationByName")
}

func (m *mockStoreReader) ListApplicationsByOrganisation(ctx context.Context, orgID pgtype.UUID) ([]storegen.Application, error) {
	if m.listAppsByOrgFunc != nil {
		return m.listAppsByOrgFunc(ctx, orgID)
	}
	return nil, errors.New("unexpected ListApplicationsByOrganisation")
}

func (m *mockStoreReader) UpsertApplication(ctx context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error) {
	if m.upsertAppFunc != nil {
		return m.upsertAppFunc(ctx, arg)
	}
	return storegen.Application{}, errors.New("unexpected UpsertApplication")
}

func (m *mockStoreReader) UpdateApplicationSettings(ctx context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error) {
	if m.updateAppSettingsFunc != nil {
		return m.updateAppSettingsFunc(ctx, arg)
	}
	return storegen.Application{}, errors.New("unexpected UpdateApplicationSettings")
}

func (m *mockStoreReader) DeleteApplication(ctx context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error) {
	if m.deleteAppFunc != nil {
		return m.deleteAppFunc(ctx, arg)
	}
	return storegen.Application{}, errors.New("unexpected DeleteApplication")
}

func (m *mockStoreReader) ListExecutorsByApplication(ctx context.Context, appID pgtype.UUID) ([]storegen.Executor, error) {
	if m.listExecutorsByAppFunc != nil {
		return m.listExecutorsByAppFunc(ctx, appID)
	}
	return nil, errors.New("unexpected ListExecutorsByApplication")
}

func (m *mockStoreReader) ListAPIKeys(ctx context.Context, orgID pgtype.UUID) ([]storegen.ApiKey, error) {
	if m.listAPIKeysFunc != nil {
		return m.listAPIKeysFunc(ctx, orgID)
	}
	return nil, errors.New("unexpected ListAPIKeys")
}

func (m *mockStoreReader) GetAPIKeyByLookup(ctx context.Context, lookup string) (storegen.ApiKey, error) {
	return storegen.ApiKey{}, nil
}

func (m *mockStoreReader) CreateAPIKey(ctx context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error) {
	if m.createAPIKeyFunc != nil {
		return m.createAPIKeyFunc(ctx, arg)
	}
	return storegen.ApiKey{}, errors.New("unexpected CreateAPIKey")
}

func (m *mockStoreReader) RevokeAPIKey(ctx context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error) {
	if m.revokeAPIKeyFunc != nil {
		return m.revokeAPIKeyFunc(ctx, arg)
	}
	return storegen.ApiKey{}, errors.New("unexpected RevokeAPIKey")
}

func (m *mockStoreReader) UpsertOrganisation(ctx context.Context, name string) (storegen.Organisation, error) {
	if m.upsertOrgFunc != nil {
		return m.upsertOrgFunc(ctx, name)
	}
	return storegen.Organisation{}, errors.New("unexpected UpsertOrganisation")
}

func (m *mockStoreReader) CreateAlertingRule(ctx context.Context, arg storegen.CreateAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, nil
}

func (m *mockStoreReader) GetAlertingRule(ctx context.Context, arg storegen.GetAlertingRuleParams) (storegen.AlertingRule, error) {
	return storegen.AlertingRule{}, nil
}

func (m *mockStoreReader) ListAlertingRulesByApplication(ctx context.Context, applicationID pgtype.UUID) ([]storegen.AlertingRule, error) {
	if m.listAlertRulesFunc != nil {
		return m.listAlertRulesFunc(ctx, applicationID)
	}
	return nil, nil
}

func (m *mockStoreReader) DeleteAlertingRule(ctx context.Context, arg storegen.DeleteAlertingRuleParams) (int64, error) {
	return 1, nil
}

func (m *mockStoreReader) CreateUser(ctx context.Context, arg storegen.CreateUserParams) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockStoreReader) UpsertUser(ctx context.Context, arg storegen.UpsertUserParams) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockStoreReader) GetUserBySubject(ctx context.Context, subject string) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockStoreReader) GetUserByUsername(ctx context.Context, username string) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockStoreReader) GetUserByID(ctx context.Context, id pgtype.UUID) (storegen.User, error) {
	return storegen.User{}, nil
}
func (m *mockStoreReader) ListMembersByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ListMembersByOrganisationRow, error) {
	return nil, nil
}
func (m *mockStoreReader) GetMember(ctx context.Context, arg storegen.GetMemberParams) (storegen.GetMemberRow, error) {
	return storegen.GetMemberRow{}, nil
}
func (m *mockStoreReader) UpsertMemberRole(ctx context.Context, arg storegen.UpsertMemberRoleParams) (storegen.OrganisationMember, error) {
	return storegen.OrganisationMember{}, nil
}
func (m *mockStoreReader) RemoveMember(ctx context.Context, arg storegen.RemoveMemberParams) (storegen.OrganisationMember, error) {
	return storegen.OrganisationMember{}, nil
}
func (m *mockStoreReader) GetUserPrimaryOrganisation(ctx context.Context, userID pgtype.UUID) (storegen.GetUserPrimaryOrganisationRow, error) {
	return storegen.GetUserPrimaryOrganisationRow{}, nil
}
func (m *mockStoreReader) ListRoles(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Role, error) {
	return nil, nil
}
func (m *mockStoreReader) GetRole(ctx context.Context, arg storegen.GetRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (m *mockStoreReader) CreateRole(ctx context.Context, arg storegen.CreateRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (m *mockStoreReader) DeleteRole(ctx context.Context, arg storegen.DeleteRoleParams) (storegen.Role, error) {
	return storegen.Role{}, nil
}
func (m *mockStoreReader) ListDomainClaims(ctx context.Context, organisationID pgtype.UUID) ([]storegen.DomainClaim, error) {
	return nil, nil
}
func (m *mockStoreReader) GetDomainClaim(ctx context.Context, domain string) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (m *mockStoreReader) CreateDomainClaim(ctx context.Context, arg storegen.CreateDomainClaimParams) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (m *mockStoreReader) DeleteDomainClaim(ctx context.Context, arg storegen.DeleteDomainClaimParams) (storegen.DomainClaim, error) {
	return storegen.DomainClaim{}, nil
}
func (m *mockStoreReader) CreateAuditLog(ctx context.Context, arg storegen.CreateAuditLogParams) (storegen.AuditLog, error) {
	return storegen.AuditLog{}, nil
}
func (m *mockStoreReader) ListAuditLogs(ctx context.Context, arg storegen.ListAuditLogsParams) ([]storegen.AuditLog, error) {
	return nil, nil
}

type mockRouter struct {
	dispatchFunc func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)
}

func (m *mockRouter) Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
	if m.dispatchFunc != nil {
		return m.dispatchFunc(ctx, orgName, appName, msg)
	}
	return nil, errors.New("unexpected Dispatch call")
}

func makeUUID(b byte) pgtype.UUID {
	var id pgtype.UUID
	id.Bytes[0] = b
	id.Valid = true
	return id
}

func TestApplicationManagement(t *testing.T) {
	ctx := context.Background()
	orgID := makeUUID(10)
	appID := makeUUID(20)

	t.Run("RegisterApp success and errors", func(t *testing.T) {
		priv := true
		store := &mockStoreReader{
			upsertOrgFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				if name != "local" {
					t.Fatalf("expected name 'local', got %q", name)
				}
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			upsertAppFunc: func(ctx context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error) {
				if arg.Name != "my-app" {
					t.Fatalf("expected app name 'my-app', got %q", arg.Name)
				}
				return storegen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		resp, err := srv.RegisterApp(ctx, gen.RegisterAppRequestObject{
			OrgName: "",
			AppName: "my-app",
			Body:    &gen.RegisterAppJSONRequestBody{PrivateMode: &priv},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.RegisterApp204Response); !ok {
			t.Fatalf("expected RegisterApp204Response, got %T", resp)
		}

		// Store error on upsert org
		store.upsertOrgFunc = func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{}, errors.New("db error")
		}
		resp, err = srv.RegisterApp(ctx, gen.RegisterAppRequestObject{AppName: "my-app"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prob, ok := resp.(gen.RegisterAppdefaultApplicationProblemPlusJSONResponse); !ok || prob.StatusCode != http.StatusInternalServerError {
			t.Fatalf("expected 500, got %T (%+v)", resp, resp)
		}
	})

	t.Run("GetApp success and not found", func(t *testing.T) {
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				if name == "my-org" {
					return storegen.Organisation{ID: orgID, Name: name}, nil
				}
				return storegen.Organisation{}, errors.New("org not found")
			},
			getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
				if arg.Name == "my-app" {
					return storegen.Application{
						ID:             appID,
						OrganisationID: orgID,
						Name:           arg.Name,
						Settings:       []byte(`{"privateMode":true,"executorTimeoutSecs":120}`),
					}, nil
				}
				return storegen.Application{}, errors.New("app not found")
			},
		}
		srv := api.NewServer(nil, store, nil)

		// Success
		resp, err := srv.GetApp(ctx, gen.GetAppRequestObject{OrgName: "my-org", AppName: "my-app"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		okResp, ok := resp.(gen.GetApp200JSONResponse)
		if !ok {
			t.Fatalf("expected GetApp200JSONResponse, got %T", resp)
		}
		if okResp.Name != "my-app" || okResp.ExecutorTimeoutSecs != 120 || !okResp.PrivateMode {
			t.Errorf("unexpected app payload: %+v", okResp)
		}

		// Default timeout when <= 0
		store.getAppByNameFunc = func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
			if arg.Name == "my-app" {
				return storegen.Application{
					ID:             appID,
					OrganisationID: orgID,
					Name:           arg.Name,
					Settings:       []byte(`{"executorTimeoutSecs":0}`),
				}, nil
			}
			return storegen.Application{}, errors.New("app not found")
		}
		resp, err = srv.GetApp(ctx, gen.GetAppRequestObject{OrgName: "my-org", AppName: "my-app"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if okResp, ok := resp.(gen.GetApp200JSONResponse); !ok || okResp.ExecutorTimeoutSecs != 60 {
			t.Errorf("expected default timeout 60 for 0, got %+v", resp)
		}

		// Org not found
		resp, err = srv.GetApp(ctx, gen.GetAppRequestObject{OrgName: "nonexistent", AppName: "my-app"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prob, ok := resp.(gen.GetAppdefaultApplicationProblemPlusJSONResponse); !ok || prob.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %T", resp)
		}

		// App not found
		resp, err = srv.GetApp(ctx, gen.GetAppRequestObject{OrgName: "my-org", AppName: "nonexistent"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prob, ok := resp.(gen.GetAppdefaultApplicationProblemPlusJSONResponse); !ok || prob.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %T", resp)
		}
	})

	t.Run("ListApps success", func(t *testing.T) {
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			listAppsByOrgFunc: func(ctx context.Context, id pgtype.UUID) ([]storegen.Application, error) {
				return []storegen.Application{
					{ID: appID, OrganisationID: orgID, Name: "app-1", Settings: []byte(`{}`)},
					{ID: makeUUID(21), OrganisationID: orgID, Name: "app-2", Settings: []byte(`{}`)},
				}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		resp, err := srv.ListApps(ctx, gen.ListAppsRequestObject{OrgName: "my-org"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		listResp, ok := resp.(gen.ListApps200JSONResponse)
		if !ok {
			t.Fatalf("expected ListApps200JSONResponse, got %T", resp)
		}
		if len(listResp) != 2 {
			t.Fatalf("expected 2 apps, got %d", len(listResp))
		}
	})

	t.Run("UpdateApp sparse patch", func(t *testing.T) {
		updated := false
		timeout := int64(300)
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
				return storegen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name, Settings: []byte(`{"privateMode":false}`)}, nil
			},
			updateAppSettingsFunc: func(ctx context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error) {
				if !strings.Contains(string(arg.Settings), `"executorTimeoutSecs":300`) {
					t.Fatalf("settings missing updated timeout: %s", string(arg.Settings))
				}
				updated = true
				return storegen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name, Settings: arg.Settings}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		resp, err := srv.UpdateApp(ctx, gen.UpdateAppRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
			Body:    &gen.UpdateAppJSONRequestBody{ExecutorTimeoutSecs: &timeout},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.UpdateApp204Response); !ok {
			t.Fatalf("expected UpdateApp204Response, got %T", resp)
		}
		if !updated {
			t.Fatal("expected updateAppSettingsFunc to be called")
		}
	})

	t.Run("DeleteApp", func(t *testing.T) {
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			deleteAppFunc: func(ctx context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error) {
				return storegen.Application{ID: appID, Name: arg.Name}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		resp, err := srv.DeleteApp(ctx, gen.DeleteAppRequestObject{OrgName: "my-org", AppName: "my-app"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.DeleteApp204Response); !ok {
			t.Fatalf("expected DeleteApp204Response, got %T", resp)
		}
	})

	t.Run("ListAppVersions and SetLatestAppVersion", func(t *testing.T) {
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
				return storegen.Application{
					ID:             appID,
					OrganisationID: orgID,
					Name:           arg.Name,
					Settings:       []byte(`{"latestVersion":"v2.0.0"}`),
				}, nil
			},
			listExecutorsByAppFunc: func(ctx context.Context, id pgtype.UUID) ([]storegen.Executor, error) {
				return []storegen.Executor{
					{ApplicationVersion: "v1.0.0", ConnectedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}},
					{ApplicationVersion: "v2.0.0", ConnectedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}},
				}, nil
			},
			updateAppSettingsFunc: func(ctx context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error) {
				if !strings.Contains(string(arg.Settings), `"latestVersion":"v3.0.0"`) {
					t.Fatalf("expected v3.0.0, got: %s", string(arg.Settings))
				}
				return storegen.Application{ID: appID, Settings: arg.Settings}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		resp, err := srv.ListAppVersions(ctx, gen.ListAppVersionsRequestObject{OrgName: "my-org", AppName: "my-app"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		vResp, ok := resp.(gen.ListAppVersions200JSONResponse)
		if !ok {
			t.Fatalf("expected ListAppVersions200JSONResponse, got %T", resp)
		}
		if len(vResp) != 2 {
			t.Fatalf("expected 2 distinct versions, got %d", len(vResp))
		}

		setResp, err := srv.SetLatestAppVersion(ctx, gen.SetLatestAppVersionRequestObject{
			OrgName: "my-org",
			AppName: "my-app",
			Body:    &gen.SetLatestAppVersionJSONRequestBody{VersionName: "v3.0.0"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := setResp.(gen.SetLatestAppVersion204Response); !ok {
			t.Fatalf("expected SetLatestAppVersion204Response, got %T", setResp)
		}
	})

	t.Run("ListExecutors", func(t *testing.T) {
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
				return storegen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
			},
			listExecutorsByAppFunc: func(ctx context.Context, id pgtype.UUID) ([]storegen.Executor, error) {
				return []storegen.Executor{
					{
						ExecutorID:         "exec-1",
						ApplicationID:      appID,
						ApplicationVersion: "v1.0.0",
						Hostname:           "host-1",
						Status:             storegen.ExecutorStatusConnected,
						Metadata:           []byte(`{"language":"go","dbosVersion":"0.1.0","hostId":"h1"}`),
					},
					{
						ExecutorID:         "exec-2",
						ApplicationID:      appID,
						ApplicationVersion: "v1.0.0",
						Status:             storegen.ExecutorStatusDead,
					},
				}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		resp, err := srv.ListExecutors(ctx, gen.ListExecutorsRequestObject{OrgName: "my-org", AppName: "my-app"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		execResp, ok := resp.(gen.ListExecutors200JSONResponse)
		if !ok {
			t.Fatalf("expected ListExecutors200JSONResponse, got %T", resp)
		}
		if len(execResp) != 2 {
			t.Fatalf("expected 2 executors, got %d", len(execResp))
		}
		if execResp[0].Status != gen.HEALTHY || execResp[1].Status != gen.DEAD {
			t.Errorf("unexpected statuses: %v, %v", execResp[0].Status, execResp[1].Status)
		}
		if *execResp[0].Language != "go" || *execResp[0].DbosVersion != "0.1.0" {
			t.Errorf("metadata parsing error: language=%v, dbosVersion=%v", *execResp[0].Language, *execResp[0].DbosVersion)
		}
	})

	t.Run("Autoscale and Alerting Stubs", func(t *testing.T) {
		srv := api.NewServer(nil, &mockStoreReader{}, nil)

		if _, err := srv.GetAutoscale(ctx, gen.GetAutoscaleRequestObject{}); err != nil {
			t.Error(err)
		}
		if _, err := srv.GetAutoscaleVersion(ctx, gen.GetAutoscaleVersionRequestObject{}); err != nil {
			t.Error(err)
		}
		if _, err := srv.GetAutoscalingPolicy(ctx, gen.GetAutoscalingPolicyRequestObject{}); err != nil {
			t.Error(err)
		}
		if _, err := srv.SetAutoscalingPolicy(ctx, gen.SetAutoscalingPolicyRequestObject{}); err != nil {
			t.Error(err)
		}
		if _, err := srv.DeleteAutoscalingPolicy(ctx, gen.DeleteAutoscalingPolicyRequestObject{}); err != nil {
			t.Error(err)
		}
		if _, err := srv.ListAlertingRules(ctx, gen.ListAlertingRulesRequestObject{}); err != nil {
			t.Error(err)
		}
		if _, err := srv.CreateAlertingRule(ctx, gen.CreateAlertingRuleRequestObject{}); err != nil {
			t.Error(err)
		}
		if _, err := srv.DeleteAlertingRule(ctx, gen.DeleteAlertingRuleRequestObject{}); err != nil {
			t.Error(err)
		}
	})
}

func TestTokensAndPermissions(t *testing.T) {
	ctx := context.Background()
	orgID := makeUUID(30)
	keyID := makeUUID(31)

	t.Run("ListTokens", func(t *testing.T) {
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			listAPIKeysFunc: func(ctx context.Context, id pgtype.UUID) ([]storegen.ApiKey, error) {
				return []storegen.ApiKey{
					{
						ID:               keyID,
						OrganisationID:   orgID,
						Name:             "prod-key",
						ApplicationNames: []string{"app-a"},
						Permissions:      []string{"admin"},
						CreatedAt:        pgtype.Timestamptz{Time: time.Now(), Valid: true},
					},
				}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		resp, err := srv.ListTokens(ctx, gen.ListTokensRequestObject{OrgName: "my-org"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		tokResp, ok := resp.(gen.ListTokens200JSONResponse)
		if !ok {
			t.Fatalf("expected ListTokens200JSONResponse, got %T", resp)
		}
		if len(tokResp) != 1 || tokResp[0].TokenName != "prod-key" {
			t.Errorf("unexpected token payload: %+v", tokResp)
		}
	})

	t.Run("CreateToken", func(t *testing.T) {
		created := false
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			createAPIKeyFunc: func(ctx context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error) {
				if arg.Name != "new-token" || !strings.HasPrefix(arg.Lookup, auth.KeyPrefix) {
					t.Fatalf("unexpected create params: %+v", arg)
				}
				created = true
				return storegen.ApiKey{ID: keyID, Name: arg.Name}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)
		apps := []string{"app-1"}
		perms := []string{"viewer"}
		resp, err := srv.CreateToken(ctx, gen.CreateTokenRequestObject{
			OrgName:   "my-org",
			TokenName: "new-token",
			Body:      &gen.CreateTokenJSONRequestBody{AppNames: &apps, Permissions: &perms},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		createResp, ok := resp.(gen.CreateToken201JSONResponse)
		if !ok {
			t.Fatalf("expected CreateToken201JSONResponse, got %T", resp)
		}
		if !strings.HasPrefix(createResp.Token, auth.KeyPrefix) || createResp.TokenName != "new-token" {
			t.Errorf("unexpected token output: %+v", createResp)
		}
		if !created {
			t.Fatal("expected createAPIKeyFunc to be called")
		}
	})

	t.Run("CreateToken_ScopeAndPermissionClamping", func(t *testing.T) {
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			createAPIKeyFunc: func(ctx context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error) {
				return storegen.ApiKey{ID: keyID, Name: arg.Name}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)

		// Context with an app-scoped caller that has application.write for app-1 only
		restrictedCtx := auth.WithIdentity(ctx, &auth.UserIdentity{
			Subject:          "user-restricted",
			IsAdmin:          false,
			Role:             auth.RoleOperator,
			ApplicationNames: []string{"app-1"},
			Permissions:      []string{auth.PermApplicationWrite, auth.PermApplicationRead},
		})

		// 1. App-scoped caller cannot mint unscoped tokens (empty appNames)
		unscopedApps := []string{}
		resp1, err := srv.CreateToken(restrictedCtx, gen.CreateTokenRequestObject{
			OrgName:   "my-org",
			TokenName: "unscoped-token",
			Body:      &gen.CreateTokenJSONRequestBody{AppNames: &unscopedApps},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		prob1, ok := resp1.(gen.CreateTokendefaultApplicationProblemPlusJSONResponse)
		if !ok || prob1.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for unscoped token mint by app-scoped caller, got %T (%d)", resp1, prob1.StatusCode)
		}

		// 2. App-scoped caller cannot mint token for an app outside its scope (app-2)
		outOfScopeApps := []string{"app-2"}
		resp2, err := srv.CreateToken(restrictedCtx, gen.CreateTokenRequestObject{
			OrgName:   "my-org",
			TokenName: "out-of-scope-token",
			Body:      &gen.CreateTokenJSONRequestBody{AppNames: &outOfScopeApps},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		prob2, ok := resp2.(gen.CreateTokendefaultApplicationProblemPlusJSONResponse)
		if !ok || prob2.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for app outside scope, got %T (%d)", resp2, prob2.StatusCode)
		}

		// 3. Caller cannot mint permissions outside its scope (admin)
		validApps := []string{"app-1"}
		outOfScopePerms := []string{auth.RoleAdmin}
		resp3, err := srv.CreateToken(restrictedCtx, gen.CreateTokenRequestObject{
			OrgName:   "my-org",
			TokenName: "admin-escalation-token",
			Body:      &gen.CreateTokenJSONRequestBody{AppNames: &validApps, Permissions: &outOfScopePerms},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		prob3, ok := resp3.(gen.CreateTokendefaultApplicationProblemPlusJSONResponse)
		if !ok || prob3.StatusCode != http.StatusForbidden {
			t.Fatalf("expected 403 Forbidden for permission escalation, got %T (%d)", resp3, prob3.StatusCode)
		}

		// 4. In-scope minting succeeds
		inScopePerms := []string{auth.PermApplicationRead}
		resp4, err := srv.CreateToken(restrictedCtx, gen.CreateTokenRequestObject{
			OrgName:   "my-org",
			TokenName: "valid-in-scope-token",
			Body:      &gen.CreateTokenJSONRequestBody{AppNames: &validApps, Permissions: &inScopePerms},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		createResp4, ok := resp4.(gen.CreateToken201JSONResponse)
		if !ok || createResp4.TokenName != "valid-in-scope-token" {
			t.Fatalf("expected 201 Created for valid in-scope token, got %T", resp4)
		}
	})

	t.Run("DeleteToken success and not found", func(t *testing.T) {
		revoked := false
		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: name}, nil
			},
			listAPIKeysFunc: func(ctx context.Context, id pgtype.UUID) ([]storegen.ApiKey, error) {
				return []storegen.ApiKey{
					{ID: keyID, Name: "key-to-delete"},
				}, nil
			},
			revokeAPIKeyFunc: func(ctx context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error) {
				if arg.ID != keyID {
					t.Fatalf("expected key ID %v, got %v", keyID, arg.ID)
				}
				revoked = true
				return storegen.ApiKey{ID: keyID}, nil
			},
		}
		srv := api.NewServer(nil, store, nil)

		// Success
		resp, err := srv.DeleteToken(ctx, gen.DeleteTokenRequestObject{OrgName: "my-org", TokenName: "key-to-delete"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.DeleteToken204Response); !ok {
			t.Fatalf("expected DeleteToken204Response, got %T", resp)
		}
		if !revoked {
			t.Fatal("expected revokeAPIKeyFunc to be called")
		}

		// Not found
		resp, err = srv.DeleteToken(ctx, gen.DeleteTokenRequestObject{OrgName: "my-org", TokenName: "nonexistent-key"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prob, ok := resp.(gen.DeleteTokendefaultApplicationProblemPlusJSONResponse); !ok || prob.StatusCode != http.StatusNotFound {
			t.Fatalf("expected 404, got %T", resp)
		}
	})

	t.Run("ListPermissions", func(t *testing.T) {
		srv := api.NewServer(nil, nil, nil)
		resp, err := srv.ListPermissions(ctx, gen.ListPermissionsRequestObject{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		perms, ok := resp.(gen.ListPermissions200JSONResponse)
		if !ok {
			t.Fatalf("expected ListPermissions200JSONResponse, got %T", resp)
		}
		if len(perms) != 3 {
			t.Fatalf("expected 3 permissions, got %d", len(perms))
		}
	})
}

func TestOIDCStubsReturn404(t *testing.T) {
	ctx := context.Background()
	srv := api.NewServer(nil, nil, nil)

	check404 := func(name string, resp any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s returned error: %v", name, err)
		}
		// All generated default responses have StatusCode and Body fields
		switch v := resp.(type) {
		case gen.GetOrgdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 || *v.Body.Detail != "Endpoint requires OAuth and is not available in no-auth mode" {
				t.Errorf("%s invalid: %+v", name, v)
			}
		case gen.UpdateOrgdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.JoinOrgdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.GenerateSecretdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.ListMembersdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.RemoveMemberdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.GrantRoledefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.ListRolesdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.CreateRoledefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.DeleteRoledefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.ListDomainClaimsdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.RequestDomainClaimdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.ReleaseDomainClaimdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.ListAuditLogsdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.RegisterUserdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		case gen.GetCurrentUserdefaultApplicationProblemPlusJSONResponse:
			if v.StatusCode != 404 {
				t.Errorf("%s status = %d", name, v.StatusCode)
			}
		default:
			t.Errorf("%s unexpected response type: %T", name, resp)
		}
	}

	resp1, err := srv.GetOrg(ctx, gen.GetOrgRequestObject{})
	check404("GetOrg", resp1, err)

	resp2, err := srv.UpdateOrg(ctx, gen.UpdateOrgRequestObject{})
	check404("UpdateOrg", resp2, err)

	resp3, err := srv.JoinOrg(ctx, gen.JoinOrgRequestObject{})
	check404("JoinOrg", resp3, err)

	resp4, err := srv.GenerateSecret(ctx, gen.GenerateSecretRequestObject{})
	check404("GenerateSecret", resp4, err)

	resp5, err := srv.ListMembers(ctx, gen.ListMembersRequestObject{})
	check404("ListMembers", resp5, err)

	resp6, err := srv.RemoveMember(ctx, gen.RemoveMemberRequestObject{})
	check404("RemoveMember", resp6, err)

	resp7, err := srv.GrantRole(ctx, gen.GrantRoleRequestObject{})
	check404("GrantRole", resp7, err)

	resp8, err := srv.ListRoles(ctx, gen.ListRolesRequestObject{})
	check404("ListRoles", resp8, err)

	resp9, err := srv.CreateRole(ctx, gen.CreateRoleRequestObject{})
	check404("CreateRole", resp9, err)

	resp10, err := srv.DeleteRole(ctx, gen.DeleteRoleRequestObject{})
	check404("DeleteRole", resp10, err)

	resp11, err := srv.ListDomainClaims(ctx, gen.ListDomainClaimsRequestObject{})
	check404("ListDomainClaims", resp11, err)

	resp12, err := srv.RequestDomainClaim(ctx, gen.RequestDomainClaimRequestObject{})
	check404("RequestDomainClaim", resp12, err)

	resp13, err := srv.ReleaseDomainClaim(ctx, gen.ReleaseDomainClaimRequestObject{})
	check404("ReleaseDomainClaim", resp13, err)

	resp14, err := srv.ListAuditLogs(ctx, gen.ListAuditLogsRequestObject{})
	check404("ListAuditLogs", resp14, err)

	resp15, err := srv.RegisterUser(ctx, gen.RegisterUserRequestObject{})
	check404("RegisterUser", resp15, err)

	resp16, err := srv.GetCurrentUser(ctx, gen.GetCurrentUserRequestObject{})
	check404("GetCurrentUser", resp16, err)
}

func TestWorkflowMutations(t *testing.T) {
	ctx := context.Background()

	t.Run("CancelWorkflow success and router error", func(t *testing.T) {
		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req, ok := msg.(*protocol.CancelWorkflowRequest)
				if !ok {
					t.Fatalf("expected *protocol.CancelWorkflowRequest, got %T", msg)
				}
				if req.WorkflowID != "wf-1" || !req.CancelChildren {
					t.Fatalf("unexpected cancel request: %+v", req)
				}
				return &protocol.CancelWorkflowResponse{
					Envelope: protocol.Envelope{Type: protocol.MessageTypeCancel},
					Success:  true,
				}, nil
			},
		}
		srv := api.NewServer(r, nil, nil)
		cancelKids := true
		resp, err := srv.CancelWorkflow(ctx, gen.CancelWorkflowRequestObject{
			OrgName:    "my-org",
			AppName:    "my-app",
			WorkflowId: "wf-1",
			Body:       &gen.CancelWorkflowJSONRequestBody{CancelChildren: &cancelKids},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.CancelWorkflow204Response); !ok {
			t.Fatalf("expected CancelWorkflow204Response, got %T", resp)
		}

		// Router error
		r.dispatchFunc = func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
			return nil, router.ErrNoLiveExecutor
		}
		resp, err = srv.CancelWorkflow(ctx, gen.CancelWorkflowRequestObject{
			WorkflowId: "wf-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prob, ok := resp.(gen.CancelWorkflowdefaultApplicationProblemPlusJSONResponse); !ok || prob.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %T", resp)
		}
	})

	t.Run("BulkCancelWorkflows", func(t *testing.T) {
		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.CancelWorkflowRequest)
				if len(req.WorkflowIDs) != 2 {
					t.Fatalf("expected 2 IDs, got %d", len(req.WorkflowIDs))
				}
				return &protocol.CancelWorkflowResponse{Success: true}, nil
			},
		}
		srv := api.NewServer(r, nil, nil)

		resp, err := srv.BulkCancelWorkflows(ctx, gen.BulkCancelWorkflowsRequestObject{
			Body: &gen.BulkCancelWorkflowsJSONRequestBody{
				WorkflowIds: []string{"w1", "w2"},
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.BulkCancelWorkflows204Response); !ok {
			t.Fatalf("expected BulkCancelWorkflows204Response, got %T", resp)
		}

		// Missing body
		resp, err = srv.BulkCancelWorkflows(ctx, gen.BulkCancelWorkflowsRequestObject{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prob, ok := resp.(gen.BulkCancelWorkflowsdefaultApplicationProblemPlusJSONResponse); !ok || prob.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected 400, got %T", resp)
		}
	})

	t.Run("ResumeWorkflow and BulkResumeWorkflows", func(t *testing.T) {
		qName := "fast-queue"
		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.ResumeWorkflowRequest)
				if *req.QueueName != "fast-queue" {
					t.Fatalf("expected fast-queue, got %v", req.QueueName)
				}
				return &protocol.ResumeWorkflowResponse{Success: true}, nil
			},
		}
		srv := api.NewServer(r, nil, nil)

		resp, err := srv.ResumeWorkflow(ctx, gen.ResumeWorkflowRequestObject{
			WorkflowId: "wf-1",
			Body:       &gen.ResumeWorkflowJSONRequestBody{QueueName: &qName},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.ResumeWorkflow204Response); !ok {
			t.Fatalf("expected ResumeWorkflow204Response, got %T", resp)
		}

		bulkResp, err := srv.BulkResumeWorkflows(ctx, gen.BulkResumeWorkflowsRequestObject{
			Body: &gen.BulkResumeWorkflowsJSONRequestBody{
				WorkflowIds: []string{"wf-1"},
				QueueName:   &qName,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := bulkResp.(gen.BulkResumeWorkflows204Response); !ok {
			t.Fatalf("expected BulkResumeWorkflows204Response, got %T", bulkResp)
		}
	})

	t.Run("DeleteWorkflow and BulkDeleteWorkflows", func(t *testing.T) {
		delKids := true
		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.DeleteWorkflowRequest)
				if !req.DeleteChildren {
					t.Fatalf("expected deleteChildren=true")
				}
				return &protocol.DeleteWorkflowResponse{Success: true}, nil
			},
		}
		srv := api.NewServer(r, nil, nil)

		resp, err := srv.DeleteWorkflow(ctx, gen.DeleteWorkflowRequestObject{
			WorkflowId: "wf-1",
			Params:     gen.DeleteWorkflowParams{DeleteChildren: &delKids},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.DeleteWorkflow204Response); !ok {
			t.Fatalf("expected DeleteWorkflow204Response, got %T", resp)
		}

		bulkResp, err := srv.BulkDeleteWorkflows(ctx, gen.BulkDeleteWorkflowsRequestObject{
			Body: &gen.BulkDeleteWorkflowsJSONRequestBody{
				WorkflowIds:    []string{"wf-1"},
				DeleteChildren: &delKids,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := bulkResp.(gen.BulkDeleteWorkflows204Response); !ok {
			t.Fatalf("expected BulkDeleteWorkflows204Response, got %T", bulkResp)
		}
	})

	t.Run("ForkWorkflow returns 201 with workflow ID", func(t *testing.T) {
		newID := "forked-wf-123"
		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.ForkWorkflowRequest)
				if req.Body.WorkflowID != "orig-1" {
					t.Fatalf("unexpected orig ID: %s", req.Body.WorkflowID)
				}
				return &protocol.ForkWorkflowResponse{
					NewWorkflowID: &newID,
				}, nil
			},
		}
		srv := api.NewServer(r, nil, nil)
		resp, err := srv.ForkWorkflow(ctx, gen.ForkWorkflowRequestObject{
			WorkflowId: "orig-1",
			Body:       &gen.ForkWorkflowJSONRequestBody{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		forkResp, ok := resp.(gen.ForkWorkflow201JSONResponse)
		if !ok {
			t.Fatalf("expected ForkWorkflow201JSONResponse, got %T", resp)
		}
		if forkResp.Body.WorkflowId != "forked-wf-123" {
			t.Errorf("expected workflowId 'forked-wf-123', got %q", forkResp.Body.WorkflowId)
		}
	})

	t.Run("ForkWorkflow stranded fork protection returns 409 when no live executor matches", func(t *testing.T) {
		appID := pgtype.UUID{Bytes: [16]byte{1, 2, 3}, Valid: true}
		orgID := pgtype.UUID{Bytes: [16]byte{4, 5, 6}, Valid: true}
		targetVersion := "v1.0-legacy"
		statusSuccess := "SUCCESS"

		store := &mockStoreReader{
			getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
				return storegen.Organisation{ID: orgID, Name: "default"}, nil
			},
			getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
				return storegen.Application{ID: appID, Name: "checkout"}, nil
			},
			listExecutorsByAppFunc: func(ctx context.Context, app pgtype.UUID) ([]storegen.Executor, error) {
				return []storegen.Executor{
					{
						ExecutorID:         "exec-live-1",
						ApplicationVersion: "v2.0",
						LeaseExpiresAt:     pgtype.Timestamptz{Time: time.Now().Add(10 * time.Minute), Valid: true},
					},
				}, nil
			},
		}

		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				if getReq, ok := msg.(*protocol.GetWorkflowRequest); ok {
					if getReq.WorkflowID == "wf-target-legacy" {
						return &protocol.GetWorkflowResponse{
							Envelope: protocol.Envelope{Type: protocol.MessageTypeGetWorkflow},
							Output: &protocol.ListWorkflowsResponseBody{
								WorkflowUUID:       "wf-target-legacy",
								Status:             &statusSuccess,
								ApplicationVersion: &targetVersion,
							},
						}, nil
					}
				}
				return nil, nil
			},
		}

		srv := api.NewServer(r, store, nil)

		// 1. Without application_version override -> 409 Conflict
		resp, err := srv.ForkWorkflow(ctx, gen.ForkWorkflowRequestObject{
			OrgName:    "default",
			AppName:    "checkout",
			WorkflowId: "wf-target-legacy",
			Body:       &gen.ForkWorkflowJSONRequestBody{},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		conflictResp, ok := resp.(gen.ForkWorkflowdefaultApplicationProblemPlusJSONResponse)
		if !ok {
			t.Fatalf("expected 409 Conflict Problem response, got %T", resp)
		}
		if conflictResp.StatusCode != http.StatusConflict {
			t.Errorf("expected status 409, got %d", conflictResp.StatusCode)
		}

		// 2. With application_version override to live version "v2.0" -> 201 Created
		r.dispatchFunc = func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
			if forkReq, ok := msg.(*protocol.ForkWorkflowRequest); ok {
				newID := "forked-override-1"
				if forkReq.Body.ApplicationVersion != nil && *forkReq.Body.ApplicationVersion == "v2.0" {
					return &protocol.ForkWorkflowResponse{
						NewWorkflowID: &newID,
					}, nil
				}
			}
			return nil, nil
		}
		overrideVersion := "v2.0"
		resp2, err := srv.ForkWorkflow(ctx, gen.ForkWorkflowRequestObject{
			OrgName:    "default",
			AppName:    "checkout",
			WorkflowId: "wf-target-legacy",
			Body: &gen.ForkWorkflowJSONRequestBody{
				AppVersion: &overrideVersion,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		forkResp, ok := resp2.(gen.ForkWorkflow201JSONResponse)
		if !ok {
			t.Fatalf("expected 201 Created, got %T", resp2)
		}
		if forkResp.Body.WorkflowId != "forked-override-1" {
			t.Errorf("expected workflow ID 'forked-override-1', got %q", forkResp.Body.WorkflowId)
		}
	})

	t.Run("BulkForkWorkflowsFromFailure", func(t *testing.T) {
		forked := []string{"forked-1", "forked-2"}
		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				return &protocol.ForkFromFailureResponse{
					ForkedWorkflowIDs: forked,
				}, nil
			},
		}
		srv := api.NewServer(r, nil, nil)
		fromFail := true
		resp, err := srv.BulkForkWorkflowsFromFailure(ctx, gen.BulkForkWorkflowsFromFailureRequestObject{
			Body: &gen.BulkForkWorkflowsFromFailureJSONRequestBody{
				WorkflowIds:     []string{"wf-f1", "wf-f2"},
				FromLastFailure: &fromFail,
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		bulkResp, ok := resp.(gen.BulkForkWorkflowsFromFailure200JSONResponse)
		if !ok {
			t.Fatalf("expected BulkForkWorkflowsFromFailure200JSONResponse, got %T", resp)
		}
		if len(bulkResp.WorkflowIds) != 2 {
			t.Fatalf("expected 2 forked IDs, got %d", len(bulkResp.WorkflowIds))
		}
	})

	t.Run("ImportWorkflow", func(t *testing.T) {
		r := &mockRouter{
			dispatchFunc: func(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
				req := msg.(*protocol.ImportWorkflowRequest)
				if req.SerializedWorkflow != "serialized-data" {
					t.Fatalf("unexpected data: %s", req.SerializedWorkflow)
				}
				return &protocol.ImportWorkflowResponse{Success: true}, nil
			},
		}
		srv := api.NewServer(r, nil, nil)
		resp, err := srv.ImportWorkflow(ctx, gen.ImportWorkflowRequestObject{
			Body: &gen.ImportWorkflowJSONRequestBody{
				SerializedWorkflow: "serialized-data",
			},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := resp.(gen.ImportWorkflow201Response); !ok {
			t.Fatalf("expected ImportWorkflow201Response, got %T", resp)
		}
	})
}

func (m *mockStoreReader) TouchAPIKeyLastUsed(ctx context.Context, id pgtype.UUID) error {
	return nil
}

func TestAlertingRulesSSRFAndSanitization(t *testing.T) {
	ctx := context.Background()
	orgID := makeUUID(40)
	appID := makeUUID(41)

	store := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
			return storegen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
		},
	}
	srv := api.NewServer(nil, store, nil)

	// 1. SSRF URL blocked: loopback and cloud metadata
	ssrfURLs := []string{"http://127.0.0.1:1", "http://localhost:8080/hook", "http://169.254.169.254/latest/meta-data"}
	for _, u := range ssrfURLs {
		req := gen.CreateAlertingRuleRequestObject{
			OrgName: "test-org",
			AppName: "test-app",
			Body: &gen.CreateAlertInputBody{
				RuleType: "WorkflowFailure",
				RuleMetadata: map[string]any{
					"destinations": []any{
						map[string]any{"type": "webhook", "url": u},
					},
				},
			},
		}
		resp, err := srv.CreateAlertingRule(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		probResp, ok := resp.(gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse)
		if !ok || probResp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 Bad Request for SSRF URL %s, got: %T", u, resp)
		}
	}

	// 2. Secret path redaction in ListAlertingRules
	ruleMeta := map[string]any{
		"destinations": []any{
			map[string]any{"type": "slack", "url": "https://hooks.slack.com/services/T0000/B0000/SUPERSECRETPATHTOKEN", "secret": "shh"},
			map[string]any{"type": "webhook", "url": "https://example.com/alerts/SUPERSECRETHOOKTOKEN", "secret": "shh"},
		},
	}
	ruleMetaBytes, _ := json.Marshal(ruleMeta)
	listStore := &mockStoreReader{
		getOrgByNameFunc: func(ctx context.Context, name string) (storegen.Organisation, error) {
			return storegen.Organisation{ID: orgID, Name: name}, nil
		},
		getAppByNameFunc: func(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error) {
			return storegen.Application{ID: appID, OrganisationID: orgID, Name: arg.Name}, nil
		},
		listAlertRulesFunc: func(ctx context.Context, aID pgtype.UUID) ([]storegen.AlertingRule, error) {
			return []storegen.AlertingRule{
				{
					ID:            makeUUID(42),
					ApplicationID: appID,
					RuleType:      "WorkflowFailure",
					RuleMetadata:  ruleMetaBytes,
				},
			}, nil
		},
	}
	srvList := api.NewServer(nil, listStore, nil)
	listResp, err := srvList.ListAlertingRules(ctx, gen.ListAlertingRulesRequestObject{
		OrgName: "test-org",
		AppName: "test-app",
	})
	if err != nil {
		t.Fatalf("ListAlertingRules: %v", err)
	}
	rules, ok := listResp.(gen.ListAlertingRules200JSONResponse)
	if !ok || len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %v", listResp)
	}
	dests, _ := rules[0].RuleMetadata.(map[string]any)["destinations"].([]any)
	if len(dests) != 2 {
		t.Fatalf("expected 2 destinations, got %d", len(dests))
	}
	slackDest := dests[0].(map[string]any)
	if slackDest["url"] != "https://hooks.slack.com/..." {
		t.Errorf("expected slack URL 'https://hooks.slack.com/...', got %q", slackDest["url"])
	}
	if strings.Contains(fmt.Sprintf("%v", slackDest["url"]), "SUPERSECRETPATHTOKEN") {
		t.Errorf("secret token leaked in slack url: %v", slackDest["url"])
	}
	webhookDest := dests[1].(map[string]any)
	if webhookDest["url"] != "https://example.com/..." {
		t.Errorf("expected webhook URL 'https://example.com/...', got %q", webhookDest["url"])
	}
	if strings.Contains(fmt.Sprintf("%v", webhookDest["url"]), "SUPERSECRETHOOKTOKEN") {
		t.Errorf("secret token leaked in webhook url: %v", webhookDest["url"])
	}
}
