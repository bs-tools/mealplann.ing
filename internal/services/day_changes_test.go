package services_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestSetPortionEaten checks that marking a portion of a food eaten takes
// it out of the pantry, and un-marking it puts it back.
func TestSetPortionEaten(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	sam := createPerson(t, e, peoplev1.Person_builder{Name: "Sam", EatingTimes: []*peoplev1.EatingTime{
		eatingTime("Snack", smallMeal, "15:30", noMeal),
	}}.Build())
	nuts := customFood(t, e, "Almonds", 600, 20, 20, 4, 50, nil)
	if _, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: nuts.GetId(), Amount: 200, Unit: grams}.Build()); err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	created, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId: sam.GetId(), EatingTimeId: eatingTimeID(t, sam, "Snack"), Date: monday,
		Food: plansv1.FoodAmount_builder{FoodId: nuts.GetId(), Amount: 30, Unit: grams}.Build(),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	id := created.GetPortion().GetId()
	stock := func() float64 {
		t.Helper()
		resp, err := e.pantry.ListPantry(ctx, pantryv1.ListPantryRequest_builder{FoodIds: []string{nuts.GetId()}}.Build())
		if err != nil {
			t.Fatalf("ListPantry: %v", err)
		}
		total := 0.0
		for _, it := range resp.GetItems() {
			total += it.GetGrams()
		}
		return total
	}

	for range 2 {
		resp, err := e.plans.SetPortionEaten(ctx, plansv1.SetPortionEatenRequest_builder{Id: id, Eaten: true}.Build())
		if err != nil {
			t.Fatalf("SetPortionEaten: %v", err)
		}
		if !resp.GetPortion().GetEaten() {
			t.Errorf("got the portion not eaten, want it eaten")
		}
	}
	if got := stock(); got != 170 {
		t.Errorf("got %v g of almonds, want 170 g, taken out once", got)
	}
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if p := week.GetDays()[0].GetPeople()[0].GetEatingTimes()[0].GetPortions()[0]; !p.GetEaten() {
		t.Errorf("got %v in the week, want it eaten", p)
	}
	// Updating it keeps it eaten.
	updated, err := e.plans.UpdatePortion(ctx, plansv1.UpdatePortionRequest_builder{
		Portion:    plansv1.Portion_builder{Id: id, Notes: "a handful"}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"notes"}},
	}.Build())
	if err != nil || !updated.GetPortion().GetEaten() {
		t.Errorf("got %v, %v updating it, want it still eaten", updated.GetPortion(), err)
	}

	resp, err := e.plans.SetPortionEaten(ctx, plansv1.SetPortionEatenRequest_builder{Id: id}.Build())
	if err != nil {
		t.Fatalf("SetPortionEaten: %v", err)
	}
	if !resp.GetPantryRestored() || resp.GetPortion().GetEaten() {
		t.Errorf("got %v, want it un-marked, putting the almonds back", resp)
	}
	if got := stock(); got != 200 {
		t.Errorf("got %v g of almonds, want 200 g back", got)
	}
}

// TestMoveCook checks that moving a cook moves its meal with it, and
// removes its portions eaten before it's now cooked.
func TestMoveCook(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	sam := createPerson(t, e, peoplev1.Person_builder{Name: "Sam", EatingTimes: []*peoplev1.EatingTime{
		eatingTime("Lunch", mainMeal, "12:30", noMeal),
		eatingTime("Dinner", mainMeal, "19:00", dinner),
	}}.Build())
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, nil)
	recipe, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu bowls", Servings: 4, FridgeDays: new(int32(4)),
		Ingredients: []*recipesv1.Ingredient{ingredient(tofu.GetId(), 800, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	planned, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{
		RecipeId: recipe.GetRecipe().GetId(), Date: monday, Meal: dinner, Servings: 4,
	}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	cookID := planned.GetCook().GetId()
	lunchID := eatingTimeID(t, sam, "Lunch")
	leftover := func(date string) {
		t.Helper()
		if _, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
			plansv1.MealToPlan_builder{
				RecipeId: recipe.GetRecipe().GetId(), Date: date, PersonId: sam.GetId(), EatingTimeId: lunchID,
				LeftoverOf: plansv1.LeftoverOf_builder{Date: monday, Meal: dinner}.Build(),
			}.Build(),
		}}.Build()); err != nil {
			t.Fatalf("PlanMeals: %v", err)
		}
	}
	leftover(tuesday)
	leftover(wednesday)

	move := func(preview bool) *plansv1.MoveCookResponse {
		t.Helper()
		resp, err := e.plans.MoveCook(ctx, plansv1.MoveCookRequest_builder{Id: cookID, Date: tuesday, Meal: dinner, Preview: preview}.Build())
		if err != nil {
			t.Fatalf("MoveCook: %v", err)
		}
		return resp
	}
	// Tuesday's lunch is before Tuesday's dinner: it's removed. Monday's
	// dinner moves to Tuesday's, and Wednesday's lunch stays.
	resp := move(true)
	if len(resp.GetMovedPortions()) != 1 || resp.GetMovedPortions()[0].GetDate() != tuesday ||
		resp.GetMovedPortions()[0].GetEatingTimeId() != eatingTimeID(t, sam, "Dinner") {
		t.Errorf("got moved %v, want Monday's dinner moved to Tuesday's", resp.GetMovedPortions())
	}
	if len(resp.GetRemovedPortions()) != 1 || resp.GetRemovedPortions()[0].GetDate() != tuesday {
		t.Errorf("got removed %v, want Tuesday's lunch", resp.GetRemovedPortions())
	}
	if !resp.GetPreviewed() {
		t.Errorf("got a preview not marked previewed")
	}
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if got := week.GetCooks()[0].GetCook().GetDate(); got != monday {
		t.Errorf("got the cook on %s after a preview, want it unmoved", got)
	}

	resp = move(false)
	if resp.GetCook().GetDate() != tuesday || resp.GetPreviewed() {
		t.Errorf("got %v, want the cook on Tuesday", resp.GetCook())
	}
	week, err = e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	var where []string
	for _, day := range week.GetDays() {
		for _, et := range day.GetPeople()[0].GetEatingTimes() {
			for range et.GetPortions() {
				where = append(where, day.GetDate()+" "+et.GetEatingTime().GetName())
			}
		}
	}
	want := []string{tuesday + " Dinner", wednesday + " Lunch"}
	if len(where) != len(want) || where[0] != want[0] || where[1] != want[1] {
		t.Errorf("got portions at %q, want %q", where, want)
	}

	// A cook marked done can't move.
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook: plansv1.Cook_builder{Id: cookID, Done: true}.Build(), UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	_, err = e.plans.MoveCook(ctx, plansv1.MoveCookRequest_builder{Id: cookID, Date: wednesday, Meal: dinner}.Build())
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("got %v moving a cook done, want FAILED_PRECONDITION", err)
	}
}
