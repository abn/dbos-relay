// Package auth mints, verifies, and validates credentials for Relay.
package auth

const (
	// Standard grantable permissions confirmed by discovery D4.
	PermApplicationRead  = "application.read"
	PermApplicationWrite = "application.write"
	PermWebsocketConnect = "websocket.connect"

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
	}
}

// RolePermissions returns default permissions granted to the specified role.

// HasPermission reports whether granted contains required.
