package users

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/users/internal/gen"
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

// UnitSystem is how a user reads amounts, which are stored in metric
// either way.
type UnitSystem string

// The unit systems.
const (
	// Metric is grams, milliliters, and teaspoons and tablespoons for
	// small volumes.
	Metric UnitSystem = "metric"
	// Imperial is US customary units: ounces, pounds, cups, teaspoons and
	// tablespoons.
	Imperial UnitSystem = "imperial"
)

// SetUnitSystem sets how a user reads amounts.
func (r *Repository) SetUnitSystem(ctx context.Context, userID pgtype.UUID, system UnitSystem) error {
	if err := r.querier.SetUnitSystem(ctx, &gen.SetUnitSystemParams{ID: userID, UnitSystem: string(system)}); err != nil {
		return fmt.Errorf("failed to set unit system: %w", err)
	}
	return nil
}
