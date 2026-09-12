package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DecodeRequest parses the JSON payload into the corresponding concrete Message request type.
func DecodeRequest(data []byte) (Message, error) {
	return decodeWithDirection(data, false)
}

// DecodeResponse parses the JSON payload into the corresponding concrete Message response type.
func DecodeResponse(data []byte) (Message, error) {
	return decodeWithDirection(data, true)
}

// Decode parses the JSON payload into the corresponding concrete Message type.
// Deprecated: Use DecodeRequest or DecodeResponse instead.
func Decode(data []byte) (Message, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("malformed message: %w", err)
	}
	var typeStr string
	if typeBytes, ok := raw["type"]; ok {
		if err := json.Unmarshal(typeBytes, &typeStr); err != nil {
			return nil, fmt.Errorf("malformed type field: %w", err)
		}
	} else {
		return nil, fmt.Errorf("missing type field")
	}

	msgType := MessageType(typeStr)

	isResponse := false
	if _, ok := raw["error_message"]; ok {
		isResponse = true
	} else if _, ok := raw["success"]; ok {
		isResponse = true
	} else if _, ok := raw["output"]; ok {
		isResponse = true
	} else {
		switch msgType {
		case MessageTypeExecutorInfo:
			if _, ok := raw["executor_id"]; ok {
				isResponse = true
			}
		case MessageTypeForkWorkflow:
			if _, ok := raw["new_workflow_id"]; ok {
				if _, hasBody := raw["body"]; !hasBody {
					isResponse = true
				}
			}
		case MessageTypeForkFromFailure:
			if _, ok := raw["forked_workflow_ids"]; ok {
				isResponse = true
			}
		case MessageTypeExistPendingWorkflows:
			if _, ok := raw["exist"]; ok {
				isResponse = true
			}
		case MessageTypeGetMetrics:
			if _, ok := raw["metrics"]; ok {
				isResponse = true
			}
		case MessageTypeExportWorkflow:
			if _, ok := raw["serialized_workflow"]; ok {
				if _, hasExport := raw["export_children"]; !hasExport {
					isResponse = true
				}
			}
		case MessageTypeBackfillSchedule:
			if _, ok := raw["workflow_ids"]; ok {
				isResponse = true
			}
		case MessageTypeTriggerSchedule:
			if _, ok := raw["workflow_id"]; ok {
				if _, hasSched := raw["schedule_name"]; !hasSched {
					isResponse = true
				}
			}
		case MessageTypeGetWorkflowEvents:
			if _, ok := raw["events"]; ok {
				isResponse = true
			}
		case MessageTypeGetWorkflowNotifications:
			if _, ok := raw["notifications"]; ok {
				isResponse = true
			}
		case MessageTypeGetWorkflowStreams:
			if _, ok := raw["streams"]; ok {
				isResponse = true
			}
		}
	}
	return decodeWithDirection(data, isResponse)
}

func decodeWithDirection(data []byte, isResponse bool) (Message, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("malformed message: %w", err)
	}

	var typeStr string
	if typeBytes, ok := raw["type"]; ok {
		if err := json.Unmarshal(typeBytes, &typeStr); err != nil {
			return nil, fmt.Errorf("malformed type field: %w", err)
		}
	} else {
		return nil, fmt.Errorf("missing type field")
	}

	msgType := MessageType(typeStr)

	var msg Message
	if isResponse {
		switch msgType {
		case MessageTypeExecutorInfo:
			msg = &ExecutorInfoResponse{}
		case MessageTypeRecovery:
			msg = &RecoveryResponse{}
		case MessageTypeCancel:
			msg = &CancelWorkflowResponse{}
		case MessageTypeResume:
			msg = &ResumeWorkflowResponse{}
		case MessageTypeListWorkflows, MessageTypeListQueuedWorkflows:
			msg = &ListWorkflowsResponse{}
		case MessageTypeListSteps:
			msg = &ListStepsResponse{}
		case MessageTypeGetWorkflow:
			msg = &GetWorkflowResponse{}
		case MessageTypeForkWorkflow:
			msg = &ForkWorkflowResponse{}
		case MessageTypeForkFromFailure:
			msg = &ForkFromFailureResponse{}
		case MessageTypeExistPendingWorkflows:
			msg = &ExistPendingWorkflowsResponse{}
		case MessageTypeRetention:
			msg = &RetentionResponse{}
		case MessageTypeGetMetrics:
			msg = &GetMetricsResponse{}
		case MessageTypeExportWorkflow:
			msg = &ExportWorkflowResponse{}
		case MessageTypeImportWorkflow:
			msg = &ImportWorkflowResponse{}
		case MessageTypeDelete:
			msg = &DeleteWorkflowResponse{}
		case MessageTypeAlert:
			msg = &AlertResponse{}
		case MessageTypeListSchedules:
			msg = &ListSchedulesResponse{}
		case MessageTypeGetSchedule:
			msg = &GetScheduleResponse{}
		case MessageTypePauseSchedule:
			msg = &PauseScheduleResponse{}
		case MessageTypeResumeSchedule:
			msg = &ResumeScheduleResponse{}
		case MessageTypeBackfillSchedule:
			msg = &BackfillScheduleResponse{}
		case MessageTypeTriggerSchedule:
			msg = &TriggerScheduleResponse{}
		case MessageTypeGetWorkflowEvents:
			msg = &GetWorkflowEventsResponse{}
		case MessageTypeGetWorkflowNotifications:
			msg = &GetWorkflowNotificationsResponse{}
		case MessageTypeGetWorkflowStreams:
			msg = &GetWorkflowStreamsResponse{}
		case MessageTypeGetWorkflowAggregates:
			msg = &GetWorkflowAggregatesResponse{}
		case MessageTypeGetStepAggregates:
			msg = &GetStepAggregatesResponse{}
		case MessageTypeListApplicationVersions:
			msg = &ListApplicationVersionsResponse{}
		case MessageTypeSetLatestApplicationVersion:
			msg = &SetLatestApplicationVersionResponse{}
		case MessageTypeListQueues:
			msg = &ListQueuesResponse{}
		case MessageTypeGetQueue:
			msg = &GetQueueResponse{}
		default:
			return nil, fmt.Errorf("unknown message type for response: %q", msgType)
		}
	} else {
		switch msgType {
		case MessageTypeExecutorInfo:
			msg = &ExecutorInfoRequest{}
		case MessageTypeRecovery:
			msg = &RecoveryRequest{}
		case MessageTypeCancel:
			msg = &CancelWorkflowRequest{}
		case MessageTypeResume:
			msg = &ResumeWorkflowRequest{}
		case MessageTypeListWorkflows, MessageTypeListQueuedWorkflows:
			msg = &ListWorkflowsRequest{}
		case MessageTypeListSteps:
			msg = &ListStepsRequest{}
		case MessageTypeGetWorkflow:
			msg = &GetWorkflowRequest{}
		case MessageTypeForkWorkflow:
			msg = &ForkWorkflowRequest{}
		case MessageTypeForkFromFailure:
			msg = &ForkFromFailureRequest{}
		case MessageTypeExistPendingWorkflows:
			msg = &ExistPendingWorkflowsRequest{}
		case MessageTypeRetention:
			msg = &RetentionRequest{}
		case MessageTypeGetMetrics:
			msg = &GetMetricsRequest{}
		case MessageTypeExportWorkflow:
			msg = &ExportWorkflowRequest{}
		case MessageTypeImportWorkflow:
			msg = &ImportWorkflowRequest{}
		case MessageTypeDelete:
			msg = &DeleteWorkflowRequest{}
		case MessageTypeAlert:
			msg = &AlertRequest{}
		case MessageTypeListSchedules:
			msg = &ListSchedulesRequest{}
		case MessageTypeGetSchedule:
			msg = &GetScheduleRequest{}
		case MessageTypePauseSchedule:
			msg = &PauseScheduleRequest{}
		case MessageTypeResumeSchedule:
			msg = &ResumeScheduleRequest{}
		case MessageTypeBackfillSchedule:
			msg = &BackfillScheduleRequest{}
		case MessageTypeTriggerSchedule:
			msg = &TriggerScheduleRequest{}
		case MessageTypeGetWorkflowEvents:
			msg = &GetWorkflowEventsRequest{}
		case MessageTypeGetWorkflowNotifications:
			msg = &GetWorkflowNotificationsRequest{}
		case MessageTypeGetWorkflowStreams:
			msg = &GetWorkflowStreamsRequest{}
		case MessageTypeGetWorkflowAggregates:
			msg = &GetWorkflowAggregatesRequest{}
		case MessageTypeGetStepAggregates:
			msg = &GetStepAggregatesRequest{}
		case MessageTypeListApplicationVersions:
			msg = &ListApplicationVersionsRequest{}
		case MessageTypeSetLatestApplicationVersion:
			msg = &SetLatestApplicationVersionRequest{}
		case MessageTypeListQueues:
			msg = &ListQueuesRequest{}
		case MessageTypeGetQueue:
			msg = &GetQueueRequest{}
		default:
			return nil, fmt.Errorf("unknown message type for request: %q", msgType)
		}
	}

	if err := json.NewDecoder(bytes.NewReader(data)).Decode(msg); err != nil {
		return nil, fmt.Errorf("malformed payload for type %q: %w", msgType, err)
	}

	return msg, nil
}

// Encode serializes a Message to its JSON wire format.
func Encode(msg Message) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to encode message: %w", err)
	}
	return data, nil
}
