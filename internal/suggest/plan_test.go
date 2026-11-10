package suggest

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/store"
)

// planInput returns the input of planning dinners from Monday for days,
// for the people, with nothing planned yet, a shop on Monday, and a fixed
// number of rollouts, so tests don't depend on the machine's speed.
func planInput(days int, ps []*people.Person, rs ...*recipes.Recipe) *PlanInput {
	in := &PlanInput{
		Plan:     &plans.Plan{From: monday.AddDate(0, 0, -VarietyDays), To: sunday.AddDate(0, 0, 1)},
		Planned:  map[plans.Slot]*rules.EatingTime{},
		Recipes:  rs,
		Foods:    foodsByID(),
		ShopDate: func(time.Time) time.Time { return monday },
		Rand:     rand.New(rand.NewPCG(1, 2)),
		Budget:   time.Hour,
		Rollouts: 20,
	}
	for d := range days {
		in.Slots = append(in.Slots, &PlanSlot{Date: monday.AddDate(0, 0, d), Meal: people.MealDinner, People: ps})
	}
	return in
}

// plannedNames returns the names of the recipes planned at each slot,
// empty for none.
func plannedNames(result *PlanResult) []string {
	var names []string
	for _, s := range result.Slots {
		if s.Suggestion == nil {
			names = append(names, "")
			continue
		}
		names = append(names, s.Suggestion.Recipe.Name)
	}
	return names
}

func TestPlan(t *testing.T) {
	t.Parallel()
	b := ben()
	// The recipes are alike, so each of the three dinners has a different
	// one, as repeating one within a few days costs variety.
	curry := recipe("Curry", 2, ingredient(chickpeas, 400))
	hummus := recipe("Hummus", 2, ingredient(chickpeas, 400))
	falafel := recipe("Falafel", 2, ingredient(chickpeas, 400))
	result := Plan(planInput(3, []*people.Person{b}, curry, hummus, falafel))

	names := plannedNames(result)
	if len(names) != 3 || slices.Contains(names, "") {
		t.Fatalf("got %v, want 3 dinners planned", names)
	}
	unique := slices.Clone(names)
	slices.Sort(unique)
	if unique = slices.Compact(unique); len(unique) != 3 {
		t.Errorf("got %v, want 3 different recipes", names)
	}
	if result.Rollouts != 20 {
		t.Errorf("got %d rollouts, want 20", result.Rollouts)
	}
	// The score is the slots' total, and each slot offers the other
	// recipes as alternatives.
	var total float64
	for _, s := range result.Slots {
		total += s.Suggestion.Score
		if len(s.Alternatives) != 2 || slices.Contains(s.Alternatives, s.Suggestion) {
			t.Errorf("%s: got alternatives %v, want the 2 other recipes", s.Suggestion.Recipe.Name, s.Alternatives)
		}
	}
	if diff := total - result.Score; diff > 0.05 || diff < -0.05 {
		t.Errorf("got score %v, want the slots' total %v", result.Score, total)
	}
}

func TestPlanSharesPacks(t *testing.T) {
	t.Parallel()
	b := ben()
	// Two coconut recipes use a 400 g can between them; the curry alone
	// would waste half a can.
	thai := recipe("Thai curry", 2, ingredient(coconutMilk, 200), ingredient(tofu, 200))
	soup := recipe("Coconut soup", 2, ingredient(coconutMilk, 200), ingredient(onions, 150))
	plain := recipe("Plain rice", 2, ingredient(rice, 150))
	names := plannedNames(Plan(planInput(2, []*people.Person{b}, thai, soup, plain)))
	if !slices.Contains(names, "Thai curry") || !slices.Contains(names, "Coconut soup") {
		t.Errorf("got %v, want the curry and the soup sharing a can", names)
	}
}

func TestPlanFixed(t *testing.T) {
	t.Parallel()
	b := ben()
	curry := recipe("Curry", 2, ingredient(chickpeas, 400))
	hummus := recipe("Hummus", 2, ingredient(chickpeas, 400))
	in := planInput(2, []*people.Person{b}, curry, hummus)
	// The user keeps the hummus on Tuesday, though the curry scores as
	// well and comes first by name, so Monday gets the curry.
	in.Slots[1].Fixed = hummus
	result := Plan(in)
	if got := plannedNames(result); !slices.Equal(got, []string{"Curry", "Hummus"}) {
		t.Errorf("got %v, want the curry, then the kept hummus", got)
	}
	// The kept slot can still be swapped, for the curry.
	if alt := result.Slots[1].Alternatives; len(alt) != 1 || alt[0].Recipe != curry {
		t.Errorf("got alternatives %v for the kept slot, want the curry", alt)
	}
}

func TestPlanFitsRules(t *testing.T) {
	t.Parallel()
	a := anna()
	// The pudding is a favorite, but breaks Anna's rules on carbohydrate
	// and fast carbs, so the dal is planned while it breaks none.
	pudding := recipe("Juice and rice pudding", 2, ingredient(juice, 300), ingredient(rice, 100))
	pudding.Favorite = true
	dal := recipe("Tofu dal", 4, ingredient(tofu, 400), ingredient(spinach, 200))
	result := Plan(planInput(1, []*people.Person{a}, pudding, dal))
	if got := plannedNames(result); !slices.Equal(got, []string{"Tofu dal"}) {
		t.Errorf("got %v, want the dal", got)
	}
	if got := result.Slots[0].Fitting; got != 1 {
		t.Errorf("got %d recipes fitting the rules, want only the dal", got)
	}
}

func TestPlanWeights(t *testing.T) {
	t.Parallel()
	b := ben()
	plain := recipe("Plain curry", 2, ingredient(chickpeas, 400))
	favorite := recipe("Favorite curry", 2, ingredient(chickpeas, 400))
	favorite.Favorite = true
	// Without favorites counting, the favorite has no edge, and its
	// component isn't shown.
	in := planInput(1, []*people.Person{b}, plain, favorite)
	in.Weights = map[Kind]float64{Favorite: 0}
	result := Plan(in)
	for _, sg := range append([]*Suggestion{result.Slots[0].Suggestion}, result.Slots[0].Alternatives...) {
		if len(components(sg, Favorite)) > 0 {
			t.Errorf("%s: got a favorite component, want none", sg.Recipe.Name)
		}
	}
	// Doubled, it's worth 10 points.
	in = planInput(1, []*people.Person{b}, plain, favorite)
	in.Weights = map[Kind]float64{Favorite: 2}
	result = Plan(in)
	wantComponents(t, result.Slots[0].Suggestion, Favorite, "10 a household favorite")
}

func TestPlanBudget(t *testing.T) {
	t.Parallel()
	b := ben()
	in := planInput(2, []*people.Person{b}, recipe("Curry", 2, ingredient(chickpeas, 400)))
	// A clock that moves a second each time it's read runs out of a
	// half-second budget after the first rollout.
	clock := monday
	in.Now = func() time.Time {
		clock = clock.Add(time.Second)
		return clock
	}
	in.Budget = 500 * time.Millisecond
	in.Rollouts = 0
	if result := Plan(in); result.Rollouts != 1 {
		t.Errorf("got %d rollouts, want 1", result.Rollouts)
	}
}

// BenchmarkPlanRollout measures a rollout of two weeks of the three
// shared meals for Ben and Anna, from 40 recipes of 6 ingredients, which
// sets how many rollouts fit in the budget.
func BenchmarkPlanRollout(b *testing.B) {
	r := rand.New(rand.NewPCG(3, 4))
	var rs []*recipes.Recipe
	for i := range 40 {
		var ingredients []*recipes.Ingredient
		for range 6 {
			ingredients = append(ingredients, ingredient(allFoods[r.IntN(len(allFoods))], float64(50+r.IntN(300))))
		}
		rs = append(rs, recipe(fmt.Sprintf("Recipe %d", i), 4, ingredients...))
	}
	ps := []*people.Person{ben(), anna()}
	in := planInput(0, ps, rs...)
	for d := range 14 {
		for _, meal := range []people.Meal{people.MealBreakfast, people.MealLunch, people.MealDinner} {
			in.Slots = append(in.Slots, &PlanSlot{Date: monday.AddDate(0, 0, d), Meal: meal, People: ps})
		}
	}
	in.Rollouts = 1
	for b.Loop() {
		Plan(in)
	}
}

func TestPlanDone(t *testing.T) {
	t.Parallel()
	b := ben()
	in := planInput(2, []*people.Person{b}, recipe("Curry", 2, ingredient(chickpeas, 400)))
	// Stopped before it starts, the search still plans greedily once.
	done := make(chan struct{})
	close(done)
	in.Done = done
	in.Rollouts = 0
	result := Plan(in)
	if result.Rollouts != 1 || result.Slots[0].Suggestion == nil {
		t.Errorf("got %d rollouts and %v, want 1 greedy rollout", result.Rollouts, plannedNames(result))
	}
}

func TestFitsMeal(t *testing.T) {
	t.Parallel()
	// Only breakfast needs its tag; the other slots take anything, a
	// breakfast for lunch too.
	for _, tt := range []struct {
		tags []string
		want map[string]bool
	}{
		{nil, map[string]bool{"breakfast": false, "lunch": true, "dinner": true, "": true, "snack": true}},
		{[]string{"quick"}, map[string]bool{"breakfast": false, "lunch": true, "dinner": true, "": true, "snack": true}},
		{[]string{" Breakfast "}, map[string]bool{"breakfast": true, "lunch": true, "dinner": true, "": true, "snack": true}},
		{[]string{"dinner"}, map[string]bool{"breakfast": false, "lunch": true, "dinner": true, "": true, "snack": true}},
		{[]string{"snack"}, map[string]bool{"breakfast": false, "lunch": true, "dinner": true, "": true, "snack": true}},
	} {
		r := &recipes.Recipe{Name: "R", Tags: tt.tags}
		for tag, want := range tt.want {
			if got := FitsMeal(r, tag); got != want {
				t.Errorf("FitsMeal(%v, %q) = %v, want %v", tt.tags, tag, got, want)
			}
		}
	}
}

func TestPlanSnacks(t *testing.T) {
	t.Parallel()
	a := anna()
	curry := recipe("Curry", 2, ingredient(chickpeas, 400))
	curry.Tags = []string{"dinner"}
	stew := recipe("Stew", 2, ingredient(chickpeas, 400))
	bar := recipe("Bar", 4, ingredient(tofu, 400))
	bar.Tags = []string{"snack"}
	snackSlot := func(in *PlanInput) *PlanSlot {
		s := &PlanSlot{Date: tuesday, People: []*people.Person{a}, EatingTimeID: etID(a, "Mid-afternoon")}
		in.Slots = append(in.Slots, s)
		return s
	}

	// With a snack recipe, the snack is it, though repeating it costs
	// variety, and the rest of Monday's dinner an alternative; never a new
	// cook of a dinner.
	in := planInput(1, []*people.Person{a}, curry, stew, bar)
	in.Plan.Cooks = append(in.Plan.Cooks, &plans.Cook{ID: store.NewUUID(), RecipeID: bar.ID, RecipeName: bar.Name, Date: monday, Servings: 1})
	snackSlot(in)
	result := Plan(in)
	if sg := result.Slots[1].Suggestion; sg == nil || sg.Recipe != bar || sg.Leftover != nil {
		t.Fatalf("got %v at the snack, want the bar", sg)
	}
	leftover := false
	for _, alt := range result.Slots[1].Alternatives {
		if alt.Leftover == nil && alt.Recipe != bar {
			t.Errorf("got a new cook of %s as an alternative, want only the bar and leftovers", alt.Recipe.Name)
		}
		leftover = leftover || alt.Leftover != nil && alt.Recipe == result.Slots[0].Suggestion.Recipe
	}
	if !leftover {
		t.Error("got no alternative of the rest of Monday's dinner")
	}

	// Without one, the rest of Monday's dinner beats cooking another.
	in = planInput(1, []*people.Person{a}, curry, stew)
	snackSlot(in)
	result = Plan(in)
	if sg := result.Slots[1].Suggestion; sg == nil || sg.Leftover == nil || sg.Recipe != result.Slots[0].Suggestion.Recipe {
		t.Errorf("got %v at the snack, want the rest of Monday's dinner", sg)
	}

	// A leftover the user chose is kept.
	in = planInput(1, []*people.Person{a}, curry, stew, bar)
	in.Slots[0].Fixed = stew
	snackSlot(in).FixedLeftover = &LeftoverRef{Date: monday, Meal: people.MealDinner, RecipeID: stew.ID}
	result = Plan(in)
	if sg := result.Slots[1].Suggestion; sg == nil || sg.Leftover == nil || sg.Recipe != stew {
		t.Errorf("got %v at the snack, want the kept leftover of the stew", sg)
	}
}
