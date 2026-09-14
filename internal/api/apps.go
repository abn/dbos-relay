package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api/gen"
	storegen "github.com/abn/relay/internal/store/gen"
)

var (
	orgNameRegex = regexp.MustCompile(`^[a-z0-9_]{3,30}$`)
	appNameRegex = regexp.MustCompile(`^[a-z0-9-_]{3,30}$`)
)

func validateOrgName(name string) error {
	if !orgNameRegex.MatchString(name) {
		return fmt.Errorf("organization name must be 3-30 characters and contain only lowercase alphanumeric characters and underscores")
	}
	return nil
}

func validateAppName(name string) error {
	if !appNameRegex.MatchString(name) {
		return fmt.Errorf("application name must be 3-30 characters and contain only lowercase alphanumeric characters, dashes, and underscores")
	}
	return nil
}

type appSettings struct {
	PrivateMode         bool    `json:"privateMode,omitempty"`
	ExecutorTimeoutSecs int64   `json:"executorTimeoutSecs,omitempty"`
	GcRowsThreshold     *int64  `json:"gcRowsThreshold,omitempty"`
	GcTimeThresholdMs   *int64  `json:"gcTimeThresholdMs,omitempty"`
	GlobalTimeoutMs     *int64  `json:"globalTimeoutMs,omitempty"`
	LatestVersion       *string `json:"latestVersion,omitempty"`
}

func normalizeOrg(orgName string) string {
	if orgName == "" {
		return "local"
	}
	return orgName
}

func mapApplication(app storegen.Application, orgID pgtype.UUID) gen.Application {
	var s appSettings
	if len(app.Settings) > 0 {
		_ = json.Unmarshal(app.Settings, &s)
	}
	timeoutSecs := s.ExecutorTimeoutSecs
	if timeoutSecs == 0 {
		timeoutSecs = 60
	}
	return gen.Application{
		Id:                  formatUUID(app.ID),
		Name:                app.Name,
		OrgId:               formatUUID(orgID),
		Status:              gen.AVAILABLE,
		DbosCloud:           false,
		PrivateMode:         s.PrivateMode,
		ExecutorTimeoutSecs: timeoutSecs,
		GcRowsThreshold:     s.GcRowsThreshold,
		GcTimeThresholdMs:   s.GcTimeThresholdMs,
		GlobalTimeoutMs:     s.GlobalTimeoutMs,
	}
}

func mapExecutor(e storegen.Executor) gen.Executor {
	var md map[string]interface{}
	if len(e.Metadata) > 0 {
		_ = json.Unmarshal(e.Metadata, &md)
	}

	var language, dbosVersion, hostId *string
	if md != nil {
		if v, ok := md["language"].(string); ok {
			language = &v
			delete(md, "language")
		}
		if v, ok := md["dbosVersion"].(string); ok {
			dbosVersion = &v
			delete(md, "dbosVersion")
		}
		if v, ok := md["hostId"].(string); ok {
			hostId = &v
			delete(md, "hostId")
		}
	}

	var status gen.ExecutorStatus
	switch e.Status {
	case storegen.ExecutorStatusConnected:
		status = gen.HEALTHY
	case storegen.ExecutorStatusDisconnected:
		status = gen.DISCONNECTED
	case storegen.ExecutorStatusDead:
		status = gen.DEAD
	default:
		status = gen.DEAD
	}

	var hostname *string
	if e.Hostname != "" {
		h := e.Hostname
		hostname = &h
	}

	var mdPtr *map[string]interface{}
	if md != nil {
		mdPtr = &md
	}

	return gen.Executor{
		AppId:            formatUUID(e.ApplicationID),
		AppVersion:       e.ApplicationVersion,
		CreatedAt:        e.ConnectedAt.Time,
		UpdatedAt:        e.LastSeenAt.Time,
		DbosVersion:      dbosVersion,
		ExecutorId:       e.ExecutorID,
		ExecutorMetadata: mdPtr,
		HostId:           hostId,
		Hostname:         hostname,
		Language:         language,
		Status:           status,
	}
}

// ListApps returns all applications registered for an organisation.
func (s *Server) ListApps(ctx context.Context, request gen.ListAppsRequestObject) (gen.ListAppsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return gen.ListAppsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error()),
		}, nil
	}

	apps, err := s.store.ListApplicationsByOrganisation(ctx, org.ID)
	if err != nil {
		return gen.ListAppsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	res := make([]gen.Application, 0, len(apps))
	for _, a := range apps {
		res = append(res, mapApplication(a, org.ID))
	}

	return gen.ListApps200JSONResponse(res), nil
}

// GetApp returns details for a single application.
func (s *Server) GetApp(ctx context.Context, request gen.GetAppRequestObject) (gen.GetAppResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return gen.GetAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error()),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.GetAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Application not found", err.Error()),
		}, nil
	}

	return gen.GetApp200JSONResponse(mapApplication(app, org.ID)), nil
}

// RegisterApp registers a new application or updates an existing one.
func (s *Server) RegisterApp(ctx context.Context, request gen.RegisterAppRequestObject) (gen.RegisterAppResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)
	if err := validateOrgName(orgName); err != nil {
		return gen.RegisterAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       MakeErrorModel(http.StatusUnprocessableEntity, "Validation Error", err.Error()),
		}, nil
	}
	if err := validateAppName(request.AppName); err != nil {
		return gen.RegisterAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusUnprocessableEntity,
			Body:       MakeErrorModel(http.StatusUnprocessableEntity, "Validation Error", err.Error()),
		}, nil
	}

	org, err := s.store.UpsertOrganisation(ctx, orgName)
	if err != nil {
		return gen.RegisterAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	settings := appSettings{}
	if request.Body != nil && request.Body.PrivateMode != nil {
		settings.PrivateMode = *request.Body.PrivateMode
	}

	settingsBytes, err := json.Marshal(settings)
	if err != nil {
		return gen.RegisterAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	_, err = s.store.UpsertApplication(ctx, storegen.UpsertApplicationParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
		Settings:       settingsBytes,
	})
	if err != nil {
		return gen.RegisterAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	return gen.RegisterApp204Response{}, nil
}

// UpdateApp updates application settings.
func (s *Server) UpdateApp(ctx context.Context, request gen.UpdateAppRequestObject) (gen.UpdateAppResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return gen.UpdateAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error()),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.UpdateAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Application not found", err.Error()),
		}, nil
	}

	var settings appSettings
	if len(app.Settings) > 0 {
		_ = json.Unmarshal(app.Settings, &settings)
	}

	if request.Body != nil {
		if request.Body.PrivateMode != nil {
			settings.PrivateMode = *request.Body.PrivateMode
		}
		if request.Body.ExecutorTimeoutSecs != nil {
			settings.ExecutorTimeoutSecs = *request.Body.ExecutorTimeoutSecs
		}
		if request.Body.GcRowsThreshold != nil {
			settings.GcRowsThreshold = request.Body.GcRowsThreshold
		}
		if request.Body.GcTimeThresholdMs != nil {
			settings.GcTimeThresholdMs = request.Body.GcTimeThresholdMs
		}
		if request.Body.GlobalTimeoutMs != nil {
			settings.GlobalTimeoutMs = request.Body.GlobalTimeoutMs
		}
	}

	settingsBytes, err := json.Marshal(settings)
	if err != nil {
		return gen.UpdateAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	_, err = s.store.UpdateApplicationSettings(ctx, storegen.UpdateApplicationSettingsParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
		Settings:       settingsBytes,
	})
	if err != nil {
		return gen.UpdateAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	return gen.UpdateApp204Response{}, nil
}

// DeleteApp deletes an application.
func (s *Server) DeleteApp(ctx context.Context, request gen.DeleteAppRequestObject) (gen.DeleteAppResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return gen.DeleteAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error()),
		}, nil
	}

	_, err = s.store.DeleteApplication(ctx, storegen.DeleteApplicationParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.DeleteAppdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Application not found", err.Error()),
		}, nil
	}

	return gen.DeleteApp204Response{}, nil
}

// ListAppVersions lists distinct versions for an application.
func (s *Server) ListAppVersions(ctx context.Context, request gen.ListAppVersionsRequestObject) (gen.ListAppVersionsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return gen.ListAppVersionsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error()),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.ListAppVersionsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Application not found", err.Error()),
		}, nil
	}

	execs, err := s.store.ListExecutorsByApplication(ctx, app.ID)
	if err != nil {
		return gen.ListAppVersionsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	seen := make(map[string]bool)
	versions := make([]gen.ApplicationVersion, 0)
	for _, e := range execs {
		if e.ApplicationVersion != "" && !seen[e.ApplicationVersion] {
			seen[e.ApplicationVersion] = true
			t := e.ConnectedAt.Time
			versions = append(versions, gen.ApplicationVersion{
				VersionId:        e.ApplicationVersion,
				VersionName:      e.ApplicationVersion,
				CreatedAt:        t,
				VersionTimestamp: t,
			})
		}
	}

	var settings appSettings
	if len(app.Settings) > 0 {
		_ = json.Unmarshal(app.Settings, &settings)
	}

	if settings.LatestVersion != nil && *settings.LatestVersion != "" && !seen[*settings.LatestVersion] {
		seen[*settings.LatestVersion] = true
		t := app.CreatedAt.Time
		versions = append(versions, gen.ApplicationVersion{
			VersionId:        *settings.LatestVersion,
			VersionName:      *settings.LatestVersion,
			CreatedAt:        t,
			VersionTimestamp: t,
		})
	}

	return gen.ListAppVersions200JSONResponse(versions), nil
}

// SetLatestAppVersion sets the active/latest application version.
func (s *Server) SetLatestAppVersion(ctx context.Context, request gen.SetLatestAppVersionRequestObject) (gen.SetLatestAppVersionResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return gen.SetLatestAppVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error()),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.SetLatestAppVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Application not found", err.Error()),
		}, nil
	}

	if request.Body == nil {
		return gen.SetLatestAppVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Missing request body"),
		}, nil
	}

	var settings appSettings
	if len(app.Settings) > 0 {
		_ = json.Unmarshal(app.Settings, &settings)
	}

	settings.LatestVersion = &request.Body.VersionName
	settingsBytes, err := json.Marshal(settings)
	if err != nil {
		return gen.SetLatestAppVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	_, err = s.store.UpdateApplicationSettings(ctx, storegen.UpdateApplicationSettingsParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
		Settings:       settingsBytes,
	})
	if err != nil {
		return gen.SetLatestAppVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	return gen.SetLatestAppVersion204Response{}, nil
}

// ListExecutors lists live and recent executors for an application.
func (s *Server) ListExecutors(ctx context.Context, request gen.ListExecutorsRequestObject) (gen.ListExecutorsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return gen.ListExecutorsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Organisation not found", err.Error()),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.ListExecutorsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Application not found", err.Error()),
		}, nil
	}

	execs, err := s.store.ListExecutorsByApplication(ctx, app.ID)
	if err != nil {
		return gen.ListExecutorsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	resp := make([]gen.Executor, 0, len(execs))
	for _, e := range execs {
		resp = append(resp, mapExecutor(e))
	}

	return gen.ListExecutors200JSONResponse(resp), nil
}

// GetAutoscale returns autoscaling recommendations.
func (s *Server) GetAutoscale(ctx context.Context, request gen.GetAutoscaleRequestObject) (gen.GetAutoscaleResponseObject, error) {
	return gen.GetAutoscale200JSONResponse([]gen.QueueAutoscale{}), nil
}

// GetAutoscaleVersion returns autoscaling recommendations for a specific version.
func (s *Server) GetAutoscaleVersion(ctx context.Context, request gen.GetAutoscaleVersionRequestObject) (gen.GetAutoscaleVersionResponseObject, error) {
	return gen.GetAutoscaleVersion200JSONResponse(gen.QueueAutoscale{}), nil
}

// GetAutoscalingPolicy returns autoscaling policy.
func (s *Server) GetAutoscalingPolicy(ctx context.Context, request gen.GetAutoscalingPolicyRequestObject) (gen.GetAutoscalingPolicyResponseObject, error) {
	return gen.GetAutoscalingPolicy200JSONResponse(gen.PolicyOutputBody{}), nil
}

// SetAutoscalingPolicy sets autoscaling policy.
func (s *Server) SetAutoscalingPolicy(ctx context.Context, request gen.SetAutoscalingPolicyRequestObject) (gen.SetAutoscalingPolicyResponseObject, error) {
	return gen.SetAutoscalingPolicy200JSONResponse(gen.PolicyOutputBody{}), nil
}

// DeleteAutoscalingPolicy deletes autoscaling policy.
func (s *Server) DeleteAutoscalingPolicy(ctx context.Context, request gen.DeleteAutoscalingPolicyRequestObject) (gen.DeleteAutoscalingPolicyResponseObject, error) {
	return gen.DeleteAutoscalingPolicy204Response{}, nil
}

// ListAlertingRules lists alerting rules.
func (s *Server) ListAlertingRules(ctx context.Context, request gen.ListAlertingRulesRequestObject) (gen.ListAlertingRulesResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.ListAlertingRulesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.ListAlertingRulesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("application %q not found", request.AppName)),
		}, nil
	}

	dbRules, err := s.store.ListAlertingRulesByApplication(ctx, app.ID)
	if err != nil {
		return gen.ListAlertingRulesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	out := make([]gen.AlertingRule, 0, len(dbRules))
	for _, r := range dbRules {
		out = append(out, mapStoreAlertRuleToAPI(r))
	}
	return gen.ListAlertingRules200JSONResponse(out), nil
}

func isBlockedDestinationURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return true
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.Equal(net.ParseIP("169.254.169.254")) {
			return true
		}
	}
	return false
}

// CreateAlertingRule creates an alerting rule.
func (s *Server) CreateAlertingRule(ctx context.Context, request gen.CreateAlertingRuleRequestObject) (gen.CreateAlertingRuleResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("application %q not found", request.AppName)),
		}, nil
	}

	recvAppID := app.ID
	if request.Body != nil && request.Body.ReceivingAppName != nil && *request.Body.ReceivingAppName != "" {
		recvApp, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
			OrganisationID: org.ID,
			Name:           *request.Body.ReceivingAppName,
		})
		if err != nil {
			return gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("receiving application %q not found", *request.Body.ReceivingAppName)),
			}, nil
		}
		recvAppID = recvApp.ID
	}

	var metaBytes []byte
	if request.Body != nil && request.Body.RuleMetadata != nil {
		metaBytes, _ = json.Marshal(request.Body.RuleMetadata)
	}
	if len(metaBytes) == 0 {
		metaBytes = []byte(`{}`)
	}

	ruleType := "WorkflowFailure"
	var minInterval *int32
	if request.Body != nil {
		rt := string(request.Body.RuleType)
		switch strings.ToLower(strings.ReplaceAll(rt, "_", "")) {
		case "workflowfailure":
			ruleType = "WorkflowFailure"
		case "slowqueue":
			ruleType = "SlowQueue"
		case "unresponsiveapplication":
			ruleType = "UnresponsiveApplication"
		default:
			return gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", fmt.Sprintf("invalid rule type %q: must be WorkflowFailure, SlowQueue, or UnresponsiveApplication", rt)),
			}, nil
		}
		minInterval = request.Body.MinIntervalSecs
	}

	// Reject secret_from and blocked destination URLs in RuleMetadata over REST
	if request.Body != nil && request.Body.RuleMetadata != nil {
		if metaMap, ok := request.Body.RuleMetadata.(map[string]any); ok {
			if dests, ok := metaMap["destinations"].([]any); ok {
				for _, d := range dests {
					if dMap, ok := d.(map[string]any); ok {
						if _, hasSF := dMap["secret_from"]; hasSF {
							return gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse{
								StatusCode: http.StatusBadRequest,
								Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "secret_from is only permitted in operator declarative manifests"),
							}, nil
						}
						if u, ok := dMap["url"].(string); ok && u != "" {
							if isBlockedDestinationURL(u) {
								return gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse{
									StatusCode: http.StatusBadRequest,
									Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "destination URL points to a blocked address"),
								}, nil
							}
						}
					}
				}
			}
		}
	}

	dbRule, err := s.store.CreateAlertingRule(ctx, storegen.CreateAlertingRuleParams{
		ApplicationID:          app.ID,
		ReceivingApplicationID: recvAppID,
		RuleType:               ruleType,
		RuleMetadata:           metaBytes,
		MinIntervalSecs:        minInterval,
	})
	if err != nil {
		return gen.CreateAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	return gen.CreateAlertingRule201JSONResponse(mapStoreAlertRuleToAPI(dbRule)), nil
}

// DeleteAlertingRule deletes an alerting rule.
func (s *Server) DeleteAlertingRule(ctx context.Context, request gen.DeleteAlertingRuleRequestObject) (gen.DeleteAlertingRuleResponseObject, error) {
	org, err := s.store.GetOrganisationByName(ctx, request.OrgName)
	if err != nil {
		return gen.DeleteAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("organisation %q not found", request.OrgName)),
		}, nil
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           request.AppName,
	})
	if err != nil {
		return gen.DeleteAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("application %q not found", request.AppName)),
		}, nil
	}

	var ruleID pgtype.UUID
	if err := ruleID.Scan(request.RuleId); err != nil {
		return gen.DeleteAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "invalid rule id format"),
		}, nil
	}

	rows, err := s.store.DeleteAlertingRule(ctx, storegen.DeleteAlertingRuleParams{
		ID:            ruleID,
		ApplicationID: app.ID,
	})
	if err != nil {
		return gen.DeleteAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}
	if rows == 0 {
		return gen.DeleteAlertingRuledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("rule %q not found", request.RuleId)),
		}, nil
	}

	return gen.DeleteAlertingRule204Response{}, nil
}

func mapStoreAlertRuleToAPI(r storegen.AlertingRule) gen.AlertingRule {
	var meta any
	if len(r.RuleMetadata) > 0 {
		_ = json.Unmarshal(r.RuleMetadata, &meta)
	}
	meta = sanitizeAlertMetadata(meta)
	var lastFired *time.Time
	if r.LastFiredAt.Valid {
		lastFired = &r.LastFiredAt.Time
	}
	return gen.AlertingRule{
		Id:              formatUUID(r.ID),
		AppId:           formatUUID(r.ApplicationID),
		ReceivingAppId:  formatUUID(r.ReceivingApplicationID),
		RuleType:        gen.AlertingRuleRuleType(r.RuleType),
		RuleMetadata:    meta,
		MinIntervalSecs: r.MinIntervalSecs,
		LastFiredAt:     lastFired,
	}
}

func sanitizeAlertMetadata(meta any) any {
	metaMap, ok := meta.(map[string]any)
	if !ok {
		return meta
	}
	destsRaw, ok := metaMap["destinations"]
	if !ok {
		return meta
	}
	dests, ok := destsRaw.([]any)
	if !ok {
		return meta
	}
	newMeta := make(map[string]any, len(metaMap))
	for k, v := range metaMap {
		newMeta[k] = v
	}
	var sanitizedDests []any
	for _, d := range dests {
		dMap, ok := d.(map[string]any)
		if !ok {
			sanitizedDests = append(sanitizedDests, d)
			continue
		}
		newDMap := make(map[string]any, len(dMap))
		for k, v := range dMap {
			newDMap[k] = v
		}
		if _, hasSec := newDMap["secret"]; hasSec {
			newDMap["secret"] = "[REDACTED]"
		}
		if rk, ok := newDMap["routing_key"].(string); ok && rk != "" {
			if len(rk) > 4 {
				newDMap["routing_key"] = "xxxx..." + rk[len(rk)-4:]
			} else {
				newDMap["routing_key"] = "[REDACTED]"
			}
		}

		t, _ := newDMap["type"].(string)
		if u, ok := newDMap["url"].(string); ok && u != "" {
			if t == "slack" || t == "webhook" {
				parsed, err := url.Parse(u)
				if err != nil || parsed.Host == "" {
					newDMap["url"] = "[REDACTED]"
				} else {
					newDMap["url"] = fmt.Sprintf("%s://%s/...", parsed.Scheme, parsed.Host)
				}
			}
		}

		delete(newDMap, "secret_from")
		sanitizedDests = append(sanitizedDests, newDMap)
	}
	newMeta["destinations"] = sanitizedDests
	return newMeta
}
