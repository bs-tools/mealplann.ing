package nutrition

import "testing"

func TestLabelAndUnit(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		n     Nutrient
		label string
		unit  string
	}{
		{EnergyKcal, "Energy", "kcal"},
		{ProteinGrams, "Protein", "g"},
		{IronMilligrams, "Iron", "mg"},
		{VitaminB12Micrograms, "Vitamin B12", "µg"},
		{Omega3ALAGrams, "Omega-3 (ALA)", "g"},
	} {
		if got := tt.n.Label(); got != tt.label {
			t.Errorf("%v.Label() = %q, want %q", tt.n, got, tt.label)
		}
		if got := tt.n.Unit(); got != tt.unit {
			t.Errorf("%v.Unit() = %q, want %q", tt.n, got, tt.unit)
		}
	}
	for n := range Count {
		if n.Label() == "" {
			t.Errorf("%v has no label", n)
		}
		if got, ok := ParseNutrient(n.String()); !ok || got != n {
			t.Errorf("ParseNutrient(%q) = %v, %v, want %v", n.String(), got, ok, n)
		}
	}
	if _, ok := ParseNutrient("calories"); ok {
		t.Error("ParseNutrient(calories) is a nutrient")
	}
}

func TestAmount(t *testing.T) {
	t.Parallel()
	// 200 g of a food with protein and no iron data.
	food := &Food{Per100Grams: nutrients(map[Nutrient]float64{ProteinGrams: 10, CarbohydrateGrams: 20})}
	a := food.AmountOf(200, Grams)
	if a.Grams != 200 || a.UnknownWeight || *a.Nutrients[ProteinGrams] != 20 || a.Nutrients[IronMilligrams] != nil ||
		a.Incomplete[ProteinGrams] || !a.Incomplete[IronMilligrams] {
		t.Errorf("got %+v, want 200 g with 20 g of protein and unknown iron", a)
	}

	// Half a serving of a recipe of 2 servings with all nutrients known.
	recipe := ForRecipe([]Ingredient{{Food: &Food{Per100Grams: all(1)}, Amount: 400, Unit: Grams}}, 2)
	b := recipe.Servings(0.5)
	if !near(b.Grams, 100) || !near(*b.Nutrients[IronMilligrams], 1) || b.Incomplete[IronMilligrams] {
		t.Errorf("got %+v, want 100 g with 1 mg of iron", b)
	}

	// The sum keeps what's known and what isn't.
	var total Amount
	total.Add(&a)
	total.Add(&b)
	if !near(total.Grams, 300) || !near(*total.Nutrients[ProteinGrams], 21) || !near(*total.Nutrients[IronMilligrams], 1) ||
		total.Incomplete[ProteinGrams] || !total.Incomplete[IronMilligrams] {
		t.Errorf("got %+v, want 300 g, 21 g of protein, and at least 1 mg of iron", total)
	}
	if v, complete := total.Value(IronMilligrams); !near(v, 1) || complete {
		t.Errorf("Value(iron) = %v, %v, want 1, incomplete", v, complete)
	}

	// A food that can't be weighed is unknown, and so is the sum.
	unweighed := (&Food{}).AmountOf(2, Pieces)
	total.Add(&unweighed)
	if !total.UnknownWeight || !total.Incomplete[ProteinGrams] || !near(total.Grams, 300) {
		t.Errorf("got %+v, want an unknown weight and incomplete protein", total)
	}

	// A recipe without ingredients weighs nothing known.
	if empty := ForRecipe(nil, 1).Servings(1); !empty.UnknownWeight || !empty.Incomplete[EnergyKcal] {
		t.Errorf("got %+v for an empty recipe, want unknown", empty)
	}
}
