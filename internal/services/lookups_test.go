package services_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/proto"
)

// TestLookups checks the ways agents find things without reading them
// all: recipes by food, recipes and foods by name, and filtered pantries.
func TestLookups(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	beans := customFood(t, e, "Black beans, canned", 130, 9, 24, 0, 0.5, nil)
	rice := customFood(t, e, "Brown rice", 360, 8, 76, 1, 3, nil)
	tomatoes := customFood(t, e, "Tomatoes", 20, 1, 4, 3, 0.2, func(f *foodsv1.Food) {
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_PRODUCE)
	})
	create := func(name string, foods ...*foodsv1.Food) *recipesv1.Recipe {
		t.Helper()
		var ingredients []*recipesv1.Ingredient
		for _, f := range foods {
			ingredients = append(ingredients, ingredient(f.GetId(), 100, grams))
		}
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 2, Ingredients: ingredients, Instructions: []*recipesv1.Step{recipesv1.Step_builder{Text: "Cook it."}.Build()},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	create("Rice and beans", beans, rice)
	create("Bean chili", beans, tomatoes)
	create("Tomato rice", rice, tomatoes)

	// Recipes using any of the foods.
	listByFoods := func(foods ...*foodsv1.Food) []string {
		t.Helper()
		var ids []string
		for _, f := range foods {
			ids = append(ids, f.GetId())
		}
		resp, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{FoodIds: ids}.Build())
		if err != nil {
			t.Fatalf("ListRecipes: %v", err)
		}
		return recipeNames(resp.GetRecipes())
	}
	if got, want := listByFoods(beans), []string{"Bean chili", "Rice and beans"}; !slices.Equal(got, want) {
		t.Errorf("got %v using the beans, want %v", got, want)
	}
	if got, want := listByFoods(beans, rice), []string{"Bean chili", "Rice and beans", "Tomato rice"}; !slices.Equal(got, want) {
		t.Errorf("got %v using the beans or rice, want %v", got, want)
	}
	_, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{FoodIds: []string{"nope"}}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Recipes and foods by name, ignoring case, with similar names when
	// there's no such name.
	got, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Name: "bean CHILI"}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	if got.GetRecipe().GetName() != "Bean chili" {
		t.Errorf("got %v, want the bean chili", got.GetRecipe())
	}
	_, err = e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Name: "Bean chilli"}.Build())
	wantCode(t, err, connect.CodeNotFound)
	if err == nil || !strings.Contains(err.Error(), `"Bean chili"`) {
		t.Errorf("got %v, want the similar name", err)
	}
	_, err = e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Name: "Lasagne"}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherRecipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Name: "Bean chili"}.Build())
	wantCode(t, err, connect.CodeNotFound)
	food, err := e.foods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Name: "brown rice"}.Build())
	if err != nil {
		t.Fatalf("GetFood: %v", err)
	}
	if food.GetFood().GetId() != rice.GetId() {
		t.Errorf("got %v, want the rice", food.GetFood())
	}
	_, err = e.foods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Name: "Black beans"}.Build())
	wantCode(t, err, connect.CodeNotFound)
	if err == nil || !strings.Contains(err.Error(), `"Black beans, canned"`) {
		t.Errorf("got %v, want the similar name", err)
	}

	// A summary of a recipe leaves out its steps and most nutrients.
	full, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Name: "Bean chili"}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	short, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Name: "Bean chili", Summary: true}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	n := short.GetRecipe().GetNutrition()
	if len(short.GetRecipe().GetSteps()) != 0 || n.HasPerHundredGrams() || n.GetPerServing().HasFatGrams() ||
		!near(n.GetPerServing().GetEnergyKcal(), full.GetRecipe().GetNutrition().GetPerServing().GetEnergyKcal()) ||
		len(short.GetRecipe().GetIngredients()) != 2 {
		t.Errorf("got %v, want the ingredients and main nutrients only", short.GetRecipe())
	}
	if proto.Size(short) >= proto.Size(full) {
		t.Errorf("got a summary of %d bytes, want smaller than the full %d", proto.Size(short), proto.Size(full))
	}

	// The pantry by food, store section and use-by date.
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: beans.GetId(), Amount: 400, Unit: grams, ExpiryDate: "2099-01-01"}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: rice.GetId(), Amount: 1000, Unit: grams}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: tomatoes.GetId(), Amount: 500, Unit: grams, ExpiryDate: "2000-01-01"}.Build())
	pantry := func(req *pantryv1.ListPantryRequest) []string {
		t.Helper()
		resp, err := e.pantry.ListPantry(ctx, req)
		if err != nil {
			t.Fatalf("ListPantry: %v", err)
		}
		var names []string
		for _, it := range resp.GetItems() {
			names = append(names, it.GetFoodName())
			if req.GetSummary() && it.HasCreateTime() {
				t.Errorf("got %v, want no times in a summary", it)
			}
		}
		return names
	}
	for name, tt := range map[string]struct {
		req  *pantryv1.ListPantryRequest
		want []string
	}{
		"all":     {pantryv1.ListPantryRequest_builder{Summary: true}.Build(), []string{"Black beans, canned", "Brown rice", "Tomatoes"}},
		"foods":   {pantryv1.ListPantryRequest_builder{FoodIds: []string{rice.GetId(), beans.GetId()}}.Build(), []string{"Black beans, canned", "Brown rice"}},
		"section": {pantryv1.ListPantryRequest_builder{StoreSection: foodsv1.StoreSection_STORE_SECTION_PRODUCE}.Build(), []string{"Tomatoes"}},
		// The rice has no use-by date, the beans' is far off, and the
		// tomatoes' is past.
		"expiring": {pantryv1.ListPantryRequest_builder{ExpiringWithinDays: proto.Int32(7)}.Build(), []string{"Tomatoes"}},
	} {
		if got := pantry(tt.req); !slices.Equal(got, tt.want) {
			t.Errorf("%s: got %v, want %v", name, got, tt.want)
		}
	}
	_, err = e.pantry.ListPantry(ctx, pantryv1.ListPantryRequest_builder{ExpiringWithinDays: proto.Int32(-1)}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
}

func TestGetWeekSummary(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	tomatoes := customFood(t, e, "Tomatoes", 20, 1, 4, 3, 0.2, nil)
	resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tomato soup", Servings: 4, Ingredients: []*recipesv1.Ingredient{ingredient(tomatoes.GetId(), 800, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: resp.GetRecipe().GetId(), Date: friday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	full, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	short, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday, Summary: true}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	// The summary has the same plan, with only the main nutrients.
	if len(short.GetDays()) != 7 || len(short.GetCooks()) != 1 {
		t.Fatalf("got %v, want the week", short)
	}
	fullDay, shortDay := full.GetDays()[4].GetPeople()[0], short.GetDays()[4].GetPeople()[0]
	if !shortDay.GetNutrients().HasEnergyKcal() || shortDay.GetNutrients().GetEnergyKcal() != fullDay.GetNutrients().GetEnergyKcal() ||
		shortDay.GetNutrients().HasFatGrams() || !fullDay.GetNutrients().HasFatGrams() || len(shortDay.GetIncompleteNutrients()) != 0 {
		t.Errorf("got day %v, want the main nutrients of %v", shortDay, fullDay)
	}
	if len(shortDay.GetEatingTimes()) != len(fullDay.GetEatingTimes()) || len(shortDay.GetWarnings()) != len(fullDay.GetWarnings()) {
		t.Errorf("got day %v, want the eating times and warnings of %v", shortDay, fullDay)
	}
	if proto.Size(short) >= proto.Size(full) {
		t.Errorf("got a summary of %d bytes, want smaller than the full %d", proto.Size(short), proto.Size(full))
	}
}
