package user

import (
	"context"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	usersv1 "github.com/bs-tools/mealplann.ing/internal/gen/users/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/users"
)

func (s *Service) GetCurrentUser(ctx context.Context, req *usersv1.GetCurrentUserRequest) (*usersv1.GetCurrentUserResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	u, err := userToProto(user)
	if err != nil {
		return nil, err
	}
	return usersv1.GetCurrentUserResponse_builder{User: u}.Build(), nil
}

// UpdateSettings changes the signed in user's settings.
func (s *Service) UpdateSettings(ctx context.Context, req *usersv1.UpdateSettingsRequest) (*usersv1.UpdateSettingsResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	system, ok := unitSystemsFromProto[req.GetUnitSystem()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument, "unit_system must be UNIT_SYSTEM_METRIC or UNIT_SYSTEM_IMPERIAL")
	}
	if err := s.settings.SetUnitSystem(ctx, user.ID(), system); err != nil {
		return nil, connect.NewError(connect.CodeInternal, "failed to update settings").WithCause(err)
	}
	// The user was read with the request, so it's changed to match.
	if u, ok := user.(*users.PasskeyUser); ok {
		changed := *u
		changed.UnitSystem = system
		user = &changed
	}
	u, err := userToProto(user)
	if err != nil {
		return nil, err
	}
	return usersv1.UpdateSettingsResponse_builder{User: u}.Build(), nil
}

// unitSystemsFromProto are the unit systems users can choose.
var unitSystemsFromProto = map[usersv1.UnitSystem]users.UnitSystem{
	usersv1.UnitSystem_UNIT_SYSTEM_METRIC:   users.Metric,
	usersv1.UnitSystem_UNIT_SYSTEM_IMPERIAL: users.Imperial,
}

func userToProto(user users.User) (*usersv1.User, error) {
	switch u := user.(type) {
	case *users.PasskeyUser:
		return usersv1.User_builder{
			Id:          store.UUIDToString(user.ID()),
			Name:        u.Name,
			AvatarUrl:   AvatarURL(u.AvatarKey),
			Admin:       u.IsAdmin(),
			HouseholdId: store.UUIDToString(u.HouseholdID()),
			UnitSystem:  unitSystemToProto(u.UnitSystem),
		}.Build(), nil
	default:
		return nil, connect.Errorf(connect.CodeInternal, "unknown user type: %T", u)
	}
}

// unitSystemToProto converts a unit system to the API, metric if unknown.
func unitSystemToProto(system users.UnitSystem) usersv1.UnitSystem {
	if system == users.Imperial {
		return usersv1.UnitSystem_UNIT_SYSTEM_IMPERIAL
	}
	return usersv1.UnitSystem_UNIT_SYSTEM_METRIC
}
