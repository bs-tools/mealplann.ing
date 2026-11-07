package users

import (
	"log/slog"

	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/bs-tools/foodplann.ing/internal/users/internal/gen"
	"github.com/jackc/pgx/v5/pgtype"
)

type User interface {
	ID() pgtype.UUID
	// HouseholdID returns the ID of the user's household, which owns the
	// data they share with the other users in it.
	HouseholdID() pgtype.UUID
	// IsAdmin returns whether the user is an administrator, who can invite
	// new users into their household.
	IsAdmin() bool
}

// Repository allows storing and querying of users and related data.
type Repository struct {
	log     *slog.Logger
	querier *gen.Queries
	db      *store.DB
}

// NewRepository creates a new Repository from the provided logger and database.
func NewRepository(log *slog.Logger, db *store.DB) (*Repository, error) {
	return &Repository{
		log:     log,
		querier: gen.New(db),
		db:      db,
	}, nil
}
