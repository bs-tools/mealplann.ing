package services_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// weekWarnings returns the kinds of the warnings of a week, of its eating
// times and its cooks.
func weekWarnings(t *testing.T, e *env, monday string) []plansv1.WarningKind {
	t.Helper()
	week, err := e.plans.GetWeek(context.Background(), plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	var kinds []plansv1.WarningKind
	for _, day := range week.GetDays() {
		for _, pd := range day.GetPeople() {
			for _, et := range pd.GetEatingTimes() {
				kinds = append(kinds, warningKinds(et.GetWarnings())...)
			}
		}
	}
	for _, c := range week.GetCooks() {
		kinds = append(kinds, warningKinds(c.GetWarnings())...)
	}
	return kinds
}

func TestFreezer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// Ben, and a soup that keeps 3 days in the fridge and 30 frozen.
	ben := createPerson(t, e, ben())
	lentils := customFood(t, e, "Lentils", 116, 9, 20, 1.8, 0.4, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name:        "Lentil soup",
		Servings:    6,
		Ingredients: []*recipesv1.Ingredient{ingredient(lentils.GetId(), 900, grams)},
		FridgeDays:  new(int32(3)),
		Freezable:   true,
		FreezerDays: new(int32(30)),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	if got := created.GetRecipe(); !got.HasFreezerDays() || got.GetFreezerDays() != 30 {
		t.Errorf("got freezer days %v, want 30", got.GetFreezerDays())
	}

	// Cook 6 servings on Monday for Ben's dinner: 5 are left over.
	planned, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{
		RecipeId: created.GetRecipe().GetId(), Date: monday, Meal: dinner, Servings: 6,
	}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	cook := planned.GetCook()
	if got := weekWarnings(t, e, monday); !slices.Contains(got, plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED) {
		t.Errorf("got warnings %v, want the 5 servings left over", got)
	}

	// Freeze 4 of them: they're in the freezer, and only 1 is left over.
	updated, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetId(), FrozenServings: 4}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"frozen_servings"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	if updated.GetCook().GetFrozenServings() != 4 || updated.GetCook().GetServings() != 6 {
		t.Errorf("got %v, want 4 of the 6 servings frozen", updated.GetCook())
	}
	leftovers, err := e.plans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if got := leftovers.GetLeftovers(); len(got) != 1 || got[0].GetRemainingServings() != 4 || got[0].GetPlannedServings() != 0 ||
		got[0].GetKeepsUntil() != "2026-12-02" || got[0].GetFrozenFrom() != "2026-11-06" || got[0].GetCook().GetRecipeName() != "Lentil soup" {
		t.Errorf("got %v, want the 4 frozen servings, keeping until 2 Dec", got)
	}
	// It expires once keeps_until is past, which depends on today.
	if keepsUntil, _ := time.Parse(time.DateOnly, "2026-12-02"); leftovers.GetLeftovers()[0].GetExpired() != time.Now().UTC().After(keepsUntil.AddDate(0, 0, 1)) {
		t.Errorf("got expired %v on %v", leftovers.GetLeftovers()[0].GetExpired(), time.Now())
	}

	// Eat one two weeks later: it comes from the freezer, to thaw the day
	// before, and 3 are left in it.
	later := func(date string) *plansv1.CreatePortionRequest {
		return plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
			PersonId:     ben.GetId(),
			EatingTimeId: eatingTimeID(t, ben, "Dinner"),
			Date:         date,
			Cook:         plansv1.CookShare_builder{CookId: cook.GetId(), Servings: 1}.Build(),
		}.Build()}.Build()
	}
	if _, err := e.plans.CreatePortion(ctx, later("2026-11-16")); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	if got := weekWarnings(t, e, "2026-11-16"); !slices.Equal(got, []plansv1.WarningKind{plansv1.WarningKind_WARNING_KIND_LEFTOVER_THAW}) {
		t.Errorf("got warnings %v, want a reminder to thaw it", got)
	}
	leftovers, err = e.plans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if got := leftovers.GetLeftovers(); len(got) != 1 || got[0].GetRemainingServings() != 3 || got[0].GetPlannedServings() != 1 {
		t.Errorf("got %v, want 3 servings left in the freezer", got)
	}
	// Monday's cook has 1 eaten fresh, 1 planned from the freezer and 3 in
	// it, so 1 serving is still neither eaten nor frozen.
	if got := weekWarnings(t, e, monday); !slices.Contains(got, plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED) {
		t.Errorf("got warnings %v, want the 1 serving neither eaten nor frozen", got)
	}

	// Eaten after 30 days, it won't keep even frozen.
	if _, err := e.plans.CreatePortion(ctx, later("2026-12-07")); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	if got := weekWarnings(t, e, "2026-12-07"); !slices.Contains(got, plansv1.WarningKind_WARNING_KIND_LEFTOVER_PAST_FREEZER_DAYS) {
		t.Errorf("got warnings %v, want it past its freezer days", got)
	}
	// Planning 3 more late servings needs more frozen than 4.
	for _, date := range []string{"2026-11-17", "2026-11-18", "2026-11-19"} {
		if _, err := e.plans.CreatePortion(ctx, later(date)); err != nil {
			t.Fatalf("CreatePortion: %v", err)
		}
	}
	if got := weekWarnings(t, e, monday); !slices.Contains(got, plansv1.WarningKind_WARNING_KIND_FROZEN_SERVINGS_SHORT) {
		t.Errorf("got warnings %v, want too few servings frozen", got)
	}
	// All 4 frozen servings are planned, so the freezer is empty.
	leftovers, err = e.plans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if len(leftovers.GetLeftovers()) != 0 {
		t.Errorf("got %v, want an empty freezer", leftovers.GetLeftovers())
	}

	// More can't be frozen than is cooked.
	_, err = e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetId(), FrozenServings: 7}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"frozen_servings"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: created.GetRecipe().GetId(), FreezerDays: new(int32(0))}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"freezer_days"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// The other household's freezer is empty.
	other, err := e.otherPlans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if len(other.GetLeftovers()) != 0 {
		t.Errorf("got the other household's freezer %v, want it empty", other.GetLeftovers())
	}
}

// TestTakeFromFreezer checks that frozen leftovers can be taken out of the
// freezer for someone, or off plan, without leaving servings unassigned.
func TestTakeFromFreezer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ben := createPerson(t, e, ben())
	lentils := customFood(t, e, "Lentils", 116, 9, 20, 1.8, 0.4, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Lentil soup", Servings: 4, Ingredients: []*recipesv1.Ingredient{ingredient(lentils.GetId(), 600, grams)},
		FridgeDays: new(int32(3)), Freezable: true, FreezerDays: new(int32(60)),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	// A batch from the freezer: 4 servings, all frozen.
	cook, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook:      plansv1.Cook_builder{RecipeId: created.GetRecipe().GetId(), Date: monday, Servings: 4, FrozenServings: 4, Done: true}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	id := cook.GetCook().GetId()
	take := func(servings float64, personID, date string) (*plansv1.TakeFromFreezerResponse, error) {
		b := plansv1.TakeFromFreezerRequest_builder{CookId: id, Servings: servings, Date: date, PersonId: personID}
		if personID != "" {
			b.EatingTimeId = eatingTimeID(t, ben, "Lunch")
		}
		return e.plans.TakeFromFreezer(ctx, b.Build())
	}

	// One for Ben's lunch on Friday, and one eaten without a plan.
	resp, err := take(1, ben.GetId(), friday)
	if err != nil {
		t.Fatalf("TakeFromFreezer for Ben: %v", err)
	}
	if resp.GetPortion().GetCook().GetServings() != 1 || resp.GetLeftover().GetRemainingServings() != 3 {
		t.Errorf("got %v, want a portion, and 3 servings left", resp)
	}
	resp, err = take(1, "", "")
	if err != nil {
		t.Fatalf("TakeFromFreezer off plan: %v", err)
	}
	if got := resp.GetLeftover(); got.GetRemainingServings() != 2 || got.GetCook().GetOffPlanServings() != 1 {
		t.Errorf("got %v, want 2 servings left, 1 taken off plan", got)
	}
	if got := weekWarnings(t, e, monday); slices.Contains(got, plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED) {
		t.Errorf("got warnings %v, want none unassigned", got)
	}

	// Frozen the day it was cooked, a serving can be thawed within the
	// fridge days too, for Tuesday's lunch, which comes from the freezer.
	resp, err = take(1, ben.GetId(), tuesday)
	if err != nil {
		t.Fatalf("TakeFromFreezer for Tuesday: %v", err)
	}
	if !resp.GetPortion().GetFromFreezer() || resp.GetLeftover().GetRemainingServings() != 1 {
		t.Errorf("got %v, want a portion from the freezer, and 1 serving left", resp)
	}
	// The freezer has only 1, and nothing is eaten before it's cooked.
	_, err = take(2, "", "")
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = take(1, ben.GetId(), "2026-11-01")
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = take(0, "", "")
	wantCode(t, err, connect.CodeInvalidArgument)

	// Throwing the rest out empties the freezer, with nothing unassigned.
	if _, err := take(1, "", ""); err != nil {
		t.Fatalf("TakeFromFreezer: %v", err)
	}
	leftovers, err := e.plans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if len(leftovers.GetLeftovers()) != 0 {
		t.Errorf("got %v, want an empty freezer", leftovers.GetLeftovers())
	}
	if got := weekWarnings(t, e, monday); slices.Contains(got, plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED) {
		t.Errorf("got warnings %v, want none unassigned", got)
	}
}

// TestRemoveLeftovers checks that a cook's leftovers in the fridge can be
// removed, like servings that were never there, without leaving servings
// unassigned.
func TestRemoveLeftovers(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	lentils := customFood(t, e, "Lentils", 116, 9, 20, 1.8, 0.4, nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Lentil soup", Servings: 4, Ingredients: []*recipesv1.Ingredient{ingredient(lentils.GetId(), 600, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	cook, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook:      plansv1.Cook_builder{RecipeId: created.GetRecipe().GetId(), Date: monday, Servings: 4, Done: true}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	left := func() float64 {
		t.Helper()
		week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: monday}.Build())
		if err != nil {
			t.Fatalf("GetWeek: %v", err)
		}
		return week.GetCooks()[0].GetLeftServings()
	}
	if got := left(); got != 4 {
		t.Fatalf("got %v servings left, want 4", got)
	}
	update := func(removed float64) error {
		_, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
			Cook:       plansv1.Cook_builder{Id: cook.GetCook().GetId(), RemovedServings: removed}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"removed_servings"}},
		}.Build())
		return err
	}
	if err := update(4); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	if got := left(); got != 0 {
		t.Errorf("got %v servings left, want none", got)
	}
	if got := weekWarnings(t, e, monday); slices.Contains(got, plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED) {
		t.Errorf("got warnings %v, want none unassigned", got)
	}
	if err := update(5); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("got %v removing more than cooked, want invalid argument", err)
	}
}
