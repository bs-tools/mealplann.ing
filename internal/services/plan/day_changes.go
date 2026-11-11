package plan

import (
	"context"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/services/person"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Service) SetPortionEaten(ctx context.Context, req *plansv1.SetPortionEatenRequest) (*plansv1.SetPortionEatenResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	householdID := user.HouseholdID()
	id, err := parseID("id", req.GetId(), "find portions with GetWeek")
	if err != nil {
		return nil, err
	}
	p, err := s.plans.GetPortion(ctx, householdID, id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get portion", portionNotFound)
	}
	resp := plansv1.SetPortionEatenResponse_builder{}
	// Only a portion of a food takes it out of the pantry: a share of a
	// cook took its ingredients when the cook was marked done.
	if p.FoodID.Valid {
		if req.GetEaten() {
			list, err := s.foods.ListFoodsByID(ctx, householdID, []pgtype.UUID{p.FoodID})
			if err != nil {
				return nil, s.toConnectError(ctx, err, "failed to read the portion's food", "")
			}
			if len(list) > 0 {
				f := list[0]
				if g, ok := f.Nutrition().Grams(p.Amount, p.Unit); ok {
					deductions, err := s.pantry.UseForPortion(ctx, householdID, id, []pantry.Use{{FoodID: f.ID, Grams: g}})
					if err != nil {
						return nil, s.toConnectError(ctx, err, "failed to take the portion's food out of the pantry", portionNotFound)
					}
					resp.PantryChanges = deductionChanges(deductions, map[pgtype.UUID]string{f.ID: f.Name})
				}
			}
		} else {
			restored, err := s.pantry.RestoreForPortion(ctx, householdID, id)
			if err != nil {
				return nil, s.toConnectError(ctx, err, "failed to put the portion's food back in the pantry", "")
			}
			if resp.PantryChanges, err = s.restoredChanges(ctx, householdID, restored); err != nil {
				return nil, s.toConnectError(ctx, err, "failed to read the portion's food", "")
			}
			resp.PantryRestored = len(restored) > 0
		}
	}
	if err := s.plans.SetPortionEaten(ctx, householdID, id, req.GetEaten()); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to mark portion eaten", portionNotFound)
	}
	p.Eaten = req.GetEaten()
	resp.Portion = portionToProto(p)
	return resp.Build(), nil
}

func (s *Service) MoveCook(ctx context.Context, req *plansv1.MoveCookRequest) (*plansv1.MoveCookResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	householdID := user.HouseholdID()
	id, err := parseID("id", req.GetId(), "find cooks with GetWeek")
	if err != nil {
		return nil, err
	}
	date, err := parseDate("date", req.GetDate())
	if err != nil {
		return nil, err
	}
	meal, ok := person.MealsFromProto[req.GetMeal()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"meal must be HOUSEHOLD_MEAL_BREAKFAST, HOUSEHOLD_MEAL_LUNCH, HOUSEHOLD_MEAL_DINNER, or unspecified for none")
	}
	c, err := s.plans.GetCook(ctx, householdID, id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get cook", cookNotFound)
	}
	if c.Done {
		return nil, connect.Errorf(connect.CodeFailedPrecondition,
			"%s is marked cooked on %s, so it can't move: un-mark it with UpdateCook first", c.RecipeName, store.FormatDate(c.Date))
	}
	portions, err := s.plans.ListCookPortions(ctx, householdID, id)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read the cook's portions", "")
	}
	household, err := s.people.ListPeople(ctx, householdID)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read people", "")
	}
	byID := map[pgtype.UUID]*people.Person{}
	for _, p := range household {
		byID[p.ID] = p
	}

	resp := plansv1.MoveCookResponse_builder{Previewed: req.GetPreview()}
	var moves []plans.PortionMove
	var deletes []pgtype.UUID
	for _, p := range portions {
		who := byID[p.PersonID]
		var et *people.EatingTime
		if who != nil {
			et = who.EatingTime(p.EatingTimeID)
		}
		// The cook's own meal moves with it, to each person's eating time
		// of the new one.
		if c.Meal != people.MealNone && et != nil && p.Date.Equal(c.Date) && et.Meal == c.Meal {
			if to := eatingTimeOfMeal(who, meal); to != nil {
				moves = append(moves, plans.PortionMove{ID: p.ID, EatingTimeID: to.ID, Date: date})
				moved := *p
				moved.Date, moved.EatingTimeID = date, to.ID
				resp.MovedPortions = append(resp.MovedPortions, portionToProto(&moved))
				continue
			}
			deletes = append(deletes, p.ID)
			resp.RemovedPortions = append(resp.RemovedPortions, portionToProto(p))
			continue
		}
		if eatenBefore(p.Date, et, who, date, meal) {
			deletes = append(deletes, p.ID)
			resp.RemovedPortions = append(resp.RemovedPortions, portionToProto(p))
		}
	}
	moved := *c
	moved.Date, moved.Meal = date, meal
	if req.GetPreview() {
		resp.Cook = cookToProto(&moved)
		return resp.Build(), nil
	}
	updated, err := s.plans.MoveCook(ctx, &moved, moves, deletes)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to move cook", cookNotFound)
	}
	resp.Cook = cookToProto(updated)
	return resp.Build(), nil
}

// eatingTimeOfMeal returns a person's eating time of a shared meal, or nil.
func eatingTimeOfMeal(p *people.Person, meal people.Meal) *people.EatingTime {
	if p == nil || meal == people.MealNone {
		return nil
	}
	for _, et := range p.EatingTimes {
		if et.Meal == meal {
			return et
		}
	}
	return nil
}

// eatenBefore reports whether a portion, on a date at an eating time of its
// person, is eaten before a cook on a date for a meal: on an earlier date,
// or on the same date at an earlier time, by the clock when both eating
// times have one, else by the order of their meals.
func eatenBefore(date time.Time, et *people.EatingTime, who *people.Person, cookDate time.Time, meal people.Meal) bool {
	if date.Before(cookDate) {
		return true
	}
	if !date.Equal(cookDate) || meal == people.MealNone || et == nil {
		return false
	}
	if at := eatingTimeOfMeal(who, meal); at != nil && et.TimeOfDay != "" && at.TimeOfDay != "" {
		return et.TimeOfDay < at.TimeOfDay
	}
	return et.Meal != people.MealNone && compareMeals(et.Meal, meal) < 0
}
