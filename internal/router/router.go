package router

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/dataplane"
	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

var (
	ErrOrgNotFound          = errors.New("organisation not found")
	ErrAppNotFound          = errors.New("application not found")
	ErrNoLiveExecutor       = errors.New("no live executor connected")
	ErrExecutorTimeout      = errors.New("executor request timed out")
	ErrExecutorError        = errors.New("executor returned an error")
	ErrOperationUnsupported = errors.New("operation not supported")
	ErrStoreUnavailable     = errors.New("store unavailable")
	ErrDataPlaneUnavailable = errors.New("data-plane unavailable")
	ErrReadOnlyMode         = dataplane.ErrReadOnlyMode
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
	GetIntegration(appID pgtype.UUID) (dataplane.Integration, bool)
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
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		switch orgName {
		case "local":
			org, err = r.store.GetOrganisationByName(ctx, "default")
		case "default":
			org, err = r.store.GetOrganisationByName(ctx, "local")
		}
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrOrgNotFound, orgName)
		}
		return nil, fmt.Errorf("%w: %w", ErrStoreUnavailable, err)
	}

	app, err := r.store.GetApplicationByName(ctx, gen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           appName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrAppNotFound, appName)
		}
		return nil, fmt.Errorf("%w: %w", ErrStoreUnavailable, err)
	}

	hasDataPlane := r.dataplane != nil && r.dataplane.HasDataPlane(app.ID)
	integration := dataplane.IntegrationHybrid
	if hasDataPlane {
		if mode, ok := r.dataplane.GetIntegration(app.ID); ok {
			integration = mode
		}
	}

	// Direct mode prefers the system database for every operation. An
	// unsupported database operation falls through to a live executor. A
	// read may be retried over the socket after a database failure, because a
	// read is safe to repeat. A mutation is not: a timeout can arrive after the
	// write committed, so retrying fork, rewind, trigger, backfill, or resume
	// over the socket could apply it twice. A read-only database also fails
	// closed; the socket must not be used to bypass the operator's choice.
	var directErr error
	if integration == dataplane.IntegrationDirect && hasDataPlane {
		dpRes, dpErr := r.dataplane.Dispatch(ctx, app.ID, msg)
		if dpErr == nil {
			SetServedFrom(ctx, "database")
			return dpRes, nil
		}
		switch {
		case errors.Is(dpErr, dataplane.ErrReadOnlyMode):
			return nil, fmt.Errorf("%w: %w", ErrReadOnlyMode, dpErr)
		case errors.Is(dpErr, dataplane.ErrUnsupportedOperation):
			// Fall through to the executor.
		case dataplane.IsMutating(msg.GetMessageType()):
			return dataPlaneFailure(msg, dpErr)
		case isDataPlaneNotFound(dpErr):
			return dataPlaneFailure(msg, dpErr)
		default:
			directErr = dpErr
		}
	}

	// For fleet-wide aggregate queries in hybrid mode, prefer the data plane to
	// protect worker executors from heavy analytical reads.
	if integration == dataplane.IntegrationHybrid && isFleetAggregate(msg.GetMessageType()) && hasDataPlane {
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
					seenOwners := make(map[pgtype.UUID]bool)
					attempts := 0
					const maxPeerForwards = 3

					for _, exec := range execs {
						if ctx.Err() != nil {
							break
						}
						if attempts >= maxPeerForwards {
							break
						}
						if !exec.OwnerInstanceID.Valid || exec.OwnerInstanceID == r.localInstanceID {
							continue
						}
						if exec.LeaseExpiresAt.Valid && exec.LeaseExpiresAt.Time.Before(time.Now()) {
							continue
						}
						if seenOwners[exec.OwnerInstanceID] {
							continue
						}
						seenOwners[exec.OwnerInstanceID] = true

						inst, instErr := ir.GetInstance(ctx, exec.OwnerInstanceID)
						if instErr != nil || inst.AdvertiseAddress == "" || inst.Port <= 0 {
							continue
						}
						if inst.HeartbeatAt.Valid && time.Since(inst.HeartbeatAt.Time) > 2*time.Minute {
							continue
						}

						attempts++
						scheme := "http"
						addr := inst.AdvertiseAddress
						if strings.HasPrefix(addr, "https://") {
							scheme = "https"
							addr = strings.TrimPrefix(addr, "https://")
						} else if strings.HasPrefix(addr, "http://") {
							scheme = "http"
							addr = strings.TrimPrefix(addr, "http://")
						} else if s := os.Getenv("RELAY_PEER_SCHEME"); s != "" {
							scheme = s
						}
						targetURL := fmt.Sprintf("%s://%s:%d/internal/v1/forward/%s", scheme, addr, inst.Port, app.ID)

						forwardTimeout := 5 * time.Second
						if dl, ok := ctx.Deadline(); ok {
							remaining := time.Until(dl)
							if remaining < forwardTimeout {
								forwardTimeout = remaining
							}
						}
						if forwardTimeout <= 0 {
							break
						}

						attemptCtx, cancel := context.WithTimeout(ctx, forwardTimeout)
						fRes, fErr := r.forwarder.Forward(attemptCtx, targetURL, msg)
						cancel()
						if fErr == nil {
							SetServedFrom(ctx, "executor")
							return fRes, nil
						}
					}
				}
			}
		}

		if isNoExecutor && directErr != nil {
			return dataPlaneFailure(msg, directErr)
		}

		// If no executor is available locally or via peer forwarder, fall back to
		// the data plane in hybrid mode. Direct mode already tried it above, and
		// Attached mode never touches the database.
		if isNoExecutor && integration == dataplane.IntegrationHybrid && hasDataPlane {
			dpRes, dpErr := r.dataplane.Dispatch(ctx, app.ID, msg)
			if dpErr == nil {
				SetServedFrom(ctx, "database")
				return dpRes, nil
			}
			return dataPlaneFailure(msg, dpErr)
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

func isDataPlaneNotFound(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

// dataPlaneFailure maps a data-plane error onto the router's public error
// taxonomy. A missing record is a successful empty response, not an error,
// because the REST surface distinguishes not-found from unavailable.
func dataPlaneFailure(msg protocol.Message, dpErr error) (protocol.Message, error) {
	if errors.Is(dpErr, dataplane.ErrUnsupportedOperation) {
		return nil, fmt.Errorf("%w: %w", ErrNoLiveExecutor, dpErr)
	}
	if errors.Is(dpErr, dataplane.ErrReadOnlyMode) {
		return nil, fmt.Errorf("%w: %w", ErrReadOnlyMode, dpErr)
	}
	if isDataPlaneNotFound(dpErr) {
		switch req := msg.(type) {
		case *protocol.GetWorkflowRequest:
			return &protocol.GetWorkflowResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetWorkflow,
					RequestID: req.RequestID,
				},
				Output: nil,
			}, nil
		case *protocol.GetScheduleRequest:
			return &protocol.GetScheduleResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetSchedule,
					RequestID: req.RequestID,
				},
				Output: nil,
			}, nil
		case *protocol.GetQueueRequest:
			return &protocol.GetQueueResponse{
				Envelope: protocol.Envelope{
					Type:      protocol.MessageTypeGetQueue,
					RequestID: req.RequestID,
				},
				Output: nil,
			}, nil
		}
	}
	if errors.Is(dpErr, context.DeadlineExceeded) || strings.Contains(strings.ToLower(dpErr.Error()), "timed out") {
		return nil, fmt.Errorf("%w: data-plane statement timeout: %w", ErrExecutorTimeout, dpErr)
	}
	return nil, fmt.Errorf("%w: data-plane fallback failed: %w", ErrDataPlaneUnavailable, dpErr)
}
