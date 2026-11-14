package services_test

import (
	"context"
	"slices"
	"testing"

	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

// TestComponentSupplyUnits checks that a cook supplying a component given
// in grams supplies the servings the grams are, as the recipe converts
// them: by the component's yield, or else its ingredients' weight, not the
// grams as servings; and that one given in servings is unchanged, and
// frozen servings supply it once there are enough of the converted amount.
func TestComponentSupplyUnits(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	pinto := customFood(t, e, "Dried pinto beans", 347, 21, 63, 2, 1, nil)
	kidney := customFood(t, e, "Dried kidney beans", 333, 24, 60, 2, 1, nil)
	create := func(r *recipesv1.Recipe) *recipesv1.Recipe {
		t.Helper()
		created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe %s: %v", r.GetName(), err)
		}
		return created.GetRecipe()
	}
	// Pinto beans make 475 g in 3 servings; kidney beans don't say, so 150 g
	// of dried beans weigh 150 g, in 2 servings.
	pintoBeans := create(recipesv1.Recipe_builder{
		Name: "Pinto beans", Servings: 3, Freezable: true, FridgeDays: new(int32(4)), FreezerDays: new(int32(90)),
		Ingredients: []*recipesv1.Ingredient{ingredient(pinto.GetId(), 200, grams)},
		YieldAmount: 475, YieldUnit: foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS,
	}.Build())
	kidneyBeans := create(recipesv1.Recipe_builder{
		Name: "Kidney beans", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{ingredient(kidney.GetId(), 150, grams)},
	}.Build())
	inGrams := func(id string, amount float64) *recipesv1.Ingredient {
		return recipesv1.Ingredient_builder{RecipeId: id, Amount: amount, Unit: grams}.Build()
	}
	chili := create(recipesv1.Recipe_builder{
		Name: "Chili", Servings: 4, Ingredients: []*recipesv1.Ingredient{
			inGrams(pintoBeans.GetId(), 250),
			inGrams(kidneyBeans.GetId(), 75),
		},
	}.Build())
	stew := create(recipesv1.Recipe_builder{
		Name: "Stew", Servings: 2, Ingredients: []*recipesv1.Ingredient{componentIngredient(pintoBeans.GetId(), 1)},
	}.Build())

	// The chili plans the beans it needs: 250 g of pinto beans are 1.579
	// servings, and 75 g of kidney beans 1 serving.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: chili.GetId(), Date: friday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, cooks := weekCooks(t, e, monday)
	supplied := func(c *plansv1.PlannedCook) []float64 {
		var servings []float64
		for _, s := range c.GetCook().GetSupplies() {
			servings = append(servings, s.GetServings())
		}
		slices.Sort(servings)
		return servings
	}
	if got := supplied(cooks["Chili "+friday]); len(got) != 2 || !near(got[0], 1) || !near(got[1], 1.579) {
		t.Errorf("got supplies of %v servings, want 1 of kidney beans and 1.579 of pinto beans", got)
	}
	// The pot of pinto beans has 1.579 of its 3 servings planned, not 250.
	if pot := cooks["Pinto beans "+friday]; pot == nil || !near(pot.GetAssignedServings(), 1.579) {
		t.Errorf("got the pinto beans %v, want 1.579 servings for the chili", pot)
	}

	// A stew giving its beans in servings supplies them as they are.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: stew.GetId(), Date: saturday, Meal: dinner, Servings: 2}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	if got := supplied(cooks["Stew "+saturday]); len(got) != 1 || !near(got[0], 1) {
		t.Errorf("got supplies of %v servings, want 1 serving of pinto beans", got)
	}

	// Frozen pinto beans from Monday, 2 servings, supply the chili's 1.579
	// from the freezer, with no warning that too few are frozen.
	frozen, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook:      plansv1.Cook_builder{RecipeId: pintoBeans.GetId(), Date: monday, Servings: 2, FrozenServings: 2, Done: true}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	if _, err := e.plans.SetCookSupplier(ctx, plansv1.SetCookSupplierRequest_builder{
		CookId: cooks["Chili "+friday].GetCook().GetId(), ComponentRecipeId: pintoBeans.GetId(), SupplierCookId: frozen.GetCook().GetId(),
	}.Build()); err != nil {
		t.Fatalf("SetCookSupplier: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	for _, s := range cooks["Chili "+friday].GetCook().GetSupplies() {
		if s.GetComponentRecipeId() == pintoBeans.GetId() && (!s.GetFromFreezer() || !near(s.GetServings(), 1.579)) {
			t.Errorf("got supply %v, want 1.579 servings from the freezer", s)
		}
	}
	for _, w := range cooks["Pinto beans "+monday].GetWarnings() {
		if w.GetKind() == plansv1.WarningKind_WARNING_KIND_FROZEN_SERVINGS_SHORT {
			t.Errorf("got warning %v, want enough frozen", w)
		}
	}
}
