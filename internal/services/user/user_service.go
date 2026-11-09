package user

import (
	"context"
	"log/slog"

	"github.com/bs-tools/mealplann.ing/internal/blob"
	"github.com/bs-tools/mealplann.ing/internal/errreport"
	"github.com/bs-tools/mealplann.ing/internal/gen/users/v1/usersv1connect"
	"github.com/bs-tools/mealplann.ing/internal/users"
	"github.com/jackc/pgx/v5/pgtype"
)

var _ usersv1connect.UserServiceHandler = (*Service)(nil)

type APITokenRepository interface {
	CreateAPIToken(ctx context.Context, userID pgtype.UUID, name string) (*users.APIToken, string, error)
	ListAPITokens(ctx context.Context, userID pgtype.UUID) ([]*users.APIToken, error)
	DeleteAPIToken(ctx context.Context, userID pgtype.UUID, tokenID pgtype.UUID) error
}

type AvatarRepository interface {
	SetAvatarKey(ctx context.Context, userID pgtype.UUID, key string) (string, error)
}

// SettingsRepository stores users' settings.
type SettingsRepository interface {
	SetUnitSystem(ctx context.Context, userID pgtype.UUID, system users.UnitSystem) error
}

// HouseholdRepository stores households.
type HouseholdRepository interface {
	GetHousehold(ctx context.Context, id pgtype.UUID) (*users.Household, error)
	RenameHousehold(ctx context.Context, id pgtype.UUID, name string) (*users.Household, error)
}

// Service implements API handlers for the user service.
type Service struct {
	log        *slog.Logger
	report     *errreport.Reporter
	tokens     APITokenRepository
	avatars    AvatarRepository
	households HouseholdRepository
	settings   SettingsRepository
	blobs      blob.Store
}

// NewService creates a new Service from the provided logger, repositories
// and the blob store avatar images are stored in.
func NewService(log *slog.Logger, report *errreport.Reporter, tokens APITokenRepository, avatars AvatarRepository, households HouseholdRepository, settings SettingsRepository, blobs blob.Store) *Service {
	return &Service{
		log:        log,
		report:     report,
		tokens:     tokens,
		avatars:    avatars,
		households: households,
		settings:   settings,
		blobs:      blobs,
	}
}
