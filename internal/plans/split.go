package plans

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/jackc/pgx/v5/pgtype"
)

// Slot is one of a person's eating times on a date.
type Slot struct {
	PersonID     pgtype.UUID
	EatingTimeID pgtype.UUID
	// Date is at midnight UTC.
	Date time.Time
}

// Share is servings of a shared meal eaten at a slot.
type Share struct {
	Slot
	Servings float64
}

// SplitInput is a shared meal to split into portions.
type SplitInput struct {
	// Date is the date the meal is eaten, at midnight UTC.
	Date time.Time
	// Meal is the shared meal.
	Meal people.Meal
	// People are the people eating it, each with an eating time for the
	// meal.
	People []*people.Person
	// GramsPerServing is the weight of a serving, 0 if unknown.
	GramsPerServing float64
	// PlannedGrams are the grams already planned at slots, which count
	// toward their maximum portion.
	PlannedGrams map[Slot]float64
	// Servings is how many servings to cook, 0 for one per person.
	Servings float64
}

// Split is a shared meal split into portions.
type Split struct {
	// Shares are the portions, by person and date.
	Shares []Share
	// Servings is how many servings to cook.
	Servings float64
	// Unassigned is how many of them nobody is planned to eat.
	Unassigned float64
}

// TooFewServingsError is returned when a meal's servings are fewer than the
// servings planned for people.
type TooFewServingsError struct {
	// Needed is the servings planned for people.
	Needed float64
}

func (e *TooFewServingsError) Error() string {
	return fmt.Sprintf("the meal needs at least %v servings", e.Needed)
}

// ErrNoEatingTime is returned when one of a split's people has no eating
// time for its meal.
var ErrNoEatingTime = errors.New("a person has no eating time for the meal")

const (
	// stepsPerServing is the precision of shares: a hundredth of a serving,
	// a few grams.
	stepsPerServing = 100
	// servingStep is the size of a step.
	servingStep = 1.0 / stepsPerServing
	// minShare is the smallest share worth planning at an eating time,
	// rather than leaving it over: a twentieth of a serving.
	minShare = 0.05
)

// SplitMeal splits a shared meal into portions. Each person eats one
// serving, at their eating time for the meal. If a serving weighs more
// than a person's maximum portion, less what's already planned there, their
// share is capped at it, and the rest of their serving goes to their small
// eating times after it that day, then the next day's, each capped the same
// way. What fits nowhere is left over, with any extra servings asked for.
// It returns ErrNoEatingTime if a person has no eating time for the meal,
// and a *TooFewServingsError if in.Servings is fewer than the shares.
func SplitMeal(in *SplitInput) (*Split, error) {
	split := &Split{}
	planned := map[Slot]float64{}
	for slot, grams := range in.PlannedGrams {
		planned[slot] = grams
	}
	var assigned float64
	for _, p := range in.People {
		index := -1
		for i, et := range p.EatingTimes {
			if et.Meal == in.Meal {
				index = i
				break
			}
		}
		if index < 0 {
			return nil, fmt.Errorf("%w: %s has none for %s", ErrNoEatingTime, p.Name, in.Meal)
		}
		at := Slot{PersonID: p.ID, EatingTimeID: p.EatingTimes[index].ID, Date: in.Date}
		limit := p.Rules.MaxPortionGrams
		if limit == nil || in.GramsPerServing <= 0 || in.GramsPerServing <= *limit-planned[at] {
			// The whole serving fits, or can't be weighed to be capped.
			split.Shares = append(split.Shares, Share{Slot: at, Servings: 1})
			planned[at] += in.GramsPerServing
			assigned++
			continue
		}
		// The slots the serving can go to: the meal's, then the small
		// eating times after it that day, then the next day's.
		slots := []Slot{at}
		next := in.Date.AddDate(0, 0, 1)
		for _, et := range p.EatingTimes[index+1:] {
			if et.Kind == people.KindSmall {
				slots = append(slots, Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: in.Date})
			}
		}
		for _, et := range p.EatingTimes {
			if et.Kind == people.KindSmall {
				slots = append(slots, Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: next})
			}
		}
		remaining := 1.0
		for _, slot := range slots {
			if remaining < minShare {
				break
			}
			room := (*limit - planned[slot]) / in.GramsPerServing
			share := min(remaining, floorStep(room))
			if share < minShare {
				continue
			}
			split.Shares = append(split.Shares, Share{Slot: slot, Servings: share})
			planned[slot] += share * in.GramsPerServing
			remaining = roundStep(remaining - share)
			assigned = roundStep(assigned + share)
		}
	}
	// Each person's serving is cooked, even a part of it that fits in none
	// of their eating times.
	servings := float64(len(in.People))
	if in.Servings > 0 {
		if in.Servings < assigned-servingStep/2 {
			return nil, &TooFewServingsError{Needed: assigned}
		}
		servings = in.Servings
	}
	split.Servings = servings
	split.Unassigned = max(roundStep(servings-assigned), 0)
	return split, nil
}

// floorStep rounds servings down to servingStep, so a capped share doesn't
// go over the cap.
func floorStep(v float64) float64 {
	// The small epsilon keeps 0.29999999 from becoming 0.29.
	return math.Floor(v*stepsPerServing+1e-9) / stepsPerServing
}

// roundStep rounds servings to servingStep, removing float noise.
func roundStep(v float64) float64 {
	return math.Round(v*stepsPerServing) / stepsPerServing
}

// minCookServings is the fewest servings a cook can have.
const minCookServings = 0.25

// EatingTimeServings returns the servings of a recipe for one person's
// eating time, and the servings to cook for it: a serving, or less if it
// weighs more than the person's maximum portion less what's planned there
// already, plannedGrams. If the eating time is full already, it's a
// portion of the maximum, which the rules then warn about. The cook is
// rounded up to minCookServings.
func EatingTimeServings(p *people.Person, gramsPerServing, plannedGrams float64) (servings, cookServings float64) {
	servings = 1.0
	if limit := p.Rules.MaxPortionGrams; limit != nil && gramsPerServing > 0 {
		room := *limit - plannedGrams
		if room < minShare*gramsPerServing {
			room = *limit
		}
		servings = max(min(1, floorStep(room/gramsPerServing)), minShare)
	}
	cookServings = max(math.Ceil(servings/minCookServings-1e-9)*minCookServings, minCookServings)
	return servings, cookServings
}
