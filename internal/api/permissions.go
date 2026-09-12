package api

import (
	"context"

	"github.com/abn/relay/internal/api/gen"
	"github.com/abn/relay/internal/auth"
)

// ListPermissions returns the static permission catalogue.
func (s *Server) ListPermissions(ctx context.Context, request gen.ListPermissionsRequestObject) (gen.ListPermissionsResponseObject, error) {
	return gen.ListPermissions200JSONResponse(auth.CatalogPermissions()), nil
}
