package plans

import (
	"testing"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/rules"
)

// recipeOf returns a recipe's nutrition of a serving of kcal and protein.
func recipeOf(kcal, protein float64) *nutrition.Recipe {
	r := &nutrition.Recipe{GramsPerServing: 300}
	r.PerServing[nutrition.EnergyKcal] = &kcal
	r.PerServing[nutrition.ProteinGrams] = &protein
	return r
}

func TestServingsToward(t *testing.T) {
	t.Parallel()
	date := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	min, max := 90.0, 600.0
	tests := []struct {
		name    string
		targets []rules.Target
		// lunch is the energy and protein planned at lunch already.
		lunch  *nutrition.Recipe
		recipe *nutrition.Recipe
		want   float64
	}{
		{"no targets", nil, nil, recipeOf(500, 20), 1},
		// Dinner is a third of the day, so it should have 30 g of protein:
		// a serving has 20.
		{"protein minimum", []rules.Target{{Nutrient: nutrition.ProteinGrams, Min: &min}}, nil, recipeOf(500, 20), 1.5},
		// A serving has 30 g, a third of the day's minimum already.
		{"minimum met", []rules.Target{{Nutrient: nutrition.ProteinGrams, Min: &min}}, nil, recipeOf(500, 30), 1},
		// Lunch has 500 kcal, so dinner has room for 100 of the day's 600.
		{"energy maximum", []rules.Target{{Nutrient: nutrition.EnergyKcal, Max: &max}}, recipeOf(500, 20), recipeOf(400, 20), 0.5},
		// The maximum counts for the whole day, so a first meal under it
		// isn't sized down.
		{"maximum not reached", []rules.Target{{Nutrient: nutrition.EnergyKcal, Max: &max}}, nil, recipeOf(400, 20), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ben, _ := household(0)
			ben.Targets = tt.targets
			planned := map[Slot]*rules.EatingTime{}
			if tt.lunch != nil {
				planned[Slot{PersonID: ben.ID, EatingTimeID: ben.EatingTimes[1].ID, Date: date}] = &rules.EatingTime{Amount: tt.lunch.Servings(1)}
			}
			if got := ServingsToward(ben, tt.recipe, date, people.MealDinner, planned); got != tt.want {
				t.Errorf("got %v servings, want %v", got, tt.want)
			}
		})
	}
}

// TestSplitMealServingsFor checks that a meal's shares follow each
// person's servings, and that it cooks whole servings.
func TestSplitMealServingsFor(t *testing.T) {
	t.Parallel()
	ben, anna := household(0)
	anna.Rules.MaxPortionGrams = nil
	split, err := SplitMeal(&SplitInput{
		Date:            time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
		Meal:            people.MealDinner,
		People:          []*people.Person{ben, anna},
		GramsPerServing: 300,
		ServingsFor: func(p *people.Person) float64 {
			if p == ben {
				return 1.5
			}
			return 0.75
		},
	})
	if err != nil {
		t.Fatalf("SplitMeal: %v", err)
	}
	if len(split.Shares) != 2 || split.Shares[0].Servings != 1.5 || split.Shares[1].Servings != 0.75 {
		t.Errorf("got shares %v, want 1.5 for Ben and 0.75 for Anna", split.Shares)
	}
	if split.Servings != 3 || split.Unassigned != 0.75 {
		t.Errorf("got %v servings with %v unassigned, want 3 with 0.75", split.Servings, split.Unassigned)
	}
}
