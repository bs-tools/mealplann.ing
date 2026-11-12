package users

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/users/internal/gen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// calendarFeedPrefix identifies calendar feed secrets, so they can be
// recognised if leaked, and told from API tokens.
const calendarFeedPrefix = "cal_"

// CalendarFeed is a user's calendar feed, without its secret, which only
// creating it returns.
type CalendarFeed struct {
	*gen.CalendarFeed
}

// CreateCalendarFeed creates the user's calendar feed, replacing any old
// one, and returns it with its secret. Only a hash of the secret is
// stored.
func (r *Repository) CreateCalendarFeed(ctx context.Context, userID pgtype.UUID) (*CalendarFeed, string, error) {
	secret := calendarFeedPrefix + rand.Text()
	hash := sha256.Sum256([]byte(secret))
	feed, err := r.querier.UpsertCalendarFeed(ctx, &gen.UpsertCalendarFeedParams{
		UserID:       userID,
		SecretSHA256: hash[:],
	})
	if err != nil {
		return nil, "", fmt.Errorf("failed to create calendar feed: %w", err)
	}
	return &CalendarFeed{CalendarFeed: feed}, secret, nil
}

// GetCalendarFeed returns the user's calendar feed, or store.ErrNotFound
// if they have none.
func (r *Repository) GetCalendarFeed(ctx context.Context, userID pgtype.UUID) (*CalendarFeed, error) {
	feed, err := r.querier.GetCalendarFeed(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get calendar feed: %w", err)
	}
	return &CalendarFeed{CalendarFeed: feed}, nil
}

// DeleteCalendarFeed deletes the user's calendar feed, so its URL stops
// working. Deleting none is no error.
func (r *Repository) DeleteCalendarFeed(ctx context.Context, userID pgtype.UUID) error {
	if _, err := r.querier.DeleteCalendarFeed(ctx, userID); err != nil {
		return fmt.Errorf("failed to delete calendar feed: %w", err)
	}
	return nil
}

// GetCalendarFeedUser returns the user of the calendar feed with the
// secret, recording that it was read. It returns store.ErrNotFound if the
// secret matches no feed.
func (r *Repository) GetCalendarFeedUser(ctx context.Context, secret string) (User, error) {
	if !strings.HasPrefix(secret, calendarFeedPrefix) {
		return nil, store.ErrNotFound
	}
	hash := sha256.Sum256([]byte(secret))
	userID, err := r.querier.UseCalendarFeed(ctx, hash[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to read calendar feed: %w", err)
	}
	return r.loadUser(ctx, r.querier, userID)
}
