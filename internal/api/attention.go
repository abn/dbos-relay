package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/protocol"
	storegen "github.com/abn/relay/internal/store/gen"
)

// FlappingExecutor records an executor that has flapped and triggered multiple recovery dispatches.
type FlappingExecutor struct {
	ExecutorID    string    `json:"executor_id"`
	RecoveryCount int64     `json:"recovery_count"`
	LastRecovery  time.Time `json:"last_recovery"`
}

// NeedsAttentionReport aggregates workflows and executors requiring operator intervention.
type NeedsAttentionReport struct {
	FailedWorkflows    []gen.Workflow     `json:"failed_workflows"`
	StuckWorkflows     []gen.Workflow     `json:"stuck_workflows"`
	OrphanedWorkflows  []gen.Workflow     `json:"orphaned_workflows"`
	FlappingExecutors  []FlappingExecutor `json:"flapping_executors"`
	TotalNeedsAttention int               `json:"total_needs_attention"`
}

// RecoveryStore provides recovery history queries for flapping detection.
type RecoveryStore interface {
	ListRecentRecoveryDispatches(ctx context.Context, arg storegen.ListRecentRecoveryDispatchesParams) ([]storegen.RecoveryDispatch, error)
}

// GetNeedsAttention compiles the needs-attention board dataset for an application.
func (s *Server) GetNeedsAttention(ctx context.Context, orgName, appName string, stuckSLA time.Duration) (*NeedsAttentionReport, int, gen.ErrorModel) {
	orgName = normalizeOrg(orgName)
	org, err := s.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return nil, http.StatusNotFound, MakeErrorModel(http.StatusNotFound, "Not Found", "organisation not found: "+orgName)
	}

	app, err := s.store.GetApplicationByName(ctx, storegen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           appName,
	})
	if err != nil {
		return nil, http.StatusNotFound, MakeErrorModel(http.StatusNotFound, "Not Found", "application not found: "+appName)
	}

	if stuckSLA <= 0 {
		stuckSLA = 15 * time.Minute
		if len(app.Settings) > 0 {
			var sMap map[string]any
			if err := json.Unmarshal(app.Settings, &sMap); err == nil {
				if sVal, ok := sMap["stuck_sla_secs"].(float64); ok && sVal > 0 {
					stuckSLA = time.Duration(sVal) * time.Second
				}
			}
		}
	}

	report := &NeedsAttentionReport{
		FailedWorkflows:   []gen.Workflow{},
		StuckWorkflows:    []gen.Workflow{},
		OrphanedWorkflows: []gen.Workflow{},
		FlappingExecutors: []FlappingExecutor{},
	}

	limit := 50

	// 1. Query Failed Workflows (ERROR or FAILED)
	failedReq := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: uuid.NewString(),
		},
		Body: protocol.ListWorkflowsRequestBody{
			Status:   protocol.StringOrList{"ERROR", "FAILED"},
			Limit:    &limit,
			SortDesc: true,
		},
	}
	if res, err := s.router.Dispatch(ctx, orgName, appName, failedReq); err == nil {
		if wfRes, ok := res.(*protocol.ListWorkflowsResponse); ok && wfRes != nil {
			for _, item := range wfRes.Output {
				report.FailedWorkflows = append(report.FailedWorkflows, mapWorkflow(item))
			}
		}
	}

	// 2. Query Active Executors to identify active versions
	activeVersions := make(map[string]bool)
	executors, _ := s.store.ListExecutorsByApplication(ctx, app.ID)
	for _, exec := range executors {
		if exec.Status == "connected" && exec.ApplicationVersion != "" {
			activeVersions[exec.ApplicationVersion] = true
		}
	}

	// 3. Query Pending Workflows to detect Stuck and Orphaned items
	pendingReq := &protocol.ListWorkflowsRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeListWorkflows,
			RequestID: uuid.NewString(),
		},
		Body: protocol.ListWorkflowsRequestBody{
			Status:   protocol.StringOrList{"PENDING"},
			Limit:    &limit,
			SortDesc: true,
		},
	}
	now := time.Now().UTC()
	if res, err := s.router.Dispatch(ctx, orgName, appName, pendingReq); err == nil {
		if wfRes, ok := res.(*protocol.ListWorkflowsResponse); ok && wfRes != nil {
			for _, item := range wfRes.Output {
				wf := mapWorkflow(item)

				// Check if orphaned by version
				if item.ApplicationVersion != nil && *item.ApplicationVersion != "" {
					if !activeVersions[*item.ApplicationVersion] && len(activeVersions) > 0 {
						report.OrphanedWorkflows = append(report.OrphanedWorkflows, wf)
					}
				}

				// Check if stuck beyond SLA
				if !wf.CreatedAt.IsZero() && now.Sub(wf.CreatedAt) > stuckSLA {
					report.StuckWorkflows = append(report.StuckWorkflows, wf)
				}
			}
		}
	}

	// 4. Query Flapping Executors (dispatched for recovery > 1 time in past hour)
	if rStore, ok := s.store.(RecoveryStore); ok {
		oneHourAgo := now.Add(-1 * time.Hour)
		dispatches, err := rStore.ListRecentRecoveryDispatches(ctx, storegen.ListRecentRecoveryDispatchesParams{
			ApplicationID: app.ID,
			DispatchedAt:  pgtype.Timestamptz{Time: oneHourAgo, Valid: true},
			Limit:         100,
		})
		if err == nil {
			counts := make(map[string]int64)
			lastSeen := make(map[string]time.Time)
			for _, d := range dispatches {
				counts[d.DeadExecutorID]++
				if d.DispatchedAt.Valid && d.DispatchedAt.Time.After(lastSeen[d.DeadExecutorID]) {
					lastSeen[d.DeadExecutorID] = d.DispatchedAt.Time
				}
			}
			for execID, count := range counts {
				if count > 1 {
					report.FlappingExecutors = append(report.FlappingExecutors, FlappingExecutor{
						ExecutorID:    execID,
						RecoveryCount: count,
						LastRecovery:  lastSeen[execID],
					})
				}
			}
		}
	}

	report.TotalNeedsAttention = len(report.FailedWorkflows) + len(report.StuckWorkflows) + len(report.OrphanedWorkflows) + len(report.FlappingExecutors)
	return report, http.StatusOK, gen.ErrorModel{}
}
