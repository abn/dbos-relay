package api

import (
	"context"
	"log/slog"

	"github.com/abn/relay/internal/router"
	storegen "github.com/abn/relay/internal/store/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

// StoreReader provides database access for the API server.
type StoreReader interface {
	GetOrganisationByName(ctx context.Context, name string) (storegen.Organisation, error)
	GetApplicationByName(ctx context.Context, arg storegen.GetApplicationByNameParams) (storegen.Application, error)
	ListApplicationsByOrganisation(ctx context.Context, organisationID pgtype.UUID) ([]storegen.Application, error)
	UpsertApplication(ctx context.Context, arg storegen.UpsertApplicationParams) (storegen.Application, error)
	UpdateApplicationSettings(ctx context.Context, arg storegen.UpdateApplicationSettingsParams) (storegen.Application, error)
	DeleteApplication(ctx context.Context, arg storegen.DeleteApplicationParams) (storegen.Application, error)
	ListExecutorsByApplication(ctx context.Context, applicationID pgtype.UUID) ([]storegen.Executor, error)
	ListAPIKeys(ctx context.Context, organisationID pgtype.UUID) ([]storegen.ApiKey, error)
	CreateAPIKey(ctx context.Context, arg storegen.CreateAPIKeyParams) (storegen.ApiKey, error)
	RevokeAPIKey(ctx context.Context, arg storegen.RevokeAPIKeyParams) (storegen.ApiKey, error)
	UpsertOrganisation(ctx context.Context, name string) (storegen.Organisation, error)
}

// Server implements gen.StrictServerInterface.
type Server struct {
	router router.Router
	store  StoreReader
	logger *slog.Logger
}

// NewServer creates a new API server instance.
func NewServer(r router.Router, s StoreReader, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		router: r,
		store:  s,
		logger: logger,
	}
}
