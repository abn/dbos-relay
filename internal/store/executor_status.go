package store

import (
	"github.com/abn/relay/internal/store/gen"
)

// IsLiveStatus reports whether an executor status counts as available for
// dispatch and resume decisions. The store persists the ExecutorStatus
// enum, so only the connected value qualifies; any other spelling,
// including display labels such as HEALTHY, does not satisfy liveness.
// Metrics label mapping lives with the exposition code and is the only
// place that translates these values for display.
func IsLiveStatus(status gen.ExecutorStatus) bool {
	return status == gen.ExecutorStatusConnected
}
