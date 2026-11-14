package services_test

import (
	"context"
	"math"
	"testing"

	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestComponentFoodAlternative checks that a component listing a food it
// can be replaced with, like canned beans for a pot of beans, is cooked and
// bought for without it in the pantry, and with it there, plans no cook of
// the component, buys nothing for it, and takes the food out of the
// pantry when the dish is cooked.
func TestComponentFoodAlternative(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	dried := customFood(t, e, "Dried black beans", 341, 22, 62, 2, 1, nil)
	canned := customFood(t, e, "Black beans, canned", 91, 6, 16, 0.3, 0.3, nil)
	create := func(r *recipesv1.Recipe) *recipesv1.Recipe {
		t.Helper()
		created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe %s: %v", r.GetName(), err)
		}
		return created.GetRecipe()
	}
	pot := create(recipesv1.Recipe_builder{
		Name: "Black beans, soaked", Servings: 3,
		Ingredients: []*recipesv1.Ingredient{ingredient(dried.GetId(), 200, grams)},
		YieldAmount: 450, YieldUnit: foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS,
	}.Build())
	chili := create(recipesv1.Recipe_builder{Name: "Chili", Servings: 4, Ingredients: []*recipesv1.Ingredient{
		recipesv1.Ingredient_builder{
			RecipeId: pot.GetId(), Amount: 250, Unit: grams,
			Alternatives: []*recipesv1.IngredientAlternative{recipesv1.IngredientAlternative_builder{FoodId: canned.GetId()}.Build()},
		}.Build(),
	}}.Build())
	if got := chili.GetIngredients()[0].GetAlternatives(); len(got) != 1 || got[0].GetFoodName() != "Black beans, canned" {
		t.Fatalf("got alternatives %v, want the canned beans", got)
	}
	items := func() []string {
		t.Helper()
		list, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: monday}.Build())
		if err != nil {
			t.Fatalf("CreateShoppingList: %v", err)
		}
		var items []string
		for _, it := range list.GetList().GetItems() {
			items = append(items, it.GetName())
		}
		return items
	}

	// Without canned beans, a pot of beans is planned and bought for.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: chili.GetId(), Date: thursday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, cooks := weekCooks(t, e, monday)
	if cooks["Black beans, soaked "+thursday] == nil {
		t.Errorf("got cooks %v, want a pot of beans", cooks)
	}
	if got := items(); len(got) != 1 || got[0] != "Dried black beans" {
		t.Errorf("got items %v, want the dried beans", got)
	}

	// With 400 g of canned beans, Friday's chili plans no pot, nothing is
	// bought for it, and cooking it takes 250 g of the canned beans.
	if _, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: canned.GetId(), Amount: 400, Unit: grams}.Build()); err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: chili.GetId(), Date: friday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	if cooks["Black beans, soaked "+friday] != nil || len(cooks["Chili "+friday].GetCook().GetSupplies()) != 0 {
		t.Errorf("got cooks %v, want no pot of beans for Friday", cooks)
	}
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cooks["Chili "+friday].GetCook().GetId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	stock, err := e.pantry.ListPantry(ctx, pantryv1.ListPantryRequest_builder{FoodIds: []string{canned.GetId()}}.Build())
	if err != nil {
		t.Fatalf("ListPantry: %v", err)
	}
	if lots := stock.GetItems(); len(lots) != 1 || math.Abs(lots[0].GetGrams()-150) > 0.01 {
		t.Errorf("got %v left, want 150 g of canned beans", lots)
	}
}
