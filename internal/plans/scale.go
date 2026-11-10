package plans

import (
	"math"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/rules"
)

// servingSteps are the servings of a shared meal a person can be planned,
// as people serve themselves: from half a serving to two, in quarters.
var servingSteps = []float64{0.5, 0.75, 1, 1.25, 1.5, 1.75, 2}

const (
	// smallMealShare is how much of a day a small meal is, as a share of a
	// main meal, to spread daily targets over a day's eating times.
	smallMealShare = 0.5
	// changeCost is what each serving away from one costs, so a person
	// keeps one serving unless more or less gets them closer to their
	// targets.
	changeCost = 0.05
)

// ServingsToward returns how many servings of a shared meal of a recipe,
// on a date, brings a person closest to their daily targets, given what's
// planned at each slot: one, unless more or less, from half to two, gets
// them closer. A daily minimum counts for the share of the day planned,
// the meal's eating time included, so a meal isn't sized to meet a whole
// day's minimum alone, and a maximum for the whole day. The eating time's rules aren't sized for: a portion over
// the maximum spills into the next small meals (see SplitMeal), and
// shrinking a meal for one rule, like its carbohydrate, would break
// others, like its protein, and the day's. A person without daily targets
// has one serving.
func ServingsToward(p *people.Person, r *nutrition.Recipe, date time.Time, meal people.Meal, planned map[Slot]*rules.EatingTime) float64 {
	if r == nil || len(p.Targets) == 0 {
		return 1
	}
	var mealSlot Slot
	var day nutrition.Amount
	total, covered := 0.0, 0.0
	for _, et := range p.EatingTimes {
		weight := 1.0
		if et.Kind == people.KindSmall {
			weight = smallMealShare
		}
		total += weight
		slot := Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date}
		var amount *nutrition.Amount
		if planned[slot] != nil {
			amount = &planned[slot].Amount
			day.Add(amount)
		}
		switch {
		case et.Meal == meal && mealSlot.EatingTimeID.Valid:
			// Only the first eating time of the meal eats it.
		case et.Meal == meal:
			mealSlot = slot
			covered += weight
		case amount != nil && (amount.Grams > 0 || amount.UnknownWeight):
			covered += weight
		}
	}
	if !mealSlot.EatingTimeID.Valid || total == 0 {
		return 1
	}
	share := covered / total

	best, bestCost := 1.0, math.Inf(1)
	for _, servings := range servingSteps {
		serving := r.Servings(servings)
		withMeal := day
		withMeal.Add(&serving)
		cost := changeCost * math.Abs(servings-1) / 0.25
		for _, t := range p.Targets {
			v, _ := withMeal.Value(t.Nutrient)
			if t.Min != nil && *t.Min > 0 {
				cost += math.Max(*t.Min*share-v, 0) / *t.Min
			}
			// A maximum counts for the whole day: a day half planned
			// can still have its other meals lighter.
			if t.Max != nil && *t.Max > 0 {
				cost += math.Max(v-*t.Max, 0) / *t.Max
			}
		}
		if cost < bestCost-1e-9 {
			best, bestCost = servings, cost
		}
	}
	return best
}
