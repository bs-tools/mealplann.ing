package rules

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
)

// amount returns an amount weighing grams with the given nutrients, the
// others unknown and incomplete.
func amount(grams float64, nutrients map[nutrition.Nutrient]float64) nutrition.Amount {
	a := nutrition.Amount{Grams: grams}
	for n := range nutrition.Count {
		if v, ok := nutrients[n]; ok {
			a.Nutrients[n] = new(v)
		} else {
			a.Incomplete[n] = true
		}
	}
	return a
}

func kinds(warnings []Warning) []Kind {
	var list []Kind
	for _, w := range warnings {
		list = append(list, w.Kind)
	}
	return list
}

func TestCheckEatingTime(t *testing.T) {
	t.Parallel()
	// The rules of someone after a gastrectomy, with reactive hypoglycemia.
	r := &Rules{
		MaxPortionGrams:      new(200.0),
		MinProteinGrams:      new(10.0),
		MaxCarbohydrateGrams: new(30.0),
		MinPairingRatio:      new(0.5),
		MaxSugarsGrams:       new(10.0),
		WarnFastCarbs:        true,
	}
	balanced := map[nutrition.Nutrient]float64{
		nutrition.ProteinGrams:      12,
		nutrition.FatGrams:          8,
		nutrition.CarbohydrateGrams: 20,
		nutrition.SugarsGrams:       3,
	}
	tests := map[string]struct {
		rules         *Rules
		et            EatingTime
		want          []Kind
		wantUncertain bool
	}{
		"fine": {
			et: EatingTime{Amount: amount(180, balanced)},
		},
		"nothing planned": {
			et: EatingTime{},
		},
		"no rules": {
			rules: &Rules{},
			et:    EatingTime{Amount: amount(900, map[nutrition.Nutrient]float64{nutrition.CarbohydrateGrams: 120}), FastCarbFoods: []string{"Dates"}},
		},
		"too large": {
			et:   EatingTime{Amount: amount(260, balanced)},
			want: []Kind{PortionTooLarge},
		},
		"at the limit": {
			et: EatingTime{Amount: amount(200.01, balanced)},
		},
		"protein low": {
			et: EatingTime{Amount: amount(150, map[nutrition.Nutrient]float64{
				nutrition.ProteinGrams: 6, nutrition.FatGrams: 10, nutrition.CarbohydrateGrams: 20, nutrition.SugarsGrams: 2,
			})},
			want: []Kind{ProteinLow},
		},
		"protein maybe low": {
			et: func() EatingTime {
				a := amount(150, balanced)
				a.Nutrients[nutrition.ProteinGrams] = new(6.0)
				a.Incomplete[nutrition.ProteinGrams] = true
				return EatingTime{Amount: a}
			}(),
			want:          []Kind{ProteinLow},
			wantUncertain: true,
		},
		"carbohydrate high and unpaired": {
			et: EatingTime{Amount: amount(150, map[nutrition.Nutrient]float64{
				nutrition.ProteinGrams: 11, nutrition.FatGrams: 2, nutrition.CarbohydrateGrams: 40, nutrition.SugarsGrams: 4,
			})},
			want: []Kind{CarbohydrateHigh, CarbohydrateUnpaired},
		},
		"no carbohydrate is paired": {
			et: EatingTime{Amount: amount(100, map[nutrition.Nutrient]float64{
				nutrition.ProteinGrams: 15, nutrition.FatGrams: 5, nutrition.CarbohydrateGrams: 0, nutrition.SugarsGrams: 0,
			})},
		},
		"sugars high": {
			et: EatingTime{Amount: amount(150, map[nutrition.Nutrient]float64{
				nutrition.ProteinGrams: 12, nutrition.FatGrams: 8, nutrition.CarbohydrateGrams: 20, nutrition.SugarsGrams: 15,
			})},
			want: []Kind{SugarsHigh},
		},
		"fast carbs": {
			et:   EatingTime{Amount: amount(150, balanced), FastCarbFoods: []string{"White rice"}},
			want: []Kind{FastCarbs},
		},
		"weight unknown": {
			et: func() EatingTime {
				a := amount(150, balanced)
				a.UnknownWeight = true
				return EatingTime{Amount: a, UnweighedNames: []string{"Chana masala"}}
			}(),
			want:          []Kind{WeightUnknown},
			wantUncertain: true,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rules := tt.rules
			if rules == nil {
				rules = r
			}
			et := tt.et
			et.Name = "Mid-morning"
			got := CheckEatingTime(rules, &et)
			if !slices.Equal(kinds(got), tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for _, w := range got {
				if w.Uncertain != tt.wantUncertain {
					t.Errorf("got uncertain %v, want %v: %v", w.Uncertain, tt.wantUncertain, w)
				}
				if !strings.HasPrefix(w.Message, "Mid-morning") {
					t.Errorf("message %q doesn't name the eating time", w.Message)
				}
			}
		})
	}
}

func TestCheckEatingTimeMessages(t *testing.T) {
	t.Parallel()
	r := &Rules{MaxPortionGrams: new(200.0), MinPairingRatio: new(0.5)}
	got := CheckEatingTime(r, &EatingTime{Name: "Evening", Amount: amount(260, map[nutrition.Nutrient]float64{
		nutrition.ProteinGrams: 4, nutrition.FatGrams: 2, nutrition.CarbohydrateGrams: 30,
	})})
	want := []string{
		"Evening is 260 g, over the maximum portion of 200 g: move 60 g or more to another small meal, or plan less.",
		"Evening's carbohydrate isn't balanced: 6 g of protein and fat for 30 g of carbohydrate is a ratio of 0.2, under the minimum of 0.5. Add about 9 g of protein or fat, like nuts, seeds, tofu or soy yogurt, or plan less carbohydrate.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %d warnings", got, len(want))
	}
	for i, w := range got {
		if w.Message != want[i] {
			t.Errorf("got message %q, want %q", w.Message, want[i])
		}
	}
	if got[1].Value != 0.2 || got[1].Limit != 0.5 {
		t.Errorf("got ratio %v under %v, want 0.2 under 0.5", got[1].Value, got[1].Limit)
	}
}

func TestCheckDay(t *testing.T) {
	t.Parallel()
	targets := []Target{
		{Nutrient: nutrition.ProteinGrams, Min: new(60.0)},
		{Nutrient: nutrition.IronMilligrams, Min: new(14.0)},
		{Nutrient: nutrition.SodiumMilligrams, Max: new(2000.0)},
		{Nutrient: nutrition.EnergyKcal, Min: new(1500.0), Max: new(2200.0)},
	}
	day := &Day{Amount: amount(1200, map[nutrition.Nutrient]float64{
		nutrition.ProteinGrams:     50,
		nutrition.SodiumMilligrams: 2500,
		nutrition.EnergyKcal:       1800,
	})}
	// Iron is unknown, so it may be low.
	day.Amount.Nutrients[nutrition.IronMilligrams] = new(9.0)
	got := CheckDay(targets, day)
	if !slices.Equal(kinds(got), []Kind{TargetLow, TargetLow, TargetHigh}) {
		t.Fatalf("got %v, want protein and iron low, sodium high", got)
	}
	if got[0].Nutrient != nutrition.ProteinGrams || got[0].Uncertain ||
		got[0].Message != "Protein is 50 g, under the daily minimum of 60 g: add foods rich in protein." {
		t.Errorf("got %+v, want protein certainly low", got[0])
	}
	if got[1].Nutrient != nutrition.IronMilligrams || !got[1].Uncertain || !strings.HasPrefix(got[1].Message, "Iron may be low: at least 9 mg") {
		t.Errorf("got %+v, want iron maybe low", got[1])
	}
	if got[2].Nutrient != nutrition.SodiumMilligrams || got[2].Value != 2500 || got[2].Limit != 2000 {
		t.Errorf("got %+v, want sodium high", got[2])
	}

	// With eating times still to plan, low is uncertain, and says so.
	day.Unplanned = 2
	got = CheckDay(targets[:1], day)
	if len(got) != 1 || !got[0].Uncertain || !strings.HasSuffix(got[0].Message, "2 eating times are still unplanned.") {
		t.Errorf("got %v, want protein low with 2 eating times unplanned", got)
	}

	// An empty day has nothing to warn about.
	if got := CheckDay(targets, &Day{Unplanned: 6}); len(got) != 0 {
		t.Errorf("got %v for an empty day, want none", got)
	}
}

func TestCheckPortion(t *testing.T) {
	t.Parallel()
	monday := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return monday.AddDate(0, 0, n) }
	keeps3 := &Cook{RecipeName: "Chana masala", Date: monday, FridgeDays: new(int32(3))}
	freezable := &Cook{RecipeName: "Lentil soup", Date: monday, FridgeDays: new(int32(3)), Freezable: true}
	unknown := &Cook{RecipeName: "Stew", Date: monday}
	// The soup is frozen on Monday, and keeps 30 days frozen; the chili was
	// frozen though its recipe isn't marked freezable.
	frozen := &Cook{RecipeName: "Lentil soup", Date: monday, FridgeDays: new(int32(3)), Freezable: true, FreezerDays: new(int32(30)), FrozenServings: 2}
	frozenChili := &Cook{RecipeName: "Chili", Date: monday, FridgeDays: new(int32(3)), FrozenServings: 2}
	tests := map[string]struct {
		cook *Cook
		date time.Time
		want Kind
	}{
		"same day":         {cook: keeps3, date: monday},
		"within days":      {cook: keeps3, date: day(3)},
		"past fridge days": {cook: keeps3, date: day(4), want: LeftoverPastFridgeDays},
		"freeze":           {cook: freezable, date: day(5), want: LeftoverFreeze},
		"unknown":          {cook: unknown, date: day(10)},
		"before cook":      {cook: keeps3, date: day(-1), want: PortionBeforeCook},
		"frozen, fresh":    {cook: frozen, date: day(2)},
		"thaw":             {cook: frozen, date: day(20), want: LeftoverThaw},
		"last frozen day":  {cook: frozen, date: day(30), want: LeftoverThaw},
		"past freezer":     {cook: frozen, date: day(31), want: LeftoverPastFreezerDays},
		"frozen anyway":    {cook: frozenChili, date: day(60), want: LeftoverThaw},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := CheckPortion(tt.cook, tt.date)
			switch {
			case tt.want == 0 && got != nil:
				t.Errorf("got %v, want none", got)
			case tt.want != 0 && (got == nil || got.Kind != tt.want):
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
	got := CheckPortion(keeps3, day(4))
	if want := "Chana masala cooked on Mon 2 Nov keeps 3 days in the fridge, so it won't keep until Fri 6 Nov: eat it by Thu 5 Nov, or plan another cook."; got.Message != want {
		t.Errorf("got message %q, want %q", got.Message, want)
	}
	if got := CheckPortion(freezable, day(5)); !got.Info {
		t.Errorf("freezing is %v, want info", got)
	}
	if got := CheckPortion(frozen, day(20)); !got.Info || got.Message != "This portion of Lentil soup comes from the freezer: thaw it in the fridge on Sat 21 Nov." {
		t.Errorf("got %+v, want a reminder to thaw it the day before", got)
	}
	if got := CheckPortion(frozen, day(31)); got.Info ||
		got.Message != "Lentil soup frozen on Mon 2 Nov keeps 30 days in the freezer, so it won't keep until Thu 3 Dec: eat it by Wed 2 Dec, or plan another cook." {
		t.Errorf("got %+v, want a warning that it won't keep frozen", got)
	}
}

func TestCheckServings(t *testing.T) {
	t.Parallel()
	c := &Cook{RecipeName: "Dal", Servings: 4}
	if got := CheckServings(c, 4); got != nil {
		t.Errorf("got %v for all servings planned, want none", got)
	}
	if got := CheckServings(c, 3.98); got != nil {
		t.Errorf("got %v for servings planned up to rounding, want none", got)
	}
	got := CheckServings(c, 2.5)
	if got == nil || got.Kind != ServingsUnassigned || got.Value != 1.5 || !got.Info ||
		got.Message != "1.5 of the 4 servings of Dal aren't planned for anyone: plan them as leftovers, freeze them, or cook fewer servings." {
		t.Errorf("got %+v, want 1.5 servings unassigned", got)
	}
	if got := CheckServings(c, 5); got == nil || got.Kind != ServingsOverassigned || got.Info {
		t.Errorf("got %+v, want servings overassigned", got)
	}
	// A cook nobody eats any of is likely left behind, so it warns.
	got = CheckServings(c, 0)
	if got == nil || got.Kind != ServingsUnassigned || got.Info || got.Value != 4 ||
		got.Message != "Nobody is planned to eat any of the 4 servings of Dal: plan it for a meal, or delete the cook if it was left behind." {
		t.Errorf("got %+v, want a warning for a cook nobody eats", got)
	}
	// Once cooked, it's leftovers in the kitchen, only noted.
	if got := CheckServings(&Cook{RecipeName: "Dal", Servings: 4, Done: true}, 0); got == nil || !got.Info {
		t.Errorf("got %+v for a cooked one nobody eats yet, want a note", got)
	}
	// Frozen servings are kept, not left behind.
	if got := CheckServings(&Cook{RecipeName: "Dal", Servings: 4, FrozenServings: 4}, 0); got != nil {
		t.Errorf("got %+v for a cook frozen whole, want none", got)
	}

	// With 2 servings frozen and 1 of them planned, the other is in the
	// freezer, not unassigned.
	frozen := &Cook{RecipeName: "Dal", Servings: 4, FrozenServings: 2, LateServings: 1}
	if got := CheckServings(frozen, 3); got != nil {
		t.Errorf("got %v with a serving in the freezer, want none", got)
	}
	if got := CheckServings(frozen, 2.5); got == nil || got.Kind != ServingsUnassigned || got.Value != 0.5 {
		t.Errorf("got %+v, want half a serving unassigned beside the frozen one", got)
	}
	// Planning 3 servings after the fridge days needs 3 frozen.
	short := &Cook{RecipeName: "Dal", Servings: 4, FrozenServings: 2, LateServings: 3}
	if got := CheckServings(short, 4); got == nil || got.Kind != FrozenServingsShort || got.Info ||
		got.Message != "3 servings of Dal are planned after it keeps in the fridge, but only 2 are frozen: freeze more, or eat them sooner." {
		t.Errorf("got %+v, want too few frozen", got)
	}
}

func TestCheckSupply(t *testing.T) {
	t.Parallel()
	friday := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	if got := CheckSupply("Chili", friday, "Cooked chickpeas", friday.AddDate(0, 0, -1)); got != nil {
		t.Errorf("got %v for chickpeas cooked the day before, want none", got)
	}
	if got := CheckSupply("Chili", friday, "Cooked chickpeas", friday); got != nil {
		t.Errorf("got %v for chickpeas cooked the same day, want none", got)
	}
	got := CheckSupply("Chili", friday, "Cooked chickpeas", friday.AddDate(0, 0, 1))
	if got == nil || got.Kind != ComponentAfterCook || got.Info ||
		got.Message != "The Cooked chickpeas for Chili on Fri 6 Nov is cooked on Sat 7 Nov, after it: move its cook earlier, or have another cook make it." {
		t.Errorf("got %+v, want the chickpeas cooked too late", got)
	}
}
