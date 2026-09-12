package auth

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

type identityKey struct{}

// UserIdentity represents the resolved caller of an authenticated request.
type UserIdentity struct {
	Subject  string
	Username string
	Email    string
	IsAdmin  bool
	OrgName  string
	Role     string
	Token    string
	IsAPIKey bool
	OrgID pgtype.UUID
	ApplicationNames []string
	Permissions []string
}

// WithIdentity stores the UserIdentity in ctx.
func WithIdentity(ctx context.Context, id *UserIdentity) context.Context {
	return context.WithValue(ctx, identityKey{}, id)
}

// IdentityFromContext extracts the UserIdentity from ctx.
func IdentityFromContext(ctx context.Context) (*UserIdentity, bool) {
	id, ok := ctx.Value(identityKey{}).(*UserIdentity)
	return id, ok && id != nil
}
