package liveness

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/dataplane"
)

// DirectRecovery re-enqueues a dead executor's pending workflows through the
// application's system database. It is used when the application is configured
// in Direct mode and the operator has enabled read-write access.
type DirectRecovery interface {
	HasDirectRecovery(appID pgtype.UUID) bool
	Recover(ctx context.Context, appID pgtype.UUID, executorIDs []string, appVersion string) ([]string, error)
	CheckPendingWorkflows(ctx context.Context, appID pgtype.UUID, executorID, appVersion string) (bool, error)
}

// DataPlaneProbePolicy limits probe heartbeats to Direct mode applications.
type DataPlaneProbePolicy struct {
	manager dataplane.Manager
}

// NewDataPlaneProbePolicy creates a probe policy backed by the data-plane manager.
func NewDataPlaneProbePolicy(manager dataplane.Manager) *DataPlaneProbePolicy {
	return &DataPlaneProbePolicy{manager: manager}
}

// AllowProbe reports whether the application uses Direct mode.
func (p *DataPlaneProbePolicy) AllowProbe(appID pgtype.UUID) bool {
	if p == nil || p.manager == nil {
		return false
	}
	integration, ok := p.manager.GetIntegration(appID)
	return ok && integration == dataplane.IntegrationDirect
}

// DataPlaneRecovery adapts the data-plane manager to the recovery dispatcher.
type DataPlaneRecovery struct {
	manager dataplane.Manager
}

// NewDataPlaneRecovery creates a Direct recovery adapter.
func NewDataPlaneRecovery(manager dataplane.Manager) *DataPlaneRecovery {
	return &DataPlaneRecovery{manager: manager}
}

// HasDirectRecovery reports whether the application has a Direct connection
// that can be used for recovery.
func (d *DataPlaneRecovery) HasDirectRecovery(appID pgtype.UUID) bool {
	if d == nil || d.manager == nil {
		return false
	}
	if !d.manager.HasDataPlane(appID) {
		return false
	}
	integration, ok := d.manager.GetIntegration(appID)
	if !ok {
		return false
	}
	return integration == dataplane.IntegrationDirect
}

// Recover delegates the re-enqueue to the data-plane manager.
func (d *DataPlaneRecovery) Recover(ctx context.Context, appID pgtype.UUID, executorIDs []string, appVersion string) ([]string, error) {
	return d.manager.Recover(ctx, appID, executorIDs, appVersion)
}

// CheckPendingWorkflows verifies that the dead executor owns no PENDING work.
func (d *DataPlaneRecovery) CheckPendingWorkflows(ctx context.Context, appID pgtype.UUID, executorID, appVersion string) (bool, error) {
	return d.manager.HasPendingWorkflows(ctx, appID, executorID, appVersion)
}
