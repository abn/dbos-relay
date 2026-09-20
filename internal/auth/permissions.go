// Package auth mints, verifies, and validates credentials for Relay.
package auth

const (
	// Standard grantable permissions confirmed by discovery D4.
	PermApplicationRead  = "application.read"
	PermApplicationWrite = "application.write"
	PermWebsocketConnect = "websocket.connect"
	PermMetricRead       = "metric.read"

	// Standard global roles.
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// CatalogPermissions returns all supported grantable permissions.
func CatalogPermissions() []string {
	return []string{
		PermApplicationRead,
		PermApplicationWrite,
		PermWebsocketConnect,
		PermMetricRead,
	}
}

// RolePermissions returns default permissions granted to the specified role.
func RolePermissions(role string) []string {
	switch role {
	case RoleAdmin, RoleOperator:
		return []string{PermApplicationRead, PermApplicationWrite, PermWebsocketConnect, PermMetricRead}
	case RoleViewer:
		return []string{PermApplicationRead, PermMetricRead}
	default:
		return []string{PermApplicationRead}
	}
}

// HasPermission reports whether granted contains required.
func HasPermission(granted []string, required string) bool {
	for _, p := range granted {
		if p == required {
			return true
		}
	}
	return false
}
