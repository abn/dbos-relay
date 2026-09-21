package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/router"
	storegen "github.com/abn/relay/internal/store/gen"
)

// Autoscaling recommendations follow the upstream formula
// (https://docs.dbos.dev/production/autoscaling):
// desiredExecutors = ceil(queueDepth / workerConcurrency), additionally
// capped at ceil(concurrency / workerConcurrency) when the queue defines
// a global concurrency limit. Backlog counts come from executor dispatch
// (list_queued_workflows grouped by application version); Relay recommends
// counts and never scales anything itself. See ADR 0012.

func mapAutoscalingPolicy(p storegen.AutoscalingPolicy) gen.AutoscalePolicy {
	out := gen.AutoscalePolicy{Queue: p.Queue}
	if p.MaxOldVersions != nil || p.MaxExecutorsOld != nil {
		out.Rollout = &gen.RolloutPolicy{
			MaxOldApplicationVersions:             p.MaxOldVersions,
			MaxExecutorsForOldApplicationVersions: p.MaxExecutorsOld,
		}
	}
	return out
}

func policyOutput(p storegen.AutoscalingPolicy) gen.PolicyOutputBody {
	return gen.PolicyOutputBody{Policy: mapAutoscalingPolicy(p)}
}

// resolveAutoscaleApp loads the organisation and application rows for
// autoscaling operations.
func (s *Server) resolveAutoscaleApp(ctx context.Context, orgName, appName string) (storegen.Organisation, storegen.Application, *gen.ErrorModel, int) {
	org, err := s.store.GetOrganisationByName(ctx, normalizeOrg(orgName))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			model := MakeErrorModel(http.StatusNotFound, "Organisation not found", fmt.Sprintf("organisation %q not found", normalizeOrg(orgName)))
			return storegen.Organisation{}, storegen.Application{}, &model, http.StatusNotFound
		}
		model := MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "database store is unavailable")
		return storegen.Organisation{}, storegen.Application{}, &model, http.StatusServiceUnavailable
	}
	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           appName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			model := MakeErrorModel(http.StatusNotFound, "Application not found", fmt.Sprintf("application %q not found", appName))
			return storegen.Organisation{}, storegen.Application{}, &model, http.StatusNotFound
		}
		model := MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "database store is unavailable")
		return storegen.Organisation{}, storegen.Application{}, &model, http.StatusServiceUnavailable
	}
	return org, app, nil, 0
}

// queueValidationError marks queue definition problems, which surface as
// 400 Bad Request. Dispatch failures keep router error mapping instead.
type queueValidationError struct{ msg string }

func (e *queueValidationError) Error() string { return e.msg }

// fetchQueue dispatches get_queue to a healthy executor.
func (s *Server) fetchQueue(ctx context.Context, orgName, appName, queue string) (*protocol.QueueOutput, error) {
	req := &protocol.GetQueueRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetQueue,
			RequestID: uuid.NewString(),
		},
		Name: queue,
	}
	res, err := s.router.Dispatch(ctx, orgName, appName, req)
	if err != nil {
		return nil, err
	}
	resp, ok := res.(*protocol.GetQueueResponse)
	if !ok {
		return nil, &queueValidationError{"unexpected response type from router"}
	}
	if resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
		return nil, &queueValidationError{fmt.Sprintf("get queue failed: %s", *resp.ErrorMessage)}
	}
	if resp.Output == nil {
		return nil, &queueValidationError{fmt.Sprintf("queue %q not found", queue)}
	}
	return resp.Output, nil
}

// queueDispatchError maps fetchQueue and backlog errors: validation
// problems become 400, dispatch failures keep router mapping (502/503).
func queueDispatchError(err error) (int, gen.ErrorModel) {
	var validation *queueValidationError
	if errors.As(err, &validation) {
		return http.StatusBadRequest, MakeErrorModel(http.StatusBadRequest, "Bad Request", validation.msg)
	}
	if errors.Is(err, router.ErrNoLiveExecutor) {
		return http.StatusServiceUnavailable, MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", err.Error())
	}
	return RouterErrorToModel(err)
}

// listQueuedBacklog pages through queued workflows for one queue,
// grouping ENQUEUED and PENDING rows by application version.
func (s *Server) listQueuedBacklog(ctx context.Context, orgName, appName, queue string) (map[string]int64, error) {
	depths := make(map[string]int64)
	seen := make(map[string]struct{})
	queries := []protocol.ListWorkflowsRequestBody{
		{QueuesOnly: true, QueueName: protocol.StringOrList{queue}},
		{QueueName: protocol.StringOrList{queue}, Status: protocol.StringOrList{"PENDING"}},
	}
	const page = 1000
	for _, base := range queries {
		offset := 0
		for {
			lim := page
			off := offset
			body := base
			body.Limit = &lim
			body.Offset = &off
			req := &protocol.ListWorkflowsRequest{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeListQueuedWorkflows,
					RequestID: uuid.NewString(),
				},
				Body: body,
			}
			res, err := s.router.Dispatch(ctx, orgName, appName, req)
			if err != nil {
				return nil, err
			}
			resp, ok := res.(*protocol.ListWorkflowsResponse)
			if !ok {
				return nil, &queueValidationError{"unexpected response type from router"}
			}
			if resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
				return nil, &queueValidationError{fmt.Sprintf("list queued workflows failed: %s", *resp.ErrorMessage)}
			}
			for _, wf := range resp.Output {
				// Dedup across the ENQUEUED and PENDING listings: an
				// executor that reports a workflow in both must not
				// inflate the depth.
				if _, dup := seen[wf.WorkflowUUID]; dup {
					continue
				}
				seen[wf.WorkflowUUID] = struct{}{}
				version := ""
				if wf.ApplicationVersion != nil {
					version = *wf.ApplicationVersion
				}
				depths[version]++
			}
			if len(resp.Output) < page {
				break
			}
			offset += page
		}
	}
	return depths, nil
}

// latestAutoscaleVersion resolves the latest registered version: the
// recorded latest version when set, otherwise the version of the most
// recently connected executor, otherwise the lexicographic maximum of
// versions seen in the backlog. Executor connection times approximate
// registration order because executors self-report versions without
// registration timestamps.
func latestAutoscaleVersion(app storegen.Application, execs []storegen.Executor, depths map[string]int64) string {
	var settings appSettings
	if len(app.Settings) > 0 {
		_ = json.Unmarshal(app.Settings, &settings)
	}
	if settings.LatestVersion != nil && *settings.LatestVersion != "" {
		return *settings.LatestVersion
	}
	best := ""
	var bestTime time.Time
	bestSet := false
	for _, e := range execs {
		if e.ApplicationVersion == "" || !e.ConnectedAt.Valid {
			continue
		}
		if !bestSet || e.ConnectedAt.Time.After(bestTime) || (e.ConnectedAt.Time.Equal(bestTime) && e.ApplicationVersion > best) {
			best = e.ApplicationVersion
			bestTime = e.ConnectedAt.Time
			bestSet = true
		}
	}
	if bestSet {
		return best
	}
	latest := ""
	for v := range depths {
		if v > latest {
			latest = v
		}
	}
	return latest
}

// computeAutoscale builds one recommendation per in-scope version. Old
// versions with no remaining work are omitted unless onlyVersion selects
// them, in which case they report zero so a drained deployment is visible
// rather than missing.
func computeAutoscale(queue string, latest string, depths map[string]int64, workerConcurrency int, globalConcurrency *int, maxExecOld *int64, onlyVersion string, observedAt int64) []gen.QueueAutoscale {
	ceilDiv := func(n, d int64) int64 {
		if d <= 0 {
			return n
		}
		return (n + d - 1) / d
	}
	globalCap := int64(-1)
	if globalConcurrency != nil && *globalConcurrency > 0 {
		globalCap = ceilDiv(int64(*globalConcurrency), int64(workerConcurrency))
	}
	desiredFor := func(depth int64, isLatest bool) int64 {
		desired := ceilDiv(depth, int64(workerConcurrency))
		if globalCap >= 0 && desired > globalCap {
			desired = globalCap
		}
		if isLatest && desired < 1 {
			desired = 1
		}
		if !isLatest && maxExecOld != nil && desired > *maxExecOld {
			desired = *maxExecOld
		}
		return desired
	}

	versions := make([]string, 0, len(depths)+1)
	seen := make(map[string]bool)
	for v := range depths {
		if onlyVersion != "" && v != onlyVersion {
			continue
		}
		versions = append(versions, v)
		seen[v] = true
	}
	if latest != "" && !seen[latest] && (onlyVersion == "" || onlyVersion == latest) {
		versions = append(versions, latest)
	}
	sort.Strings(versions)

	var out []gen.QueueAutoscale
	for _, v := range versions {
		isLatest := v == latest
		depth := depths[v]
		// Old versions with no remaining work are omitted: their
		// absence signals that their deployment can be deleted. The
		// single-version endpoint synthesizes zero rows instead.
		if !isLatest && depth == 0 {
			continue
		}
		out = append(out, gen.QueueAutoscale{
			ApplicationVersion: v,
			IsLatest:           isLatest,
			DesiredExecutors:   desiredFor(depth, isLatest),
			QueueName:          queue,
			QueueDepth:         depth,
			ObservedAt:         observedAt,
		})
	}
	// Latest first, then old versions. Recency ordering beyond that is
	// applied by the all-versions handler, which knows executor times.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsLatest != out[j].IsLatest {
			return out[i].IsLatest
		}
		return out[i].ApplicationVersion > out[j].ApplicationVersion
	})
	return out
}

// GetAutoscalingPolicy returns the stored autoscaling policy.
func (s *Server) GetAutoscalingPolicy(ctx context.Context, request gen.GetAutoscalingPolicyRequestObject) (gen.GetAutoscalingPolicyResponseObject, error) {
	_, app, errModel, status := s.resolveAutoscaleApp(ctx, request.OrgName, request.AppName)
	if errModel != nil {
		return gen.GetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       *errModel,
		}, nil
	}
	policy, err := s.store.GetAutoscalingPolicy(ctx, app.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.GetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Not Found", "no autoscaling policy configured for application"),
			}, nil
		}
		return gen.GetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}
	return gen.GetAutoscalingPolicy200JSONResponse(policyOutput(policy)), nil
}

// SetAutoscalingPolicy validates the queue against a running executor
// before storing the policy.
func (s *Server) SetAutoscalingPolicy(ctx context.Context, request gen.SetAutoscalingPolicyRequestObject) (gen.SetAutoscalingPolicyResponseObject, error) {
	fail := func(code int, title, detail string) (gen.SetAutoscalingPolicyResponseObject, error) {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpAutoscalingSet, auditStatusFailure, string(gen.AuditTargetTypeApplication), request.AppName, nil)
		return gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
			StatusCode: code,
			Body:       MakeErrorModel(code, title, detail),
		}, nil
	}
	_, app, errModel, status := s.resolveAutoscaleApp(ctx, request.OrgName, request.AppName)
	if errModel != nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpAutoscalingSet, auditStatusFailure, string(gen.AuditTargetTypeApplication), request.AppName, nil)
		return gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       *errModel,
		}, nil
	}
	if request.Body == nil || request.Body.Queue == "" {
		return fail(http.StatusBadRequest, "Bad Request", "autoscaling policy must name a queue")
	}
	if request.Body.Rollout != nil {
		if request.Body.Rollout.MaxOldApplicationVersions != nil && *request.Body.Rollout.MaxOldApplicationVersions < 0 {
			return fail(http.StatusBadRequest, "Bad Request", "maxOldApplicationVersions must not be negative")
		}
		if request.Body.Rollout.MaxExecutorsForOldApplicationVersions != nil && *request.Body.Rollout.MaxExecutorsForOldApplicationVersions < 0 {
			return fail(http.StatusBadRequest, "Bad Request", "maxExecutorsForOldApplicationVersions must not be negative")
		}
	}

	orgName := normalizeOrg(request.OrgName)
	queue, err := s.fetchQueue(ctx, orgName, request.AppName, request.Body.Queue)
	if err != nil {
		status, model := queueDispatchError(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpAutoscalingSet, auditStatusFailure, string(gen.AuditTargetTypeApplication), request.AppName, nil)
		return gen.SetAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       model,
		}, nil
	}
	if queue.PartitionQueue {
		return fail(http.StatusBadRequest, "Bad Request", fmt.Sprintf("queue %q is partitioned and cannot drive autoscaling", request.Body.Queue))
	}
	if queue.WorkerConcurrency == nil || *queue.WorkerConcurrency <= 0 {
		return fail(http.StatusBadRequest, "Bad Request", fmt.Sprintf("queue %q has no worker concurrency set", request.Body.Queue))
	}

	var maxOld, maxExecOld *int64
	if request.Body.Rollout != nil {
		maxOld = request.Body.Rollout.MaxOldApplicationVersions
		maxExecOld = request.Body.Rollout.MaxExecutorsForOldApplicationVersions
	}
	stored, err := s.store.UpsertAutoscalingPolicy(ctx, storegen.UpsertAutoscalingPolicyParams{
		ApplicationID:   app.ID,
		Queue:           request.Body.Queue,
		MaxOldVersions:  maxOld,
		MaxExecutorsOld: maxExecOld,
	})
	if err != nil {
		return fail(http.StatusInternalServerError, "Internal Server Error", err.Error())
	}
	details := map[string]any{"queue": stored.Queue}
	if stored.MaxOldVersions != nil {
		details["max_old_versions"] = *stored.MaxOldVersions
	}
	if stored.MaxExecutorsOld != nil {
		details["max_executors_old"] = *stored.MaxExecutorsOld
	}
	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpAutoscalingSet, auditStatusSuccess, string(gen.AuditTargetTypeApplication), request.AppName, details)
	return gen.SetAutoscalingPolicy200JSONResponse(policyOutput(stored)), nil
}

// DeleteAutoscalingPolicy turns autoscaling off.
func (s *Server) DeleteAutoscalingPolicy(ctx context.Context, request gen.DeleteAutoscalingPolicyRequestObject) (gen.DeleteAutoscalingPolicyResponseObject, error) {
	_, app, errModel, status := s.resolveAutoscaleApp(ctx, request.OrgName, request.AppName)
	if errModel != nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpAutoscalingDelete, auditStatusFailure, string(gen.AuditTargetTypeApplication), request.AppName, nil)
		return gen.DeleteAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       *errModel,
		}, nil
	}
	if rows, err := s.store.DeleteAutoscalingPolicy(ctx, app.ID); err != nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpAutoscalingDelete, auditStatusFailure, string(gen.AuditTargetTypeApplication), request.AppName, nil)
		return gen.DeleteAutoscalingPolicydefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	} else {
		// The off switch is idempotent: deleting an absent policy
		// still returns 204, and the entry records whether a policy
		// was actually removed.
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpAutoscalingDelete, auditStatusSuccess, string(gen.AuditTargetTypeApplication), request.AppName, map[string]any{"deleted": rows > 0})
	}
	return gen.DeleteAutoscalingPolicy204Response{}, nil
}

// GetAutoscale returns recommendations for all versions that should run.
func (s *Server) GetAutoscale(ctx context.Context, request gen.GetAutoscaleRequestObject) (gen.GetAutoscaleResponseObject, error) {
	_, app, errModel, status := s.resolveAutoscaleApp(ctx, request.OrgName, request.AppName)
	if errModel != nil {
		return gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       *errModel,
		}, nil
	}
	policy, err := s.store.GetAutoscalingPolicy(ctx, app.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Not Found", "no autoscaling policy configured for application"),
			}, nil
		}
		return gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	orgName := normalizeOrg(request.OrgName)
	queue, err := s.fetchQueue(ctx, orgName, request.AppName, policy.Queue)
	if err != nil {
		status, model := queueDispatchError(err)
		return gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       model,
		}, nil
	}
	if queue.WorkerConcurrency == nil || *queue.WorkerConcurrency <= 0 {
		return gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", fmt.Sprintf("queue %q has no worker concurrency set", policy.Queue)),
		}, nil
	}

	depths, err := s.listQueuedBacklog(ctx, orgName, request.AppName, policy.Queue)
	if err != nil {
		status, model := queueDispatchError(err)
		return gen.GetAutoscaledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       model,
		}, nil
	}

	execs, _ := s.store.ListExecutorsByApplication(ctx, app.ID)
	latest := latestAutoscaleVersion(app, execs, depths)
	observedAt := time.Now().UTC().UnixMilli()
	recs := computeAutoscale(policy.Queue, latest, depths, *queue.WorkerConcurrency, queue.Concurrency, policy.MaxExecutorsOld, "", observedAt)

	// All-versions shape: latest first, then at most maxOldApplicationVersions
	// old versions with remaining work, most recently connected first.
	// Version recency is approximated from executor connection times;
	// versions with no executor record sort last.
	latestSeen := make(map[string]time.Time)
	for _, e := range execs {
		if t := e.ConnectedAt.Time; t.After(latestSeen[e.ApplicationVersion]) {
			latestSeen[e.ApplicationVersion] = t
		}
	}
	var latestRec *gen.QueueAutoscale
	var oldRecs []gen.QueueAutoscale
	for i := range recs {
		if recs[i].IsLatest {
			c := recs[i]
			latestRec = &c
		} else {
			oldRecs = append(oldRecs, recs[i])
		}
	}
	sort.SliceStable(oldRecs, func(i, j int) bool {
		ti, iok := latestSeen[oldRecs[i].ApplicationVersion]
		tj, jok := latestSeen[oldRecs[j].ApplicationVersion]
		if iok != jok {
			return iok
		}
		if iok && !ti.Equal(tj) {
			return ti.After(tj)
		}
		return oldRecs[i].ApplicationVersion > oldRecs[j].ApplicationVersion
	})
	maxOld := int64(0)
	if policy.MaxOldVersions != nil {
		maxOld = *policy.MaxOldVersions
	}
	if int64(len(oldRecs)) > maxOld {
		oldRecs = oldRecs[:maxOld]
	}
	out := make([]gen.QueueAutoscale, 0, len(oldRecs)+1)
	if latestRec != nil {
		out = append(out, *latestRec)
	}
	out = append(out, oldRecs...)
	return gen.GetAutoscale200JSONResponse(out), nil
}

// GetAutoscaleVersion returns the recommendation for one version.
func (s *Server) GetAutoscaleVersion(ctx context.Context, request gen.GetAutoscaleVersionRequestObject) (gen.GetAutoscaleVersionResponseObject, error) {
	_, app, errModel, status := s.resolveAutoscaleApp(ctx, request.OrgName, request.AppName)
	if errModel != nil {
		return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       *errModel,
		}, nil
	}
	policy, err := s.store.GetAutoscalingPolicy(ctx, app.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusNotFound,
				Body:       MakeErrorModel(http.StatusNotFound, "Not Found", "no autoscaling policy configured for application"),
			}, nil
		}
		return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", err.Error()),
		}, nil
	}

	orgName := normalizeOrg(request.OrgName)
	queue, err := s.fetchQueue(ctx, orgName, request.AppName, policy.Queue)
	if err != nil {
		status, model := queueDispatchError(err)
		return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       model,
		}, nil
	}
	if queue.WorkerConcurrency == nil || *queue.WorkerConcurrency <= 0 {
		return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", fmt.Sprintf("queue %q has no worker concurrency set", policy.Queue)),
		}, nil
	}

	depths, err := s.listQueuedBacklog(ctx, orgName, request.AppName, policy.Queue)
	if err != nil {
		status, model := queueDispatchError(err)
		return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       model,
		}, nil
	}

	execs, _ := s.store.ListExecutorsByApplication(ctx, app.ID)
	latest := latestAutoscaleVersion(app, execs, depths)
	version := request.Version
	if version == "latest" {
		version = latest
	}
	if version == "" {
		return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", "no application version is known for autoscaling"),
		}, nil
	}
	known := version == latest
	if !known {
		if _, ok := depths[version]; ok {
			known = true
		}
	}
	if !known {
		for _, e := range execs {
			if e.ApplicationVersion == version {
				known = true
				break
			}
		}
	}
	if !known {
		return gen.GetAutoscaleVersiondefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Not Found", fmt.Sprintf("application version %q was never registered", request.Version)),
		}, nil
	}

	observedAt := time.Now().UTC().UnixMilli()
	recs := computeAutoscale(policy.Queue, latest, depths, *queue.WorkerConcurrency, queue.Concurrency, policy.MaxExecutorsOld, version, observedAt)
	if len(recs) == 0 {
		// Known but drained: the all-versions endpoint omits such
		// versions, but the version endpoint reports them at zero
		// (latest keeps its minimum of one).
		desired := int64(0)
		if version == latest {
			desired = 1
		}
		return gen.GetAutoscaleVersion200JSONResponse(gen.QueueAutoscale{
			ApplicationVersion: version,
			IsLatest:           version == latest,
			DesiredExecutors:   desired,
			QueueName:          policy.Queue,
			QueueDepth:         0,
			ObservedAt:         observedAt,
		}), nil
	}
	return gen.GetAutoscaleVersion200JSONResponse(recs[0]), nil
}
