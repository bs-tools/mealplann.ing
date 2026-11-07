package suggest

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bs-tools/foodplann.ing/internal/foods"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/pantry"
	"github.com/bs-tools/foodplann.ing/internal/people"
	"github.com/bs-tools/foodplann.ing/internal/plans"
	"github.com/bs-tools/foodplann.ing/internal/recipes"
	"github.com/bs-tools/foodplann.ing/internal/rules"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// The week of the tests, from Monday 2 November 2026.
var (
	monday    = date("2026-11-02")
	tuesday   = date("2026-11-03")
	wednesday = date("2026-11-04")
	thursday  = date("2026-11-05")
	saturday  = date("2026-11-07")
	sunday    = date("2026-11-08")
)

func date(s string) time.Time {
	d, err := store.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

// food returns a food with energy and macronutrients per 100 g.
func food(name string, kcal, protein, carbohydrate, sugars, fat float64, edit func(*foods.Food)) *foods.Food {
	f := &foods.Food{ID: store.NewUUID(), Name: name}
	f.Per100Grams[nutrition.EnergyKcal] = new(kcal)
	f.Per100Grams[nutrition.ProteinGrams] = new(protein)
	f.Per100Grams[nutrition.CarbohydrateGrams] = new(carbohydrate)
	f.Per100Grams[nutrition.SugarsGrams] = new(sugars)
	f.Per100Grams[nutrition.FatGrams] = new(fat)
	if edit != nil {
		edit(f)
	}
	return f
}

// The foods of the tests.
var (
	coconutMilk = food("Coconut milk", 200, 2, 3, 2, 20, func(f *foods.Food) { f.PackageGrams = 400; f.ShelfLifeDays = 365 })
	spinach     = food("Spinach", 23, 3, 4, 0.4, 0.4, func(f *foods.Food) { f.ShelfLifeDays = 3; f.StoreSection = foods.StoreSectionProduce })
	onions      = food("Onions", 40, 1, 9, 4, 0.1, func(f *foods.Food) { f.ShelfLifeDays = 30; f.GramsPerPiece = 150 })
	tofu        = food("Tofu", 144, 15, 3, 0.6, 9, func(f *foods.Food) { f.PackageGrams = 400; f.ShelfLifeDays = 30 })
	chickpeas   = food("Chickpeas, canned", 139, 7, 23, 0, 2.6, func(f *foods.Food) { f.PackageGrams = 400; f.ShelfLifeDays = 730 })
	rice        = food("White rice", 360, 7, 80, 0, 0.6, func(f *foods.Food) { f.ShelfLifeDays = 365 })
	basil       = food("Basil", 23, 3, 3, 0.3, 0.6, func(f *foods.Food) { f.ShelfLifeDays = 3; f.StoreSection = foods.StoreSectionProduce })
	cumin       = food("Cumin", 375, 18, 44, 2, 22, func(f *foods.Food) { f.AlwaysInStock = true })
	juice       = food("Apple juice", 46, 0.1, 11, 10, 0.1, func(f *foods.Food) { f.ShelfLifeDays = 60 })
	allFoods    = []*foods.Food{coconutMilk, spinach, onions, tofu, chickpeas, rice, basil, cumin, juice}
)

// ingredient is grams of a food in a recipe.
func ingredient(f *foods.Food, grams float64) *recipes.Ingredient {
	return &recipes.Ingredient{
		FoodID:   f.ID,
		Amount:   grams,
		Unit:     nutrition.Grams,
		FoodName: f.Name,
		Food:     *f.Nutrition(),
		FastCarb: f.FastCarb,
	}
}

func recipe(name string, servings int32, ingredients ...*recipes.Ingredient) *recipes.Recipe {
	return &recipes.Recipe{ID: store.NewUUID(), Name: name, Servings: servings, Ingredients: ingredients}
}

func eatingTime(name string, kind people.Kind, meal people.Meal) *people.EatingTime {
	return &people.EatingTime{ID: store.NewUUID(), Name: name, Kind: kind, Meal: meal}
}

// ben eats a regular breakfast, lunch and dinner, and aims for 60 g of
// protein a day.
func ben() *people.Person {
	return &people.Person{
		ID:   store.NewUUID(),
		Name: "Ben",
		EatingTimes: []*people.EatingTime{
			eatingTime("Breakfast", people.KindMain, people.MealBreakfast),
			eatingTime("Lunch", people.KindMain, people.MealLunch),
			eatingTime("Dinner", people.KindMain, people.MealDinner),
		},
		Targets: []rules.Target{{Nutrient: nutrition.ProteinGrams, Min: new(60.0)}},
	}
}

// anna eats small portions of the shared meals with small meals between,
// with her care team's rules.
func anna() *people.Person {
	return &people.Person{
		ID:   store.NewUUID(),
		Name: "Anna",
		EatingTimes: []*people.EatingTime{
			eatingTime("Breakfast", people.KindMain, people.MealBreakfast),
			eatingTime("Mid-morning", people.KindSmall, people.MealNone),
			eatingTime("Lunch", people.KindMain, people.MealLunch),
			eatingTime("Mid-afternoon", people.KindSmall, people.MealNone),
			eatingTime("Dinner", people.KindMain, people.MealDinner),
			eatingTime("Evening", people.KindSmall, people.MealNone),
		},
		Rules: rules.Rules{
			MaxPortionGrams:      new(200.0),
			MinProteinGrams:      new(10.0),
			MaxCarbohydrateGrams: new(30.0),
			MinPairingRatio:      new(0.5),
			MaxSugarsGrams:       new(10.0),
			WarnFastCarbs:        true,
		},
		Targets: []rules.Target{
			{Nutrient: nutrition.ProteinGrams, Min: new(60.0), Max: new(120.0)},
		},
	}
}

func etID(p *people.Person, name string) pgtype.UUID {
	for _, et := range p.EatingTimes {
		if et.Name == name {
			return et.ID
		}
	}
	panic("no eating time " + name)
}

func foodsByID() map[pgtype.UUID]*foods.Food {
	m := map[pgtype.UUID]*foods.Food{}
	for _, f := range allFoods {
		m[f.ID] = f
	}
	return m
}

// input returns the input of a dinner on a date for the people, with the
// recipes, the shop on Monday and nothing else planned.
func input(date time.Time, ps []*people.Person, rs ...*recipes.Recipe) *Input {
	return &Input{
		Date:     date,
		Meal:     people.MealDinner,
		People:   ps,
		ShopDate: monday,
		Week:     &plans.Plan{From: monday, To: sunday},
		Planned:  map[plans.Slot]*rules.EatingTime{},
		Recipes:  rs,
		Foods:    foodsByID(),
	}
}

// cook adds a cook of a recipe on a date to the input's week.
func (in *Input) cook(r *recipes.Recipe, date time.Time, servings float64) {
	c := &plans.Cook{ID: store.NewUUID(), RecipeID: r.ID, RecipeName: r.Name, Date: date, Servings: servings, Meal: people.MealDinner}
	in.Week.Cooks = append(in.Week.Cooks, c)
	in.Cooks = append(in.Cooks, c)
	if !slices.Contains(in.Recipes, r) {
		in.Recipes = append(in.Recipes, r)
	}
}

// suggestionOf returns the suggestion of a recipe.
func suggestionOf(t *testing.T, list []*Suggestion, r *recipes.Recipe) *Suggestion {
	t.Helper()
	for _, s := range list {
		if s.Recipe == r {
			return s
		}
	}
	t.Fatalf("no suggestion of %s", r.Name)
	return nil
}

// components describes a suggestion's components of a kind, e.g. "+6 uses
// the other 200 g ...".
func components(s *Suggestion, kind Kind) []string {
	var lines []string
	for _, c := range s.Components {
		if c.Kind == kind {
			lines = append(lines, format(c.Points)+" "+c.Reason)
		}
	}
	return lines
}

func wantComponents(t *testing.T, s *Suggestion, kind Kind, want ...string) {
	t.Helper()
	if got := components(s, kind); !slices.Equal(got, want) {
		t.Errorf("%s: got components\n%s\nwant\n%s", s.Recipe.Name, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestSharedIngredients(t *testing.T) {
	t.Parallel()
	b := ben()
	curry := recipe("Spinach curry", 2, ingredient(spinach, 200), ingredient(onions, 150), ingredient(cumin, 5))
	saag := recipe("Saag tofu", 2, ingredient(spinach, 200), ingredient(onions, 150), ingredient(tofu, 400), ingredient(cumin, 5))
	fried := recipe("Fried rice", 2, ingredient(rice, 150), ingredient(tofu, 200), ingredient(basil, 10))
	in := input(wednesday, []*people.Person{b}, saag, fried)
	in.cook(curry, tuesday, 2)
	list := Suggest(in)

	// The saag shares the fresh spinach and the onions with the curry, but
	// not the cumin, always in stock, and buys only tofu.
	s := suggestionOf(t, list, saag)
	wantComponents(t, s, SharedIngredients,
		"4 shares spinach and onions with Tuesday's Spinach curry",
		"2 only 1 new item to buy",
	)
	if s.NewItemCount != 1 || len(s.ItemsToBuy) != 3 {
		t.Errorf("got %d new items in %v, want tofu new and spinach and onions more", s.NewItemCount, s.ItemsToBuy)
	}
	for _, it := range s.ItemsToBuy {
		if it.Food == tofu && (it.AlreadyListed || it.AmountText != "1 × 400 g") {
			t.Errorf("got %+v, want a new pack of tofu", it)
		}
		if it.Food == spinach && (!it.AlreadyListed || it.AmountText != "100 g more") {
			t.Errorf("got %+v, want 100 g more spinach", it)
		}
	}
	// The fried rice shares nothing, and buys three new items.
	wantComponents(t, suggestionOf(t, list, fried), SharedIngredients)
	// The curry isn't sharing with itself.
	wantComponents(t, suggestionOf(t, list, curry), SharedIngredients, "3 nothing new to buy")
}

func TestPackLeftovers(t *testing.T) {
	t.Parallel()
	b := ben()
	thai := recipe("Thai curry", 2, ingredient(coconutMilk, 200), ingredient(tofu, 400))
	soup := recipe("Coconut soup", 1, ingredient(coconutMilk, 200), ingredient(spinach, 100))
	stew := recipe("Coconut stew", 1, ingredient(coconutMilk, 100))
	in := input(thursday, []*people.Person{b}, soup, stew)
	in.cook(thai, tuesday, 2)
	list := Suggest(in)

	// The curry buys a 400 g can for 200 g: the soup uses the rest, and
	// the stew half of it.
	wantComponents(t, suggestionOf(t, list, soup), PackLeftovers,
		"6 uses the other 200 g of the 400 g pack of coconut milk bought for Tuesday's Thai curry")
	wantComponents(t, suggestionOf(t, list, stew), PackLeftovers,
		"3 uses 100 g of the 200 g of coconut milk left from the 400 g pack bought for Tuesday's Thai curry")
	if s := suggestionOf(t, list, soup); slices.ContainsFunc(s.ItemsToBuy, func(it ItemToBuy) bool { return it.Food == coconutMilk }) {
		t.Errorf("got items %v, want no more coconut milk", s.ItemsToBuy)
	}
}

func TestPantry(t *testing.T) {
	t.Parallel()
	b := ben()
	saag := recipe("Saag", 1, ingredient(spinach, 150), ingredient(chickpeas, 400))
	lots := []*pantry.Item{
		{FoodID: spinach.ID, Grams: 200, ExpiryDate: wednesday},
		{FoodID: chickpeas.ID, Grams: 400, ExpiryDate: date("2027-06-01")},
		{FoodID: cumin.ID, Grams: 50},
	}
	for name, test := range map[string]struct {
		date time.Time
		want []string
	}{
		"before the spinach's use-by": {tuesday, []string{
			"6 uses 150 g of spinach from the pantry, best used by Wed 4 Nov",
			"2 uses 400 g of chickpeas, canned from the pantry",
		}},
		// On Thursday, the spinach has gone off.
		"after": {thursday, []string{"2 uses 400 g of chickpeas, canned from the pantry"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := input(test.date, []*people.Person{b}, saag)
			in.Lots = lots
			wantComponents(t, Suggest(in)[0], Pantry, test.want...)
		})
	}

	// The pantry covers the week's own needs first: with a cook taking all
	// the chickpeas on Monday, a recipe on Tuesday takes none.
	in := input(tuesday, []*people.Person{b}, saag)
	in.Lots = lots
	in.cook(recipe("Hummus", 4, ingredient(chickpeas, 400)), monday, 4)
	wantComponents(t, suggestionOf(t, Suggest(in), saag), Pantry,
		"6 uses 150 g of spinach from the pantry, best used by Wed 4 Nov")
}

func TestNutrition(t *testing.T) {
	t.Parallel()
	b, a := ben(), anna()
	// A serving is 100 g of tofu: 15 g of protein.
	tofuBowl := recipe("Tofu bowl", 1, ingredient(tofu, 100))
	in := input(wednesday, []*people.Person{b, a}, tofuBowl)
	// Ben has 31 g of protein planned that day, and Anna 110 g of her
	// maximum of 120 g.
	in.Planned[plans.Slot{PersonID: b.ID, EatingTimeID: etID(b, "Lunch"), Date: wednesday}] = &rules.EatingTime{
		Name: "Lunch", Amount: nutrition.Amount{Grams: 300, Nutrients: nutrition.Nutrients{nutrition.ProteinGrams: new(31.0)}},
	}
	in.Planned[plans.Slot{PersonID: a.ID, EatingTimeID: etID(a, "Lunch"), Date: wednesday}] = &rules.EatingTime{
		Name: "Lunch", Amount: nutrition.Amount{Grams: 150, Nutrients: nutrition.Nutrients{nutrition.ProteinGrams: new(110.0)}},
	}
	wantComponents(t, Suggest(in)[0], Nutrition,
		"3 adds 15 g of protein toward Ben's daily minimum of 60 g",
		"-4.5 takes Anna's protein to 125 g, over the daily maximum of 120 g",
	)
}

func TestRules(t *testing.T) {
	t.Parallel()
	a := anna()
	// 200 g of tofu bowl has 30 g of protein: Anna's minimum of 10 g is met.
	tofuBowl := recipe("Tofu bowl", 1, ingredient(tofu, 200))
	// 200 g of rice and juice has 101 g of carbohydrate, 6.7 g of protein
	// and fat, and 9.4 g of sugars from fast carbs.
	fastJuice := food("Apple juice", 46, 0.1, 11, 10, 0.1, func(f *foods.Food) { f.FastCarb = true })
	riceJuice := recipe("Rice and juice", 1, ingredient(rice, 100), ingredient(fastJuice, 100))
	in := input(wednesday, []*people.Person{a}, tofuBowl, riceJuice)
	in.Foods[fastJuice.ID] = fastJuice
	list := Suggest(in)

	s := suggestionOf(t, list, tofuBowl)
	if !s.FitsRules || len(s.Warnings) != 0 {
		t.Errorf("got %v, %v, want the tofu bowl to fit Anna's rules", s.FitsRules, s.Warnings)
	}
	wantComponents(t, s, Rules, "2 gives Anna 30 g of protein at Dinner, meeting the minimum of 10 g")

	s = suggestionOf(t, list, riceJuice)
	if s.FitsRules {
		t.Errorf("got fits_rules, want the rice to break Anna's rules")
	}
	wantComponents(t, s, Rules,
		"-8 Anna's Dinner would have 7.1 g of protein, under the minimum of 10 g",
		"-8 Anna's Dinner would have 91 g of carbohydrate, over the maximum of 30 g",
		"-8 Anna's Dinner would have a protein and fat to carbohydrate ratio of 0.09, under the minimum of 0.50",
		"-8 Anna's Dinner would have fast carbs: Apple juice",
	)
	if len(s.Warnings) != 4 || !strings.HasPrefix(s.Warnings[0].Message, "Anna: Dinner has") {
		t.Errorf("got warnings %v, want 4 with Anna's name", s.Warnings)
	}
	if list[0].Recipe != tofuBowl {
		t.Errorf("got %s first, want the tofu bowl", list[0].Recipe.Name)
	}
}

func TestEatingTimeSlot(t *testing.T) {
	t.Parallel()
	a := anna()
	// A serving of 300 g of stew, with 50 g already planned at
	// Mid-afternoon: half a serving fits in Anna's 200 g.
	stew := recipe("Chickpea stew", 1, ingredient(chickpeas, 200), ingredient(spinach, 100))
	stew.Tags = []string{"Snack"}
	in := input(wednesday, []*people.Person{a}, stew)
	in.Meal = people.MealNone
	in.EatingTimeID = etID(a, "Mid-afternoon")
	slot := plans.Slot{PersonID: a.ID, EatingTimeID: in.EatingTimeID, Date: wednesday}
	in.Planned[slot] = &rules.EatingTime{Name: "Mid-afternoon", Amount: nutrition.Amount{Grams: 50}}
	s := Suggest(in)[0]
	if len(s.Portions) != 1 || s.Portions[0].Slot != slot || s.Portions[0].Servings != 0.5 || s.CookServings != 0.5 {
		t.Errorf("got portions %+v of %v servings, want half a serving at Mid-afternoon", s.Portions, s.CookServings)
	}
	wantComponents(t, s, MealTag, "2 tagged snack")
}

func TestKeeping(t *testing.T) {
	t.Parallel()
	// Cara has dinner last, so the rest of her serving goes to the next
	// day's snack.
	cara := &people.Person{
		ID:   store.NewUUID(),
		Name: "Cara",
		EatingTimes: []*people.EatingTime{
			eatingTime("Snack", people.KindSmall, people.MealNone),
			eatingTime("Dinner", people.KindMain, people.MealDinner),
		},
		Rules: rules.Rules{MaxPortionGrams: new(200.0)},
	}
	pesto := recipe("Pesto pasta", 1, ingredient(basil, 50), ingredient(rice, 350))
	for name, test := range map[string]struct {
		date       time.Time
		fridgeDays *int32
		want       []string
	}{
		"basil keeps until Tuesday": {tuesday, new(int32(2)), []string{
			"1 keeps 2 days in the fridge, for Cara's leftovers on Wed 4 Nov",
		}},
		"basil keeps until Thursday": {thursday, new(int32(2)), []string{
			"2 its fresh foods keep from the shop on Mon 2 Nov until Thu 5 Nov",
			"1 keeps 2 days in the fridge, for Cara's leftovers on Fri 6 Nov",
		}},
		"basil won't keep until Saturday": {saturday, nil, []string{
			"-4 basil keeps 3 days, so it won't keep from the shop on Mon 2 Nov until Sat 7 Nov: buy it on a second trip",
		}},
		"leftovers won't keep": {wednesday, new(int32(0)), []string{
			"-5 keeps 0 days in the fridge, not until Cara's leftovers on Thu 5 Nov",
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := *pesto
			r.FridgeDays = test.fridgeDays
			wantComponents(t, Suggest(input(test.date, []*people.Person{cara}, &r))[0], Keeping, test.want...)
		})
	}
}

func TestFlavor(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		names []string
		want  []string
	}{
		"chickpeas and cumin": {[]string{"Chickpeas, canned", "Ground cumin"}, []string{"chickpeas and cumin"}},
		"three foods":         {[]string{"Firm tofu", "Fresh ginger", "Soy sauce (tamari)"}, []string{"tofu, ginger and soy sauce"}},
		"alternatives":        {[]string{"Garbanzo beans", "Tahini"}, []string{"chickpeas and tahini"}},
		"not within words":    {[]string{"Chickpeas", "Slime"}, nil},
		"one ingredient":      {[]string{"Coconut milk curry paste"}, nil},
		"missing a food":      {[]string{"Tofu", "Ginger"}, nil},
		"several":             {[]string{"Red lentils", "Tomatoes, canned", "Cumin seeds"}, []string{"lentils and tomato", "lentils and cumin"}},
		"after punctuation":   {[]string{"Beans, black", "Lime (juice)", "Spinach,garlic"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, p := range matchPairings(test.names) {
				got = append(got, p.Name)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}

	// A recipe gets points for at most two pairings.
	b := ben()
	dal := recipe("Dal", 2, ingredient(food("Red lentils", 0, 0, 0, 0, 0, nil), 200), ingredient(food("Tomatoes", 0, 0, 0, 0, 0, nil), 200), ingredient(cumin, 5),
		ingredient(spinach, 100), ingredient(food("Garlic", 0, 0, 0, 0, 0, nil), 10))
	in := input(wednesday, []*people.Person{b}, dal)
	for _, ing := range dal.Ingredients {
		if _, ok := in.Foods[ing.FoodID]; !ok {
			in.Foods[ing.FoodID] = &foods.Food{ID: ing.FoodID, Name: ing.FoodName}
		}
	}
	wantComponents(t, Suggest(in)[0], Flavor, "1.5 lentils and tomato go well together", "1.5 lentils and cumin go well together")
}

func TestVariety(t *testing.T) {
	t.Parallel()
	b := ben()
	curry := recipe("Curry", 2, ingredient(chickpeas, 400))
	for name, test := range map[string]struct {
		date time.Time
		want []string
	}{
		"the next day": {wednesday, []string{"-9 already planned on Tue 3 Nov"}},
		"later":        {sunday, []string{"-6 already planned on Tue 3 Nov"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := input(test.date, []*people.Person{b})
			in.cook(curry, tuesday, 2)
			wantComponents(t, Suggest(in)[0], Variety, test.want...)
		})
	}
}

func TestMealTag(t *testing.T) {
	t.Parallel()
	b := ben()
	tagged := func(tags ...string) *recipes.Recipe {
		r := recipe(strings.Join(tags, ","), 1)
		r.Tags = tags
		return r
	}
	dinner, lunch, oats, none := tagged("Dinner", "quick"), tagged("lunch"), tagged("breakfast"), tagged("batch-friendly")
	list := Suggest(input(wednesday, []*people.Person{b}, dinner, lunch, oats, none))
	wantComponents(t, suggestionOf(t, list, dinner), MealTag, "2 tagged dinner")
	wantComponents(t, suggestionOf(t, list, lunch), MealTag)
	wantComponents(t, suggestionOf(t, list, oats), MealTag, "-2 tagged breakfast, not dinner")
	wantComponents(t, suggestionOf(t, list, none), MealTag)
}

func TestSuggest(t *testing.T) {
	t.Parallel()
	b, a := ben(), anna()
	// The week: a Thai curry on Tuesday buys a can of coconut milk for
	// half of it, and the pantry has spinach to use by Wednesday.
	thai := recipe("Thai curry", 2, ingredient(coconutMilk, 200), ingredient(tofu, 200), ingredient(rice, 150))
	// The soup uses the rest of the can and the spinach, with tofu for
	// protein; the pasta buys basil and rice; the rice pudding is sweet.
	soup := recipe("Coconut spinach soup", 2, ingredient(coconutMilk, 200), ingredient(spinach, 150), ingredient(tofu, 200))
	soup.FridgeDays = new(int32(3))
	pasta := recipe("Basil rice", 2, ingredient(basil, 30), ingredient(rice, 200), ingredient(onions, 150))
	pudding := recipe("Rice pudding", 2, ingredient(rice, 120), ingredient(juice, 200))
	in := input(wednesday, []*people.Person{a, b}, pasta, pudding, soup)
	in.cook(thai, tuesday, 2)
	in.Lots = []*pantry.Item{{FoodID: spinach.ID, Grams: 150, ExpiryDate: wednesday}}
	in.Max = 3
	list := Suggest(in)

	// The soup comes first. The Thai curry, planned the day before, comes
	// before the basil rice and the rice pudding, which break Anna's rules
	// on carbohydrate.
	var names []string
	for _, s := range list {
		names = append(names, s.Recipe.Name)
	}
	if want := []string{"Coconut spinach soup", "Thai curry", "Basil rice"}; !slices.Equal(names, want) {
		t.Errorf("got %v, want %v", names, want)
	}
	soupSuggestion := list[0]
	for _, want := range []string{
		"uses the other 200 g of the 400 g pack of coconut milk bought for Tuesday's Thai curry",
		"uses 150 g of spinach from the pantry, best used by Wed 4 Nov",
	} {
		if !slices.ContainsFunc(soupSuggestion.Components, func(c Component) bool { return c.Reason == want }) {
			t.Errorf("got %v, want %q", soupSuggestion.Components, want)
		}
	}
	var total float64
	for i, c := range soupSuggestion.Components {
		total += c.Points
		if i > 0 && c.Points > soupSuggestion.Components[i-1].Points {
			t.Errorf("got components %v, want highest points first", soupSuggestion.Components)
		}
	}
	if diff := total - soupSuggestion.Score; diff > 0.05 || diff < -0.05 {
		t.Errorf("got score %v, want the sum of the components, %v", soupSuggestion.Score, total)
	}
	// Anna's serving is split like PlanMeal does: 200 g at dinner and the
	// rest in the evening.
	var annas []string
	for _, p := range soupSuggestion.Portions {
		if p.Person == a {
			annas = append(annas, p.EatingTime.Name+" "+format(p.Grams))
		}
	}
	if want := []string{"Dinner 198", "Evening 77"}; !slices.Equal(annas, want) {
		t.Errorf("got Anna's portions %v, want %v", annas, want)
	}
	if soupSuggestion.CookServings != 2 {
		t.Errorf("got %v servings to cook, want 2", soupSuggestion.CookServings)
	}

	// The same scores come out with more suggestions asked for, ties by
	// name.
	in.Max = 0
	if all := Suggest(in); len(all) != 4 || all[0].Score != list[0].Score {
		t.Errorf("got %d suggestions, want all 4", len(all))
	}
}
