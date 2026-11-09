package services_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/store"
)

// slotSummary describes a slot of a suggested plan, e.g. "2026-11-02
// dinner: Curry", or its state when it has no recipe.
func slotSummary(s *suggestionsv1.PlanSlot) string {
	what := s.GetState().String()
	if s.HasSuggestion() {
		what = s.GetSuggestion().GetRecipeName()
	}
	if names := s.GetPlannedRecipeNames(); len(names) > 0 {
		what = "planned " + names[0]
	}
	return s.GetDate() + " " + s.GetMeal().String() + ": " + what
}

func planSummary(resp *suggestionsv1.SuggestPlanResponse) []string {
	var lines []string
	for _, s := range resp.GetSlots() {
		lines = append(lines, slotSummary(s))
	}
	return lines
}

func TestSuggestPlan(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// Ben eats three regular meals, and the household has four dinners and
	// a breakfast. The curries are quick; the stew is slow.
	b := ben()
	b.SetUserId(store.UUIDToString(e.userID))
	createPerson(t, e, b)
	chickpeas := customFood(t, e, "Chickpeas", 139, 7, 23, 0, 2.6, nil)
	tofu := customFood(t, e, "Tofu", 144, 15, 3, 0.6, 9, nil)
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	newRecipe := func(name string, tags []string, minutes int32, f *foodsv1.Food) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name:        name,
			Servings:    2,
			Tags:        tags,
			CookSeconds: minutes * 60,
			Ingredients: []*recipesv1.Ingredient{ingredient(f.GetId(), 400, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	curry := newRecipe("Chickpea curry", []string{"dinner"}, 20, chickpeas)
	newRecipe("Chickpea stew", []string{"dinner", "slow"}, 180, chickpeas)
	newRecipe("Tofu stir-fry", []string{"dinner"}, 15, tofu)
	newRecipe("Tofu scramble", []string{"dinner"}, 15, tofu)
	porridge := newRecipe("Porridge", []string{"breakfast"}, 10, oats)

	// Plan three dinners: each gets a different recipe, with
	// alternatives, and the seed to plan the same again.
	req := suggestionsv1.SuggestPlanRequest_builder{
		StartDate: monday,
		Days:      3,
		Meals:     []peoplev1.HouseholdMeal{dinner},
		Seed:      42,
	}.Build()
	resp, err := e.suggestions.SuggestPlan(ctx, req)
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if len(resp.GetSlots()) != 3 || resp.GetSeed() != 42 || resp.GetPlansTried() < 1 || resp.GetRecipeCount() != 5 {
		t.Fatalf("got %v, %d plans tried from %d recipes, want 3 dinners", planSummary(resp), resp.GetPlansTried(), resp.GetRecipeCount())
	}
	var names []string
	var total float64
	for _, s := range resp.GetSlots() {
		if s.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SUGGESTED || len(s.GetAlternatives()) == 0 {
			t.Errorf("got %v with %d alternatives, want a suggestion with alternatives", slotSummary(s), len(s.GetAlternatives()))
		}
		names = append(names, s.GetSuggestion().GetRecipeName())
		total += s.GetSuggestion().GetScore()
	}
	if slices.Contains(names, "Porridge") || len(slices.Compact(slices.Sorted(slices.Values(names)))) != 3 {
		t.Errorf("got dinners %v, want 3 different dinners", names)
	}
	if diff := total - resp.GetScore(); diff > 0.05 || diff < -0.05 {
		t.Errorf("got score %v, want the slots' total %v", resp.GetScore(), total)
	}
	// The same seed plans the same.
	again, err := e.suggestions.SuggestPlan(ctx, req)
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if got, want := planSummary(again), planSummary(resp); !slices.Equal(got, want) {
		t.Errorf("got %v with the same seed, want %v", got, want)
	}

	// Keep porridge for Monday's dinner, whatever its tags, leave Tuesday
	// empty, and only plan quick recipes without the "slow" tag.
	resp, err = e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: monday,
		Days:      3,
		Meals:     []peoplev1.HouseholdMeal{dinner},
		Criteria: suggestionsv1.PlanCriteria_builder{
			MaxTotalSeconds:  30 * 60,
			ExcludeRecipeIds: []string{curry.GetId()},
			Favorites:        suggestionsv1.Emphasis_EMPHASIS_HIGH,
		}.Build(),
		Choices: []*suggestionsv1.PlanChoice{
			suggestionsv1.PlanChoice_builder{Date: monday, Meal: dinner, RecipeId: porridge.GetId()}.Build(),
			suggestionsv1.PlanChoice_builder{Date: tuesday, Meal: dinner}.Build(),
		},
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	slots := resp.GetSlots()
	if got := slots[0]; got.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_KEPT || got.GetSuggestion().GetRecipeName() != "Porridge" {
		t.Errorf("got %v, want the kept porridge", slotSummary(got))
	}
	if got := slots[1]; got.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SKIPPED || got.HasSuggestion() {
		t.Errorf("got %v, want Tuesday left empty", slotSummary(got))
	}
	if got := slots[2].GetSuggestion().GetRecipeName(); got != "Tofu stir-fry" && got != "Tofu scramble" {
		t.Errorf("got %v on Wednesday, want a quick tofu dinner, not the excluded curry or the slow stew", got)
	}
	if resp.GetRecipeCount() != 3 {
		t.Errorf("got %d recipes meeting the criteria, want 3: the tofu ones and the porridge", resp.GetRecipeCount())
	}

	// Save the first plan: its three dinners are planned, for Ben.
	var meals []*plansv1.MealToPlan
	for _, s := range again.GetSlots() {
		meals = append(meals, plansv1.MealToPlan_builder{RecipeId: s.GetSuggestion().GetRecipeId(), Date: s.GetDate(), Meal: s.GetMeal()}.Build())
	}
	saved, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: meals}.Build())
	if err != nil {
		t.Fatalf("PlanMeals: %v", err)
	}
	if len(saved.GetCooks()) != 3 || saved.GetPortionCount() != 3 || saved.GetCooks()[0].GetDate() != monday {
		t.Errorf("got %v and %d portions, want 3 cooks from Monday, each with Ben's portion", saved.GetCooks(), saved.GetPortionCount())
	}
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if len(week.GetCooks()) != 3 {
		t.Errorf("got %d cooks in the week, want the 3 planned", len(week.GetCooks()))
	}
	// Planned again, the saved dinners are planned already, and Thursday's
	// gets a suggestion.
	resp, err = e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: wednesday, Days: 2, Meals: []peoplev1.HouseholdMeal{dinner},
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if got := resp.GetSlots(); len(got) != 2 || got[0].GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_PLANNED ||
		got[1].GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SUGGESTED || resp.GetSeed() == 0 {
		t.Errorf("got %v with seed %d, want Wednesday planned and Thursday suggested, with a new seed", planSummary(resp), resp.GetSeed())
	}
	// Without meals, every meal Ben eats is planned.
	resp, err = e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{StartDate: friday, Days: 1}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if got := resp.GetSlots(); len(got) != 3 || got[0].GetMeal() != breakfast || got[2].GetMeal() != dinner {
		t.Errorf("got %v, want Friday's breakfast, lunch and dinner", planSummary(resp))
	}

	// Bad requests fail.
	for name, tt := range map[string]struct {
		req  *suggestionsv1.SuggestPlanRequest
		code connect.Code
	}{
		"no days":       {suggestionsv1.SuggestPlanRequest_builder{StartDate: monday}.Build(), connect.CodeInvalidArgument},
		"too many days": {suggestionsv1.SuggestPlanRequest_builder{StartDate: monday, Days: 15}.Build(), connect.CodeInvalidArgument},
		"bad date":      {suggestionsv1.SuggestPlanRequest_builder{StartDate: "Monday", Days: 1}.Build(), connect.CodeInvalidArgument},
		"choice outside the days": {suggestionsv1.SuggestPlanRequest_builder{StartDate: monday, Days: 1, Choices: []*suggestionsv1.PlanChoice{
			suggestionsv1.PlanChoice_builder{Date: friday, Meal: dinner}.Build(),
		}}.Build(), connect.CodeInvalidArgument},
		"two choices for a slot": {suggestionsv1.SuggestPlanRequest_builder{StartDate: monday, Days: 1, Choices: []*suggestionsv1.PlanChoice{
			suggestionsv1.PlanChoice_builder{Date: monday, Meal: dinner}.Build(),
			suggestionsv1.PlanChoice_builder{Date: monday, Meal: dinner}.Build(),
		}}.Build(), connect.CodeInvalidArgument},
		"nothing meets the criteria": {suggestionsv1.SuggestPlanRequest_builder{StartDate: saturday, Days: 1, Criteria: suggestionsv1.PlanCriteria_builder{
			MaxTotalSeconds: 60,
		}.Build()}.Build(), connect.CodeFailedPrecondition},
	} {
		_, err := e.suggestions.SuggestPlan(ctx, tt.req)
		if connect.CodeOf(err) != tt.code {
			t.Errorf("%s: got %v, want %v", name, err, tt.code)
		}
	}
	_, err = e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: curry.GetId(), Date: friday, Meal: dinner}.Build(),
		plansv1.MealToPlan_builder{RecipeId: curry.GetId(), Date: friday, Meal: dinner}.Build(),
	}}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// The other household can't plan with these recipes, and has nobody
	// to plan for.
	_, err = e.otherPlans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: []*plansv1.MealToPlan{
		plansv1.MealToPlan_builder{RecipeId: curry.GetId(), Date: friday, Meal: dinner}.Build(),
	}}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherSuggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{StartDate: monday, Days: 7}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.anonSuggestions.SuggestPlan(ctx, req)
	wantCode(t, err, connect.CodeUnauthenticated)
}

// personalSlotSummary describes a slot of one person's eating time, e.g.
// "2026-11-02 Cleo Mid-morning: Hummus plate".
func personalSlotSummary(s *suggestionsv1.PlanSlot) string {
	what := s.GetState().String()
	if s.HasSuggestion() {
		what = s.GetSuggestion().GetRecipeName()
	}
	return s.GetDate() + " " + s.GetPersonName() + " " + s.GetEatingTimeName() + ": " + what
}

// TestSuggestPlanPersonalEatingTimes checks that a plan covers each
// person's own eating times, like snacks, with the shared meals, and only
// puts breakfasts at breakfast.
func TestSuggestPlanPersonalEatingTimes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	createPerson(t, e, ben())
	cleo := createPerson(t, e, peoplev1.Person_builder{
		Name: "Cleo",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Breakfast", mainMeal, "07:30", breakfast),
			eatingTime("Mid-morning", smallMeal, "10:00", noMeal),
			eatingTime("Lunch", mainMeal, "12:30", lunch),
			eatingTime("Dinner", mainMeal, "19:00", dinner),
			eatingTime("Evening", smallMeal, "21:00", noMeal),
		},
	}.Build())
	beans := customFood(t, e, "Beans", 130, 9, 24, 0, 0.5, nil)
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	chickpeas := customFood(t, e, "Chickpeas", 139, 7, 23, 0, 2.6, nil)
	recipeIDs := map[string]string{}
	for _, r := range []struct {
		name string
		tags []string
		food *foodsv1.Food
	}{
		{"Porridge", []string{"breakfast"}, oats},
		{"Granola", []string{"Breakfast"}, oats},
		{"Hummus plate", []string{"snack"}, chickpeas},
		{"Roasted chickpeas", []string{"snack"}, chickpeas},
		// The chili has no meal tag, so it's a main meal, never breakfast,
		// however well it scores there.
		{"Chili", nil, beans},
		{"Bean curry", []string{"dinner"}, beans},
		{"Bean salad", []string{"lunch"}, beans},
	} {
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: r.name, Servings: 4, Tags: r.tags, Favorite: r.name == "Chili",
			Ingredients: []*recipesv1.Ingredient{ingredient(r.food.GetId(), 400, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		recipeIDs[r.name] = resp.GetRecipe().GetId()
	}
	// Breakfast only takes breakfasts, and snacks snacks, or the rest of
	// an earlier meal; lunch and dinner take anything.
	var everything []string
	for name := range recipeIDs {
		everything = append(everything, name)
	}
	snacks := []string{"Hummus plate", "Roasted chickpeas"}
	allowed := map[string][]string{
		breakfast.String(): {"Porridge", "Granola"},
		lunch.String():     everything,
		dinner.String():    everything,
		"Mid-morning":      snacks,
		"Evening":          snacks,
	}
	fits := func(slot string, sg *suggestionsv1.Suggestion) bool {
		return slices.Contains(allowed[slot], sg.GetRecipeName()) || (slot == "Mid-morning" || slot == "Evening") && sg.HasLeftoverOf()
	}

	// Two days of the whole household: each day's three shared meals, then
	// Cleo's two snacks.
	resp, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{StartDate: monday, Days: 2, Seed: 7}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	var got []string
	for _, s := range resp.GetSlots() {
		slot := s.GetMeal().String()
		if s.GetEatingTimeId() != "" {
			slot = s.GetEatingTimeName()
			if s.GetPersonId() != cleo.GetId() || s.GetPersonName() != "Cleo" || s.GetMeal() != noMeal {
				t.Errorf("got %v, want one of Cleo's eating times", personalSlotSummary(s))
			}
		}
		got = append(got, s.GetDate()+" "+slot)
		if sg := s.GetSuggestion(); !fits(slot, sg) {
			t.Errorf("got %s at %s on %s, want one of %v", sg.GetRecipeName(), slot, s.GetDate(), allowed[slot])
		}
		for _, alt := range s.GetAlternatives() {
			if !fits(slot, alt) {
				t.Errorf("got alternative %s at %s, want one of %v", alt.GetRecipeName(), slot, allowed[slot])
			}
		}
	}
	want := []string{
		monday + " HOUSEHOLD_MEAL_BREAKFAST", monday + " HOUSEHOLD_MEAL_LUNCH", monday + " HOUSEHOLD_MEAL_DINNER",
		monday + " Mid-morning", monday + " Evening",
		tuesday + " HOUSEHOLD_MEAL_BREAKFAST", tuesday + " HOUSEHOLD_MEAL_LUNCH", tuesday + " HOUSEHOLD_MEAL_DINNER",
		tuesday + " Mid-morning", tuesday + " Evening",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got slots %v, want %v", got, want)
	}

	// Without the snacks, only the shared meals.
	shared, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: monday, Days: 2, SkipPersonalEatingTimes: true,
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if n := len(shared.GetSlots()); n != 6 {
		t.Errorf("got %d slots, want the 6 shared meals", n)
	}

	// Keeping a snack, and leaving another empty.
	monMorning, monEvening := resp.GetSlots()[3], resp.GetSlots()[4]
	kept, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: monday, Days: 1,
		Choices: []*suggestionsv1.PlanChoice{
			suggestionsv1.PlanChoice_builder{Date: monday, PersonId: cleo.GetId(), EatingTimeId: monMorning.GetEatingTimeId(), RecipeId: recipeIDs["Roasted chickpeas"]}.Build(),
			suggestionsv1.PlanChoice_builder{Date: monday, PersonId: cleo.GetId(), EatingTimeId: monEvening.GetEatingTimeId()}.Build(),
		},
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if s := kept.GetSlots()[3]; s.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_KEPT || s.GetSuggestion().GetRecipeName() != "Roasted chickpeas" {
		t.Errorf("got %v, want the kept roasted chickpeas", personalSlotSummary(s))
	}
	if s := kept.GetSlots()[4]; s.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SKIPPED {
		t.Errorf("got %v, want the evening left empty", personalSlotSummary(s))
	}
	// A shared meal's eating time isn't one person's.
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	var cleoBreakfast string
	for _, p := range week.GetDays()[0].GetPeople() {
		if p.GetPersonId() == cleo.GetId() {
			cleoBreakfast = p.GetEatingTimes()[0].GetEatingTime().GetId()
		}
	}
	_, err = e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: monday, Days: 1,
		Choices: []*suggestionsv1.PlanChoice{
			suggestionsv1.PlanChoice_builder{Date: monday, PersonId: cleo.GetId(), EatingTimeId: cleoBreakfast}.Build(),
		},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Without breakfast recipes, breakfast can't be planned, and says why,
	// but the snacks still can.
	unfilled, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{
		StartDate: monday, Days: 1, Criteria: suggestionsv1.PlanCriteria_builder{ExcludeTags: []string{"breakfast"}}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	if s := unfilled.GetSlots()[0]; s.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_UNFILLED || !strings.Contains(s.GetUnfilledReason(), `"breakfast"`) {
		t.Errorf("got %v (%q), want it unfilled, as no recipe is tagged breakfast", slotSummary(s), s.GetUnfilledReason())
	}
	if s := unfilled.GetSlots()[3]; !s.HasSuggestion() {
		t.Errorf("got %v, want a snack", personalSlotSummary(s))
	}

	// Saving the plan plans Cleo's snacks as her own portions, and planned
	// again, they're planned already.
	var meals []*plansv1.MealToPlan
	for _, s := range resp.GetSlots() {
		meals = append(meals, plansv1.MealToPlan_builder{
			RecipeId: s.GetSuggestion().GetRecipeId(), Date: s.GetDate(), Meal: s.GetMeal(),
			PersonId: s.GetPersonId(), EatingTimeId: s.GetEatingTimeId(),
		}.Build())
	}
	saved, err := e.plans.PlanMeals(ctx, plansv1.PlanMealsRequest_builder{Meals: meals}.Build())
	if err != nil {
		t.Fatalf("PlanMeals: %v", err)
	}
	// Each shared meal feeds both, and each snack Cleo alone.
	if len(saved.GetCooks()) != 10 || saved.GetPortionCount() != 6*2+4 {
		t.Errorf("got %d cooks and %d portions, want 10 cooks and 16 portions", len(saved.GetCooks()), saved.GetPortionCount())
	}
	week, err = e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	for _, p := range week.GetDays()[0].GetPeople() {
		if p.GetPersonId() != cleo.GetId() {
			continue
		}
		snack := p.GetEatingTimes()[1]
		if len(snack.GetPortions()) != 1 || snack.GetPortions()[0].GetName() != monMorning.GetSuggestion().GetRecipeName() || snack.GetPortions()[0].GetServings() != 1 {
			t.Errorf("got %v at Cleo's mid-morning, want a serving of %s", snack.GetPortions(), monMorning.GetSuggestion().GetRecipeName())
		}
	}
	again, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{StartDate: monday, Days: 1}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	for _, s := range again.GetSlots() {
		if s.GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_PLANNED {
			t.Errorf("got %v, want it planned already", personalSlotSummary(s))
		}
	}
}

// TestSuggestPlanSnackFilledByMeal checks that a person's snack the rest
// of a shared meal spills into isn't planned too.
func TestSuggestPlanSnackFilledByMeal(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	// Dee eats at most 200 g at a time.
	createPerson(t, e, peoplev1.Person_builder{
		Name: "Dee",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Breakfast", mainMeal, "07:30", breakfast),
			eatingTime("Mid-morning", smallMeal, "10:00", noMeal),
		},
		Rules: peoplev1.EatingRules_builder{MaxPortionGrams: new(200.0)}.Build(),
	}.Build())
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	for _, r := range []struct {
		name  string
		tag   string
		grams float64
	}{{"Big porridge", "breakfast", 1200}, {"Oat bites", "snack", 200}} {
		if _, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: r.name, Servings: 4, Tags: []string{r.tag},
			Ingredients: []*recipesv1.Ingredient{ingredient(oats.GetId(), r.grams, grams)},
		}.Build()}.Build()); err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
	}
	// A serving of porridge is 300 g: 200 g at breakfast, and the rest at
	// mid-morning, which needs no snack.
	resp, err := e.suggestions.SuggestPlan(ctx, suggestionsv1.SuggestPlanRequest_builder{StartDate: monday, Days: 1}.Build())
	if err != nil {
		t.Fatalf("SuggestPlan: %v", err)
	}
	slots := resp.GetSlots()
	if len(slots) != 2 || slots[0].GetSuggestion().GetRecipeName() != "Big porridge" ||
		slots[1].GetState() != suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_FILLED_BY_MEAL {
		var got []string
		for _, s := range slots {
			got = append(got, personalSlotSummary(s))
		}
		t.Errorf("got %v, want the porridge, filling the mid-morning snack too", got)
	}
}
