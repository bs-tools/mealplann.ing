package services_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestIngredientAlternatives checks that an ingredient made with either of
// two foods, like firm or extra firm tofu, is bought as its own food, but
// uses the alternative the pantry has: it isn't bought, and cooking takes
// it out of the pantry.
func TestIngredientAlternatives(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	firm := customFood(t, e, "Tofu, firm", 144, 15, 3, 0.6, 9, nil)
	extraFirm := customFood(t, e, "Tofu, extra firm", 150, 16, 3, 0.6, 9, nil)
	alternative := func(id string) *recipesv1.IngredientAlternative {
		return recipesv1.IngredientAlternative_builder{FoodId: id}.Build()
	}
	tofu := ingredient(firm.GetId(), 400, grams)
	tofu.SetAlternatives([]*recipesv1.IngredientAlternative{alternative(extraFirm.GetId())})
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu bowls", Servings: 2, Ingredients: []*recipesv1.Ingredient{tofu},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	got := created.GetRecipe().GetIngredients()[0].GetAlternatives()
	if len(got) != 1 || got[0].GetFoodName() != "Tofu, extra firm" {
		t.Fatalf("got alternatives %v, want the extra firm tofu", got)
	}
	// The ingredient's own food isn't an alternative to itself.
	own := ingredient(firm.GetId(), 400, grams)
	own.SetAlternatives([]*recipesv1.IngredientAlternative{alternative(firm.GetId())})
	_, err = e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Bad", Servings: 2, Ingredients: []*recipesv1.Ingredient{own},
	}.Build()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	cook, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: created.GetRecipe().GetId(), Date: monday, Servings: 2}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	items := func() []string {
		t.Helper()
		list, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: monday}.Build())
		if err != nil {
			t.Fatalf("CreateShoppingList: %v", err)
		}
		var items []string
		for _, it := range list.GetList().GetItems() {
			items = append(items, it.GetName()+" "+it.GetAmountText())
		}
		return items
	}
	// With neither in the pantry, the ingredient's own food is bought.
	if got := items(); len(got) != 1 || got[0] != "Tofu, firm 400 g" {
		t.Errorf("got items %v, want 400 g of firm tofu", got)
	}
	// With the alternative in the pantry, nothing is.
	if _, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: extraFirm.GetId(), Amount: 450, Unit: grams}.Build()); err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	if got := items(); len(got) != 0 {
		t.Errorf("got items %v, want none, with extra firm tofu in the pantry", got)
	}
	// Cooking it takes the alternative out of the pantry.
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetCook().GetId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	stock, err := e.pantry.ListPantry(ctx, pantryv1.ListPantryRequest_builder{FoodIds: []string{extraFirm.GetId()}}.Build())
	if err != nil {
		t.Fatalf("ListPantry: %v", err)
	}
	if lots := stock.GetItems(); len(lots) != 1 || lots[0].GetGrams() != 50 {
		t.Errorf("got %v left, want 50 g of extra firm tofu", lots)
	}
}
