package services_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

// TestComponentAlternatives checks that a component can list recipes it's
// made with instead: planning takes frozen leftovers of either, oldest
// first, converting its grams with the supplying recipe's yield, and a cook
// of either, but no other, can supply it.
func TestComponentAlternatives(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	pinto := customFood(t, e, "Dried pinto beans", 347, 21, 63, 2, 1, nil)
	create := func(r *recipesv1.Recipe) *recipesv1.Recipe {
		t.Helper()
		created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe %s: %v", r.GetName(), err)
		}
		return created.GetRecipe()
	}
	beans := func(name string, servings int32, yield float64) *recipesv1.Recipe {
		return create(recipesv1.Recipe_builder{
			Name: name, Servings: servings, Freezable: true, FridgeDays: new(int32(4)), FreezerDays: new(int32(90)),
			Ingredients: []*recipesv1.Ingredient{ingredient(pinto.GetId(), 200, grams)},
			YieldAmount: yield, YieldUnit: foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS,
		}.Build())
	}
	soaked := beans("Pinto beans, soaked", 3, 475)
	quick := beans("Pinto beans, quick", 4, 600)
	other := beans("Black beans", 3, 450)
	in := recipesv1.Ingredient_builder{
		RecipeId: soaked.GetId(), Amount: 250, Unit: grams,
		Alternatives: []*recipesv1.IngredientAlternative{recipesv1.IngredientAlternative_builder{RecipeId: quick.GetId()}.Build()},
	}.Build()
	chili := create(recipesv1.Recipe_builder{Name: "Chili", Servings: 4, Ingredients: []*recipesv1.Ingredient{in}}.Build())
	if got := chili.GetIngredients()[0].GetAlternatives(); len(got) != 1 || got[0].GetRecipeName() != "Pinto beans, quick" {
		t.Fatalf("got alternatives %v, want the quick pinto beans", got)
	}
	// A component's alternative must be a recipe.
	bad := recipesv1.Ingredient_builder{
		RecipeId: soaked.GetId(), Amount: 1,
		Alternatives: []*recipesv1.IngredientAlternative{recipesv1.IngredientAlternative_builder{FoodId: pinto.GetId()}.Build()},
	}.Build()
	_, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Bad", Servings: 2, Ingredients: []*recipesv1.Ingredient{bad},
	}.Build()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Quick beans frozen on Monday, and soaked ones on Tuesday: the chili
	// takes the older, the quick ones, 250 g of 600 g in 4 servings.
	freeze := func(r *recipesv1.Recipe, date string) *plansv1.Cook {
		t.Helper()
		c, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
			Cook:      plansv1.Cook_builder{RecipeId: r.GetId(), Date: date, Servings: float64(r.GetServings()), FrozenServings: float64(r.GetServings()), Done: true}.Build(),
			FromStock: true,
		}.Build())
		if err != nil {
			t.Fatalf("CreateCook: %v", err)
		}
		return c.GetCook()
	}
	frozenQuick := freeze(quick, monday)
	frozenSoaked := freeze(soaked, tuesday)
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: chili.GetId(), Date: friday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, cooks := weekCooks(t, e, monday)
	supply := func() *plansv1.CookSupply {
		t.Helper()
		s := cooks["Chili "+friday].GetCook().GetSupplies()
		if len(s) != 1 {
			t.Fatalf("got supplies %v, want one", s)
		}
		return s[0]
	}
	if s := supply(); s.GetSupplierCookId() != frozenQuick.GetId() || !s.GetFromFreezer() || !near(s.GetServings(), 1.667) ||
		len(s.GetAlternativeRecipeIds()) != 1 || s.GetAlternativeRecipeIds()[0] != quick.GetId() {
		t.Errorf("got supply %v, want 1.667 servings of the frozen quick beans", s)
	}
	if cooks["Pinto beans, soaked "+friday] != nil {
		t.Errorf("got a new pot of soaked beans, want the frozen quick ones used")
	}

	// The soaked beans can supply it instead, as 1.579 of their servings;
	// black beans can't.
	chiliID := cooks["Chili "+friday].GetCook().GetId()
	set := func(supplierID string) error {
		_, err := e.plans.SetCookSupplier(ctx, plansv1.SetCookSupplierRequest_builder{
			CookId: chiliID, ComponentRecipeId: soaked.GetId(), SupplierCookId: supplierID,
		}.Build())
		return err
	}
	if err := set(frozenSoaked.GetId()); err != nil {
		t.Fatalf("SetCookSupplier: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	if s := supply(); s.GetSupplierCookId() != frozenSoaked.GetId() || !s.GetFromFreezer() || !near(s.GetServings(), 1.579) {
		t.Errorf("got supply %v, want 1.579 servings of the frozen soaked beans", s)
	}
	blackBeans := freeze(other, monday)
	wantCode(t, set(blackBeans.GetId()), connect.CodeInvalidArgument)
}
