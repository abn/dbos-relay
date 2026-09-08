package hub

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/auth"
	"github.com/abn/relay/internal/problem"
	"github.com/abn/relay/internal/store/gen"
)

// AuthStore specifies the queries required for conductor authentication.
type AuthStore interface {
	GetAPIKeyByLookup(ctx context.Context, lookup string) (gen.ApiKey, error)
	GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error)
	CreateApplication(ctx context.Context, arg gen.CreateApplicationParams) (gen.Application, error)
}

// Authenticate verifies the conductor key and returns the application ID.
func Authenticate(ctx context.Context, q AuthStore, appName, conductorKey string) (pgtype.UUID, error) {
	if appName == "" || conductorKey == "" {
		return pgtype.UUID{}, errors.New("missing app name or conductor key")
	}

	lookup := auth.Lookup(conductorKey)
	keyRecord, err := q.GetAPIKeyByLookup(ctx, lookup)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, errors.New("invalid conductor key")
		}
		return pgtype.UUID{}, err
	}

	if !auth.Verify(conductorKey, keyRecord.KeyHash) {
		return pgtype.UUID{}, errors.New("invalid conductor key")
	}

	// Validate scope: application_names empty means all apps.
	allowed := false
	if len(keyRecord.ApplicationNames) == 0 {
		allowed = true
	} else {
		for _, name := range keyRecord.ApplicationNames {
			if name == appName {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		return pgtype.UUID{}, errors.New("key does not have access to this application")
	}

	// Resolve application ID.
	app, err := q.GetApplicationByName(ctx, gen.GetApplicationByNameParams{
		OrganisationID: keyRecord.OrganisationID,
		Name:           appName,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Get or Create
			app, err = q.CreateApplication(ctx, gen.CreateApplicationParams{
				OrganisationID: keyRecord.OrganisationID,
				Name:           appName,
				Settings:       []byte("{}"),
			})
			if err != nil {
				return pgtype.UUID{}, err
			}
		} else {
			return pgtype.UUID{}, err
		}
	}

	return app.ID, nil
}

// HandleAuthError writes a problem response based on the authentication error.
func HandleAuthError(w http.ResponseWriter, r *http.Request, err error) {
	if strings.Contains(err.Error(), "invalid conductor key") || strings.Contains(err.Error(), "missing app name") {
		problem.Write(w, &problem.Problem{Status: http.StatusUnauthorized, Title: "Authentication Failed", Detail: err.Error()})
		return
	}
	if strings.Contains(err.Error(), "does not have access") {
		problem.Write(w, &problem.Problem{Status: http.StatusForbidden, Title: "Forbidden", Detail: err.Error()})
		return
	}
	problem.Write(w, &problem.Problem{Status: http.StatusInternalServerError, Title: "Internal Server Error", Detail: "an error occurred during authentication"})
}
