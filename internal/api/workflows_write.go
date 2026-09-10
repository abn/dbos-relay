package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	storegen "github.com/abn/relay/internal/store/gen"
)

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
		return gen.CancelWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	if cancelRes, ok := res.(*protocol.CancelWorkflowResponse); ok {
		if cancelRes.ErrorMessage != nil && *cancelRes.ErrorMessage != "" {
			return gen.CancelWorkflowdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *cancelRes.ErrorMessage),
			}, nil
		}
	}

	return gen.CancelWorkflow204Response{}, nil
}

// BulkCancelWorkflows cancels multiple workflows at once.
func (s *Server) BulkCancelWorkflows(ctx context.Context, request gen.BulkCancelWorkflowsRequestObject) (gen.BulkCancelWorkflowsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	if request.Body == nil {
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
		return gen.BulkCancelWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	if cancelRes, ok := res.(*protocol.CancelWorkflowResponse); ok {
		if cancelRes.ErrorMessage != nil && *cancelRes.ErrorMessage != "" {
			return gen.BulkCancelWorkflowsdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *cancelRes.ErrorMessage),
			}, nil
		}
	}

	return gen.BulkCancelWorkflows204Response{}, nil
}

// ResumeWorkflow resumes a suspended or queued workflow.
func (s *Server) ResumeWorkflow(ctx context.Context, request gen.ResumeWorkflowRequestObject) (gen.ResumeWorkflowResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

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
		return gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	if resumeRes, ok := res.(*protocol.ResumeWorkflowResponse); ok {
		if resumeRes.ErrorMessage != nil && *resumeRes.ErrorMessage != "" {
			return gen.ResumeWorkflowdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *resumeRes.ErrorMessage),
			}, nil
		}
	}

	return gen.ResumeWorkflow204Response{}, nil
}

// BulkResumeWorkflows resumes multiple workflows at once.
func (s *Server) BulkResumeWorkflows(ctx context.Context, request gen.BulkResumeWorkflowsRequestObject) (gen.BulkResumeWorkflowsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	if request.Body == nil {
		return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", "Missing request body"),
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
		return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	if resumeRes, ok := res.(*protocol.ResumeWorkflowResponse); ok {
		if resumeRes.ErrorMessage != nil && *resumeRes.ErrorMessage != "" {
			return gen.BulkResumeWorkflowsdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *resumeRes.ErrorMessage),
			}, nil
		}
	}

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
		return gen.DeleteWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	if delRes, ok := res.(*protocol.DeleteWorkflowResponse); ok {
		if delRes.ErrorMessage != nil && *delRes.ErrorMessage != "" {
			return gen.DeleteWorkflowdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *delRes.ErrorMessage),
			}, nil
		}
	}

	return gen.DeleteWorkflow204Response{}, nil
}

// BulkDeleteWorkflows deletes multiple workflows at once.
func (s *Server) BulkDeleteWorkflows(ctx context.Context, request gen.BulkDeleteWorkflowsRequestObject) (gen.BulkDeleteWorkflowsResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	if request.Body == nil {
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
		return gen.BulkDeleteWorkflowsdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	if delRes, ok := res.(*protocol.DeleteWorkflowResponse); ok {
		if delRes.ErrorMessage != nil && *delRes.ErrorMessage != "" {
			return gen.BulkDeleteWorkflowsdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *delRes.ErrorMessage),
			}, nil
		}
	}

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

	// Guard against stranding forks when no explicit application_version override is provided
	if (appVersion == nil || *appVersion == "") && s.store != nil {
		getMsg := &protocol.GetWorkflowRequest{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: uuid.NewString(),
			},
			WorkflowID: request.WorkflowId,
		}
		getRes, err := s.router.Dispatch(ctx, orgName, request.AppName, getMsg)
		if err == nil {
			if wfRes, ok := getRes.(*protocol.GetWorkflowResponse); ok && wfRes.Output != nil && wfRes.Output.ApplicationVersion != nil {
				targetVersion := *wfRes.Output.ApplicationVersion
				if targetVersion != "" {
					if appVersion == nil || *appVersion == "" {
						appVersion = &targetVersion
					}
					org, oErr := s.store.GetOrganisationByName(ctx, orgName)
					if oErr == nil {
						app, aErr := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
							OrganisationID: org.ID,
							Name:           request.AppName,
						})
						if aErr == nil {
							execs, eErr := s.store.ListExecutorsByApplication(ctx, app.ID)
							if eErr == nil {
								hasLiveVersion := false
								now := time.Now()
								for _, e := range execs {
									if e.LeaseExpiresAt.Valid && e.LeaseExpiresAt.Time.After(now) && e.ApplicationVersion == targetVersion {
										hasLiveVersion = true
										break
									}
								}
								if !hasLiveVersion {
									msg := fmt.Sprintf("no live executor runs target application version %q; specify an application_version override to fork", targetVersion)
									return gen.ForkWorkflowdefaultApplicationProblemPlusJSONResponse{
										StatusCode: http.StatusConflict,
										Body:       MakeErrorModel(http.StatusConflict, "Stranded Fork Conflict", msg),
									}, nil
								}
							}
						}
					}
				}
			}
		}
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
		return gen.ForkWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	resultID := ""
	if newWorkflowID != nil {
		resultID = *newWorkflowID
	}
	if forkRes, ok := res.(*protocol.ForkWorkflowResponse); ok {
		if forkRes.ErrorMessage != nil && *forkRes.ErrorMessage != "" {
			return gen.ForkWorkflowdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *forkRes.ErrorMessage),
			}, nil
		}
		if forkRes.NewWorkflowID != nil && *forkRes.NewWorkflowID != "" {
			resultID = *forkRes.NewWorkflowID
		}
	}

	return gen.ForkWorkflow201JSONResponse{
		Body: gen.ForkWorkflowOutputBody{
			WorkflowId: resultID,
		},
	}, nil
}

// BulkForkWorkflowsFromFailure forks failed workflows.
func (s *Server) BulkForkWorkflowsFromFailure(ctx context.Context, request gen.BulkForkWorkflowsFromFailureRequestObject) (gen.BulkForkWorkflowsFromFailureResponseObject, error) {
	orgName := normalizeOrg(request.OrgName)

	if request.Body == nil {
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
		return gen.BulkForkWorkflowsFromFailuredefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	var forkedIDs []string
	if forkRes, ok := res.(*protocol.ForkFromFailureResponse); ok {
		if forkRes.ErrorMessage != nil && *forkRes.ErrorMessage != "" {
			return gen.BulkForkWorkflowsFromFailuredefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *forkRes.ErrorMessage),
			}, nil
		}
		forkedIDs = forkRes.ForkedWorkflowIDs
	}
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
		return gen.ImportWorkflowdefaultApplicationProblemPlusJSONResponse{StatusCode: status, Body: model}, nil
	}

	if importRes, ok := res.(*protocol.ImportWorkflowResponse); ok {
		if importRes.ErrorMessage != nil && *importRes.ErrorMessage != "" {
			return gen.ImportWorkflowdefaultApplicationProblemPlusJSONResponse{
				StatusCode: http.StatusBadRequest,
				Body:       MakeErrorModel(http.StatusBadRequest, "Bad Request", *importRes.ErrorMessage),
			}, nil
		}
	}

	return gen.ImportWorkflow201Response{}, nil
}
