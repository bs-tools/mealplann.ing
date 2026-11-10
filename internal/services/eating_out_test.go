package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestEatingOut checks that a meal eaten out isn't planned: PlanMeal and
// SuggestPlan leave its people out, its staples are left out of the week,
// and it can't be marked over something planned already.
func TestEatingOut(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	b := createPerson(t, e, ben())
	cleo := createPerson(t, e, peoplev1.Person_builder{
		Name: "Cleo",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Lunch", mainMeal, "12:30", lunch),
			eatingTime("Dinner", mainMeal, "19:00", dinner),
		},
	}.Build())
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Oat bowl", Servings: 2, Tags: []string{"lunch"},
		Ingredients: []*recipesv1.Ingredient{ingredient(oats.GetId(), 200, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	bowl := created.GetRecipe()
	// Ben has an apple at lunch every day.
	apple := customFood(t, e, "Apple", 52, 0.3, 14, 0.2, 2.4, nil)
	if _, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{Staple: plansv1.Staple_builder{
		PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Lunch"),
		Food: plansv1.FoodAmount_builder{FoodId: apple.GetId(), Amount: 150, Unit: grams}.Build(),
	}.Build()}.Build()); err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}
	setOut := func(req *plansv1.SetEatingOutRequest) error {
		t.Helper()
		_, err := e.plans.SetEatingOut(ctx, req)
		return err
	}
	lunchOf := func(personID string) *plansv1.PlannedEatingTime {
		t.Helper()
		week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
		if err != nil {
			t.Fatalf("GetWeek: %v", err)
		}
		for _, pd := range week.GetDays()[0].GetPeople() {
			if pd.GetPersonId() != personID {
				continue
			}
			for _, et := range pd.GetEatingTimes() {
				if et.GetEatingTime().GetName() == "Lunch" {
					return et
				}
			}
		}
		t.Fatalf("no lunch for %s on Monday", personID)
		return nil
	}

	// Ben eats out at Monday's lunch: his apple is left out, and the lunch
	// is planned for Cleo alone.
	if err := setOut(plansv1.SetEatingOutRequest_builder{
		Date: monday, Meal: lunch, PersonIds: []string{b.GetId()}, EatingOut: true, Notes: "Work lunch",
	}.Build()); err != nil {
		t.Fatalf("SetEatingOut: %v", err)
	}
	if got := lunchOf(b.GetId()); !got.GetEatingOut() || got.GetEatingOutNotes() != "Work lunch" || len(got.GetPortions()) != 0 {
		t.Errorf("got Ben's lunch eating out %v, notes %q and %d portions, want eaten out at work, without his apple",
			got.GetEatingOut(), got.GetEatingOutNotes(), len(got.GetPortions()))
	}
	planned, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: bowl.GetId(), Date: monday, Meal: lunch}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if got := planned.GetPortions(); len(got) != 1 || got[0].GetPersonId() != cleo.GetId() {
		t.Errorf("got %v, want Cleo's portion only", got)
	}
	// Cleo can't eat out where her lunch is planned.
	err = setOut(plansv1.SetEatingOutRequest_builder{Date: monday, Meal: lunch, EatingOut: true}.Build())
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("got %v marking a planned lunch eaten out, want FAILED_PRECONDITION", err)
	}

	// Everyone eats out at Tuesday's dinner: it isn't planned, nor
	// suggested.
	if err := setOut(plansv1.SetEatingOutRequest_builder{Date: tuesday, Meal: dinner, EatingOut: true}.Build()); err != nil {
		t.Fatalf("SetEatingOut: %v", err)
	}
	_, err = e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: bowl.GetId(), Date: tuesday, Meal: dinner}.Build())
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("got %v planning a dinner everyone eats out at, want FAILED_PRECONDITION", err)
	}
	resp, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: tuesday, Days: 2, Meals: []peoplev1.HouseholdMeal{dinner},
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	var states []suggestionsv1.PlanSlotState
	for _, s := range resp.GetSlots() {
		states = append(states, s.GetState())
	}
	if want := []suggestionsv1.PlanSlotState{
		suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_EATING_OUT,
		suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SUGGESTED,
	}; !slices.Equal(states, want) {
		t.Errorf("got %v, want Tuesday's dinner eaten out and Wednesday's suggested", states)
	}

	// Ben eats at home again: his apple is back.
	if err := setOut(plansv1.SetEatingOutRequest_builder{
		Date: monday, PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Lunch"),
	}.Build()); err != nil {
		t.Fatalf("SetEatingOut: %v", err)
	}
	if got := lunchOf(b.GetId()); got.GetEatingOut() || len(got.GetPortions()) != 1 {
		t.Errorf("got Ben's lunch eating out %v with %d portions, want at home with his apple", got.GetEatingOut(), len(got.GetPortions()))
	}

	// Bad requests fail.
	for name, req := range map[string]*plansv1.SetEatingOutRequest{
		"no meal or eating time": plansv1.SetEatingOutRequest_builder{Date: monday, EatingOut: true}.Build(),
		"meal and eating time": plansv1.SetEatingOutRequest_builder{
			Date: monday, Meal: lunch, PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Lunch"), EatingOut: true,
		}.Build(),
		"another's eating time": plansv1.SetEatingOutRequest_builder{
			Date: monday, PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, cleo, "Lunch"), EatingOut: true,
		}.Build(),
		"bad date": plansv1.SetEatingOutRequest_builder{Date: "Monday", Meal: lunch, EatingOut: true}.Build(),
	} {
		if err := setOut(req); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want INVALID_ARGUMENT", name, err)
		}
	}
}
