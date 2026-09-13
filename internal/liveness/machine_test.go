package liveness_test

import (
	"errors"
	"testing"

	"github.com/abn/relay/internal/liveness"
)

func TestStateTransitions(t *testing.T) {
	tests := []struct {
		name       string
		current    liveness.State
		event      liveness.Event
		wantState  liveness.State
		wantAction liveness.Action
		wantErr    error
	}{
		// 1. Connected state
		{
			name:       "connected + connect (keepalive)",
			current:    liveness.StateConnected,
			event:      liveness.EventConnect,
			wantState:  liveness.StateConnected,
			wantAction: liveness.ActionNone,
		},
		{
			name:       "connected + disconnect",
			current:    liveness.StateConnected,
			event:      liveness.EventDisconnect,
			wantState:  liveness.StateDisconnected,
			wantAction: liveness.ActionStartGraceTimer,
		},
		{
			name:       "connected + grace_period_expired (invalid)",
			current:    liveness.StateConnected,
			event:      liveness.EventGracePeriodExpired,
			wantState:  liveness.StateConnected,
			wantAction: liveness.ActionNone,
			wantErr:    liveness.ErrInvalidTransition,
		},
		{
			name:       "connected + recovery_acked (invalid)",
			current:    liveness.StateConnected,
			event:      liveness.EventRecoveryAcked,
			wantState:  liveness.StateConnected,
			wantAction: liveness.ActionNone,
			wantErr:    liveness.ErrInvalidTransition,
		},
		{
			name:       "connected + delete",
			current:    liveness.StateConnected,
			event:      liveness.EventDelete,
			wantState:  liveness.StateDeleted,
			wantAction: liveness.ActionDeleteRecord,
		},

		// 2. Disconnected state
		{
			name:       "disconnected + connect (reconnect within grace)",
			current:    liveness.StateDisconnected,
			event:      liveness.EventConnect,
			wantState:  liveness.StateConnected,
			wantAction: liveness.ActionCancelGraceTimer,
		},
		{
			name:       "disconnected + disconnect (idempotent)",
			current:    liveness.StateDisconnected,
			event:      liveness.EventDisconnect,
			wantState:  liveness.StateDisconnected,
			wantAction: liveness.ActionNone,
		},
		{
			name:       "disconnected + grace_period_expired (timeout)",
			current:    liveness.StateDisconnected,
			event:      liveness.EventGracePeriodExpired,
			wantState:  liveness.StateDead,
			wantAction: liveness.ActionTriggerRecovery,
		},
		{
			name:       "disconnected + recovery_acked (invalid)",
			current:    liveness.StateDisconnected,
			event:      liveness.EventRecoveryAcked,
			wantState:  liveness.StateDisconnected,
			wantAction: liveness.ActionNone,
			wantErr:    liveness.ErrInvalidTransition,
		},
		{
			name:       "disconnected + delete",
			current:    liveness.StateDisconnected,
			event:      liveness.EventDelete,
			wantState:  liveness.StateDeleted,
			wantAction: liveness.ActionDeleteRecord,
		},

		// 3. Dead state
		{
			name:       "dead + connect (reconnect after death refused)",
			current:    liveness.StateDead,
			event:      liveness.EventConnect,
			wantState:  liveness.StateDead,
			wantAction: liveness.ActionNone,
			wantErr:    liveness.ErrReconnectingDeadExecutor,
		},
		{
			name:       "dead + disconnect (idempotent)",
			current:    liveness.StateDead,
			event:      liveness.EventDisconnect,
			wantState:  liveness.StateDead,
			wantAction: liveness.ActionNone,
		},
		{
			name:       "dead + grace_period_expired (idempotent)",
			current:    liveness.StateDead,
			event:      liveness.EventGracePeriodExpired,
			wantState:  liveness.StateDead,
			wantAction: liveness.ActionNone,
		},
		{
			name:       "dead + recovery_acked",
			current:    liveness.StateDead,
			event:      liveness.EventRecoveryAcked,
			wantState:  liveness.StateDeleted,
			wantAction: liveness.ActionDeleteRecord,
		},
		{
			name:       "dead + recovery_failed (retry)",
			current:    liveness.StateDead,
			event:      liveness.EventRecoveryFailed,
			wantState:  liveness.StateDead,
			wantAction: liveness.ActionRetryRecovery,
		},
		{
			name:       "dead + delete",
			current:    liveness.StateDead,
			event:      liveness.EventDelete,
			wantState:  liveness.StateDeleted,
			wantAction: liveness.ActionDeleteRecord,
		},

		// 4. Deleted state
		{
			name:       "deleted + connect (refused)",
			current:    liveness.StateDeleted,
			event:      liveness.EventConnect,
			wantState:  liveness.StateDeleted,
			wantAction: liveness.ActionNone,
			wantErr:    liveness.ErrAlreadyDeleted,
		},
		{
			name:       "deleted + disconnect (refused)",
			current:    liveness.StateDeleted,
			event:      liveness.EventDisconnect,
			wantState:  liveness.StateDeleted,
			wantAction: liveness.ActionNone,
			wantErr:    liveness.ErrAlreadyDeleted,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotState, gotAction, err := liveness.Transition(tc.current, tc.event)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("expected error %v, got %v", tc.wantErr, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			if gotState != tc.wantState {
				t.Errorf("state = %q, want %q", gotState, tc.wantState)
			}
			if gotAction != tc.wantAction {
				t.Errorf("action = %q, want %q", gotAction, tc.wantAction)
			}
		})
	}
}
