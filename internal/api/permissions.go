package api

import (
	"context"

	"github.com/abn/relay/internal/api/gen"
)

// ListPermissions returns the static permission catalogue.
func (s *Server) ListPermissions(ctx context.Context, request gen.ListPermissionsRequestObject) (gen.ListPermissionsResponseObject, error) {
	return gen.ListPermissions200JSONResponse{
		"admin",
		"developer",
		"operator",
		"viewer",
	}, nil
}
