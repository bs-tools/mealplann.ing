package plan

import (
	"context"
	"math"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	pantrysvc "github.com/bs-tools/mealplann.ing/internal/services/pantry"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) ListFrozenLeftovers(ctx context.Context, _ *plansv1.ListFrozenLeftoversRequest) (*plansv1.ListFrozenLeftoversResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	frozen, err := s.plans.ListFrozenCooks(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list frozen leftovers", "")
	}
	today := pantrysvc.Today(s.nowFn())
	leftovers := make([]*plansv1.FrozenLeftover, 0, len(frozen))
	for _, f := range frozen {
		leftovers = append(leftovers, frozenLeftoverToProto(f, today))
	}
	return plansv1.ListFrozenLeftoversResponse_builder{Leftovers: leftovers}.Build(), nil
}

// frozenLeftoverToProto converts what's in the freezer of a cook, as of
// today.
func frozenLeftoverToProto(f *plans.FrozenCook, today time.Time) *plansv1.FrozenLeftover {
	b := plansv1.FrozenLeftover_builder{
		Cook:              cookToProto(f.Cook),
		RemainingServings: round(inFreezer(f.Cook)),
		PlannedServings:   round(f.LateServings),
	}
	if f.FreezerDays != nil {
		keepsUntil := f.Date.AddDate(0, 0, int(*f.FreezerDays))
		b.KeepsUntil = store.FormatDate(keepsUntil)
		b.Expired = keepsUntil.Before(today)
	}
	if f.FridgeDays != nil {
		b.FrozenFrom = store.FormatDate(f.Date.AddDate(0, 0, int(*f.FridgeDays)+1))
	}
	return b.Build()
}

// inFreezer returns how many of a cook's frozen servings are in the
// freezer: not planned after its fridge days, nor taken off plan.
func inFreezer(c *plans.Cook) float64 {
	return math.Max(c.FrozenServings-c.LateServings-c.OffPlanServings, 0)
}

func (s *Service) TakeFromFreezer(ctx context.Context, req *plansv1.TakeFromFreezerRequest) (*plansv1.TakeFromFreezerResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	id, err := parseID("cook_id", req.GetCookId(), "find frozen leftovers with ListFrozenLeftovers")
	if err != nil {
		return nil, err
	}
	servings := req.GetServings()
	if math.IsNaN(servings) || servings <= 0 || servings > maxCookServings {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "servings must be more than 0 and at most %d", maxCookServings)
	}
	forPerson := req.GetPersonId() != "" || req.GetEatingTimeId() != "" || req.GetDate() != ""
	var portion *plans.Portion
	if forPerson {
		p, err := portionFromProto(plansv1.Portion_builder{
			PersonId:     req.GetPersonId(),
			EatingTimeId: req.GetEatingTimeId(),
			Date:         req.GetDate(),
			Cook:         plansv1.CookShare_builder{CookId: req.GetCookId(), Servings: servings}.Build(),
		}.Build())
		if err != nil {
			return nil, err
		}
		portion = p
	}
	c, err := s.plans.GetCook(ctx, user.HouseholdID(), id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get cook", cookNotFound)
	}
	list, err := s.recipes.ListRecipesByID(ctx, user.HouseholdID(), []pgtype.UUID{c.RecipeID})
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get the cook's recipe", "")
	}
	f := &plans.FrozenCook{Cook: c}
	if len(list) > 0 {
		f.FridgeDays, f.FreezerDays = list[0].FridgeDays, list[0].FreezerDays
	}
	if left := inFreezer(c); servings > left+servingsSlack {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"the freezer has %v servings of %s cooked on %s, fewer than %v: take at most those, or freeze more with the cook's frozen_servings",
			round(left), c.RecipeName, store.FormatDate(c.Date), servings)
	}
	resp := plansv1.TakeFromFreezerResponse_builder{}
	if forPerson {
		// Frozen food can be eaten any day from when it was cooked, like
		// food frozen the day it was cooked, thawed a few days later.
		if portion.Date.Before(c.Date) {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s was cooked on %s: give a date from then", c.RecipeName, store.FormatDate(c.Date))
		}
		portion.HouseholdID = user.HouseholdID()
		portion.FromFreezer = true
		created, err := s.plans.CreatePortion(ctx, portion)
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to create portion", "")
		}
		resp.Portion = portionToProto(created)
	} else {
		c.OffPlanServings = math.Min(c.OffPlanServings+servings, c.Servings)
		if _, err := s.plans.UpdateCook(ctx, c); err != nil {
			return nil, s.toConnectError(ctx, err, "failed to update cook", cookNotFound)
		}
	}
	// The cook is read again, with its portions' servings.
	if f.Cook, err = s.plans.GetCook(ctx, user.HouseholdID(), id); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get cook", cookNotFound)
	}
	resp.Leftover = frozenLeftoverToProto(f, pantrysvc.Today(s.nowFn()))
	return resp.Build(), nil
}

// servingsSlack is the least difference in servings that counts, as
// portions are split in hundredths of a serving.
const servingsSlack = 0.005
