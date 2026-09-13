package api

import (
	"context"
	"net/http"
	"time"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
)

// ListMetrics queries metrics for an application within a time window.
func (s *Server) ListMetrics(ctx context.Context, request gen.ListMetricsRequestObject) (gen.ListMetricsResponseObject, error) {
	req := &protocol.GetMetricsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeGetMetrics,
			RequestID: newRequestID(),
		},
		StartTime:       request.Params.StartTime.Format(time.RFC3339),
		EndTime:         request.Params.EndTime.Format(time.RFC3339),
		ApplicationName: []string{request.AppName},
	}

	respMsg, err := s.router.Dispatch(ctx, request.OrgName, request.AppName, req)
	if err != nil {
		status, errModel := RouterErrorToModel(err)
		return gen.ListMetricsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: status,
			Body:       errModel,
		}, nil
	}

	resp, ok := respMsg.(*protocol.GetMetricsResponse)
	if !ok {
		return gen.ListMetricsdefaultApplicationProblemPlusJSONResponse{
			StatusCode: http.StatusInternalServerError,
			Body:       MakeErrorModel(http.StatusInternalServerError, "Internal Server Error", "unexpected response type from router"),
		}, nil
	}

	metrics := make([]gen.Metric, 0, len(resp.Metrics))
	for _, d := range resp.Metrics {
		appID := request.AppName
		metricType := d.MetricType
		if metricType == "" {
			metricType = "workflow_count"
		}

		granularity := int32(60)
		timeBucket := request.Params.StartTime.UTC()

		metrics = append(metrics, gen.Metric{
			AppId:       appID,
			Granularity: granularity,
			MetricName:  d.MetricName,
			MetricType:  metricType,
			TimeBucket:  timeBucket,
			Value:       int64(d.Value),
		})
	}

	return gen.ListMetrics200JSONResponse(metrics), nil
}
