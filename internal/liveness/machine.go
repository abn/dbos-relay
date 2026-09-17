package liveness

import (
	"errors"
	"fmt"
)

// State represents the lifecycle state of an executor.
type State string

const (
	StateConnected    State = "connected"
	StateDisconnected State = "disconnected"
	StateDead         State = "dead"
	StateDeleted      State = "deleted"
)

// Event represents an event that drives state transitions.
type Event string

const (
	EventConnect            Event = "connect"
	EventDisconnect         Event = "disconnect"
	EventGracePeriodExpired Event = "grace_period_expired"
	EventRecoveryAcked      Event = "recovery_acked"
	EventRecoveryFailed     Event = "recovery_failed"
	EventDelete             Event = "delete"
)

// Action represents an action triggered by a state transition.
type Action string

const (
	ActionNone             Action = "none"
	ActionStartGraceTimer  Action = "start_grace_timer"
	ActionCancelGraceTimer Action = "cancel_grace_timer"
	ActionTriggerRecovery  Action = "trigger_recovery"
	ActionRetryRecovery    Action = "retry_recovery"
	ActionDeleteRecord     Action = "delete_record"
)

var (
	ErrReconnectingDeadExecutor = errors.New("cannot reconnect an executor that has been declared dead")
	ErrAlreadyDeleted           = errors.New("cannot transition a deleted executor")
	ErrInvalidTransition        = errors.New("invalid state transition")
)

// Transition computes the next state and action as a pure function of current state and event.
// It performs zero I/O, does not sleep, and accesses no timers or databases.
func Transition(current State, event Event) (State, Action, error) {
	switch current {
	case "", StateConnected:
		switch event {
		case EventConnect:
			// Re-registration or initial connection
			return StateConnected, ActionNone, nil
		case EventDisconnect:
			return StateDisconnected, ActionStartGraceTimer, nil
		case EventGracePeriodExpired:
			return current, ActionNone, fmt.Errorf("%w: cannot expire timer while connected", ErrInvalidTransition)
		case EventRecoveryAcked, EventRecoveryFailed:
			return current, ActionNone, fmt.Errorf("%w: cannot process recovery for a connected executor", ErrInvalidTransition)
		case EventDelete:
			return StateDeleted, ActionDeleteRecord, nil
		default:
			return current, ActionNone, fmt.Errorf("%w: unhandled event %s in state %s", ErrInvalidTransition, event, current)
		}

	case StateDisconnected:
		switch event {
		case EventConnect:
			// Reconnection within grace period: cancel timer and revert to connected
			return StateConnected, ActionCancelGraceTimer, nil
		case EventDisconnect:
			// Repeated disconnect event while already disconnected is a no-op
			return StateDisconnected, ActionNone, nil
		case EventGracePeriodExpired:
			// Grace period elapsed without reconnect: transition to dead and trigger recovery
			return StateDead, ActionTriggerRecovery, nil
		case EventRecoveryAcked, EventRecoveryFailed:
			return current, ActionNone, fmt.Errorf("%w: cannot process recovery before executor is declared dead", ErrInvalidTransition)
		case EventDelete:
			return StateDeleted, ActionDeleteRecord, nil
		default:
			return current, ActionNone, fmt.Errorf("%w: unhandled event %s in state %s", ErrInvalidTransition, event, current)
		}

	case StateDead:
		switch event {
		case EventConnect:
			// A dead executor cannot resume its old connection, but a fresh registration
			// handshake from a restarted process with the same ID should be allowed.
			return StateConnected, ActionNone, nil
		case EventDisconnect:
			// Redundant disconnect on dead executor
			return StateDead, ActionNone, nil
		case EventGracePeriodExpired:
			// Timer already fired
			return StateDead, ActionNone, nil
		case EventRecoveryAcked:
			// Healthy peer acknowledged recovery; record is ready for final deletion
			return StateDeleted, ActionDeleteRecord, nil
		case EventRecoveryFailed:
			// Recovery dispatch failed or timed out; retry with another healthy peer
			return StateDead, ActionRetryRecovery, nil
		case EventDelete:
			return StateDeleted, ActionDeleteRecord, nil
		default:
			return current, ActionNone, fmt.Errorf("%w: unhandled event %s in state %s", ErrInvalidTransition, event, current)
		}

	case StateDeleted:
		return StateDeleted, ActionNone, ErrAlreadyDeleted

	default:
		return current, ActionNone, fmt.Errorf("%w: unknown current state %s", ErrInvalidTransition, current)
	}
}
