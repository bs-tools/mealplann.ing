package services_test

import (
	"context"
	"testing"

	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestWeekCachedUntilChanged checks that a week read again is the same,
// and that each kind of change to what it's made of shows in the next
// read, though a week is kept in memory between reads: a portion's
// servings, a recipe's name, a meal eaten out, and a deleted cook. A
// summary of a week doesn't change the full week read after it.
func TestWeekCachedUntilChanged(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	b := createPerson(t, e, ben())
	beans := customFood(t, e, "Beans", 130, 9, 24, 0, 0.5, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Chili", Servings: 4,
		Ingredients: []*recipesv1.Ingredient{ingredient(beans.GetId(), 400, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	cook, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: created.GetRecipe().GetId(), Date: monday, Servings: 4}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	portion, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Dinner"), Date: tuesday,
		Cook: plansv1.CookShare_builder{CookId: cook.GetCook().GetId(), Servings: 1}.Build(),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	read := func(summary bool) *plansv1.GetWeekResponse {
		t.Helper()
		week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday, Summary: summary}.Build())
		if err != nil {
			t.Fatalf("GetWeek: %v", err)
		}
		return week
	}
	dinner := func(week *plansv1.GetWeekResponse) *plansv1.PlannedEatingTime {
		t.Helper()
		for _, et := range week.GetDays()[1].GetPeople()[0].GetEatingTimes() {
			if et.GetEatingTime().GetName() == "Dinner" {
				return et
			}
		}
		t.Fatal("no dinner on Tuesday")
		return nil
	}

	first := read(false)
	if again := read(false); !proto.Equal(first, again) {
		t.Fatal("got a different week reading it again without a change")
	}
	// A summary leaves out all but the main nutrients, like fat, but the
	// full week keeps them.
	if got := dinner(read(true)).GetNutrients().GetFatGrams(); got != 0 {
		t.Errorf("got a summary with %v g of fat at dinner, want it left out", got)
	}
	if got := dinner(read(false)).GetNutrients().GetFatGrams(); got == 0 {
		t.Error("got a full week without dinner's fat after a summary of it")
	}

	if _, err := e.plans.UpdatePortion(ctx, plansv1.UpdatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			Id:   portion.GetPortion().GetId(),
			Cook: plansv1.CookShare_builder{CookId: cook.GetCook().GetId(), Servings: 1.5}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"cook"}},
	}.Build()); err != nil {
		t.Fatalf("UpdatePortion: %v", err)
	}
	if got := dinner(read(false)).GetPortions()[0].GetServings(); got != 1.5 {
		t.Errorf("got %v servings after changing the portion, want 1.5", got)
	}

	if _, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: created.GetRecipe().GetId(), Name: "Bean chili"}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if got := read(false).GetCooks()[0].GetCook().GetRecipeName(); got != "Bean chili" {
		t.Errorf("got the cook's recipe named %q after renaming it, want Bean chili", got)
	}

	if _, err := e.plans.SetEatingOut(ctx, plansv1.SetEatingOutRequest_builder{
		PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Lunch"), Date: tuesday, EatingOut: true,
	}.Build()); err != nil {
		t.Fatalf("SetEatingOut: %v", err)
	}
	lunchOut := false
	for _, et := range read(false).GetDays()[1].GetPeople()[0].GetEatingTimes() {
		if et.GetEatingTime().GetName() == "Lunch" {
			lunchOut = et.GetEatingOut()
		}
	}
	if !lunchOut {
		t.Error("got Tuesday's lunch not eaten out after marking it")
	}

	if _, err := e.plans.DeleteCook(ctx, plansv1.DeleteCookRequest_builder{Id: cook.GetCook().GetId()}.Build()); err != nil {
		t.Fatalf("DeleteCook: %v", err)
	}
	if got := read(false).GetCooks(); len(got) != 0 {
		t.Errorf("got cooks %v after deleting the only one, want none", got)
	}
}
