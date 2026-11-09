package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	usersv1 "github.com/bs-tools/mealplann.ing/internal/gen/users/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestMeasures checks that a user's unit system is their own setting, and
// that a food's measure and density reach its recipes' ingredients, for
// apps to show amounts as people read them.
func TestMeasures(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	current := func() usersv1.UnitSystem {
		t.Helper()
		resp, err := e.users.GetCurrentUser(ctx, usersv1.GetCurrentUserRequest_builder{}.Build())
		if err != nil {
			t.Fatalf("GetCurrentUser: %v", err)
		}
		return resp.GetUser().GetUnitSystem()
	}
	if got := current(); got != usersv1.UnitSystem_UNIT_SYSTEM_METRIC {
		t.Errorf("got %v, want metric by default", got)
	}
	updated, err := e.users.UpdateSettings(ctx, usersv1.UpdateSettingsRequest_builder{UnitSystem: usersv1.UnitSystem_UNIT_SYSTEM_IMPERIAL}.Build())
	if err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if got := updated.GetUser().GetUnitSystem(); got != usersv1.UnitSystem_UNIT_SYSTEM_IMPERIAL {
		t.Errorf("UpdateSettings returned %v, want imperial", got)
	}
	if got := current(); got != usersv1.UnitSystem_UNIT_SYSTEM_IMPERIAL {
		t.Errorf("got %v after the update, want imperial", got)
	}
	_, err = e.users.UpdateSettings(ctx, usersv1.UpdateSettingsRequest_builder{}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Oil is measured by volume, at its density.
	oil := customFood(t, e, "Olive oil", 884, 0, 0, 0, 100, func(f *foodsv1.Food) {
		f.SetGramsPerMilliliter(0.92)
		f.SetMeasure(foodsv1.Measure_MEASURE_VOLUME)
	})
	if oil.GetMeasure() != foodsv1.Measure_MEASURE_VOLUME {
		t.Errorf("got measure %v, want volume", oil.GetMeasure())
	}
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Dressing", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{ingredient(oil.GetId(), 30, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	in := created.GetRecipe().GetIngredients()[0]
	if in.GetFoodMeasure() != foodsv1.Measure_MEASURE_VOLUME || in.GetFoodGramsPerMilliliter() != 0.92 {
		t.Errorf("got ingredient measure %v at %v g/ml, want volume at 0.92", in.GetFoodMeasure(), in.GetFoodGramsPerMilliliter())
	}

	// Ginger has no measure nor density, so it's listed as needing one;
	// coconut milk in milliliters isn't.
	customFood(t, e, "Ginger", 80, 2, 18, 2, 1, nil)
	customFood(t, e, "Coconut milk", 190, 2, 3, 2, 20, func(f *foodsv1.Food) { f.SetMeasure(foodsv1.Measure_MEASURE_MILLILITERS) })
	needing, err := e.foods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{NeedsMeasure: true}.Build())
	if err != nil {
		t.Fatalf("ListFoods: %v", err)
	}
	var names []string
	for _, f := range needing.GetFoods() {
		names = append(names, f.GetName())
	}
	if !slices.Equal(names, []string{"Ginger"}) {
		t.Errorf("got %v needing a measure, want the ginger alone", names)
	}

	// Back to automatic.
	changed, err := e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: oil.GetId()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"measure"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateFood: %v", err)
	}
	if got := changed.GetFood(); got.GetMeasure() != foodsv1.Measure_MEASURE_UNSPECIFIED || got.GetGramsPerMilliliter() != 0.92 {
		t.Errorf("got measure %v at %v g/ml, want automatic, keeping the density", got.GetMeasure(), got.GetGramsPerMilliliter())
	}
}
