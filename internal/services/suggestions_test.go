package services_test

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// suggestionNames returns the names of the suggested recipes, in order.
func suggestionNames(resp *suggestionsv1.SuggestRecipesResponse) []string {
	var names []string
	for _, s := range resp.GetSuggestions() {
		names = append(names, s.GetRecipeName())
	}
	return names
}

// reasons returns the reasons of a suggestion's components of a kind.
func reasons(s *suggestionsv1.Suggestion, kind suggestionsv1.ScoreKind) []string {
	var list []string
	for _, c := range s.GetComponents() {
		if c.GetKind() == kind {
			list = append(list, c.GetReason())
		}
	}
	return list
}

func TestSuggestRecipes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// The household: Ben, who eats three regular meals, and Anna, who eats
	// six small ones with her care team's rules.
	b := ben()
	b.SetUserId(store.UUIDToString(e.userID))
	ben := createPerson(t, e, b)
	anna := createPerson(t, e, anna())

	// The foods, with energy and macronutrients per 100 g. Coconut milk
	// comes in 400 g cans, and spinach keeps 5 days.
	coconutMilk := customFood(t, e, "Coconut milk", 197, 2, 3, 2, 21, func(f *foodsv1.Food) {
		f.SetPackageGrams(400)
		f.SetShelfLifeDays(365)
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_CANNED)
	})
	spinach := customFood(t, e, "Spinach", 23, 2.9, 3.6, 0.4, 0.4, func(f *foodsv1.Food) {
		f.SetShelfLifeDays(5)
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_PRODUCE)
	})
	chickpeas := customFood(t, e, "Chickpeas, canned", 139, 7, 23, 0, 2.6, func(f *foodsv1.Food) {
		f.SetPackageGrams(400)
		f.SetShelfLifeDays(730)
	})
	lentils := customFood(t, e, "Lentils, cooked", 116, 9, 20, 1.8, 0.4, nil)
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, func(f *foodsv1.Food) {
		f.SetPackageGrams(400)
		f.SetShelfLifeDays(30)
	})
	rice := customFood(t, e, "Rice, cooked", 130, 2.7, 28, 0.1, 0.3, nil)
	pasta := customFood(t, e, "Pasta, cooked", 158, 5.8, 31, 0.6, 0.9, nil)
	juice := customFood(t, e, "Apple juice", 46, 0.1, 11, 10, 0.1, func(f *foodsv1.Food) { f.SetFastCarb(true) })
	cumin := customFood(t, e, "Cumin", 375, 18, 44, 2, 22, func(f *foodsv1.Food) { f.SetAlwaysInStock(true) })

	// The recipes, each of 2 servings.
	newRecipe := func(name string, tags []string, ingredients ...*recipesv1.Ingredient) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
			Recipe: recipesv1.Recipe_builder{Name: name, Servings: 2, Tags: tags, FridgeDays: new(int32(3)), Ingredients: ingredients}.Build(),
		}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	curry := newRecipe("Chickpea coconut curry", []string{"dinner"},
		ingredient(coconutMilk.GetId(), 200, grams), ingredient(chickpeas.GetId(), 400, grams), ingredient(cumin.GetId(), 5, grams))
	newRecipe("Coconut spinach dal", []string{"dinner"},
		ingredient(coconutMilk.GetId(), 200, grams), ingredient(spinach.GetId(), 150, grams),
		ingredient(lentils.GetId(), 200, grams), ingredient(tofu.GetId(), 200, grams), ingredient(cumin.GetId(), 5, grams))
	newRecipe("Tofu fried rice", nil, ingredient(tofu.GetId(), 300, grams), ingredient(rice.GetId(), 300, grams))
	newRecipe("Pasta with spinach", []string{"dinner"}, ingredient(pasta.GetId(), 500, grams), ingredient(spinach.GetId(), 100, grams))
	newRecipe("Juice and rice pudding", []string{"snack"}, ingredient(rice.GetId(), 200, grams), ingredient(juice.GetId(), 200, grams))

	// The week: the curry on Tuesday, which opens a can of coconut milk,
	// cooking 3 servings as Ben's is sized up toward his protein, which
	// leaves 100 g of it, and the shop on Monday. The pantry has spinach to
	// use by Wednesday.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: curry.GetId(), Date: tuesday, Meal: dinner}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: spinach.GetId(), Amount: 200, Unit: grams, ExpiryDate: wednesday}.Build())
	if _, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: monday}.Build()); err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}

	// Suggest Wednesday's dinner: the dal uses the rest of the can and the
	// spinach, and fits Anna's rules.
	resp, err := e.suggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday, Meal: new(dinner)}.Build())
	if err != nil {
		t.Fatalf("SuggestRecipes: %v", err)
	}
	if resp.GetShopDate() != monday || resp.GetRecipeCount() != 5 || len(resp.GetSuggestions()) != 5 {
		t.Errorf("got shop date %s and %d of %d recipes, want Monday and all 5", resp.GetShopDate(), len(resp.GetSuggestions()), resp.GetRecipeCount())
	}
	names := suggestionNames(resp)
	if names[0] != "Coconut spinach dal" || names[len(names)-1] != "Juice and rice pudding" {
		t.Errorf("got %v, want the dal first and the juice and rice pudding last", names)
	}
	dal := resp.GetSuggestions()[0]
	if got, want := reasons(dal, suggestionsv1.ScoreKind_SCORE_KIND_PACK_LEFTOVERS),
		[]string{"uses the other 100 g of the 400 g pack of coconut milk bought for Tuesday's Chickpea coconut curry"}; !slices.Equal(got, want) {
		t.Errorf("got pack leftovers %v, want %v", got, want)
	}
	if got, want := reasons(dal, suggestionsv1.ScoreKind_SCORE_KIND_PANTRY),
		[]string{"uses 150 g of spinach from the pantry, best used by Wed 4 Nov"}; !slices.Equal(got, want) {
		t.Errorf("got pantry %v, want %v", got, want)
	}
	nutrition := reasons(dal, suggestionsv1.ScoreKind_SCORE_KIND_NUTRITION)
	if len(nutrition) != 2 || !strings.HasPrefix(nutrition[0], "adds 28.6 g of protein toward") {
		t.Errorf("got nutrition %v, want 28.6 g of protein toward Anna's and Ben's minimums", nutrition)
	}
	if !dal.GetFitsRules() || len(dal.GetWarnings()) != 0 || dal.GetCookServings() != 2 {
		t.Errorf("got %v, want the dal to fit Anna's rules, cooking 2 servings", dal)
	}
	// It needs another can of coconut milk, and lentils and tofu bought,
	// which the week doesn't buy yet.
	var toBuy []string
	for _, it := range dal.GetItemsToBuy() {
		toBuy = append(toBuy, it.GetFoodName()+" "+it.GetAmountText())
	}
	if want := []string{"Coconut milk 1 more × 400 g", "Lentils, cooked 200 g", "Tofu 1 × 400 g"}; !slices.Equal(toBuy, want) || dal.GetNewItemCount() != 2 {
		t.Errorf("got %v, %d new, want %v", toBuy, dal.GetNewItemCount(), want)
	}
	// Anna's serving is split within her maximum portion, like PlanMeal.
	var portions []string
	for _, p := range dal.GetPortions() {
		portions = append(portions, p.GetPersonName()+" "+p.GetEatingTimeName()+" "+fmt.Sprint(math.Round(p.GetGrams())))
	}
	if want := []string{"Anna Dinner 196", "Anna Evening 181", "Ben Dinner 378"}; !slices.Equal(portions, want) {
		t.Errorf("got portions %v, want %v", portions, want)
	}
	var total float64
	for _, c := range dal.GetComponents() {
		total += c.GetPoints()
	}
	if diff := total - dal.GetScore(); diff > 0.05 || diff < -0.05 {
		t.Errorf("got score %v, want the sum of the components, %v", dal.GetScore(), total)
	}

	// The juice and rice pudding breaks Anna's rules, saying how.
	pudding := resp.GetSuggestions()[len(names)-1]
	kinds := warningKinds(pudding.GetWarnings())
	if pudding.GetFitsRules() || !slices.Contains(kinds, plansv1.WarningKind_WARNING_KIND_CARBOHYDRATE_HIGH) ||
		!slices.Contains(kinds, plansv1.WarningKind_WARNING_KIND_FAST_CARBS) {
		t.Errorf("got %v, want the pudding to break Anna's rules", pudding)
	}
	for _, w := range pudding.GetWarnings() {
		if w.GetPersonId() != anna.GetId() || w.GetDate() != wednesday || !strings.HasPrefix(w.GetMessage(), "Anna: ") {
			t.Errorf("got warning %v, want one about Anna on Wednesday", w)
		}
	}
	// The curry was planned the day before.
	i := slices.Index(names, "Chickpea coconut curry")
	if got := reasons(resp.GetSuggestions()[i], suggestionsv1.ScoreKind_SCORE_KIND_VARIETY); !slices.Equal(got, []string{"already planned on Tue 3 Nov"}) {
		t.Errorf("got variety %v, want the curry on Tuesday", got)
	}

	// Plan the dal, as the web app's "Plan this" does: on Thursday, it's
	// planned the day before, and the pantry's spinach has gone off.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: dal.GetRecipeId(), Date: wednesday, Meal: dinner}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	resp, err = e.suggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{
		Date: thursday, Meal: new(dinner), PersonIds: []string{ben.GetId()}, MaxSuggestions: 2,
	}.Build())
	if err != nil {
		t.Fatalf("SuggestRecipes: %v", err)
	}
	if names := suggestionNames(resp); len(names) != 2 || slices.Contains(names, "Coconut spinach dal") {
		t.Errorf("got %v, want 2 suggestions without the dal", names)
	}
	for _, p := range resp.GetSuggestions()[0].GetPortions() {
		if p.GetPersonId() != ben.GetId() {
			t.Errorf("got portion %v, want only Ben's", p)
		}
	}

	// Suggest Anna's mid-afternoon small meal: one portion within her
	// maximum, the pudding tagged as a snack, but breaking her rules.
	resp, err = e.suggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{
		Date:       wednesday,
		EatingTime: suggestionsv1.EatingTimeSlot_builder{PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Mid-afternoon")}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("SuggestRecipes: %v", err)
	}
	for _, s := range resp.GetSuggestions() {
		p := s.GetPortions()
		if len(p) != 1 || p[0].GetEatingTimeName() != "Mid-afternoon" || p[0].GetGrams() > 200 || s.GetCookServings() < 0.25 {
			t.Errorf("got %s's portions %v of %v servings, want one at Mid-afternoon within 200 g", s.GetRecipeName(), p, s.GetCookServings())
		}
		if s.GetRecipeName() == "Juice and rice pudding" && (s.GetFitsRules() || !slices.Equal(reasons(s, suggestionsv1.ScoreKind_SCORE_KIND_MEAL_TAG), []string{"tagged snack"})) {
			t.Errorf("got %v, want the pudding tagged as a snack, breaking the rules", s)
		}
	}

	// Marking the curry as a favorite raises its score for Saturday's
	// dinner by the favorite's points, saying why.
	saturdayDinner := suggestionsv1.SuggestRecipesRequest_builder{Date: saturday, Meal: new(dinner), MaxSuggestions: 50}.Build()
	curryOn := func() *suggestionsv1.Suggestion {
		t.Helper()
		resp, err := e.suggestions.SuggestRecipes(ctx, saturdayDinner)
		if err != nil {
			t.Fatalf("SuggestRecipes: %v", err)
		}
		i := slices.Index(suggestionNames(resp), "Chickpea coconut curry")
		if i < 0 {
			t.Fatalf("got %v, want the curry", suggestionNames(resp))
		}
		return resp.GetSuggestions()[i]
	}
	before := curryOn()
	if _, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: curry.GetId(), Favorite: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"favorite"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	after := curryOn()
	if got := reasons(after, suggestionsv1.ScoreKind_SCORE_KIND_FAVORITE); !slices.Equal(got, []string{"a household favorite"}) {
		t.Errorf("got favorite reasons %v, want the household's favorite", got)
	}
	if diff := after.GetScore() - before.GetScore(); math.Abs(diff-5) > 0.05 {
		t.Errorf("got the score up by %v, want 5", diff)
	}

	// Bad requests fail.
	for name, req := range map[string]*suggestionsv1.SuggestRecipesRequest{
		"bad date":   suggestionsv1.SuggestRecipesRequest_builder{Date: "Wednesday", Meal: new(dinner)}.Build(),
		"no slot":    suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday}.Build(),
		"no meal":    suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday, Meal: new(noMeal)}.Build(),
		"too many":   suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday, Meal: new(dinner), MaxSuggestions: 51}.Build(),
		"bad person": suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday, Meal: new(dinner), PersonIds: []string{"nobody"}}.Build(),
		"other's eating time": suggestionsv1.SuggestRecipesRequest_builder{
			Date:       wednesday,
			EatingTime: suggestionsv1.EatingTimeSlot_builder{PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, ben, "Dinner")}.Build(),
		}.Build(),
		"unknown eating time": suggestionsv1.SuggestRecipesRequest_builder{
			Date:       wednesday,
			EatingTime: suggestionsv1.EatingTimeSlot_builder{PersonId: anna.GetId(), EatingTimeId: store.UUIDToString(store.NewUUID())}.Build(),
		}.Build(),
	} {
		_, err := e.suggestions.SuggestRecipes(ctx, req)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want INVALID_ARGUMENT", name, err)
		}
	}

	// Another household can't suggest for Anna, and has nobody to eat a
	// dinner; signed out, nothing works.
	_, err = e.otherSuggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{
		Date:       wednesday,
		EatingTime: suggestionsv1.EatingTimeSlot_builder{PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Dinner")}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.otherSuggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday, Meal: new(dinner)}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.anonSuggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday, Meal: new(dinner)}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Once the other household has someone of its own, it only gets its own
	// recipes: none.
	if _, err := e.otherPeople.CreatePerson(ctx, peoplev1.CreatePersonRequest_builder{
		Person: peoplev1.Person_builder{Name: "Cleo", EatingTimes: []*peoplev1.EatingTime{eatingTime("Dinner", mainMeal, "19:00", dinner)}}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	resp, err = e.otherSuggestions.SuggestRecipes(ctx, suggestionsv1.SuggestRecipesRequest_builder{Date: wednesday, Meal: new(dinner)}.Build())
	if err != nil {
		t.Fatalf("SuggestRecipes: %v", err)
	}
	if resp.GetRecipeCount() != 0 || len(resp.GetSuggestions()) != 0 {
		t.Errorf("got %v, want no recipes of the other household", suggestionNames(resp))
	}
}
