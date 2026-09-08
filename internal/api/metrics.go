package api

import (
	"context"
	"net/http"
	"strings"
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
		if id, ok := d.Tags["app_id"].(string); ok && id != "" {
			appID = id
		} else if id, ok := d.Tags["application"].(string); ok && id != "" {
			appID = id
		}

		metricType := "workflow_count"
		if t, ok := d.Tags["metric_type"].(string); ok && t != "" {
			metricType = t
		} else if t, ok := d.Tags["type"].(string); ok && t != "" {
			metricType = t
		} else if strings.Contains(d.MetricName, "step") {
			metricType = "step_count"
		} else if strings.Contains(d.MetricName, "recovery") {
			metricType = "recovery_count"
		}

		granularity := int32(60)
		if g, ok := d.Tags["granularity"]; ok {
			switch v := g.(type) {
			case int:
				granularity = int32(v)
			case int32:
				granularity = v
			case int64:
				granularity = int32(v)
			case float64:
				granularity = int32(v)
			}
		}

		var timeBucket time.Time
		if d.Timestamp > 1e11 {
			timeBucket = time.UnixMilli(d.Timestamp).UTC()
		} else if d.Timestamp > 0 {
			timeBucket = time.Unix(d.Timestamp, 0).UTC()
		} else {
			timeBucket = request.Params.StartTime.UTC()
		}

		metrics = append(metrics, gen.Metric{
			AppId:       appID,
			Granularity: granularity,
			MetricName:  d.MetricName,
			MetricType:  metricType,
			TimeBucket:  timeBucket,
			Value:       int64(d.MetricValue),
		})
	}

	return gen.ListMetrics200JSONResponse(metrics), nil
}
