package router

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/abn/relay/internal/protocol"
	"github.com/abn/relay/internal/store/gen"
)

var (
	ErrOrgNotFound     = errors.New("organisation not found")
	ErrAppNotFound     = errors.New("application not found")
	ErrNoLiveExecutor  = errors.New("no live executor connected")
	ErrExecutorTimeout = errors.New("executor request timed out")
	ErrExecutorError   = errors.New("executor returned an error")
)

type AppResolver interface {
	GetOrganisationByName(ctx context.Context, name string) (gen.Organisation, error)
	GetApplicationByName(ctx context.Context, arg gen.GetApplicationByNameParams) (gen.Application, error)
}

type Dispatcher interface {
	Dispatch(ctx context.Context, appID pgtype.UUID, msg protocol.Message) (protocol.Message, error)
}

type Router interface {
	Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error)
}

type DefaultRouter struct {
	store AppResolver
	hub   Dispatcher
}

func New(store AppResolver, hub Dispatcher) *DefaultRouter {
	return &DefaultRouter{store: store, hub: hub}
}

func (r *DefaultRouter) Dispatch(ctx context.Context, orgName, appName string, msg protocol.Message) (protocol.Message, error) {
	if orgName == "" {
		orgName = "local"
	}

	org, err := r.store.GetOrganisationByName(ctx, orgName)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrOrgNotFound, orgName)
	}

	app, err := r.store.GetApplicationByName(ctx, gen.GetApplicationByNameParams{
		OrganisationID: org.ID,
		Name:           appName,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrAppNotFound, appName)
	}

	res, err := r.hub.Dispatch(ctx, app.ID, msg)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "no live executor") ||
			strings.Contains(errStr, "no executors registered") ||
			strings.Contains(errStr, "no executors available") {
			return nil, fmt.Errorf("%w: %w", ErrNoLiveExecutor, err)
		}
		if errors.Is(err, context.DeadlineExceeded) || strings.Contains(errStr, "timed out") {
			return nil, fmt.Errorf("%w: %w", ErrExecutorTimeout, err)
		}
		return nil, err
	}

	return res, nil
}
