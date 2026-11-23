package services_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
)

// TestBowls checks the bowl builder: that foods and recipes are sorted
// into a bowl's parts, a recipe's sauce among them; that a bowl is
// suggested from what's on hand, its parts fitting its sauce's cuisine;
// that a bowl needs each of its parts; and that its recipe can be created
// and planned like any other.
func TestBowls(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	section := func(s foodsv1.StoreSection) func(*foodsv1.Food) {
		return func(f *foodsv1.Food) { f.SetStoreSection(s) }
	}
	produce, dry := section(foodsv1.StoreSection_STORE_SECTION_PRODUCE), section(foodsv1.StoreSection_STORE_SECTION_DRY_GOODS)
	rice := customFood(t, e, "Jasmine rice, white", 360, 7, 80, 0, 1, dry)
	tortillas := customFood(t, e, "Corn tortillas", 220, 6, 45, 1, 3, section(foodsv1.StoreSection_STORE_SECTION_BAKERY))
	tofu := customFood(t, e, "Tofu, extra firm", 145, 16, 3, 1, 8, section(foodsv1.StoreSection_STORE_SECTION_CHILLED))
	broccoli := customFood(t, e, "Broccoli", 34, 3, 7, 2, 0.4, produce)
	cucumber := customFood(t, e, "Cucumber", 15, 0.7, 4, 2, 0.1, produce)
	garlic := customFood(t, e, "Garlic", 149, 6, 33, 1, 0.5, produce)
	peanutButter := customFood(t, e, "Peanut butter", 590, 25, 20, 9, 50, dry)
	soy := customFood(t, e, "Soy sauce (tamari)", 60, 10, 6, 1, 0, func(f *foodsv1.Food) { f.SetAlwaysInStock(true) })
	lime := customFood(t, e, "Limes (juice)", 25, 0.4, 8, 2, 0.1, produce)

	// A noodle bowl with a peanut sauce, of 2 servings, and nothing else of
	// it on hand.
	noodles := customFood(t, e, "Rice noodles", 360, 6, 80, 0, 1, dry)
	_, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Peanut noodle bowls", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{
			ingredient(noodles.GetId(), 150, grams),
			recipesv1.Ingredient_builder{FoodId: peanutButter.GetId(), Amount: 40, Unit: grams, Note: "for the sauce"}.Build(),
			recipesv1.Ingredient_builder{FoodId: soy.GetId(), Amount: 20, Unit: grams, Note: "for the sauce"}.Build(),
			recipesv1.Ingredient_builder{FoodId: lime.GetId(), Amount: 15, Unit: grams, Note: "juiced; for the sauce"}.Build(),
		},
		Instructions: []*recipesv1.Step{
			recipesv1.Step_builder{Text: "Whisk the peanut butter, tamari and lime juice with a splash of water.", Section: "Sauce", IngredientIndexes: []int32{1, 2, 3}}.Build(),
			recipesv1.Step_builder{Text: "Cook the noodles.", IngredientIndexes: []int32{0}}.Build(),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	for _, lot := range []struct {
		id    string
		grams float64
	}{{rice.GetId(), 1000}, {tortillas.GetId(), 500}, {tofu.GetId(), 400}, {broccoli.GetId(), 300}, {cucumber.GetId(), 300}, {garlic.GetId(), 50}, {peanutButter.GetId(), 500}, {lime.GetId(), 100}} {
		addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: lot.id, Amount: lot.grams, Unit: grams}.Build())
	}

	parts, err := e.suggestions.ListBowlParts(ctx, suggestionsv1.ListBowlPartsRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListBowlParts: %v", err)
	}
	byName := map[string]*suggestionsv1.BowlPart{}
	for _, p := range parts.GetParts() {
		byName[p.GetName()] = p
	}
	for name, role := range map[string]suggestionsv1.BowlRole{
		"Jasmine rice, white":                    suggestionsv1.BowlRole_BOWL_ROLE_CARB,
		"Tofu, extra firm":                       suggestionsv1.BowlRole_BOWL_ROLE_PROTEIN,
		"Broccoli":                               suggestionsv1.BowlRole_BOWL_ROLE_VEGETABLE,
		"Peanut butter, soy sauce & limes sauce": suggestionsv1.BowlRole_BOWL_ROLE_SAUCE,
	} {
		if p := byName[name]; p == nil || p.GetRole() != role || !p.GetOnHand() {
			t.Errorf("got part %v for %s, want it on hand as %v", p, name, role)
		}
	}
	// Garlic is an aromatic, not a bowl's vegetable.
	if p := byName["Garlic"]; p != nil {
		t.Errorf("got garlic as a part, %v, want it left out", p)
	}
	if p := byName["Jasmine rice, white"]; p.GetAmount() != 70 {
		t.Errorf("got %g g of rice a serving, want 70 g, about 260 kcal", p.GetAmount())
	}

	suggested, err := e.suggestions.SuggestBowls(ctx, suggestionsv1.SuggestBowlsRequest_builder{Servings: 2, Seed: 1}.Build())
	if err != nil {
		t.Fatalf("SuggestBowls: %v", err)
	}
	if len(suggested.GetBowls()) != 1 {
		t.Fatalf("got %d bowls, want 1, of the one sauce on hand", len(suggested.GetBowls()))
	}
	bowl := suggested.GetBowls()[0]
	var names []string
	for _, p := range bowl.GetParts() {
		names = append(names, p.GetName())
	}
	// The peanut sauce is Asian, so the bowl has rice, not tortillas.
	if bowl.GetStyle() != "Asian" || !slices.Contains(names, "Jasmine rice, white") || slices.Contains(names, "Corn tortillas") ||
		!slices.Contains(names, "Tofu, extra firm") || !slices.Contains(names, "Broccoli") || !bowl.GetOnHand() {
		t.Errorf("got %s bowl of %v, want an Asian bowl on hand of rice, tofu and broccoli", bowl.GetStyle(), names)
	}
	if kcal := bowl.GetNutrients().GetEnergyKcal(); kcal < 400 || kcal > 800 {
		t.Errorf("got %g kcal a serving, want a meal's", kcal)
	}

	// A bowl needs a sauce.
	_, err = e.suggestions.ComposeBowl(ctx, suggestionsv1.ComposeBowlRequest_builder{Parts: []*suggestionsv1.BowlPartChoice{
		suggestionsv1.BowlPartChoice_builder{Key: byName["Jasmine rice, white"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Tofu, extra firm"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Broccoli"].GetKey()}.Build(),
	}}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Composed by hand, with more tofu, it has more protein.
	composed, err := e.suggestions.ComposeBowl(ctx, suggestionsv1.ComposeBowlRequest_builder{Servings: 2, Parts: []*suggestionsv1.BowlPartChoice{
		suggestionsv1.BowlPartChoice_builder{Key: byName["Jasmine rice, white"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Tofu, extra firm"].GetKey(), Amount: 200}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Broccoli"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Cucumber"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Peanut butter, soy sauce & limes sauce"].GetKey()}.Build(),
	}}.Build())
	if err != nil {
		t.Fatalf("ComposeBowl: %v", err)
	}
	if composed.GetBowl().GetNutrients().GetProteinGrams() <= bowl.GetNutrients().GetProteinGrams() {
		t.Errorf("got %g g of protein with 200 g of tofu, want more than %g", composed.GetBowl().GetNutrients().GetProteinGrams(), bowl.GetNutrients().GetProteinGrams())
	}

	// Its recipe is created, with the sauce's ingredients for 2 servings,
	// and planned like any other.
	draft := composed.GetBowl().GetRecipe()
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: draft}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe of the bowl: %v", err)
	}
	r := created.GetRecipe()
	if r.GetServings() != 2 || len(r.GetIngredients()) != 7 || len(r.GetInstructions()) < 4 {
		t.Errorf("got recipe %s of %d servings, %d ingredients and %d steps, want 2 servings, 7 ingredients and the steps", r.GetName(), r.GetServings(), len(r.GetIngredients()), len(r.GetInstructions()))
	}
	for _, in := range r.GetIngredients() {
		if in.GetFoodId() == peanutButter.GetId() && in.GetAmount() != 40 {
			t.Errorf("got %g g of peanut butter, want the recipe's 40 g for its 2 servings", in.GetAmount())
		}
		if in.GetFoodId() == tofu.GetId() && in.GetAmount() != 400 {
			t.Errorf("got %g g of tofu, want 400 g, 200 g a serving", in.GetAmount())
		}
	}
	if !strings.Contains(r.GetName(), "Tofu & jasmine rice bowl") {
		t.Errorf("got name %q, want it named by its tofu and rice", r.GetName())
	}
	today := time.Now().UTC().Format(time.DateOnly)
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: r.GetId(), Date: today, Meal: dinner, Servings: 1}.Build()); err != nil {
		t.Fatalf("PlanMeal of the bowl: %v", err)
	}
	// Composed again, it's the recipe made before.
	again, err := e.suggestions.ComposeBowl(ctx, suggestionsv1.ComposeBowlRequest_builder{Servings: 2, Parts: []*suggestionsv1.BowlPartChoice{
		suggestionsv1.BowlPartChoice_builder{Key: byName["Jasmine rice, white"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Tofu, extra firm"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Broccoli"].GetKey()}.Build(),
		suggestionsv1.BowlPartChoice_builder{Key: byName["Peanut butter, soy sauce & limes sauce"].GetKey()}.Build(),
	}}.Build())
	if err != nil {
		t.Fatalf("ComposeBowl: %v", err)
	}
	if again.GetBowl().GetExistingRecipeId() != r.GetId() {
		t.Errorf("got existing recipe %q, want %q", again.GetBowl().GetExistingRecipeId(), r.GetId())
	}
}
