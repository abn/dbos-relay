package api

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
)

func newRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func intToInt32Ptr(i *int) *int32 {
	if i == nil {
		return nil
	}
	v := int32(*i)
	return &v
}

func queueOutputToModel(q protocol.QueueOutput) gen.Queue {
	return gen.Queue{
		ApplicationName:     q.ApplicationName,
		Concurrency:         intToInt32Ptr(q.Concurrency),
		Name:                q.Name,
		PartitionQueue:      q.PartitionQueue,
		PollingIntervalSecs: q.PollingIntervalSec,
		PriorityEnabled:     q.PriorityEnabled,
		RateLimitMax:        intToInt32Ptr(q.RateLimitMax),
		RateLimitPeriodSecs: q.RateLimitPeriodSec,
		WorkerConcurrency:   intToInt32Ptr(q.WorkerConcurrency),
	}
}

// ListQueues lists all queues for the specified application.
func (s *Server) ListQueues(ctx context.Context, request gen.ListQueuesRequestObject) (gen.ListQueuesResponseObject, error) {
	req := &protocol.ListQueuesRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListQueues,
			RequestID: newRequestID(),
		},
		Body: protocol.ListQueuesRequestBody{
			ApplicationName: protocol.StringOrList{request.AppName},
		},
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListQueuesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.ListQueuesResponse)
	if !ok {
		return gen.ListQueuesdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	queues := make([]gen.Queue, 0, len(resp.Output))
	for _, q := range resp.Output {
		queues = append(queues, queueOutputToModel(q))
	}

	return gen.ListQueues200JSONResponse(queues), nil
}

// GetQueue retrieves a single queue by name.
func (s *Server) GetQueue(ctx context.Context, request gen.GetQueueRequestObject) (gen.GetQueueResponseObject, error) {
	req := &protocol.GetQueueRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetQueue,
			RequestID: newRequestID(),
		},
		Name: request.QueueName,
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.GetQueuedefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.GetQueueResponse)
	if !ok {
		return gen.GetQueuedefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	if resp.Output == nil {
		detail := fmt.Sprintf("queue %q not found", request.QueueName)
		if resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
			detail = *resp.ErrorMessage
		}
		return gen.GetQueuedefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusNotFound,
			Body:       MakeErrorModel(http.StatusNotFound, "Queue not found", detail),
		}, nil
	}

	return gen.GetQueue200JSONResponse(queueOutputToModel(*resp.Output)), nil
}
