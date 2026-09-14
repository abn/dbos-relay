// Package ha provides high availability instance clustering, heartbeats, and executor lease adoption.
package ha

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/store/gen"
)

// InstanceStore specifies the database queries needed by the instance manager.
type InstanceStore interface {
	UpsertInstance(ctx context.Context, arg gen.UpsertInstanceParams) (gen.Instance, error)
	HeartbeatInstance(ctx context.Context, id pgtype.UUID) error
	DeleteInstance(ctx context.Context, id pgtype.UUID) error
	AdoptExpiredExecutors(ctx context.Context, arg gen.AdoptExpiredExecutorsParams) ([]gen.Executor, error)
	DeleteStaleInstances(ctx context.Context, cutoff pgtype.Timestamptz) (int64, error)
	ReapExpiredExecutors(ctx context.Context, disconnectedAt pgtype.Timestamptz) (int64, error)
}

// LivenessAdopter adopts orphaned executors into the liveness state machine.
type LivenessAdopter interface {
	AdoptDisconnected(appID pgtype.UUID, executorID, version string)
}

// ManagerOptions configures the instance manager.
type ManagerOptions struct {
	InstanceID        pgtype.UUID
	AdvertiseAddress  string
	Port              int
	HeartbeatInterval time.Duration
	AdoptionInterval  time.Duration
	LeaseDuration     time.Duration
	StaleThreshold    time.Duration
	Logger            *slog.Logger
	Liveness          LivenessAdopter
}

// Manager manages the registration, heartbeat, and executor adoption for a Relay node.
type Manager struct {
	store    InstanceStore
	opts     ManagerOptions
	logger   *slog.Logger
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	id       pgtype.UUID
	addr     string
	port     int
	liveness LivenessAdopter
}

// NewManager creates a new instance manager.
func NewManager(store InstanceStore, opts ManagerOptions) *Manager {
	if !opts.InstanceID.Valid {
		id := uuid.New()
		opts.InstanceID = pgtype.UUID{Bytes: id, Valid: true}
	}
	if opts.AdvertiseAddress == "" {
		opts.AdvertiseAddress = "127.0.0.1"
	}
	if opts.Port == 0 {
		opts.Port = 8090
	}
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = 10 * time.Second
	}
	if opts.AdoptionInterval <= 0 {
		opts.AdoptionInterval = 15 * time.Second
	}
	if opts.LeaseDuration <= 0 {
		opts.LeaseDuration = 60 * time.Second
	}
	if opts.StaleThreshold <= 0 {
		opts.StaleThreshold = 2 * time.Minute
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &Manager{
		store:    store,
		opts:     opts,
		logger:   logger,
		id:       opts.InstanceID,
		addr:     opts.AdvertiseAddress,
		port:     opts.Port,
		liveness: opts.Liveness,
	}
}

// SetLiveness configures the liveness adopter callback for adopted executors.
func (m *Manager) SetLiveness(l LivenessAdopter) {
	m.liveness = l
}

// ID returns the instance ID.
func (m *Manager) ID() pgtype.UUID {
	return m.id
}

// AdvertiseAddress returns the advertised address.
func (m *Manager) AdvertiseAddress() string {
	return m.addr
}

// Port returns the advertised port.
func (m *Manager) Port() int {
	return m.port
}

// Start registers the instance and begins periodic heartbeats and executor adoptions.
func (m *Manager) Start(ctx context.Context) error {
	_, err := m.store.UpsertInstance(ctx, gen.UpsertInstanceParams{
		ID:               m.id,
		AdvertiseAddress: m.addr,
		Port:             int32(m.port),
	})
	if err != nil {
		return fmt.Errorf("registering instance: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	// Reconcile pre-existing expired executor leases
	m.reconcileStartup(runCtx)

	m.wg.Add(2)
	go m.heartbeatLoop(runCtx)
	go m.adoptionLoop(runCtx)

	m.logger.Info("instance registered and active",
		"id", m.id,
		"advertise_address", m.addr,
		"port", m.port,
	)
	return nil
}

// ReconcileStartup adopts any expired executor leases on startup.
func (m *Manager) reconcileStartup(ctx context.Context) {
	leaseExp := pgtype.Timestamptz{
		Time:  time.Now().Add(m.opts.LeaseDuration),
		Valid: true,
	}
	adopted, err := m.store.AdoptExpiredExecutors(ctx, gen.AdoptExpiredExecutorsParams{
		OwnerInstanceID: m.id,
		LeaseExpiresAt:  leaseExp,
	})
	if err != nil {
		m.logger.Warn("startup executor lease adoption failed", "error", err)
		return
	}
	if len(adopted) > 0 {
		m.logger.Info("reconciled expired executor leases on startup", "count", len(adopted))
		if m.liveness != nil {
			for _, exec := range adopted {
				m.liveness.AdoptDisconnected(exec.ApplicationID, exec.ExecutorID, exec.ApplicationVersion)
			}
		}
	}
}

// Stop shuts down the background loops and deregisters the instance.
func (m *Manager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := m.store.DeleteInstance(ctx, m.id); err != nil {
		m.logger.Warn("failed to deregister instance on shutdown", "error", err)
	} else {
		m.logger.Info("instance deregistered cleanly", "id", m.id)
	}
}

func (m *Manager) heartbeatLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(m.opts.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.store.HeartbeatInstance(ctx, m.id); err != nil {
				m.logger.Warn("instance heartbeat failed", "error", err)
			}
		}
	}
}

func (m *Manager) adoptionLoop(ctx context.Context) {
	defer m.wg.Done()
	ticker := time.NewTicker(m.opts.AdoptionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			leaseExp := pgtype.Timestamptz{
				Time:  time.Now().Add(m.opts.LeaseDuration),
				Valid: true,
			}
			adopted, err := m.store.AdoptExpiredExecutors(ctx, gen.AdoptExpiredExecutorsParams{
				OwnerInstanceID: m.id,
				LeaseExpiresAt:  leaseExp,
			})
			if err != nil {
				m.logger.Warn("executor lease adoption failed", "error", err)
			} else if len(adopted) > 0 {
				m.logger.Info("adopted expired executors from failed instances", "count", len(adopted))
				if m.liveness != nil {
					for _, exec := range adopted {
						m.liveness.AdoptDisconnected(exec.ApplicationID, exec.ExecutorID, exec.ApplicationVersion)
					}
				}
			}

			// Clean up stale instances
			staleCutoff := pgtype.Timestamptz{
				Time:  time.Now().Add(-m.opts.StaleThreshold),
				Valid: true,
			}
			if reaped, err := m.store.DeleteStaleInstances(ctx, staleCutoff); err == nil && reaped > 0 {
				m.logger.Info("reaped stale instances", "count", reaped)
			}

			// Reap expired disconnected executors
			graceCutoff := pgtype.Timestamptz{
				Time:  time.Now().Add(-m.opts.LeaseDuration),
				Valid: true,
			}
			if reapedExecs, err := m.store.ReapExpiredExecutors(ctx, graceCutoff); err == nil && reapedExecs > 0 {
				m.logger.Info("reaped expired disconnected executors", "count", reapedExecs)
			}
		}
	}
}
