package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
)

func scheduleOutputToModel(s protocol.ScheduleOutput) gen.Schedule {
	return gen.Schedule{
		ApplicationName:   s.ApplicationName,
		AutomaticBackfill: s.AutomaticBackfill,
		Context:           s.Context,
		CronExpression:    s.Schedule,
		CronTimezone:      s.CronTimezone,
		LastFiredAt:       parseTimeString(s.LastFiredAt),
		ScheduleId:        s.ScheduleID,
		ScheduleName:      s.ScheduleName,
		Status:            s.Status,
		WorkflowClass:     s.WorkflowClassName,
		WorkflowName:      s.WorkflowName,
	}
}

// ListSchedules lists all schedules for an application with optional filters.
func (s *Server) ListSchedules(ctx context.Context, request gen.ListSchedulesRequestObject) (gen.ListSchedulesResponseObject, error) {
	req := &protocol.ListSchedulesRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListSchedules,
			RequestID: newRequestID(),
		},
		Body: protocol.ListSchedulesRequestBody{
			ApplicationName: protocol.StringOrList{request.AppName},
			LoadContext:     request.Params.LoadContext,
		},
	}
	if request.Params.Status != nil {
		req.Body.Status = protocol.StringOrList{*request.Params.Status}
	}
	if request.Params.WorkflowName != nil {
		req.Body.WorkflowName = protocol.StringOrList{*request.Params.WorkflowName}
	}
	if request.Params.ScheduleNamePrefix != nil {
		req.Body.ScheduleNamePrefix = protocol.StringOrList{*request.Params.ScheduleNamePrefix}
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListSchedulesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.ListSchedulesResponse)
	if !ok {
		return gen.ListSchedulesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	schedules := make([]gen.Schedule, 0, len(resp.Output))
	for _, sched := range resp.Output {
		schedules = append(schedules, scheduleOutputToModel(sched))
	}

	return gen.ListSchedules200JSONResponse(schedules), nil
}

// GetSchedule retrieves a single schedule by name.
func (s *Server) GetSchedule(ctx context.Context, request gen.GetScheduleRequestObject) (gen.GetScheduleResponseObject, error) {
	req := &protocol.GetScheduleRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetSchedule,
			RequestID: newRequestID(),
		},
		ScheduleName: request.ScheduleName,
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.GetScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.GetScheduleResponse)
	if !ok {
		return gen.GetScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.Output == nil {
		detail := fmt.Sprintf("schedule %q not found", request.ScheduleName)
		if resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
			detail = *resp.ErrorMessage
		}
		return gen.GetScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Schedule not found", detail),
		}, nil
	}

	return gen.GetSchedule200JSONResponse(scheduleOutputToModel(*resp.Output)), nil
}

// PauseSchedule pauses an active schedule.
func (s *Server) PauseSchedule(ctx context.Context, request gen.PauseScheduleRequestObject) (gen.PauseScheduleResponseObject, error) {
	req := &protocol.PauseScheduleRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypePauseSchedule,
			RequestID: newRequestID(),
		},
		ScheduleName: request.ScheduleName,
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.PauseScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.PauseScheduleResponse)
	if !ok {
		return gen.PauseScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if !resp.Success {
		detail := "failed to pause schedule"
		if resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
			detail = *resp.ErrorMessage
		}
		return gen.PauseScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Schedule Pause Failed", detail),
		}, nil
	}

	return gen.PauseSchedule204Response{}, nil
}

// ResumeSchedule resumes a paused schedule.
func (s *Server) ResumeSchedule(ctx context.Context, request gen.ResumeScheduleRequestObject) (gen.ResumeScheduleResponseObject, error) {
	req := &protocol.ResumeScheduleRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeResumeSchedule,
			RequestID: newRequestID(),
		},
		ScheduleName: request.ScheduleName,
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ResumeScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.ResumeScheduleResponse)
	if !ok {
		return gen.ResumeScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if !resp.Success {
		detail := "failed to resume schedule"
		if resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
			detail = *resp.ErrorMessage
		}
		return gen.ResumeScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Schedule Resume Failed", detail),
		}, nil
	}

	return gen.ResumeSchedule204Response{}, nil
}

// TriggerSchedule manually triggers an immediate execution of a schedule.
func (s *Server) TriggerSchedule(ctx context.Context, request gen.TriggerScheduleRequestObject) (gen.TriggerScheduleResponseObject, error) {
	req := &protocol.TriggerScheduleRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeTriggerSchedule,
			RequestID: newRequestID(),
		},
		ScheduleName: request.ScheduleName,
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.TriggerScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.TriggerScheduleResponse)
	if !ok {
		return gen.TriggerScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	var wfID string
	if resp.WorkflowID != nil {
		wfID = *resp.WorkflowID
	}
	if wfID == "" {
		detail := "no workflow id returned for triggered schedule"
		if resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
			detail = *resp.ErrorMessage
		}
		return gen.TriggerScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Trigger Schedule Failed", detail),
		}, nil
	}

	loc := fmt.Sprintf("/v2/orgs/%s/apps/%s/workflows/%s", request.OrgName, request.AppName, wfID)
	return gen.TriggerSchedule201JSONResponse{
		Body: gen.TriggerOutputBody{
			WorkflowId: wfID,
		},
		Headers: gen.TriggerSchedule201ResponseHeaders{
			Location: &loc,
		},
	}, nil
}

// BackfillSchedule triggers backfill workflow runs for a schedule across a time window.
func (s *Server) BackfillSchedule(ctx context.Context, request gen.BackfillScheduleRequestObject) (gen.BackfillScheduleResponseObject, error) {
	if request.Body == nil {
		return gen.BackfillScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusBadRequest,
			Body:       MakeErrorModel(http.StatusBadRequest, "Invalid Request", "request body is required"),
		}, nil
	}

	req := &protocol.BackfillScheduleRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeBackfillSchedule,
			RequestID: newRequestID(),
		},
		ScheduleName: request.ScheduleName,
		Start:        request.Body.StartTime.Format(time.RFC3339),
		End:          request.Body.EndTime.Format(time.RFC3339),
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.BackfillScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.BackfillScheduleResponse)
	if !ok {
		return gen.BackfillScheduledefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	ids := resp.WorkflowIDs
	if ids == nil {
		ids = []string{}
	}

	return gen.BackfillSchedule200JSONResponse{
		WorkflowIds: ids,
	}, nil
}
