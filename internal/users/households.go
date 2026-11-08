package users

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/users/internal/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Household is a group of users who share everything the app stores about
// food.
type Household struct {
	ID         pgtype.UUID
	Name       string
	CreateTime time.Time
	// The users in the household, the first to join first.
	Members []*HouseholdMember
}

// HouseholdMember is a user in a household.
type HouseholdMember struct {
	UserID pgtype.UUID
	Name   string
	// The key of the user's avatar image in the blob store. Empty if the
	// user has no avatar.
	AvatarKey string
}

// GetHousehold gets a household with its members. It returns
// store.ErrNotFound if it doesn't exist.
func (r *Repository) GetHousehold(ctx context.Context, id pgtype.UUID) (*Household, error) {
	row, err := r.querier.GetHousehold(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get household: %w", err)
	}
	return r.withMembers(ctx, row)
}

// RenameHousehold renames a household, and returns it with its members. It
// returns store.ErrNotFound if it doesn't exist.
func (r *Repository) RenameHousehold(ctx context.Context, id pgtype.UUID, name string) (*Household, error) {
	row, err := r.querier.RenameHousehold(ctx, &gen.RenameHouseholdParams{ID: id, Name: name})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to rename household: %w", err)
	}
	return r.withMembers(ctx, row)
}

func (r *Repository) withMembers(ctx context.Context, row *gen.Household) (*Household, error) {
	rows, err := r.querier.ListHouseholdMembers(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to list household members: %w", err)
	}
	h := &Household{ID: row.ID, Name: row.Name, CreateTime: row.CreateTime.Time}
	for _, m := range rows {
		h.Members = append(h.Members, &HouseholdMember{UserID: m.ID, Name: m.Name, AvatarKey: m.AvatarKey.String})
	}
	return h, nil
}
