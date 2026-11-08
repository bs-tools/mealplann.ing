package user

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	usersv1 "github.com/bs-tools/mealplann.ing/internal/gen/users/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/users"
)

// maxHouseholdNameLength is the most characters in a household's name.
const maxHouseholdNameLength = 100

func (s *Service) GetHousehold(ctx context.Context, _ *usersv1.GetHouseholdRequest) (*usersv1.GetHouseholdResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	household, err := s.households.GetHousehold(ctx, user.HouseholdID())
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, "your household doesn't exist")
		}
		return nil, connect.NewError(connect.CodeInternal, "failed to get household").WithCause(err)
	}
	return usersv1.GetHouseholdResponse_builder{Household: householdToProto(household)}.Build(), nil
}

func (s *Service) UpdateHousehold(ctx context.Context, req *usersv1.UpdateHouseholdRequest) (*usersv1.UpdateHouseholdResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	name := strings.TrimSpace(req.GetName())
	switch {
	case name == "":
		return nil, connect.NewError(connect.CodeInvalidArgument, "give the household a name")
	case utf8.RuneCountInString(name) > maxHouseholdNameLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "the household's name must be at most %d characters", maxHouseholdNameLength)
	}
	household, err := s.households.RenameHousehold(ctx, user.HouseholdID(), name)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, "your household doesn't exist")
		}
		return nil, connect.NewError(connect.CodeInternal, "failed to rename household").WithCause(err)
	}
	return usersv1.UpdateHouseholdResponse_builder{Household: householdToProto(household)}.Build(), nil
}

func householdToProto(h *users.Household) *usersv1.Household {
	members := make([]*usersv1.HouseholdMember, 0, len(h.Members))
	for _, m := range h.Members {
		members = append(members, usersv1.HouseholdMember_builder{
			UserId:    store.UUIDToString(m.UserID),
			Name:      m.Name,
			AvatarUrl: AvatarURL(m.AvatarKey),
		}.Build())
	}
	return usersv1.Household_builder{
		Id:      store.UUIDToString(h.ID),
		Name:    h.Name,
		Members: members,
	}.Build()
}
