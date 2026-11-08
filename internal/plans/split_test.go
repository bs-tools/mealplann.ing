package plans

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// household returns the two people of the household the app is for: Ben,
// who eats a regular breakfast, lunch and dinner, and Anna, who eats six
// times a day with a maximum portion: small portions of the shared meals,
// and three small meals between them.
func household(maxPortionGrams float64) (ben, anna *people.Person) {
	et := func(name string, kind people.Kind, meal people.Meal) *people.EatingTime {
		return &people.EatingTime{ID: store.NewUUID(), Name: name, Kind: kind, Meal: meal}
	}
	ben = &people.Person{
		ID:   store.NewUUID(),
		Name: "Ben",
		EatingTimes: []*people.EatingTime{
			et("Breakfast", people.KindMain, people.MealBreakfast),
			et("Lunch", people.KindMain, people.MealLunch),
			et("Dinner", people.KindMain, people.MealDinner),
		},
	}
	anna = &people.Person{
		ID:   store.NewUUID(),
		Name: "Anna",
		EatingTimes: []*people.EatingTime{
			et("Breakfast", people.KindMain, people.MealBreakfast),
			et("Mid-morning", people.KindSmall, people.MealNone),
			et("Lunch", people.KindMain, people.MealLunch),
			et("Mid-afternoon", people.KindSmall, people.MealNone),
			et("Dinner", people.KindMain, people.MealDinner),
			et("Evening", people.KindSmall, people.MealNone),
		},
		Rules: rules.Rules{MaxPortionGrams: new(maxPortionGrams)},
	}
	return ben, anna
}

// eatingTime returns the ID of a person's eating time by name.
func eatingTime(p *people.Person, name string) pgtype.UUID {
	for _, et := range p.EatingTimes {
		if et.Name == name {
			return et.ID
		}
	}
	panic("no eating time " + name)
}

func TestSplitMeal(t *testing.T) {
	t.Parallel()
	monday := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	tuesday := monday.AddDate(0, 0, 1)
	ben, anna := household(200)
	share := func(p *people.Person, name string, date time.Time, servings float64) Share {
		return Share{Slot: Slot{PersonID: p.ID, EatingTimeID: eatingTime(p, name), Date: date}, Servings: servings}
	}
	tests := map[string]struct {
		in             SplitInput
		want           []Share
		wantServings   float64
		wantUnassigned float64
	}{
		"fits": {
			in:           SplitInput{Meal: people.MealDinner, GramsPerServing: 180},
			want:         []Share{share(ben, "Dinner", monday, 1), share(anna, "Dinner", monday, 1)},
			wantServings: 2,
		},
		"rest to the evening": {
			// 300 g is 0.66 of a serving at 200 g, and the rest, 0.34, goes
			// to the evening.
			in: SplitInput{Meal: people.MealDinner, GramsPerServing: 300},
			want: []Share{
				share(ben, "Dinner", monday, 1),
				share(anna, "Dinner", monday, 0.66),
				share(anna, "Evening", monday, 0.34),
			},
			wantServings: 2,
		},
		"rest to the next day": {
			// At 450 g a serving, 0.44 fits at dinner and the evening, and
			// the rest the next mid-morning.
			in: SplitInput{Meal: people.MealDinner, GramsPerServing: 450},
			want: []Share{
				share(ben, "Dinner", monday, 1),
				share(anna, "Dinner", monday, 0.44),
				share(anna, "Evening", monday, 0.44),
				share(anna, "Mid-morning", tuesday, 0.12),
			},
			wantServings: 2,
		},
		"lunch rest to the afternoon": {
			in: SplitInput{Meal: people.MealLunch, GramsPerServing: 250},
			want: []Share{
				share(ben, "Lunch", monday, 1),
				share(anna, "Lunch", monday, 0.8),
				share(anna, "Mid-afternoon", monday, 0.2),
			},
			wantServings: 2,
		},
		"planned already": {
			// 150 g are planned in the evening already, so 50 g fit there,
			// and the rest goes to the next day.
			in: SplitInput{
				Meal:            people.MealDinner,
				GramsPerServing: 300,
				PlannedGrams: map[Slot]float64{
					{PersonID: anna.ID, EatingTimeID: eatingTime(anna, "Evening"), Date: monday}: 150,
				},
			},
			want: []Share{
				share(ben, "Dinner", monday, 1),
				share(anna, "Dinner", monday, 0.66),
				share(anna, "Evening", monday, 0.16),
				share(anna, "Mid-morning", tuesday, 0.18),
			},
			wantServings: 2,
		},
		"unknown weight": {
			in:           SplitInput{Meal: people.MealDinner},
			want:         []Share{share(ben, "Dinner", monday, 1), share(anna, "Dinner", monday, 1)},
			wantServings: 2,
		},
		"more servings": {
			in:             SplitInput{Meal: people.MealDinner, GramsPerServing: 180, Servings: 4},
			want:           []Share{share(ben, "Dinner", monday, 1), share(anna, "Dinner", monday, 1)},
			wantServings:   4,
			wantUnassigned: 2,
		},
		"one person": {
			in:           SplitInput{Meal: people.MealBreakfast, GramsPerServing: 100, People: []*people.Person{anna}},
			want:         []Share{share(anna, "Breakfast", monday, 1)},
			wantServings: 1,
		},
		"too big to fit": {
			// At 2000 g a serving, a tenth fits at each of the small
			// eating times left, and the rest is left over.
			in: SplitInput{Meal: people.MealDinner, GramsPerServing: 2000, People: []*people.Person{anna}},
			want: []Share{
				share(anna, "Dinner", monday, 0.1),
				share(anna, "Evening", monday, 0.1),
				share(anna, "Mid-morning", tuesday, 0.1),
				share(anna, "Mid-afternoon", tuesday, 0.1),
				share(anna, "Evening", tuesday, 0.1),
			},
			wantServings:   1,
			wantUnassigned: 0.5,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := tt.in
			in.Date = monday
			if in.People == nil {
				in.People = []*people.Person{ben, anna}
			}
			got, err := SplitMeal(&in)
			if err != nil {
				t.Fatalf("SplitMeal: %v", err)
			}
			if len(got.Shares) != len(tt.want) {
				t.Fatalf("got shares %v, want %v", got.Shares, tt.want)
			}
			for i, s := range got.Shares {
				if s.Slot != tt.want[i].Slot || math.Abs(s.Servings-tt.want[i].Servings) > 1e-9 {
					t.Errorf("share %d is %v, want %v", i, s, tt.want[i])
				}
			}
			if got.Servings != tt.wantServings || math.Abs(got.Unassigned-tt.wantUnassigned) > 1e-9 {
				t.Errorf("got %v servings with %v unassigned, want %v with %v", got.Servings, got.Unassigned, tt.wantServings, tt.wantUnassigned)
			}
			// No share is over the maximum portion.
			for _, s := range got.Shares {
				if s.PersonID == anna.ID && in.GramsPerServing > 0 && s.Servings*in.GramsPerServing > 200+1e-9 {
					t.Errorf("share %v weighs %v g, over 200 g", s, s.Servings*in.GramsPerServing)
				}
			}
		})
	}
}

func TestSplitMealErrors(t *testing.T) {
	t.Parallel()
	monday := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	ben, anna := household(200)
	// Ben has no small meals, but no maximum portion either.
	_, err := SplitMeal(&SplitInput{Date: monday, Meal: people.MealDinner, People: []*people.Person{ben, anna}, GramsPerServing: 300, Servings: 1.5})
	if tooFew, ok := errors.AsType[*TooFewServingsError](err); !ok || tooFew.Needed != 2 {
		t.Errorf("got %v, want too few servings, needing 2", err)
	}
	nobody := &people.Person{ID: store.NewUUID(), Name: "Guest"}
	if _, err := SplitMeal(&SplitInput{Date: monday, Meal: people.MealLunch, People: []*people.Person{nobody}}); !errors.Is(err, ErrNoEatingTime) {
		t.Errorf("got %v, want ErrNoEatingTime", err)
	}
}
