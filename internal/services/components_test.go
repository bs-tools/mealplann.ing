package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// componentIngredient is servings of a recipe in another.
func componentIngredient(recipeID string, servings float64) *recipesv1.Ingredient {
	return recipesv1.Ingredient_builder{RecipeId: recipeID, Amount: servings}.Build()
}

// weekCooks returns the week's cooks by recipe name and date, e.g.
// "Chili 2026-11-06 4".
func weekCooks(t *testing.T, e *env, monday string) (*plansv1.GetWeekResponse, map[string]*plansv1.PlannedCook) {
	t.Helper()
	week, err := e.plans.GetWeek(context.Background(), plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	cooks := map[string]*plansv1.PlannedCook{}
	for _, c := range week.GetCooks() {
		cooks[c.GetCook().GetRecipeName()+" "+c.GetCook().GetDate()] = c
	}
	return week, cooks
}

func TestRecipeComponents(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	dried := customFood(t, e, "Dried chickpeas", 380, 20, 60, 3, 6, nil)
	tomatoes := customFood(t, e, "Tomatoes", 20, 1, 4, 3, 0.2, nil)
	create := func(r *recipesv1.Recipe) (*recipesv1.Recipe, error) {
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		return resp.GetRecipe(), err
	}

	// Cooked chickpeas are soaked the night before; the chili uses half a
	// pot of them.
	chickpeas, err := create(recipesv1.Recipe_builder{
		Name:          "Cooked chickpeas",
		Servings:      4,
		Ingredients:   []*recipesv1.Ingredient{ingredient(dried.GetId(), 250, grams)},
		PrepAheadDays: 1,
		PrepAheadNote: "Soak the chickpeas",
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	chili, err := create(recipesv1.Recipe_builder{
		Name:     "Chili",
		Servings: 4,
		Ingredients: []*recipesv1.Ingredient{
			ingredient(tomatoes.GetId(), 400, grams),
			componentIngredient(chickpeas.GetId(), 2),
		},
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	if got := chili.GetIngredients()[1]; got.GetRecipeName() != "Cooked chickpeas" || got.GetFoodId() != "" || got.GetAmount() != 2 {
		t.Errorf("got %v, want 2 servings of the cooked chickpeas", got)
	}
	// A serving has the tomatoes' 80 kcal and half the chickpeas' 950, over
	// 4 servings.
	if got := chili.GetNutrition().GetPerServing().GetEnergyKcal(); !near(got, (80+475)/4.0) {
		t.Errorf("got %v kcal a serving, want %v", got, (80+475)/4.0)
	}
	if chickpeas.GetPrepAheadDays() != 1 || chickpeas.GetPrepAheadNote() != "Soak the chickpeas" {
		t.Errorf("got %v, want soaked a day ahead", chickpeas)
	}

	// Recipes can't use themselves, nor nest too deep.
	_, err = e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: chickpeas.GetId(), Ingredients: []*recipesv1.Ingredient{componentIngredient(chili.GetId(), 1)}}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"ingredients"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	burrito, err := create(recipesv1.Recipe_builder{Name: "Burrito", Servings: 2, Ingredients: []*recipesv1.Ingredient{componentIngredient(chili.GetId(), 1)}}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	_, err = create(recipesv1.Recipe_builder{Name: "Burrito plate", Servings: 2, Ingredients: []*recipesv1.Ingredient{componentIngredient(burrito.GetId(), 1)}}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = create(recipesv1.Recipe_builder{Name: "Bad", Servings: 2, Ingredients: []*recipesv1.Ingredient{
		// The chili doesn't say how many pieces it makes.
		recipesv1.Ingredient_builder{RecipeId: chili.GetId(), Amount: 1, Unit: pieces}.Build(),
	}}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.otherRecipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Theirs", Servings: 2, Ingredients: []*recipesv1.Ingredient{componentIngredient(chili.GetId(), 1)},
	}.Build()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	// The chickpeas can't be deleted while the chili uses them.
	_, err = e.recipes.DeleteRecipe(ctx, recipesv1.DeleteRecipeRequest_builder{Id: chickpeas.GetId()}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)

	// Planning Friday's chili plans the chickpeas for it too, to soak on
	// Thursday.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: chili.GetId(), Date: friday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	week, cooks := weekCooks(t, e, monday)
	pot := cooks["Cooked chickpeas "+friday]
	fridayChili := cooks["Chili "+friday]
	if pot == nil || fridayChili == nil || pot.GetCook().GetServings() != 2 || pot.GetAssignedServings() != 2 || len(pot.GetWarnings()) != 0 {
		t.Fatalf("got cooks %v, want the chili and 2 servings of chickpeas for it", week.GetCooks())
	}
	if s := fridayChili.GetCook().GetSupplies(); len(s) != 1 || s[0].GetSupplierCookId() != pot.GetCook().GetId() || s[0].GetServings() != 2 {
		t.Errorf("got supplies %v, want the pot's 2 servings", s)
	}
	if s := pot.GetCook().GetSupplying(); len(s) != 1 || s[0].GetConsumerRecipeName() != "Chili" {
		t.Errorf("got supplying %v, want the chili", s)
	}
	var tasks []string
	for _, day := range week.GetDays() {
		for _, task := range day.GetPrepTasks() {
			tasks = append(tasks, day.GetDate()+" "+task.GetNote()+" for "+task.GetForRecipeNames()[0])
		}
	}
	if want := []string{thursday + " Soak the chickpeas for Chili"}; !slices.Equal(tasks, want) {
		t.Errorf("got prep tasks %v, want %v", tasks, want)
	}

	// The shopping list buys the dried chickpeas once, for the pot.
	list, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: monday}.Build())
	if err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}
	var items []string
	for _, it := range list.GetList().GetItems() {
		items = append(items, it.GetName()+" "+it.GetAmountText())
	}
	if want := []string{"Dried chickpeas 125 g", "Tomatoes 400 g"}; !slices.Equal(slices.Sorted(slices.Values(items)), want) {
		t.Errorf("got items %v, want %v", items, want)
	}

	// Cooking the chili takes its tomatoes out of the pantry, not the
	// chickpeas, which the pot takes.
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: tomatoes.GetId(), Amount: 1000, Unit: grams}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: dried.GetId(), Amount: 1000, Unit: grams}.Build())
	done, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: fridayChili.GetCook().GetId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	if got := done.GetPantryChanges(); len(got) != 1 || got[0].GetFoodName() != "Tomatoes" || !near(got[0].GetGrams(), 400) {
		t.Errorf("got pantry changes %v, want only the 400 g of tomatoes", got)
	}

	// Saturday's chili shares Friday's pot: its own pot goes, and Friday's
	// makes 4 servings, for both.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: chili.GetId(), Date: saturday, Meal: dinner, Servings: 4}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	saturdayChili := cooks["Chili "+saturday]
	if cooks["Cooked chickpeas "+saturday] == nil {
		t.Fatalf("got cooks %v, want Saturday's own pot", cooks)
	}
	if _, err := e.plans.SetCookSupplier(ctx, plansv1.SetCookSupplierRequest_builder{
		CookId: saturdayChili.GetCook().GetId(), ComponentRecipeId: chickpeas.GetId(), SupplierCookId: pot.GetCook().GetId(),
	}.Build()); err != nil {
		t.Fatalf("SetCookSupplier: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	if cooks["Cooked chickpeas "+saturday] != nil {
		t.Errorf("got cooks %v, want Saturday's pot deleted, as nothing uses it", cooks)
	}
	if got := cooks["Cooked chickpeas "+friday]; got.GetCook().GetServings() != 4 || got.GetAssignedServings() != 4 || len(got.GetWarnings()) != 0 {
		t.Errorf("got %v, want the pot to make 4 servings, for both", got)
	}
	// A cook of another recipe can't supply the chickpeas.
	_, err = e.plans.SetCookSupplier(ctx, plansv1.SetCookSupplierRequest_builder{
		CookId: saturdayChili.GetCook().GetId(), ComponentRecipeId: chickpeas.GetId(), SupplierCookId: fridayChili.GetCook().GetId(),
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Deleting Saturday's chili keeps the pot, which Friday's uses; deleting
	// Friday's, which is done, keeps the pot too, until it's unused.
	if _, err := e.plans.DeleteCook(ctx, plansv1.DeleteCookRequest_builder{Id: saturdayChili.GetCook().GetId()}.Build()); err != nil {
		t.Fatalf("DeleteCook: %v", err)
	}
	_, cooks = weekCooks(t, e, monday)
	if cooks["Cooked chickpeas "+friday] == nil {
		t.Errorf("got cooks %v, want Friday's pot kept", cooks)
	}
	if _, err := e.plans.DeleteCook(ctx, plansv1.DeleteCookRequest_builder{Id: fridayChili.GetCook().GetId()}.Build()); err != nil {
		t.Fatalf("DeleteCook: %v", err)
	}
	if _, cooks = weekCooks(t, e, monday); len(cooks) != 0 {
		t.Errorf("got cooks %v, want the pot deleted with the last chili it was for", cooks)
	}
}

// TestComponentsFromFreezer checks that a cook of a recipe with a component
// takes it from the freezer, from an earlier cook with enough frozen,
// rather than planning a new cook of it, and thaws it the day before.
func TestComponentsFromFreezer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	dried := customFood(t, e, "Dried pinto beans", 350, 21, 63, 2, 1, nil)
	tomatoes := customFood(t, e, "Tomatoes", 20, 1, 4, 3, 0.2, nil)
	create := func(r *recipesv1.Recipe) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	beans := create(recipesv1.Recipe_builder{
		Name: "Pinto beans", Servings: 4, FridgeDays: new(int32(3)), Freezable: true, FreezerDays: new(int32(90)),
		Ingredients: []*recipesv1.Ingredient{ingredient(dried.GetId(), 250, grams)},
	}.Build())
	chili := create(recipesv1.Recipe_builder{
		Name: "Chili", Servings: 4,
		Ingredients: []*recipesv1.Ingredient{ingredient(tomatoes.GetId(), 400, grams), componentIngredient(beans.GetId(), 2)},
	}.Build())
	// A pot of beans cooked on Monday is all in the freezer.
	frozen, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook:      plansv1.Cook_builder{RecipeId: beans.GetId(), Date: monday, Servings: 4, FrozenServings: 4, Done: true}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	cookChili := func(date string) *plansv1.Cook {
		t.Helper()
		resp, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
			Cook: plansv1.Cook_builder{RecipeId: chili.GetId(), Date: date, Servings: 4}.Build(),
		}.Build())
		if err != nil {
			t.Fatalf("CreateCook: %v", err)
		}
		return resp.GetCook()
	}

	// Thursday's and Friday's chilis each take half the frozen beans.
	for _, date := range []string{thursday, friday} {
		supplies := cookChili(date).GetSupplies()
		if len(supplies) != 1 || supplies[0].GetSupplierCookId() != frozen.GetCook().GetId() || !supplies[0].GetFromFreezer() {
			t.Errorf("got %v on %s, want the frozen beans", supplies, date)
		}
	}
	leftovers, err := e.plans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if got := leftovers.GetLeftovers(); len(got) != 0 {
		t.Errorf("got %v in the freezer, want the beans all planned", got)
	}
	// Saturday's has none left, so it plans a pot of beans.
	supplies := cookChili(saturday).GetSupplies()
	if len(supplies) != 1 || supplies[0].GetFromFreezer() || supplies[0].GetSupplierDate() != saturday {
		t.Errorf("got %v on Saturday, want a new pot of beans", supplies)
	}

	// The beans are thawed the day before, and none are left over to plan.
	week, cooks := weekCooks(t, e, monday)
	var tasks []string
	for _, day := range week.GetDays() {
		for _, task := range day.GetPrepTasks() {
			tasks = append(tasks, day.GetDate()+" "+task.GetNote())
		}
	}
	for _, want := range []string{
		wednesday + " Take the pinto beans out of the freezer to thaw",
		thursday + " Take the pinto beans out of the freezer to thaw",
	} {
		if !slices.Contains(tasks, want) {
			t.Errorf("got tasks %q, want %q", tasks, want)
		}
	}
	if c := cooks["Pinto beans "+monday]; c.GetLeftServings() != 0 || len(c.GetWarnings()) != 0 {
		t.Errorf("got %v, want the frozen beans planned, with no warnings", c)
	}
}
