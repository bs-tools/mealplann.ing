package templates

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

var monday = time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)

func eatingTime(name string, kind people.Kind, meal people.Meal) *people.EatingTime {
	return &people.EatingTime{ID: store.NewUUID(), Name: name, Kind: kind, Meal: meal}
}

// household returns Ben, who eats three meals, Anna, who eats small
// portions of them and three small meals, and Charlotte, a child who eats
// the main meals and an afternoon snack.
func household() (ben, anna, charlotte *people.Person) {
	ben = &people.Person{ID: store.NewUUID(), Name: "Ben", EatingTimes: []*people.EatingTime{
		eatingTime("Breakfast", people.KindMain, people.MealBreakfast),
		eatingTime("Lunch", people.KindMain, people.MealLunch),
		eatingTime("Dinner", people.KindMain, people.MealDinner),
	}}
	anna = &people.Person{ID: store.NewUUID(), Name: "Anna", EatingTimes: []*people.EatingTime{
		eatingTime("Breakfast", people.KindMain, people.MealBreakfast),
		eatingTime("Mid-morning", people.KindSmall, people.MealNone),
		eatingTime("Lunch", people.KindMain, people.MealLunch),
		eatingTime("Mid-afternoon", people.KindSmall, people.MealNone),
		eatingTime("Dinner", people.KindMain, people.MealDinner),
		eatingTime("Evening", people.KindSmall, people.MealNone),
	}}
	charlotte = &people.Person{ID: store.NewUUID(), Name: "Charlotte", EatingTimes: []*people.EatingTime{
		eatingTime("Breakfast", people.KindMain, people.MealBreakfast),
		eatingTime("Lunch", people.KindMain, people.MealLunch),
		eatingTime("Snack", people.KindSmall, people.MealNone),
		eatingTime("Dinner", people.KindMain, people.MealDinner),
	}}
	return ben, anna, charlotte
}

func etName(p *people.Person, id pgtype.UUID) string {
	if et := p.EatingTime(id); et != nil {
		return et.Name
	}
	return "?"
}

// describe describes applied cooks, e.g. "Mon dinner 2.33: Ben Mon Dinner
// 1, Charlotte Mon Dinner 0.33", with recipes by name.
func describe(applied *Applied, ps []*people.Person, names map[pgtype.UUID]string) []string {
	var lines []string
	for _, c := range applied.Cooks {
		var parts []string
		for _, portion := range c.Portions {
			p := ps[slices.IndexFunc(ps, func(p *people.Person) bool { return p.ID == portion.PersonID })]
			parts = append(parts, fmt.Sprintf("%s %s %s %v", p.Name, portion.Date.Weekday().String()[:3], etName(p, portion.EatingTimeID), portion.Servings))
		}
		meal := string(c.Cook.Meal)
		if meal == "" {
			meal = "-"
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %v: %s", names[c.Cook.RecipeID], c.Cook.Date.Weekday().String()[:3], meal, c.Cook.Servings, strings.Join(parts, ", ")))
	}
	return lines
}

func TestApply(t *testing.T) {
	t.Parallel()
	ben, anna, charlotte := household()
	ps := []*people.Person{ben, anna, charlotte}
	chili, hummus, oats := store.NewUUID(), store.NewUUID(), store.NewUUID()
	names := map[pgtype.UUID]string{chili: "Chili", hummus: "Hummus", oats: "Oats"}
	tmpl := &Template{
		Days:  2,
		Slots: SlotsOf(ps),
		Entries: []*Entry{
			// Everyone has oats on day 1, Charlotte a third of a serving.
			{Key: "d1-breakfast", Day: 0, SlotKey: "breakfast", RecipeID: oats, Servings: 1, PersonServings: []PersonServings{{PersonID: charlotte.ID, Servings: 1.0 / 3}}},
			// Chili for dinner on day 1, and its leftovers for lunch on
			// day 2, but not for Charlotte.
			{Key: "d1-dinner", Day: 0, SlotKey: "dinner", RecipeID: chili, Servings: 1, PersonServings: []PersonServings{{PersonID: charlotte.ID, Servings: 0.5}}},
			{Key: "d2-lunch", Day: 1, SlotKey: "lunch", RecipeID: chili, Servings: 1, PersonServings: []PersonServings{{PersonID: charlotte.ID}}},
			// Hummus at the second small meal: Anna's mid-afternoon, and
			// nobody else's, as Charlotte has one small meal only.
			{Key: "d2-small-2", Day: 1, SlotKey: "small-2", RecipeID: hummus, Servings: 0.5},
		},
		// The chili is cooked once, for 6 servings.
		Batches: []*Batch{{Day: 0, Servings: 6, EntryKeys: []string{"d1-dinner", "d2-lunch"}}},
	}
	applied := Apply(tmpl, ps, monday)
	want := []string{
		"Oats Mon breakfast 2.33: Ben Mon Breakfast 1, Anna Mon Breakfast 1, Charlotte Mon Breakfast 0.3333333333333333",
		"Chili Mon dinner 6: Ben Mon Dinner 1, Anna Mon Dinner 1, Charlotte Mon Dinner 0.5, Ben Tue Lunch 1, Anna Tue Lunch 1",
		"Hummus Tue - 0.5: Anna Tue Mid-afternoon 0.5",
	}
	if got := describe(applied, ps, names); !slices.Equal(got, want) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Charlotte has no second small meal, and Ben none at all.
	var skips []string
	for _, s := range applied.Skips {
		skips = append(skips, s.String())
	}
	if want := []string{
		"Ben has no eating time for the Mid-afternoon slot, so they're left out of entry d2-small-2 on day 2",
		"Charlotte has no eating time for the Mid-afternoon slot, so they're left out of entry d2-small-2 on day 2",
	}; !slices.Equal(skips, want) {
		t.Errorf("got skips\n%s\nwant\n%s", strings.Join(skips, "\n"), strings.Join(want, "\n"))
	}

	// Applied to Charlotte alone, the lunch and the small meal are
	// nobody's.
	applied = Apply(tmpl, []*people.Person{charlotte}, monday)
	if !slices.Equal(applied.Unused, []string{"d2-lunch", "d2-small-2"}) || len(applied.Cooks) != 2 {
		t.Errorf("got unused %v and %d cooks, want the lunch she doesn't eat and the small meal unused, and 2 cooks", applied.Unused, len(applied.Cooks))
	}
}

func TestSlotsOf(t *testing.T) {
	t.Parallel()
	ben, anna, charlotte := household()
	var got []string
	for _, s := range SlotsOf([]*people.Person{ben, charlotte, anna}) {
		got = append(got, s.Key+"="+s.Name)
	}
	// Anna has the most eating times, so the slots follow hers.
	want := []string{"breakfast=Breakfast", "small-1=Mid-morning", "lunch=Lunch", "small-2=Mid-afternoon", "dinner=Dinner", "small-3=Evening"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFromPlan(t *testing.T) {
	t.Parallel()
	ben, anna, charlotte := household()
	ps := []*people.Person{ben, anna, charlotte}
	chili, oats := store.NewUUID(), store.NewUUID()
	names := map[pgtype.UUID]string{chili: "Chili", oats: "Oats"}
	tuesday := monday.AddDate(0, 0, 1)
	// The plan: oats on Monday, and a pot of chili on Monday for dinner
	// and Tuesday's lunch, with 2 servings to spare, and a week before it
	// that isn't copied.
	plan := &plans.Plan{
		Cooks: []*plans.Cook{
			{ID: store.NewUUID(), RecipeID: oats, Date: monday, Servings: 2.5, Meal: people.MealBreakfast},
			{ID: store.NewUUID(), RecipeID: chili, Date: monday, Servings: 6, Meal: people.MealDinner},
			{ID: store.NewUUID(), RecipeID: chili, Date: monday.AddDate(0, 0, -7), Servings: 2, Meal: people.MealDinner},
		},
	}
	portion := func(c *plans.Cook, p *people.Person, date time.Time, et string, servings float64) *plans.Portion {
		id := p.EatingTimes[slices.IndexFunc(p.EatingTimes, func(e *people.EatingTime) bool { return e.Name == et })].ID
		return &plans.Portion{PersonID: p.ID, EatingTimeID: id, Date: date, CookID: c.ID, Servings: servings}
	}
	plan.Portions = []*plans.Portion{
		portion(plan.Cooks[0], ben, monday, "Breakfast", 1),
		portion(plan.Cooks[0], anna, monday, "Breakfast", 1),
		portion(plan.Cooks[0], charlotte, monday, "Breakfast", 0.5),
		portion(plan.Cooks[1], ben, monday, "Dinner", 1),
		portion(plan.Cooks[1], anna, monday, "Dinner", 0.6),
		portion(plan.Cooks[1], anna, monday, "Evening", 0.4),
		portion(plan.Cooks[1], ben, tuesday, "Lunch", 1),
		portion(plan.Cooks[2], ben, monday.AddDate(0, 0, -7), "Dinner", 1),
	}
	tmpl := FromPlan(plan, ps, monday, 7)
	var entries []string
	for _, e := range tmpl.Entries {
		var own []string
		for _, s := range e.PersonServings {
			own = append(own, fmt.Sprintf("%s=%v", ps[slices.IndexFunc(ps, func(p *people.Person) bool { return p.ID == s.PersonID })].Name, s.Servings))
		}
		entries = append(entries, fmt.Sprintf("%s %s %v %v", e.Key, names[e.RecipeID], e.Servings, own))
	}
	want := []string{
		"d1-breakfast Oats 1 [Charlotte=0.5]",
		"d1-dinner Chili 1 [Anna=0.6 Charlotte=0]",
		"d1-small-3 Chili 0.4 [Ben=0 Charlotte=0]",
		"d2-lunch Chili 1 [Anna=0 Charlotte=0]",
	}
	if !slices.Equal(entries, want) {
		t.Errorf("got entries\n%s\nwant\n%s", strings.Join(entries, "\n"), strings.Join(want, "\n"))
	}
	// The oats are a cook of their own; the chili a batch, as it's eaten
	// at three slots, with servings to spare.
	if len(tmpl.Batches) != 1 || tmpl.Batches[0].Servings != 6 || tmpl.Batches[0].Day != 0 ||
		!slices.Equal(tmpl.Batches[0].EntryKeys, []string{"d1-dinner", "d1-small-3", "d2-lunch"}) {
		t.Errorf("got batches %+v, want the chili's", tmpl.Batches)
	}

	// Applied a week later, it plans the same, a week later.
	applied := Apply(tmpl, ps, monday.AddDate(0, 0, 7))
	wantCooks := []string{
		"Oats Mon breakfast 2.5: Ben Mon Breakfast 1, Anna Mon Breakfast 1, Charlotte Mon Breakfast 0.5",
		"Chili Mon dinner 6: Ben Mon Dinner 1, Anna Mon Dinner 0.6, Anna Mon Evening 0.4, Ben Tue Lunch 1",
	}
	if got := describe(applied, ps, names); !slices.Equal(got, wantCooks) {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(wantCooks, "\n"))
	}
	if len(applied.Skips) != 0 || len(applied.Unused) != 0 {
		t.Errorf("got skips %v and unused %v, want none", applied.Skips, applied.Unused)
	}
}
