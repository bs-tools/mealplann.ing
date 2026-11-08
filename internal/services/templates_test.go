package services_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	peoplev1 "github.com/bs-tools/mealplann.ing/internal/gen/people/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// The week after the tests' week.
const nextMonday = "2026-11-09"

// charlotte is a child who eats the shared meals and an afternoon snack.
func charlotte() *peoplev1.Person {
	return peoplev1.Person_builder{
		Name: "Charlotte",
		EatingTimes: []*peoplev1.EatingTime{
			eatingTime("Breakfast", mainMeal, "07:30", breakfast),
			eatingTime("Lunch", mainMeal, "12:30", lunch),
			eatingTime("Snack", smallMeal, "15:30", noMeal),
			eatingTime("Dinner", mainMeal, "18:00", dinner),
		},
	}.Build()
}

// describeApplied describes what applying a template plans, e.g. "Oats
// 2026-11-09 2.33: Ben Breakfast 1, ...", with people's names by ID.
func describeApplied(r *plansv1.AppliedTemplate, names map[string]*peoplev1.Person) []string {
	var lines []string
	for _, c := range r.GetCooks() {
		var parts []string
		for _, p := range r.GetPortions() {
			if p.GetCook().GetCookId() != c.GetId() {
				continue
			}
			person := names[p.GetPersonId()]
			et := ""
			for _, e := range person.GetEatingTimes() {
				if e.GetId() == p.GetEatingTimeId() {
					et = e.GetName()
				}
			}
			parts = append(parts, fmt.Sprintf("%s %s %s %.2f", person.GetName(), p.GetDate()[8:], et, p.GetCook().GetServings()))
		}
		lines = append(lines, fmt.Sprintf("%s %s %v: %s", c.GetRecipeName(), c.GetDate(), c.GetServings(), strings.Join(parts, ", ")))
	}
	return lines
}

func TestPlanTemplates(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// The household: Ben, Anna with her small meals, and Charlotte.
	ben := createPerson(t, e, ben())
	anna := createPerson(t, e, anna())
	charlotte := createPerson(t, e, charlotte())
	byID := map[string]*peoplev1.Person{ben.GetId(): ben, anna.GetId(): anna, charlotte.GetId(): charlotte}
	beans := customFood(t, e, "Black beans", 132, 9, 24, 0.3, 0.5, func(f *foodsv1.Food) { f.SetPackageGrams(400) })
	oatFood := customFood(t, e, "Oats", 380, 13, 60, 1, 7, nil)
	newRecipe := func(name string, f *foodsv1.Food, amount float64) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
			Name: name, Servings: 4, Ingredients: []*recipesv1.Ingredient{ingredient(f.GetId(), amount, grams)},
		}.Build()}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	chili := newRecipe("Chili", beans, 800)
	oats := newRecipe("Oats", oatFood, 200)

	// Plan Monday: oats for everyone's breakfast, and a pot of chili for
	// dinner, 6 servings, with Ben's lunch on Tuesday from it.
	if _, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: oats.GetId(), Date: monday, Meal: breakfast}.Build()); err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	planned, err := e.plans.PlanMeal(ctx, plansv1.PlanMealRequest_builder{RecipeId: chili.GetId(), Date: monday, Meal: dinner, Servings: 6}.Build())
	if err != nil {
		t.Fatalf("PlanMeal: %v", err)
	}
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId:     ben.GetId(),
		EatingTimeId: eatingTimeID(t, ben, "Lunch"),
		Date:         tuesday,
		Cook:         plansv1.CookShare_builder{CookId: planned.GetCook().GetId(), Servings: 1}.Build(),
	}.Build()}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}

	// Save the week as a template: the oats are an entry of their own, and
	// the chili a batch of its dinner and Tuesday's lunch.
	created, err := e.templates.CreatePlanTemplateFromPlan(ctx, plansv1.CreatePlanTemplateFromPlanRequest_builder{
		Name: "Week A", Source: "Our first week", StartDate: monday,
	}.Build())
	if err != nil {
		t.Fatalf("CreatePlanTemplateFromPlan: %v", err)
	}
	tmpl := created.GetTemplate()
	var keys []string
	for _, entry := range tmpl.GetEntries() {
		keys = append(keys, entry.GetKey()+" "+entry.GetRecipeName())
	}
	if tmpl.GetDays() != 7 || tmpl.GetSource() != "Our first week" || len(tmpl.GetSlots()) != 6 ||
		!slices.Equal(keys, []string{"d1-breakfast Oats", "d1-dinner Chili", "d2-lunch Chili"}) {
		t.Fatalf("got %v with slots %v and entries %v, want a week of Monday's oats and chili, and Tuesday's lunch", tmpl, tmpl.GetSlots(), keys)
	}
	if b := tmpl.GetBatches(); len(b) != 1 || b[0].GetServings() != 6 || b[0].GetRecipeName() != "Chili" ||
		!slices.Equal(b[0].GetEntryKeys(), []string{"d1-dinner", "d2-lunch"}) {
		t.Errorf("got batches %v, want the chili's", b)
	}

	// Charlotte eats a third of a serving of oats.
	entries := tmpl.GetEntries()
	entries[0].SetPersonServings([]*plansv1.PersonServings{
		plansv1.PersonServings_builder{PersonId: charlotte.GetId(), Servings: 1.0 / 3}.Build(),
	})
	updated, err := e.templates.UpdatePlanTemplate(ctx, plansv1.UpdatePlanTemplateRequest_builder{
		Template:   plansv1.PlanTemplate_builder{Id: tmpl.GetId(), Entries: entries}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"entries"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePlanTemplate: %v", err)
	}
	if got := updated.GetTemplate(); len(got.GetBatches()) != 1 || got.GetName() != "Week A" {
		t.Errorf("got %v, want the rest of the template kept", got)
	}

	// Preview it next week: what it would plan, and buy, without planning
	// anything.
	preview, err := e.templates.PreviewPlanTemplate(ctx, plansv1.PreviewPlanTemplateRequest_builder{
		TemplateId: tmpl.GetId(), StartDate: nextMonday,
	}.Build())
	if err != nil {
		t.Fatalf("PreviewPlanTemplate: %v", err)
	}
	want := []string{
		"Oats 2026-11-09 2.33: Anna 09 Breakfast 1.00, Ben 09 Breakfast 1.00, Charlotte 09 Breakfast 0.33",
		fmt.Sprintf("Chili 2026-11-09 6: %s, Ben 10 Lunch 1.00", chiliPortions(t, e)),
	}
	r := preview.GetResult()
	if got := describeApplied(r, byID); !slices.Equal(got, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if r.GetEndDate() != "2026-11-15" || len(r.GetShopping()) != 2 || len(r.GetSkipped()) != 0 {
		t.Errorf("got end %s, shopping %v and skipped %v, want a week, oats and beans, and nobody left out", r.GetEndDate(), r.GetShopping(), r.GetSkipped())
	}
	week, err := e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: nextMonday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if len(week.GetCooks()) != 0 {
		t.Errorf("got %d cooks next week after a preview, want none", len(week.GetCooks()))
	}

	// Apply it: the same cooks and portions are planned, with IDs.
	applied, err := e.templates.ApplyPlanTemplate(ctx, plansv1.ApplyPlanTemplateRequest_builder{
		TemplateId: tmpl.GetId(), StartDate: nextMonday,
	}.Build())
	if err != nil {
		t.Fatalf("ApplyPlanTemplate: %v", err)
	}
	if got := describeApplied(applied.GetResult(), byID); !slices.Equal(got, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	week, err = e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: nextMonday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if len(week.GetCooks()) != 2 {
		t.Errorf("got %d cooks next week, want the oats and the chili", len(week.GetCooks()))
	}
	// Applied to Charlotte alone, there's no lunch, as she doesn't eat
	// Tuesday's.
	preview, err = e.templates.PreviewPlanTemplate(ctx, plansv1.PreviewPlanTemplateRequest_builder{
		TemplateId: tmpl.GetId(), StartDate: nextMonday, PersonIds: []string{charlotte.GetId()},
	}.Build())
	if err != nil {
		t.Fatalf("PreviewPlanTemplate: %v", err)
	}
	if got := preview.GetResult().GetSkipped(); !slices.Equal(got, []string{"Nobody eats entry d2-lunch, so it isn't planned"}) {
		t.Errorf("got skipped %v, want Tuesday's lunch unused", got)
	}

	// A template made by hand, with a recipe deleted since, can't be
	// applied until it's fixed.
	snack := newRecipe("Bean dip", beans, 400)
	hand, err := e.templates.CreatePlanTemplate(ctx, plansv1.CreatePlanTemplateRequest_builder{Template: plansv1.PlanTemplate_builder{
		Name: "Snacks",
		Days: 2,
		Slots: []*plansv1.TemplateSlot{
			plansv1.TemplateSlot_builder{Key: "afternoon", Name: "Afternoon snack", Kind: smallMeal}.Build(),
		},
		Entries: []*plansv1.TemplateEntry{
			plansv1.TemplateEntry_builder{Day: 1, SlotKey: "afternoon", RecipeId: snack.GetId(), Servings: 0.5}.Build(),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreatePlanTemplate: %v", err)
	}
	if got := hand.GetTemplate().GetEntries()[0]; got.GetKey() != "d2-afternoon" || got.GetRecipeName() != "Bean dip" {
		t.Errorf("got %v, want the key made from the day and slot", got)
	}
	// Ben has no small meal, so he's left out.
	preview, err = e.templates.PreviewPlanTemplate(ctx, plansv1.PreviewPlanTemplateRequest_builder{
		TemplateId: hand.GetTemplate().GetId(), StartDate: nextMonday,
	}.Build())
	if err != nil {
		t.Fatalf("PreviewPlanTemplate: %v", err)
	}
	if got := preview.GetResult().GetSkipped(); len(got) != 1 || !strings.HasPrefix(got[0], "Ben has no eating time for the Afternoon snack slot") {
		t.Errorf("got skipped %v, want Ben left out", got)
	}
	if _, err := e.recipes.DeleteRecipe(ctx, recipesv1.DeleteRecipeRequest_builder{Id: snack.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	_, err = e.templates.ApplyPlanTemplate(ctx, plansv1.ApplyPlanTemplateRequest_builder{
		TemplateId: hand.GetTemplate().GetId(), StartDate: nextMonday,
	}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)

	// The templates are listed by name, with their recipes.
	list, err := e.templates.ListPlanTemplates(ctx, plansv1.ListPlanTemplatesRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListPlanTemplates: %v", err)
	}
	var names []string
	for _, s := range list.GetTemplates() {
		names = append(names, fmt.Sprintf("%s %d %v", s.GetName(), s.GetEntryCount(), s.GetRecipeNames()))
	}
	if want := []string{"Snacks 1 []", "Week A 3 [Oats Chili]"}; !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}

	// Bad templates fail.
	slot := plansv1.TemplateSlot_builder{Key: "dinner", Name: "Dinner", Kind: mainMeal, Meal: dinner}.Build()
	entry := func(key string, day int32, recipe *recipesv1.Recipe) *plansv1.TemplateEntry {
		return plansv1.TemplateEntry_builder{Key: key, Day: day, SlotKey: "dinner", RecipeId: recipe.GetId()}.Build()
	}
	for name, tt := range map[string]struct {
		tmpl *plansv1.PlanTemplate
		code connect.Code
	}{
		"taken name": {plansv1.PlanTemplate_builder{Name: "week a", Days: 1, Slots: []*plansv1.TemplateSlot{slot}}.Build(), connect.CodeAlreadyExists},
		"no slots":   {plansv1.PlanTemplate_builder{Name: "New", Days: 1}.Build(), connect.CodeInvalidArgument},
		"day out of range": {plansv1.PlanTemplate_builder{
			Name: "New", Days: 1, Slots: []*plansv1.TemplateSlot{slot},
			Entries: []*plansv1.TemplateEntry{entry("a", 1, chili)},
		}.Build(), connect.CodeInvalidArgument},
		"batch of two recipes": {plansv1.PlanTemplate_builder{
			Name: "New", Days: 2, Slots: []*plansv1.TemplateSlot{slot},
			Entries: []*plansv1.TemplateEntry{entry("a", 0, chili), entry("b", 1, oats)},
			Batches: []*plansv1.TemplateBatch{plansv1.TemplateBatch_builder{EntryKeys: []string{"a", "b"}}.Build()},
		}.Build(), connect.CodeInvalidArgument},
		"batch after its entry": {plansv1.PlanTemplate_builder{
			Name: "New", Days: 2, Slots: []*plansv1.TemplateSlot{slot},
			Entries: []*plansv1.TemplateEntry{entry("a", 0, chili)},
			Batches: []*plansv1.TemplateBatch{plansv1.TemplateBatch_builder{Day: 1, EntryKeys: []string{"a"}}.Build()},
		}.Build(), connect.CodeInvalidArgument},
		"deleted recipe": {plansv1.PlanTemplate_builder{
			Name: "New", Days: 1, Slots: []*plansv1.TemplateSlot{slot},
			Entries: []*plansv1.TemplateEntry{entry("a", 0, snack)},
		}.Build(), connect.CodeInvalidArgument},
	} {
		_, err := e.templates.CreatePlanTemplate(ctx, plansv1.CreatePlanTemplateRequest_builder{Template: tt.tmpl}.Build())
		if connect.CodeOf(err) != tt.code {
			t.Errorf("%s: got %v, want %v", name, err, tt.code)
		}
	}

	// The other household can't see or use the templates, nor make one of
	// an empty plan.
	_, err = e.otherTemplates.GetPlanTemplate(ctx, plansv1.GetPlanTemplateRequest_builder{Id: tmpl.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherTemplates.ApplyPlanTemplate(ctx, plansv1.ApplyPlanTemplateRequest_builder{TemplateId: tmpl.GetId(), StartDate: nextMonday}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherTemplates.CreatePlanTemplateFromPlan(ctx, plansv1.CreatePlanTemplateFromPlanRequest_builder{Name: "Mine", StartDate: monday}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.anonTemplates.ListPlanTemplates(ctx, plansv1.ListPlanTemplatesRequest_builder{}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Deleting a template keeps what it planned.
	if _, err := e.templates.DeletePlanTemplate(ctx, plansv1.DeletePlanTemplateRequest_builder{Id: tmpl.GetId()}.Build()); err != nil {
		t.Fatalf("DeletePlanTemplate: %v", err)
	}
	_, err = e.templates.GetPlanTemplate(ctx, plansv1.GetPlanTemplateRequest_builder{Id: tmpl.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	week, err = e.plans.GetWeek(ctx, plansv1.GetWeekRequest_builder{Monday: nextMonday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	if len(week.GetCooks()) != 2 {
		t.Errorf("got %d cooks after deleting the template, want the 2 it planned", len(week.GetCooks()))
	}
}

// chiliPortions describes the portions of Monday's chili dinner as
// PlanMeal split them, which applying the template copies, e.g. "Ben 09
// Dinner 1.00, Anna 09 Dinner 0.60, ...".
func chiliPortions(t *testing.T, e *env) string {
	t.Helper()
	week, err := e.plans.GetWeek(context.Background(), plansv1.GetWeekRequest_builder{Monday: monday}.Build())
	if err != nil {
		t.Fatalf("GetWeek: %v", err)
	}
	var parts []string
	for _, day := range week.GetDays() {
		for _, pd := range day.GetPeople() {
			for _, et := range pd.GetEatingTimes() {
				for _, p := range et.GetPortions() {
					if p.GetName() == "Chili" && day.GetDate() == monday {
						parts = append(parts, fmt.Sprintf("%s 09 %s %.2f", pd.GetPersonName(), et.GetEatingTime().GetName(), p.GetServings()))
					}
				}
			}
		}
	}
	return strings.Join(parts, ", ")
}
