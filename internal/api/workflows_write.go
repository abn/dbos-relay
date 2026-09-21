package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store"
	storegen "github.com/abn/relay/internal/store/gen"
)

// resumeVersionBlocked enforces the upstream rule that resume requires a
// healthy executor running the application's latest version, not merely
// any healthy executor. It reports the latest version and whether resume
// must be refused. When no latest version is recorded, or when the store
// cannot answer, there is no constraint to enforce and dispatch proceeds;
// the dispatch itself then reports missing applications or executors.
func (s *Server) resumeVersionBlocked(ctx context.Context, orgName, appName string) (string, bool) {
	if s.store == nil {
		return "", false
	}
	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return "", false
	}
	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           appName,
	})
	if err != nil {
		return "", false
	}
	var settings appSettings
	if len(app.Settings) > 0 {
		if err := json.Unmarshal(app.Settings, &settings); err != nil {
			s.logger.Debug("ignoring corrupt application settings for resume guard", "app", appName, "error", err)
		}
	}
	if settings.LatestVersion == nil || *settings.LatestVersion == "" {
		return "", false
	}
	latest := *settings.LatestVersion
	execs, err := s.store.ListExecutorsByApplication(ctx, app.ID)
	if err != nil {
		return "", false
	}
	for _, exec := range execs {
		if store.IsLiveStatus(exec.Status) && exec.ApplicationVersion == latest {
			return "", false
		}
	}
	return latest, true
}

// CancelWorkflow cancels a running workflow.
func (s *Server) CancelWorkflow(ctx context.Context, request gen.CancelWorkflowRequestObject) (gen.CancelWorkflowResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	cancelChildren := false
	if request.Body != nil && request.Body.CancelChildren != nil {
		cancelChildren = *request.Body.CancelChildren
	}

	msg := &protocol.CancelWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeCancel,
			RequestID: uuid.NewString(),
		},
		WorkflowID:     request.WorkflowId,
		CancelChildren: cancelChildren,
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowCancel, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.CancelWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	cancelRes, ok := res.(*protocol.CancelWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowCancel, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.CancelWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if cancelRes.ErrorMessage != nil && *cancelRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowCancel, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.CancelWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *cancelRes.ErrorMessage),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowCancel, auditStatusSuccess, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
	return gen.CancelWorkflow204Response{}, nil
}

// BulkCancelWorkflows cancels multiple workflows at once.
func (s *Server) BulkCancelWorkflows(ctx context.Context, request gen.BulkCancelWorkflowsRequestObject) (gen.BulkCancelWorkflowsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	bulkIDs := []string{}
	if request.Body != nil && request.Body.WorkflowIds != nil {
		bulkIDs = request.Body.WorkflowIds
	}

	if request.Body == nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkCancel, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkCancelWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Missing request body"),
		}, nil
	}

	cancelChildren := false
	if request.Body.CancelChildren != nil {
		cancelChildren = *request.Body.CancelChildren
	}

	msg := &protocol.CancelWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeCancel,
			RequestID: uuid.NewString(),
		},
		WorkflowIDs:    request.Body.WorkflowIds,
		CancelChildren: cancelChildren,
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkCancel, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkCancelWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	cancelRes, ok := res.(*protocol.CancelWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkCancel, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkCancelWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if cancelRes.ErrorMessage != nil && *cancelRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkCancel, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkCancelWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *cancelRes.ErrorMessage),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkCancel, auditStatusSuccess, "", "", map[string]any{"workflow_ids": bulkIDs})
	return gen.BulkCancelWorkflows204Response{}, nil
}

// ResumeWorkflow resumes a suspended or queued workflow.
func (s *Server) ResumeWorkflow(ctx context.Context, request gen.ResumeWorkflowRequestObject) (gen.ResumeWorkflowResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	if latest, blocked := s.resumeVersionBlocked(ctx, orgName, request.AppName); blocked {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowResume, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "no executor running latest application version "+latest+" is connected"),
		}, nil
	}

	var queueName *string
	if request.Body != nil {
		queueName = request.Body.QueueName
	}

	msg := &protocol.ResumeWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeResume,
			RequestID: uuid.NewString(),
		},
		WorkflowID: request.WorkflowId,
		QueueName:  queueName,
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowResume, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	resumeRes, ok := res.(*protocol.ResumeWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowResume, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if resumeRes.ErrorMessage != nil && *resumeRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowResume, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *resumeRes.ErrorMessage),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowResume, auditStatusSuccess, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
	return gen.ResumeWorkflow204Response{}, nil
}

// BulkResumeWorkflows resumes multiple workflows at once.
func (s *Server) BulkResumeWorkflows(ctx context.Context, request gen.BulkResumeWorkflowsRequestObject) (gen.BulkResumeWorkflowsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	bulkIDs := []string{}
	if request.Body != nil && request.Body.WorkflowIds != nil {
		bulkIDs = request.Body.WorkflowIds
	}

	if request.Body == nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkResume, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Missing request body"),
		}, nil
	}

	if latest, blocked := s.resumeVersionBlocked(ctx, orgName, request.AppName); blocked {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkResume, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusServiceUnavailable,
			Body:       MakeErrorModel(http.StatusServiceUnavailable, "Service Unavailable", "no executor running latest application version "+latest+" is connected"),
		}, nil
	}

	msg := &protocol.ResumeWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeResume,
			RequestID: uuid.NewString(),
		},
		WorkflowIDs: request.Body.WorkflowIds,
		QueueName:   request.Body.QueueName,
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkResume, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	resumeRes, ok := res.(*protocol.ResumeWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkResume, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if resumeRes.ErrorMessage != nil && *resumeRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkResume, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *resumeRes.ErrorMessage),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkResume, auditStatusSuccess, "", "", map[string]any{"workflow_ids": bulkIDs})
	return gen.BulkResumeWorkflows204Response{}, nil
}

// DeleteWorkflow deletes a workflow and optionally its children.
func (s *Server) DeleteWorkflow(ctx context.Context, request gen.DeleteWorkflowRequestObject) (gen.DeleteWorkflowResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	deleteChildren := false
	if request.Params.DeleteChildren != nil {
		deleteChildren = *request.Params.DeleteChildren
	}

	msg := &protocol.DeleteWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeDelete,
			RequestID: uuid.NewString(),
		},
		WorkflowID:     request.WorkflowId,
		DeleteChildren: deleteChildren,
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowDelete, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.DeleteWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	delRes, ok := res.(*protocol.DeleteWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowDelete, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.DeleteWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if delRes.ErrorMessage != nil && *delRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowDelete, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.DeleteWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *delRes.ErrorMessage),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowDelete, auditStatusSuccess, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
	return gen.DeleteWorkflow204Response{}, nil
}

// BulkDeleteWorkflows deletes multiple workflows at once.
func (s *Server) BulkDeleteWorkflows(ctx context.Context, request gen.BulkDeleteWorkflowsRequestObject) (gen.BulkDeleteWorkflowsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	bulkIDs := []string{}
	if request.Body != nil && request.Body.WorkflowIds != nil {
		bulkIDs = request.Body.WorkflowIds
	}

	if request.Body == nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkDelete, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkDeleteWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Missing request body"),
		}, nil
	}

	deleteChildren := false
	if request.Body.DeleteChildren != nil {
		deleteChildren = *request.Body.DeleteChildren
	}

	msg := &protocol.DeleteWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeDelete,
			RequestID: uuid.NewString(),
		},
		WorkflowIDs:    request.Body.WorkflowIds,
		DeleteChildren: deleteChildren,
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkDelete, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkDeleteWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	delRes, ok := res.(*protocol.DeleteWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkDelete, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkDeleteWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if delRes.ErrorMessage != nil && *delRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkDelete, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkDeleteWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *delRes.ErrorMessage),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowBulkDelete, auditStatusSuccess, "", "", map[string]any{"workflow_ids": bulkIDs})
	return gen.BulkDeleteWorkflows204Response{}, nil
}

// ForkWorkflow forks a workflow starting from a given step.
func (s *Server) ForkWorkflow(ctx context.Context, request gen.ForkWorkflowRequestObject) (gen.ForkWorkflowResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	var startStep int
	var appVersion, newWorkflowID, queueName, queuePartitionKey *string
	if request.Body != nil {
		if request.Body.StartStep != nil {
			startStep = int(*request.Body.StartStep)
		}
		appVersion = request.Body.AppVersion
		newWorkflowID = request.Body.NewWorkflowId
		queueName = request.Body.QueueName
		queuePartitionKey = request.Body.QueuePartitionKey
	}
	if newWorkflowID == nil || *newWorkflowID == "" {
		genID := uuid.NewString()
		newWorkflowID = &genID
	}

	if appVersion == nil || *appVersion == "" {
		emptyVer := ""
		appVersion = &emptyVer
	}

	msg := &protocol.ForkWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeForkWorkflow,
			RequestID: uuid.NewString(),
		},
		Body: protocol.ForkWorkflowRequestBody{
			WorkflowID:         request.WorkflowId,
			StartStep:          startStep,
			ApplicationVersion: appVersion,
			NewWorkflowID:      newWorkflowID,
			QueueName:          queueName,
			QueuePartitionKey:  queuePartitionKey,
		},
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowFork, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.ForkWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	resultID := ""
	if newWorkflowID != nil {
		resultID = *newWorkflowID
	}
	forkRes, ok := res.(*protocol.ForkWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowFork, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.ForkWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if forkRes.ErrorMessage != nil && *forkRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowFork, auditStatusFailure, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, nil)
		return gen.ForkWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *forkRes.ErrorMessage),
		}, nil
	}
	if forkRes.NewWorkflowID != nil && *forkRes.NewWorkflowID != "" {
		resultID = *forkRes.NewWorkflowID
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowFork, auditStatusSuccess, string(gen.AuditTargetTypeWorkflow), request.WorkflowId, map[string]any{"new_workflow_id": resultID})
	response := gen.ForkWorkflow201JSONResponse{
		Body: gen.ForkWorkflowOutputBody{
			WorkflowId: resultID,
		},
	}
	if resultID != "" {
		loc := fmt.Sprintf("/v2/orgs/%s/apps/%s/workflows/%s", orgName, request.AppName, resultID)
		response.Headers.Location = &loc
	}
	return response, nil
}

// BulkForkWorkflowsFromFailure forks failed workflows.
func (s *Server) BulkForkWorkflowsFromFailure(ctx context.Context, request gen.BulkForkWorkflowsFromFailureRequestObject) (gen.BulkForkWorkflowsFromFailureResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	bulkIDs := []string{}
	if request.Body != nil && request.Body.WorkflowIds != nil {
		bulkIDs = request.Body.WorkflowIds
	}

	if request.Body == nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowForkFail, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkForkWorkflowsFromFailuredefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Missing request body"),
		}, nil
	}

	var fromLastFailure, fromLastStep bool
	if request.Body.FromLastFailure != nil {
		fromLastFailure = *request.Body.FromLastFailure
	}
	if request.Body.FromLastStep != nil {
		fromLastStep = *request.Body.FromLastStep
	}
	var fromStep *int
	if request.Body.FromStep != nil {
		v := int(*request.Body.FromStep)
		fromStep = &v
	}

	msg := &protocol.ForkFromFailureRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeForkFromFailure,
			RequestID: uuid.NewString(),
		},
		Body: protocol.ForkFromFailureRequestBody{
			WorkflowIDs:        request.Body.WorkflowIds,
			ApplicationVersion: request.Body.AppVersion,
			QueueName:          request.Body.QueueName,
			QueuePartitionKey:  request.Body.QueuePartitionKey,
			FromLastFailure:    fromLastFailure,
			FromLastStep:       fromLastStep,
			FromStep:           fromStep,
			FromStepName:       request.Body.FromStepName,
		},
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowForkFail, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkForkWorkflowsFromFailuredefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	forkRes, ok := res.(*protocol.ForkFromFailureResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowForkFail, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkForkWorkflowsFromFailuredefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if forkRes.ErrorMessage != nil && *forkRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowForkFail, auditStatusFailure, "", "", map[string]any{"workflow_ids": bulkIDs})
		return gen.BulkForkWorkflowsFromFailuredefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *forkRes.ErrorMessage),
		}, nil
	}
	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowForkFail, auditStatusSuccess, "", "", map[string]any{"workflow_ids": bulkIDs, "forked_workflow_ids": forkRes.ForkedWorkflowIDs})
	forkedIDs := forkRes.ForkedWorkflowIDs
	if forkedIDs == nil {
		forkedIDs = []string{}
	}

	return gen.BulkForkWorkflowsFromFailure200JSONResponse{
		WorkflowIds: forkedIDs,
	}, nil
}

// ImportWorkflow imports a serialized workflow.
func (s *Server) ImportWorkflow(ctx context.Context, request gen.ImportWorkflowRequestObject) (gen.ImportWorkflowResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	if request.Body == nil {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowImport, auditStatusFailure, "", "", nil)
		return gen.ImportWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Missing request body"),
		}, nil
	}

	msg := &protocol.ImportWorkflowRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeImportWorkflow,
			RequestID: uuid.NewString(),
		},
		SerializedWorkflow: request.Body.SerializedWorkflow,
	}

	res, err := s.router.Dispatch(ctx, orgName, request.AppName, msg)
	if err != nil {
		status, model := RouterErrorToModel(err)
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowImport, auditStatusFailure, "", "", nil)
		return gen.ImportWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	importRes, ok := res.(*protocol.ImportWorkflowResponse)
	if !ok {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowImport, auditStatusFailure, "", "", nil)
		return gen.ImportWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}
	if importRes.ErrorMessage != nil && *importRes.ErrorMessage != "" {
		s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowImport, auditStatusFailure, "", "", nil)
		return gen.ImportWorkflowdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *importRes.ErrorMessage),
		}, nil
	}

	s.auditOperation(ctx, request.OrgName, request.AppName, auditOpWorkflowImport, auditStatusSuccess, "", "", nil)
	return gen.ImportWorkflow201Response{}, nil
}
