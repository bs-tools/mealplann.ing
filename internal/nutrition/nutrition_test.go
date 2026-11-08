package nutrition

import (
	"math"
	"slices"
	"testing"
)

// nutrients returns nutrients with the given amounts, the others unknown.
func nutrients(amounts map[Nutrient]float64) Nutrients {
	var n Nutrients
	for k, v := range amounts {
		n[k] = new(v)
	}
	return n
}

// all returns nutrients with every amount known, as v.
func all(v float64) Nutrients {
	var n Nutrients
	for i := range n {
		n[i] = new(v)
	}
	return n
}

func near(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestNutrientString(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for n := range Count {
		name := n.String()
		if name == "" || name == "unknown" || seen[name] {
			t.Errorf("nutrient %d has name %q, want a unique field name", n, name)
		}
		seen[name] = true
	}
	if got := Omega3ALAGrams.String(); got != "omega3_ala_grams" {
		t.Errorf("Omega3ALAGrams = %q, want omega3_ala_grams", got)
	}
	if got := Count.String(); got != "unknown" {
		t.Errorf("Count = %q, want unknown", got)
	}
}

func TestGrams(t *testing.T) {
	t.Parallel()
	onion := &Food{GramsPerPiece: 150}
	soyMilk := &Food{GramsPerMilliliter: 1.03}
	tests := map[string]struct {
		food   *Food
		amount float64
		unit   Unit
		want   float64
		wantOK bool
	}{
		"grams":                 {food: &Food{}, amount: 250, unit: Grams, want: 250, wantOK: true},
		"pieces":                {food: onion, amount: 2, unit: Pieces, want: 300, wantOK: true},
		"pieces without weight": {food: soyMilk, amount: 2, unit: Pieces},
		"milliliters":           {food: soyMilk, amount: 200, unit: Milliliters, want: 206, wantOK: true},
		"milliliters without density": {
			food: onion, amount: 200, unit: Milliliters,
		},
		"no unit": {food: onion, amount: 1},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := tt.food.Grams(tt.amount, tt.unit)
			if !near(got, tt.want) || ok != tt.wantOK {
				t.Errorf("Grams(%v, %v) = %v, %v, want %v, %v", tt.amount, tt.unit, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestScale(t *testing.T) {
	t.Parallel()
	n := nutrients(map[Nutrient]float64{ProteinGrams: 10, EnergyKcal: 0})
	scaled := n.Scale(2.5)
	if *scaled[ProteinGrams] != 25 || *scaled[EnergyKcal] != 0 || scaled[IodineMicrograms] != nil {
		t.Errorf("got protein %v, energy %v and iodine %v, want 25, 0 and unknown",
			*scaled[ProteinGrams], *scaled[EnergyKcal], scaled[IodineMicrograms])
	}
	if *n[ProteinGrams] != 10 {
		t.Error("scaling changed the original")
	}
}

func TestForRecipe(t *testing.T) {
	t.Parallel()
	chickpeas := &Food{Per100Grams: all(10)}
	// Onions have every nutrient but iodine, and weigh 150 g each.
	onionNutrients := all(2)
	onionNutrients[IodineMicrograms] = nil
	onion := &Food{Per100Grams: onionNutrients, GramsPerPiece: 150}
	soyMilk := &Food{Per100Grams: all(4), GramsPerMilliliter: 1.0}
	// Garlic has no weight per piece.
	garlic := &Food{Per100Grams: all(30)}

	t.Run("complete", func(t *testing.T) {
		t.Parallel()
		r := ForRecipe([]Ingredient{
			{Food: chickpeas, Amount: 400, Unit: Grams},
			{Food: soyMilk, Amount: 200, Unit: Milliliters},
		}, 4)
		// 400 g with 10 per 100 g, and 200 g with 4 per 100 g, is 48 in
		// 600 g, or 12 in each of 4 servings of 150 g.
		if !near(r.TotalGrams, 600) || !near(r.GramsPerServing, 150) {
			t.Errorf("got %v g, %v g per serving, want 600 g, 150 g per serving", r.TotalGrams, r.GramsPerServing)
		}
		for n := range Count {
			if r.PerServing[n] == nil || !near(*r.PerServing[n], 12) {
				t.Errorf("%v per serving = %v, want 12", n, r.PerServing[n])
			}
			if r.Per100Grams[n] == nil || !near(*r.Per100Grams[n], 8) {
				t.Errorf("%v per 100 g = %v, want 8", n, r.Per100Grams[n])
			}
		}
		if len(r.Incomplete) != 0 || len(r.Issues) != 0 {
			t.Errorf("got incomplete %v and issues %v, want none", r.Incomplete, r.Issues)
		}
	})

	t.Run("unknown nutrients and weights", func(t *testing.T) {
		t.Parallel()
		r := ForRecipe([]Ingredient{
			{Food: chickpeas, Amount: 100, Unit: Grams},
			{Food: onion, Amount: 2, Unit: Pieces},
			{Food: garlic, Amount: 3, Unit: Pieces},
			// Optional ingredients aren't counted, nor reported.
			{Food: garlic, Amount: 1, Unit: Pieces, Optional: true},
			{Food: soyMilk, Amount: 1000, Unit: Grams, Optional: true},
		}, 2)
		// The garlic can't be weighed, so it's left out: 100 g of
		// chickpeas and 300 g of onions.
		if !near(r.TotalGrams, 400) || !near(r.GramsPerServing, 200) {
			t.Errorf("got %v g, %v g per serving, want 400 g, 200 g per serving", r.TotalGrams, r.GramsPerServing)
		}
		// Protein is 10 + 6 in 2 servings, iodine only the chickpeas' 10.
		if got := *r.PerServing[ProteinGrams]; !near(got, 8) {
			t.Errorf("protein per serving = %v, want 8", got)
		}
		if got := *r.PerServing[IodineMicrograms]; !near(got, 5) {
			t.Errorf("iodine per serving = %v, want the lower bound 5", got)
		}
		if got := *r.Per100Grams[ProteinGrams]; !near(got, 4) {
			t.Errorf("protein per 100 g = %v, want 4", got)
		}
		// The garlic makes every nutrient incomplete.
		if len(r.Incomplete) != int(Count) {
			t.Errorf("got incomplete %v, want all nutrients", r.Incomplete)
		}
		if len(r.Issues) != 2 {
			t.Fatalf("got issues %+v, want the onion's and the garlic's", r.Issues)
		}
		if got := r.Issues[0]; got.Index != 1 || got.UnknownWeight || !slices.Equal(got.UnknownNutrients, []Nutrient{IodineMicrograms}) {
			t.Errorf("got issue %+v, want the onion lacking iodine", got)
		}
		if got := r.Issues[1]; got.Index != 2 || !got.UnknownWeight || len(got.UnknownNutrients) != 0 {
			t.Errorf("got issue %+v, want the garlic's unknown weight", got)
		}
	})

	t.Run("only some nutrients incomplete", func(t *testing.T) {
		t.Parallel()
		r := ForRecipe([]Ingredient{
			{Food: chickpeas, Amount: 100, Unit: Grams},
			{Food: onion, Amount: 1, Unit: Pieces},
		}, 1)
		if !slices.Equal(r.Incomplete, []Nutrient{IodineMicrograms}) {
			t.Errorf("got incomplete %v, want only iodine", r.Incomplete)
		}
	})

	t.Run("nutrient no ingredient has", func(t *testing.T) {
		t.Parallel()
		plain := &Food{Per100Grams: nutrients(map[Nutrient]float64{EnergyKcal: 100})}
		r := ForRecipe([]Ingredient{{Food: plain, Amount: 50, Unit: Grams}}, 1)
		if r.PerServing[IodineMicrograms] != nil || r.Per100Grams[IodineMicrograms] != nil {
			t.Error("iodine is known, want unknown")
		}
		if got := *r.PerServing[EnergyKcal]; !near(got, 50) {
			t.Errorf("energy = %v, want 50", got)
		}
		// It isn't the ingredient's issue: fixing it would take every
		// ingredient's, and the recipe already has it unset.
		if len(r.Issues) != 0 {
			t.Errorf("got issues %+v, want none", r.Issues)
		}
		// Of two ingredients lacking iodine and one lacking only fiber,
		// which the other has, only the fiber is an issue.
		noFiber := all(1)
		noFiber[FiberGrams], noFiber[IodineMicrograms] = nil, nil
		r = ForRecipe([]Ingredient{
			{Food: &Food{Per100Grams: noFiber}, Amount: 50, Unit: Grams},
			{Food: onion, Amount: 1, Unit: Pieces},
		}, 1)
		if len(r.Issues) != 1 || r.Issues[0].Index != 0 || !slices.Equal(r.Issues[0].UnknownNutrients, []Nutrient{FiberGrams}) {
			t.Errorf("got issues %+v, want only the first ingredient's fiber", r.Issues)
		}
		if !slices.Contains(r.Incomplete, FiberGrams) || !slices.Contains(r.Incomplete, IodineMicrograms) {
			t.Errorf("got incomplete %v, want fiber and iodine", r.Incomplete)
		}
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		r := ForRecipe(nil, 0)
		if r.TotalGrams != 0 || r.GramsPerServing != 0 || r.PerServing[EnergyKcal] != nil || r.Per100Grams[EnergyKcal] != nil {
			t.Errorf("got %+v, want nothing", r)
		}
	})
}
