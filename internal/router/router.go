package router

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

var (
	ErrOrgNotFound     = errors.New("organisation not found")
	ErrAppNotFound     = errors.New("application not found")
	ErrNoLiveExecutor  = errors.New("no live executor connected")
	ErrExecutorTimeout = errors.New("executor request timed out")
	ErrExecutorError   = errors.New("executor returned an error")
)

type AppResolver interface {
	GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error)
	GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error)
}

// InstanceResolver provides executor ownership and instance addressing queries for peer forwarding.
type InstanceResolver interface {
	AppResolver
	ListConnectedExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]gen.Executor, error)
	GetInstance(ctx context.Context, id pgtype.UUID) (gen.Instance, error)
}

// PeerForwarder dispatches requests to peer instances.
type PeerForwarder interface {
	Forward(ctx context.Context, targetURL string, msg protocol.Message) (protocol.Message, error)
}

// DataPlaneManager coordinates data-plane operations against application databases.
type DataPlaneManager interface {
	HasDataPlane(appID pgtype.UUID) bool
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

type contextKey struct{}

var servedFromKey = contextKey{}

// ServedFromTracker tracks the source that answered a routed request.
type ServedFromTracker struct {
	Source string
}

// ContextWithServedFromTracker attaches a tracker to the request context.
func ContextWithServedFromTracker(ctx context.Context) (context.Context, *ServedFromTracker) {
	tracker := &ServedFromTracker{Source: "executor"}
	return context.WithValue(ctx, servedFromKey, tracker), tracker
}

// SetServedFrom updates the served_from source in context.
func SetServedFrom(ctx context.Context, source string) {
	if tracker, ok := ctx.Value(servedFromKey).(*ServedFromTracker); ok {
		tracker.Source = source
	}
}

// GetServedFrom retrieves the served_from source from context.
func GetServedFrom(ctx context.Context) string {
	if tracker, ok := ctx.Value(servedFromKey).(*ServedFromTracker); ok {
		return tracker.Source
	}
	return "executor"
}

type Dispatcher interface {
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

type Router interface {
	Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)
}

type DefaultRouter struct {
	store           AppResolver
	hub             Dispatcher
	forwarder       PeerForwarder
	dataplane       DataPlaneManager
	localInstanceID pgtype.UUID
}

func New(store AppResolver, hub Dispatcher) *DefaultRouter {
	return &DefaultRouter{store: store, hub: hub}
}

// SetForwarder configures peer forwarding when executors are owned by remote instances.
func (r *DefaultRouter) SetForwarder(f PeerForwarder, localInstanceID pgtype.UUID) {
	r.forwarder = f
	r.localInstanceID = localInstanceID
}

// SetDataPlane configures data-plane fallback and aggregation dispatch.
func (r *DefaultRouter) SetDataPlane(dp DataPlaneManager) {
	r.dataplane = dp
}

func (r *DefaultRouter) Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
	if orgName == "" {
		orgName = "local"
	}

	org, err := r.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrOrgNotFound, orgName)
	}

	app, err := r.store.GetApplicationByName(ctx, gen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           appName,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAppNotFound, appName)
	}

	// For fleet-wide aggregate queries, prefer data-plane if configured to protect worker executors
	if isFleetAggregate(msg.GetMessageType()) && r.dataplane != nil && r.dataplane.HasDataPlane(app.ID) {
		dpRes, dpErr := r.dataplane.Dispatch(ctx, app.ID, msg)
		if dpErr == nil {
			SetServedFrom(ctx, "database")
			return dpRes, nil
		}
	}

	res, err := r.hub.Dispatch(ctx, app.ID, msg)
	if err != nil {
		errStr := err.Error()
		isNoExecutor := strings.Contains(errStr, "no live executor") ||
			strings.Contains(errStr, "no executors registered") ||
			strings.Contains(errStr, "no executors available")

		// If no local executor is available and a peer forwarder is configured, check for peer ownership
		if isNoExecutor && r.forwarder != nil {
			if ir, ok := r.store.(InstanceResolver); ok {
				execs, listErr := ir.ListConnectedExecutorsByApplication(ctx, app.ID)
				if listErr == nil {
					for _, exec := range execs {
						if exec.OwnerInstanceID.Valid && exec.OwnerInstanceID != r.localInstanceID {
							inst, instErr := ir.GetInstance(ctx, exec.OwnerInstanceID)
							if instErr == nil && inst.AdvertiseAddress != "" && inst.Port > 0 {
								targetURL := fmt.Sprintf("http://%s:%d/internal/v1/forward/%s", inst.AdvertiseAddress, inst.Port, app.ID)
								fRes, fErr := r.forwarder.Forward(ctx, targetURL, msg)
								if fErr == nil {
									SetServedFrom(ctx, "executor")
									return fRes, nil
								}
							}
						}
					}
				}
			}
		}

		// If no executor is available locally or via peer forwarder, fall back to data-plane if configured
		if isNoExecutor && r.dataplane != nil && r.dataplane.HasDataPlane(app.ID) {
			dpRes, dpErr := r.dataplane.Dispatch(ctx, app.ID, msg)
			if dpErr == nil {
				SetServedFrom(ctx, "database")
				return dpRes, nil
			}
			return nil, fmt.Errorf("data-plane fallback failed: %w", dpErr)
		}

		if isNoExecutor {
			return nil, fmt.Errorf("%w: %w", ErrNoLiveExecutor, err)
		}
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errStr, "timed out") {
			return nil, fmt.Errorf("%w: %w", ErrExecutorTimeout, err)
		}
		return nil, err
	}

	SetServedFrom(ctx, "executor")
	return res, nil
}

func isFleetAggregate(msgType protocol.MessageType) bool {
	return msgType == protocol.MessageTypeGetWorkflowAggregates || msgType == protocol.MessageTypeGetStepAggregates
}
