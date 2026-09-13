package liveness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

var (
	ErrNoHealthyPeers            = errors.New("no healthy peer executors available for recovery")
	ErrCrossTenantRecovery       = errors.New("cross-application or cross-organisation recovery refused")
	ErrVersionMismatchDisallowed = errors.New("no peer running matching version and cross-version recovery is not permitted")
	ErrRecoveryFailed            = errors.New("recovery failed across all available peer executors")
)

// Peer represents a connected candidate executor.
type Peer struct {
	AppID              pgtype.UUID
	OrgID              pgtype.UUID
	ExecutorID         string
	ApplicationVersion string
}

// PeerFinder queries currently connected healthy executors.
type PeerFinder interface {
	FindHealthyPeers(ctx context.Context, appID pgtype.UUID) ([]Peer, error)
}

// RecoveryTransport sends a recovery protocol request to a specific executor.
type RecoveryTransport interface {
	SendRecovery(ctx context.Context, appID pgtype.UUID, targetExecutorID string, req *protocol.RecoveryRequest) (*protocol.RecoveryResponse, error)
}

// ExecutorDeleter deletes the dead executor record once recovery is confirmed.
type ExecutorDeleter interface {
	DeleteExecutor(ctx context.Context, arg gen.DeleteExecutorParams) error
}

// RecoveryRecorder records workflow recovery dispatch outcomes for flapping tracking.
type RecoveryRecorder interface {
	RecordRecoveryDispatch(ctx context.Context, arg gen.RecordRecoveryDispatchParams) (gen.RecoveryDispatch, error)
}

// DispatcherOptions configures recovery dispatch behavior.
type DispatcherOptions struct {
	AllowVersionMismatch bool
	Logger               *slog.Logger
}

// RecoveryDispatcher orchestrates workflow recovery when an executor dies.
type RecoveryDispatcher struct {
	peers     PeerFinder
	transport RecoveryTransport
	deleter   ExecutorDeleter
	recorder  RecoveryRecorder
	opts      DispatcherOptions
}

// SetRecorder configures the recovery dispatch recorder.
func (d *RecoveryDispatcher) SetRecorder(r RecoveryRecorder) {
	d.recorder = r
}

// NewRecoveryDispatcher creates a new RecoveryDispatcher.
func NewRecoveryDispatcher(peers PeerFinder, transport RecoveryTransport, deleter ExecutorDeleter, opts DispatcherOptions) *RecoveryDispatcher {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &RecoveryDispatcher{
		peers:     peers,
		transport: transport,
		deleter:   deleter,
		opts:      opts,
	}
}

// SelectCandidates sorts healthy peers for recovering a dead executor:
// 1. Rejects peers that belong to a different application or organization.
// 2. Excludes the dead executor itself.
// 3. Prioritizes peers running the identical application version.
// 4. Includes peers running other versions only if allowed by options.
func SelectCandidates(deadAppID pgtype.UUID, deadExecutorID, deadVersion string, peers []Peer, allowCrossVersion bool) ([]Peer, error) {
	var exactMatches []Peer
	var alternateVersions []Peer

	for _, p := range peers {
		// Strict tenant isolation: refuse any peer with mismatched AppID
		if p.AppID != deadAppID {
			return nil, fmt.Errorf("%w: candidate app %v != target app %v", ErrCrossTenantRecovery, p.AppID, deadAppID)
		}
		// Do not send recovery to the dead executor itself
		if p.ExecutorID == deadExecutorID {
			continue
		}

		if p.ApplicationVersion == deadVersion {
			exactMatches = append(exactMatches, p)
		} else {
			alternateVersions = append(alternateVersions, p)
		}
	}

	if len(exactMatches) > 0 {
		if allowCrossVersion {
			return append(exactMatches, alternateVersions...), nil
		}
		return exactMatches, nil
	}

	if len(alternateVersions) > 0 {
		if !allowCrossVersion {
			return nil, ErrVersionMismatchDisallowed
		}
		return alternateVersions, nil
	}

	return nil, ErrNoHealthyPeers
}

// RecoverDeadExecutor dispatches recovery for a dead executor to a healthy peer.
// Upon acknowledgment, the dead executor record is deleted from Relay's store.
func (d *RecoveryDispatcher) RecoverDeadExecutor(ctx context.Context, appID pgtype.UUID, deadExecutorID, deadVersion string) error {
	peers, err := d.peers.FindHealthyPeers(ctx, appID)
	if err != nil {
		return fmt.Errorf("listing healthy peers: %w", err)
	}

	candidates, err := SelectCandidates(appID, deadExecutorID, deadVersion, peers, d.opts.AllowVersionMismatch)
	if err != nil {
		return err
	}

	reqID := uuid.NewString()
	recReq := &protocol.RecoveryRequest{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeRecovery,
			RequestID: reqID,
		},
		ExecutorIDs: []string{deadExecutorID},
	}

	var lastErr error
	for _, candidate := range candidates {
		d.opts.Logger.Info("dispatching workflow recovery",
			"deadExecutorID", deadExecutorID,
			"targetPeerID", candidate.ExecutorID,
			"appVersion", candidate.ApplicationVersion,
		)

		resp, err := d.transport.SendRecovery(ctx, appID, candidate.ExecutorID, recReq)
		if err != nil {
			d.opts.Logger.Warn("recovery dispatch failed on peer",
				"peerID", candidate.ExecutorID,
				"error", err,
			)
			lastErr = err
			continue
		}

		if resp == nil || !resp.Success {
			errMsg := "peer rejected recovery"
			if resp != nil && resp.ErrorMessage != nil && *resp.ErrorMessage != "" {
				errMsg = *resp.ErrorMessage
			}
			lastErr = errors.New(errMsg)
			d.opts.Logger.Warn("recovery rejected by peer",
				"peerID", candidate.ExecutorID,
				"detail", errMsg,
			)
			continue
		}

		// Recovery acknowledged! Delete the dead executor record.
		d.opts.Logger.Info("recovery confirmed, deleting dead executor record",
			"deadExecutorID", deadExecutorID,
		)

		if d.deleter != nil {
			if err := d.deleter.DeleteExecutor(ctx, gen.DeleteExecutorParams{
				ApplicationID: appID,
				ExecutorID:    deadExecutorID,
			}); err != nil {
				d.opts.Logger.Error("failed to delete dead executor record after recovery",
					"deadExecutorID", deadExecutorID,
					"error", err,
				)
				return fmt.Errorf("deleting dead executor record: %w", err)
			}
		}

		if d.recorder != nil {
			_, _ = d.recorder.RecordRecoveryDispatch(ctx, gen.RecordRecoveryDispatchParams{
				ApplicationID:    appID,
				DeadExecutorID:   deadExecutorID,
				TargetExecutorID: candidate.ExecutorID,
				DispatchedAt:     pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
				Success:          true,
			})
		}

		return nil
	}

	if lastErr != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryFailed, lastErr)
	}
	return ErrRecoveryFailed
}
