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
	"github.com/bs-tools/mealplann.ing/internal/store"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// The week of the tests, from Monday 2 November 2026.
const (
	monday    = "2026-11-02"
	tuesday   = "2026-11-03"
	wednesday = "2026-11-04"
	thursday  = "2026-11-05"
	friday    = "2026-11-06"
	saturday  = "2026-11-07"
)

// customFood creates a custom food with energy and macronutrients per
// 100 g, and nothing else known.
func customFood(t *testing.T, e *env, name string, kcal, protein, carbohydrate, sugars, fat float64, edit func(*foodsv1.Food)) *foodsv1.Food {
	t.Helper()
	f := foodsv1.Food_builder{
		Name: name,
		Nutrients: foodsv1.Nutrients_builder{
			EnergyKcal:        new(kcal),
			ProteinGrams:      new(protein),
			CarbohydrateGrams: new(carbohydrate),
			SugarsGrams:       new(sugars),
			FatGrams:          new(fat),
		}.Build(),
	}.Build()
	if edit != nil {
		edit(f)
	}
	return createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{Food: f}.Build())
}

// personDay returns a person's day in a week.
func personDay(t *testing.T, week *plansv1.GetWeekResponse, date, personID string) *plansv1.PersonDay {
	t.Helper()
	for _, d := range week.GetDays() {
		if d.GetDate() != date {
			continue
		}
		for _, p := range d.GetPeople() {
			if p.GetPersonId() == personID {
				return p
			}
		}
	}
	t.Fatalf("no day %s for person %s", date, personID)
	return nil
}

// planned returns what's planned at a person's eating time on a day.
func planned(t *testing.T, week *plansv1.GetWeekResponse, date, personID, eatingTime string) *plansv1.PlannedEatingTime {
	t.Helper()
	for _, et := range personDay(t, week, date, personID).GetEatingTimes() {
		if et.GetEatingTime().GetName() == eatingTime {
			return et
		}
	}
	t.Fatalf("no eating time %s on %s", eatingTime, date)
	return nil
}

func portionNames(et *plansv1.PlannedEatingTime) []string {
	var names []string
	for _, p := range et.GetPortions() {
		names = append(names, p.GetName())
	}
	return names
}

func warningKinds(warnings []*plansv1.Warning) []plansv1.WarningKind {
	var kinds []plansv1.WarningKind
	for _, w := range warnings {
		kinds = append(kinds, w.GetKind())
	}
	return kinds
}

func getWeek(t *testing.T, c interface {
	GetWeek(context.Context, *plansv1.GetWeekRequest) (*plansv1.GetWeekResponse, error)
},
) *plansv1.GetWeekResponse {
	t.Helper()
	week, err := c.GetWeek(context.Background(), plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	return week
}

func TestPlans(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// The household: Ben, and Anna with her rules after a gastrectomy.
	b := ben()
	b.SetUserId(store.UUIDToString(e.userID))
	ben := createPerson(t, e, b)
	anna := createPerson(t, e, anna())

	// Its foods and a dal with rice of 4 servings of 300 g, which keeps 3
	// days. Rice is a fast carb.
	lentils := customFood(t, e, "Lentils", 116, 9, 20, 2, 0.4, nil)
	rice := customFood(t, e, "White rice", 130, 2.7, 28, 0.1, 0.3, func(f *foodsv1.Food) { f.SetFastCarb(true) })
	tofu := customFood(t, e, "Tofu", 144, 15, 2, 0.5, 8, nil)
	almonds := customFood(t, e, "Almonds", 579, 21, 22, 4.4, 50, nil)
	soyMilk := customFood(t, e, "Soy milk", 43, 3.3, 1.8, 1, 1.8, func(f *foodsv1.Food) { f.SetGramsPerMilliliter(1.03) })
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Name:        "Dal with rice",
			Servings:    4,
			FridgeDays:  new(int32(3)),
			Ingredients: []*recipesv1.Ingredient{ingredient(lentils.GetId(), 600, grams), ingredient(rice.GetId(), 600, grams)},
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	dal := created.GetRecipe()

	// Plan the shared dinner on Monday: Ben gets a serving, and Anna 200 g
	// of hers at dinner and the rest in the evening.
	meal, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{
		RecipeId: dal.GetId(),
		Date:     monday,
		Meal:     dinner,
	}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	cook := meal.GetCook()
	if cook.GetServings() != 2 || cook.GetDate() != monday || cook.GetMeal() != dinner || cook.GetRecipeName() != "Dal with rice" || meal.GetUnassignedServings() != 0 {
		t.Errorf("got cook %v with %v unassigned, want 2 servings on Monday", cook, meal.GetUnassignedServings())
	}
	type share struct {
		person, eatingTime, date string
		servings                 float64
	}
	var shares []share
	for _, p := range meal.GetPortions() {
		shares = append(shares, share{p.GetPersonId(), p.GetEatingTimeId(), p.GetDate(), p.GetCook().GetServings()})
		if p.GetCook().GetCookId() != cook.GetId() || p.GetName() != "Dal with rice" {
			t.Errorf("got portion %v, want a share of the cook", p)
		}
	}
	wantShares := []share{
		{anna.GetId(), eatingTimeID(t, anna, "Dinner"), monday, 0.66},
		{anna.GetId(), eatingTimeID(t, anna, "Evening"), monday, 0.34},
		{ben.GetId(), eatingTimeID(t, ben, "Dinner"), monday, 1},
	}
	if !slices.Equal(shares, wantShares) {
		t.Errorf("got shares %v, want %v", shares, wantShares)
	}
	// Anna's portions are too high in carbohydrate, not balanced, and have
	// fast carbs, and the evening's is low in protein. Ben has no rules.
	wantKinds := []plansv1.WarningKind{
		plansv1.WarningKind_WARNING_KIND_CARBOHYDRATE_HIGH,
		plansv1.WarningKind_WARNING_KIND_CARBOHYDRATE_UNPAIRED,
		plansv1.WarningKind_WARNING_KIND_FAST_CARBS,
		plansv1.WarningKind_WARNING_KIND_PROTEIN_LOW,
		plansv1.WarningKind_WARNING_KIND_CARBOHYDRATE_UNPAIRED,
		plansv1.WarningKind_WARNING_KIND_FAST_CARBS,
	}
	if got := warningKinds(meal.GetWarnings()); !slices.Equal(got, wantKinds) {
		t.Errorf("got warnings %v, want %v", meal.GetWarnings(), wantKinds)
	}
	for _, w := range meal.GetWarnings() {
		if w.GetPersonId() != anna.GetId() || w.GetDate() != monday || w.GetEatingTimeId() == "" || w.GetMessage() == "" {
			t.Errorf("got warning %v, want one about Anna's Monday", w)
		}
	}
	if w := meal.GetWarnings()[0]; w.GetValue() != 47.52 || w.GetLimit() != 30 ||
		w.GetMessage() != "Dinner has 47.5 g of carbohydrate, over the maximum of 30 g: plan a smaller portion of the starchy or sweet foods, or swap some for protein or vegetables." {
		t.Errorf("got warning %v, want 47.52 g of carbohydrate over 30 g", w)
	}

	// Planning a meal for fewer servings than its portions, or nobody, fails.
	_, err = e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: dal.GetId(), Date: tuesday, Meal: dinner, Servings: 1}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: dal.GetId(), Date: tuesday}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{
		RecipeId: dal.GetId(), Date: tuesday, Meal: dinner, PersonIds: []string{store.UUIDToString(store.NewUUID())},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Staples: Anna's almonds every mid-morning, and Ben's soy milk on
	// weekday breakfasts.
	almondsStaple, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{
		Staple: plansv1.Staple_builder{
			PersonId:     anna.GetId(),
			EatingTimeId: eatingTimeID(t, anna, "Mid-morning"),
			Food:         plansv1.FoodAmount_builder{FoodId: almonds.GetId(), Amount: 30, Unit: grams}.Build(),
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}
	weekdays := []plansv1.Weekday{
		plansv1.Weekday_WEEKDAY_MONDAY, plansv1.Weekday_WEEKDAY_TUESDAY, plansv1.Weekday_WEEKDAY_WEDNESDAY,
		plansv1.Weekday_WEEKDAY_THURSDAY, plansv1.Weekday_WEEKDAY_FRIDAY,
	}
	milkStaple, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{
		Staple: plansv1.Staple_builder{
			PersonId:     ben.GetId(),
			EatingTimeId: eatingTimeID(t, ben, "Breakfast"),
			Food:         plansv1.FoodAmount_builder{FoodId: soyMilk.GetId(), Amount: 250, Unit: milliliters}.Build(),
			Weekdays:     weekdays,
			StartDate:    monday,
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}
	if s := milkStaple.GetStaple(); s.GetName() != "Soy milk" || !slices.Equal(s.GetWeekdays(), weekdays) || s.GetStartDate() != monday || s.GetEndDate() != "" {
		t.Errorf("got staple %v, want Ben's soy milk", s)
	}
	// Anna skips her almonds on Wednesday, and on Thursday, then changes her
	// mind about Thursday.
	for _, date := range []string{wednesday, wednesday, thursday} {
		if _, err := e.plans.SkipStaple(ctx, plansv1.SkipStapleRequest_builder{Id: almondsStaple.GetStaple().GetId(), Date: date}.Build()); err != nil {
			t.Fatalf("SkipStaple: %v", err)
		}
	}
	unskipped, err := e.plans.UnskipStaple(ctx, plansv1.UnskipStapleRequest_builder{Id: almondsStaple.GetStaple().GetId(), Date: thursday}.Build())
	if err != nil {
		t.Fatalf("UnskipStaple: %v", err)
	}
	if got := unskipped.GetStaple().GetSkippedDates(); !slices.Equal(got, []string{wednesday}) {
		t.Errorf("got skipped dates %v, want Wednesday", got)
	}

	// Anna has tofu in the afternoon, without a recipe.
	tofuPortion, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId:     anna.GetId(),
			EatingTimeId: eatingTimeID(t, anna, "Mid-afternoon"),
			Date:         monday,
			Food:         plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: 100, Unit: grams}.Build(),
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	if tofuPortion.GetPortion().GetName() != "Tofu" {
		t.Errorf("got %v, want the tofu", tofuPortion.GetPortion())
	}

	// The dal is cooked for 4 servings, and Ben has the leftovers for lunch
	// on Wednesday, and on Friday, when it no longer keeps.
	updatedCook, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetId(), Servings: 4}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"servings"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	if c := updatedCook.GetCook(); c.GetServings() != 4 || c.GetDate() != monday || c.GetMeal() != dinner {
		t.Errorf("got %v, want only the servings changed", c)
	}
	for _, date := range []string{wednesday, friday} {
		_, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
			Portion: plansv1.Portion_builder{
				PersonId:     ben.GetId(),
				EatingTimeId: eatingTimeID(t, ben, "Lunch"),
				Date:         date,
				Cook:         plansv1.CookShare_builder{CookId: cook.GetId(), Servings: 1}.Build(),
			}.Build(),
		}.Build())
		if err != nil {
			t.Fatalf("CreatePortion: %v", err)
		}
	}

	// The week shows it all.
	week := getWeek(t, e.plans)
	if len(week.GetDays()) != 7 || week.GetDays()[6].GetDate() != "2026-11-08" || len(week.GetDays()[0].GetPeople()) != 2 ||
		week.GetDays()[0].GetPeople()[0].GetPersonName() != "Anna" {
		t.Fatalf("got %d days, want 7 days of Anna and Ben", len(week.GetDays()))
	}
	// Anna's Monday: almonds, tofu, and dal at dinner and in the evening.
	if got := planned(t, week, monday, anna.GetId(), "Mid-morning"); !slices.Equal(portionNames(got), []string{"Almonds"}) ||
		got.GetPortions()[0].GetStapleId() != almondsStaple.GetStaple().GetId() || got.GetGrams() != 30 {
		t.Errorf("got Anna's mid-morning %v, want the almonds staple", got)
	}
	annaDinner := planned(t, week, monday, anna.GetId(), "Dinner")
	if p := annaDinner.GetPortions(); len(p) != 1 || p[0].GetGrams() != 198 || p[0].GetServings() != 0.66 || p[0].GetLeftover() || p[0].GetCookId() != cook.GetId() {
		t.Errorf("got Anna's dinner %v, want 198 g of the dal", annaDinner)
	}
	if got := annaDinner.GetNutrients(); got.GetCarbohydrateGrams() != 47.52 || got.GetProteinGrams() != 11.583 || got.HasIronMilligrams() {
		t.Errorf("got Anna's dinner nutrients %v, want 47.52 g of carbohydrate and 11.583 g of protein, and no minerals", got)
	}
	if got := warningKinds(planned(t, week, monday, anna.GetId(), "Mid-morning").GetWarnings()); !slices.Equal(got, []plansv1.WarningKind{plansv1.WarningKind_WARNING_KIND_PROTEIN_LOW}) {
		t.Errorf("got mid-morning warnings %v, want protein low", got)
	}
	if got := planned(t, week, monday, anna.GetId(), "Mid-afternoon"); len(got.GetWarnings()) != 0 || got.GetPortions()[0].GetFoodId() != tofu.GetId() {
		t.Errorf("got Anna's afternoon %v, want the tofu without warnings", got)
	}
	// Her day's totals, and her targets: protein is low, but two eating
	// times are still unplanned, and nothing has iron data.
	annaMonday := personDay(t, week, monday, anna.GetId())
	if annaMonday.GetGrams() != 430 || annaMonday.GetNutrients().GetProteinGrams() != 38.85 || annaMonday.GetNutrients().HasIronMilligrams() ||
		!slices.Contains(annaMonday.GetIncompleteNutrients(), "iron_milligrams") || slices.Contains(annaMonday.GetIncompleteNutrients(), "protein_grams") {
		t.Errorf("got Anna's Monday %v g, %v, want 430 g with 38.85 g of protein", annaMonday.GetGrams(), annaMonday.GetNutrients())
	}
	if got := annaMonday.GetWarnings(); len(got) != 2 ||
		got[0].GetKind() != plansv1.WarningKind_WARNING_KIND_TARGET_LOW || got[0].GetNutrient() != foodsv1.Nutrient_NUTRIENT_PROTEIN_GRAMS || !got[0].GetUncertain() ||
		got[1].GetNutrient() != foodsv1.Nutrient_NUTRIENT_IRON_MILLIGRAMS || !got[1].GetUncertain() || !strings.HasPrefix(got[1].GetMessage(), "Iron may be low") {
		t.Errorf("got Anna's Monday warnings %v, want protein and iron maybe low", got)
	}
	// Her almonds are skipped on Wednesday only.
	if got := planned(t, week, wednesday, anna.GetId(), "Mid-morning"); len(got.GetPortions()) != 0 {
		t.Errorf("got Wednesday's mid-morning %v, want it skipped", got)
	}
	if got := planned(t, week, thursday, anna.GetId(), "Mid-morning"); len(got.GetPortions()) != 1 {
		t.Errorf("got Thursday's mid-morning %v, want the almonds", got)
	}
	// Ben's soy milk is on weekdays: 250 ml weigh 257.5 g.
	if got := planned(t, week, monday, ben.GetId(), "Breakfast"); !slices.Equal(portionNames(got), []string{"Soy milk"}) || got.GetGrams() != 257.5 {
		t.Errorf("got Ben's Monday breakfast %v, want the soy milk", got)
	}
	if got := planned(t, week, saturday, ben.GetId(), "Breakfast"); len(got.GetPortions()) != 0 {
		t.Errorf("got Ben's Saturday breakfast %v, want nothing", got)
	}
	if got := personDay(t, week, monday, ben.GetId()).GetNutrients().GetProteinGrams(); got != 26.048 {
		t.Errorf("got Ben's Monday protein %v, want 26.048 g", got)
	}
	// Ben's leftovers: fine on Wednesday, past their fridge days on Friday.
	if got := planned(t, week, wednesday, ben.GetId(), "Lunch"); len(got.GetWarnings()) != 0 || !got.GetPortions()[0].GetLeftover() || got.GetPortions()[0].GetCookDate() != monday {
		t.Errorf("got Ben's Wednesday lunch %v, want a leftover without warnings", got)
	}
	fridayLunch := planned(t, week, friday, ben.GetId(), "Lunch")
	if got := fridayLunch.GetWarnings(); len(got) != 1 || got[0].GetKind() != plansv1.WarningKind_WARNING_KIND_LEFTOVER_PAST_FRIDGE_DAYS ||
		got[0].GetCookId() != cook.GetId() || got[0].GetValue() != 4 || got[0].GetLimit() != 3 {
		t.Errorf("got Ben's Friday lunch warnings %v, want the leftover past its fridge days", got)
	}
	// The cook's 4 servings are all eaten.
	if got := week.GetCooks(); len(got) != 1 || got[0].GetAssignedServings() != 4 || len(got[0].GetWarnings()) != 0 {
		t.Errorf("got cooks %v, want the dal with 4 servings assigned", got)
	}

	// Removing Anna's evening deletes her evening portion, leaving a third of
	// a serving of the dal unassigned.
	var kept []*peoplev1.EatingTime
	for _, et := range anna.GetEatingTimes() {
		if et.GetName() != "Evening" {
			kept = append(kept, et)
		}
	}
	if _, err := e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person:     peoplev1.Person_builder{Id: anna.GetId(), EatingTimes: kept}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"eating_times"}},
	}.Build()); err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	week = getWeek(t, e.plans)
	if got := week.GetCooks()[0]; got.GetAssignedServings() != 3.66 || len(got.GetWarnings()) != 1 ||
		got.GetWarnings()[0].GetKind() != plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED || got.GetWarnings()[0].GetValue() != 0.34 ||
		got.GetWarnings()[0].GetSeverity() != plansv1.Severity_SEVERITY_INFO {
		t.Errorf("got cook %v, want 0.34 servings unassigned", got)
	}

	// Move Anna's dinner to Tuesday, making it a leftover, and change the
	// servings.
	var annaDinnerID string
	for _, p := range meal.GetPortions() {
		if p.GetPersonId() == anna.GetId() && p.GetEatingTimeId() == eatingTimeID(t, anna, "Dinner") {
			annaDinnerID = p.GetId()
		}
	}
	moved, err := e.plans.UpdatePortion(ctx, plansv1.UpdatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			Id:   annaDinnerID,
			Date: tuesday,
			Cook: plansv1.CookShare_builder{Servings: 0.5}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"date", "cook.servings"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePortion: %v", err)
	}
	if p := moved.GetPortion(); p.GetDate() != tuesday || p.GetCook().GetServings() != 0.5 || p.GetCook().GetCookId() != cook.GetId() ||
		p.GetEatingTimeId() != eatingTimeID(t, anna, "Dinner") {
		t.Errorf("got %v, want Anna's dinner on Tuesday", p)
	}
	week = getWeek(t, e.plans)
	if got := planned(t, week, tuesday, anna.GetId(), "Dinner").GetPortions(); len(got) != 1 || !got[0].GetLeftover() || got[0].GetGrams() != 150 {
		t.Errorf("got Anna's Tuesday dinner %v, want 150 g of leftovers", got)
	}
	// A portion's eating time must be its person's, and a food portion
	// has no servings to change.
	_, err = e.plans.UpdatePortion(ctx, plansv1.UpdatePortionRequest_builder{
		Portion:    plansv1.Portion_builder{Id: annaDinnerID, EatingTimeId: eatingTimeID(t, ben, "Dinner")}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"eating_time_id"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.plans.UpdatePortion(ctx, plansv1.UpdatePortionRequest_builder{
		Portion:    plansv1.Portion_builder{Id: tofuPortion.GetPortion().GetId(), Cook: plansv1.CookShare_builder{Servings: 2}.Build()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"cook.servings"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Staples change in part: Ben drinks 300 ml from now on.
	updatedStaple, err := e.plans.UpdateStaple(ctx, plansv1.UpdateStapleRequest_builder{
		Staple:     plansv1.Staple_builder{Id: milkStaple.GetStaple().GetId(), Food: plansv1.FoodAmount_builder{Amount: 300}.Build()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"food.amount"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateStaple: %v", err)
	}
	if s := updatedStaple.GetStaple(); s.GetFood().GetAmount() != 300 || s.GetFood().GetUnit() != milliliters || len(s.GetWeekdays()) != 5 {
		t.Errorf("got %v, want 300 ml on weekdays", s)
	}
	staples, err := e.plans.ListStaples(ctx, plansv1.ListStaplesRequest_builder{PersonId: anna.GetId()}.Build())
	if err != nil {
		t.Fatalf("ListStaples: %v", err)
	}
	if len(staples.GetStaples()) != 1 || staples.GetStaples()[0].GetName() != "Almonds" {
		t.Errorf("got Anna's staples %v, want the almonds", staples.GetStaples())
	}

	// Invalid plans are refused, saying what's wrong.
	_, err = e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: tuesday}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	for name, s := range map[string]*plansv1.Staple{
		"nothing eaten": plansv1.Staple_builder{PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Lunch")}.Build(),
		"weekday twice": plansv1.Staple_builder{
			PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Lunch"),
			Food:     plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: 50, Unit: grams}.Build(),
			Weekdays: []plansv1.Weekday{plansv1.Weekday_WEEKDAY_MONDAY, plansv1.Weekday_WEEKDAY_MONDAY},
		}.Build(),
		"ends before it starts": plansv1.Staple_builder{
			PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Lunch"),
			Food:      plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: 50, Unit: grams}.Build(),
			StartDate: friday, EndDate: monday,
		}.Build(),
		"no unit": plansv1.Staple_builder{
			PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Lunch"),
			Food: plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: 50}.Build(),
		}.Build(),
		"another person's eating time": plansv1.Staple_builder{
			PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, ben, "Lunch"),
			Food: plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: 50, Unit: grams}.Build(),
		}.Build(),
	} {
		_, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{Staple: s}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want InvalidArgument", name, err)
		}
	}
	_, err = e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: dal.GetId(), Date: "2026-13-01", Servings: 2}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Planned recipes and foods can't be deleted.
	_, err = e.recipes.DeleteRecipe(ctx, recipesv1.DeleteRecipeRequest_builder{Id: dal.GetId()}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: almonds.GetId()}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)

	// Another household sees none of it, and can't change or use it.
	otherWeek := getWeek(t, e.otherPlans)
	if len(otherWeek.GetCooks()) != 0 || len(otherWeek.GetDays()[0].GetPeople()) != 0 {
		t.Errorf("another household sees %v", otherWeek)
	}
	_, err = e.otherPlans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook: plansv1.Cook_builder{Id: cook.GetId(), Done: true}.Build(), UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherPlans.DeletePortion(ctx, plansv1.DeletePortionRequest_builder{Id: annaDinnerID}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherPlans.SkipStaple(ctx, plansv1.SkipStapleRequest_builder{Id: almondsStaple.GetStaple().GetId(), Date: friday}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherPlans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: dal.GetId(), Date: tuesday, Meal: dinner}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherPlans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: dal.GetId(), Date: tuesday, Servings: 2}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.otherPlans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Lunch"), Date: monday,
			Food: plansv1.FoodAmount_builder{FoodId: tofu.GetId(), Amount: 50, Unit: grams}.Build(),
		}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	otherStaples, err := e.otherPlans.ListStaples(ctx, plansv1.ListStaplesRequest_builder{}.Build())
	if err != nil || len(otherStaples.GetStaples()) != 0 {
		t.Errorf("another household lists %v, %v", otherStaples.GetStaples(), err)
	}
	// Nor can it be used here: the other household's food isn't ours.
	otherFood := createFood(t, e.otherFoods, foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{Name: "Cashews"}.Build()}.Build())
	_, err = e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Lunch"), Date: monday,
			Food: plansv1.FoodAmount_builder{FoodId: otherFood.GetId(), Amount: 50, Unit: grams}.Build(),
		}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.anonPlans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Mark the cook done, then delete it, with its portions.
	done, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook: plansv1.Cook_builder{Id: cook.GetId(), Done: true}.Build(), UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build())
	if err != nil || !done.GetCook().GetDone() || done.GetCook().GetServings() != 4 {
		t.Fatalf("UpdateCook: %v, %v", done.GetCook(), err)
	}
	if _, err := e.plans.DeleteCook(ctx, plansv1.DeleteCookRequest_builder{Id: cook.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteCook: %v", err)
	}
	week = getWeek(t, e.plans)
	if got := planned(t, week, monday, ben.GetId(), "Dinner"); len(got.GetPortions()) != 0 || len(week.GetCooks()) != 0 {
		t.Errorf("got Ben's dinner %v, want nothing once the cook is deleted", got)
	}
	_, err = e.plans.DeletePortion(ctx, plansv1.DeletePortionRequest_builder{Id: annaDinnerID}.Build())
	wantCode(t, err, connect.CodeNotFound)
	// The staple goes, and the recipe and food can be deleted.
	if _, err := e.plans.DeleteStaple(ctx, plansv1.DeleteStapleRequest_builder{Id: almondsStaple.GetStaple().GetId()}.Build()); err != nil {
		t.Fatalf("DeleteStaple: %v", err)
	}
	if _, err := e.recipes.DeleteRecipe(ctx, recipesv1.DeleteRecipeRequest_builder{Id: dal.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	if _, err := e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: almonds.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteFood: %v", err)
	}
}

// TestPlanMealCapsAtWhatsPlanned checks that PlanMeal counts what's already
// planned toward the maximum portion, and leaves extra servings over.
func TestPlanMealCapsAtWhatsPlanned(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	anna := createPerson(t, e, anna())
	oats := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	yogurt := customFood(t, e, "Soy yogurt", 50, 4, 2, 1, 2, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Name:        "Porridge",
			Servings:    1,
			Ingredients: []*recipesv1.Ingredient{ingredient(oats.GetId(), 250, grams)},
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	// 150 g of yogurt are planned at breakfast already, so 50 g of the
	// porridge fit there, and the rest goes to mid-morning.
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId: anna.GetId(), EatingTimeId: eatingTimeID(t, anna, "Breakfast"), Date: monday,
			Food: plansv1.FoodAmount_builder{FoodId: yogurt.GetId(), Amount: 150, Unit: grams}.Build(),
		}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	meal, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{
		RecipeId:  created.GetRecipe().GetId(),
		Date:      monday,
		Meal:      breakfast,
		PersonIds: []string{anna.GetId()},
		Servings:  3,
	}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	var got []float64
	for _, p := range meal.GetPortions() {
		got = append(got, p.GetCook().GetServings())
	}
	if !slices.Equal(got, []float64{0.2, 0.8}) || meal.GetPortions()[1].GetEatingTimeId() != eatingTimeID(t, anna, "Mid-morning") {
		t.Errorf("got shares %v, want 0.2 at breakfast and 0.8 mid-morning", meal.GetPortions())
	}
	if meal.GetCook().GetServings() != 3 || meal.GetUnassignedServings() != 2 {
		t.Errorf("got %v servings with %v unassigned, want 3 with 2", meal.GetCook().GetServings(), meal.GetUnassignedServings())
	}
	if !slices.Contains(warningKinds(meal.GetWarnings()), plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED) {
		t.Errorf("got warnings %v, want the unassigned servings", meal.GetWarnings())
	}
	// Lunch can be planned too, but once Anna has no eating times, nobody
	// eats from it.
	_, err = e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: created.GetRecipe().GetId(), Date: monday, Meal: lunch}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	_, err = e.people.UpdatePerson(ctx, peoplev1.UpdatePersonRequest_builder{
		Person:     peoplev1.Person_builder{Id: anna.GetId()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"eating_times"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePerson: %v", err)
	}
	_, err = e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: created.GetRecipe().GetId(), Date: monday, Meal: lunch}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
}
