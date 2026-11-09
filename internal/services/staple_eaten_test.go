package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
)

func TestStapleEatenPantry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ben := createPerson(t, e, ben())
	breakfast := eatingTimeID(t, ben, "Breakfast")

	soyMilk := shopFood(t, e, "Soy milk", func(f *foodsv1.Food) { f.SetGramsPerMilliliter(1) })
	oats := shopFood(t, e, "Oats", nil)
	berries := shopFood(t, e, "Berries", nil)
	porridge, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Name:        "Porridge",
			Servings:    2,
			Ingredients: []*recipesv1.Ingredient{ingredient(oats.GetId(), 120, grams), ingredient(berries.GetId(), 100, grams)},
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: soyMilk.GetId(), Amount: 1000, Unit: grams}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: oats.GetId(), Amount: 500, Unit: grams}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: berries.GetId(), Amount: 30, Unit: grams}.Build())

	// 250 ml of soy milk every day, and a serving of porridge on weekdays.
	createStaple := func(s *plansv1.Staple) *plansv1.Staple {
		t.Helper()
		resp, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{Staple: s}.Build())
		if err != nil {
			t.Fatalf("CreateStaple: %v", err)
		}
		return resp.GetStaple()
	}
	milk := createStaple(plansv1.Staple_builder{
		PersonId: ben.GetId(), EatingTimeId: breakfast,
		Food: plansv1.FoodAmount_builder{FoodId: soyMilk.GetId(), Amount: 250, Unit: milliliters}.Build(),
	}.Build())
	weekdayPorridge := createStaple(plansv1.Staple_builder{
		PersonId: ben.GetId(), EatingTimeId: breakfast,
		Recipe: plansv1.RecipeServings_builder{RecipeId: porridge.GetRecipe().GetId(), Servings: 1}.Build(),
		Weekdays: []plansv1.Weekday{
			plansv1.Weekday_WEEKDAY_MONDAY, plansv1.Weekday_WEEKDAY_TUESDAY, plansv1.Weekday_WEEKDAY_WEDNESDAY,
			plansv1.Weekday_WEEKDAY_THURSDAY, plansv1.Weekday_WEEKDAY_FRIDAY,
		},
	}.Build())

	setEaten := func(st *plansv1.Staple, date string, eaten bool) (*plansv1.SetStapleEatenResponse, error) {
		return e.plans.SetStapleEaten(ctx, plansv1.SetStapleEatenRequest_builder{Id: st.GetId(), Date: date, Eaten: eaten}.Build())
	}
	mustSetEaten := func(st *plansv1.Staple, date string, eaten bool) *plansv1.SetStapleEatenResponse {
		t.Helper()
		resp, err := setEaten(st, date, eaten)
		if err != nil {
			t.Fatalf("SetStapleEaten: %v", err)
		}
		return resp
	}
	changes := func(resp *plansv1.SetStapleEatenResponse) []string {
		var got []string
		for _, c := range resp.GetPantryChanges() {
			got = append(got, c.GetFoodName()+" "+formatGrams(c.GetGrams()))
		}
		return got
	}

	// Skipped on Monday, marking it eaten stops skipping it, and takes the
	// milk out.
	if _, err := e.plans.SkipStaple(ctx, plansv1.SkipStapleRequest_builder{Id: milk.GetId(), Date: monday}.Build()); err != nil {
		t.Fatalf("SkipStaple: %v", err)
	}
	resp := mustSetEaten(milk, monday, true)
	if got, want := changes(resp), []string{"Soy milk 250"}; !slices.Equal(got, want) || resp.GetPantryRestored() {
		t.Errorf("got changes %v, want %v", got, want)
	}
	if got := resp.GetStaple().GetSkippedDates(); len(got) != 0 {
		t.Errorf("got skipped %v, want it eaten on Monday", got)
	}
	// Marking it again changes nothing, and it can't be skipped while eaten.
	if got := changes(mustSetEaten(milk, monday, true)); len(got) != 0 {
		t.Errorf("got changes %v marking eaten twice, want none", got)
	}
	if _, err := e.plans.SkipStaple(ctx, plansv1.SkipStapleRequest_builder{Id: milk.GetId(), Date: monday}.Build()); connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("got %v skipping a date marked eaten, want FailedPrecondition", err)
	}

	// The porridge takes half the recipe out, as far as it's in stock: the
	// 30 g of berries there are of the 50 g.
	resp = mustSetEaten(weekdayPorridge, monday, true)
	if got, want := changes(resp), []string{"Berries 30", "Oats 60"}; !slices.Equal(got, want) {
		t.Errorf("got changes %v, want %v", got, want)
	}
	if got, want := stock(listPantry(t, e)), []string{"Oats 440 g", "Soy milk 750 g"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Not on Saturday.
	if _, err := setEaten(weekdayPorridge, saturday, true); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("got %v marking eaten on a day it isn't on, want InvalidArgument", err)
	}

	// The shopping list doesn't buy for Monday's, which are out of the
	// pantry already: 4 more days of porridge, and 6 of soy milk, which the
	// 750 ml left covers 3 of.
	created, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: monday}.Build())
	if err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}
	want := []string{" · Berries · 200 g", " · Soy milk · 750 ml"}
	if got := shoppingLines(created.GetList()); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Un-marking puts back what was taken out.
	resp = mustSetEaten(weekdayPorridge, monday, false)
	if got, want := changes(resp), []string{"Berries 30", "Oats 60"}; !slices.Equal(got, want) || !resp.GetPantryRestored() {
		t.Errorf("got changes %v, want %v put back", got, want)
	}
	if got, want := stock(listPantry(t, e)), []string{"Berries 30 g", "Oats 500 g", "Soy milk 750 g"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// The week shows which dates were eaten.
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	var eaten []string
	for _, day := range week.GetDays() {
		for _, person := range day.GetPeople() {
			for _, et := range person.GetEatingTimes() {
				for _, p := range et.GetPortions() {
					if p.GetEaten() {
						eaten = append(eaten, day.GetDate()+" "+p.GetName())
					}
				}
			}
		}
	}
	if want := []string{monday + " Soy milk"}; !slices.Equal(eaten, want) {
		t.Errorf("got eaten %v, want %v", eaten, want)
	}
}
