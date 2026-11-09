// Package rules checks a plan against people's nutrition rules and daily
// targets, and checks its cooks' leftovers and servings, returning warnings
// that say what to change. The rules' values are the user's, from the
// person's care team: the checks only compare the plan with them, and the
// messages suggest changing the plan, never the rules.
//
// It has no database or API, so the web app and AI agents get the same
// warnings, and the recipe suggestions score with them.
package rules

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
)

// Kind is what a warning is about.
type Kind int

// The kinds of warnings.
const (
	// PortionTooLarge is an eating time over the maximum portion.
	PortionTooLarge Kind = iota + 1
	// ProteinLow is an eating time under the minimum of protein.
	ProteinLow
	// CarbohydrateHigh is an eating time over the maximum of carbohydrate.
	CarbohydrateHigh
	// CarbohydrateUnpaired is an eating time whose carbohydrate has too
	// little protein and fat with it.
	CarbohydrateUnpaired
	// SugarsHigh is an eating time over the maximum of sugars.
	SugarsHigh
	// FastCarbs is an eating time with fast carb foods.
	FastCarbs
	// WeightUnknown is an eating time whose weight is only partly known,
	// so its maximum portion can't be fully checked.
	WeightUnknown
	// TargetLow is a day under a nutrient's daily minimum.
	TargetLow
	// TargetHigh is a day over a nutrient's daily maximum.
	TargetHigh
	// LeftoverPastFridgeDays is a leftover eaten after it stops keeping in
	// the fridge.
	LeftoverPastFridgeDays
	// LeftoverFreeze is a leftover eaten after it stops keeping in the
	// fridge, of a recipe that can be frozen.
	LeftoverFreeze
	// PortionBeforeCook is a portion eaten before its cook.
	PortionBeforeCook
	// ServingsUnassigned is a cook with servings nobody eats.
	ServingsUnassigned
	// ServingsOverassigned is a cook whose portions add up to more than it
	// makes.
	ServingsOverassigned
	// LeftoverThaw is a leftover from the freezer, to thaw the day before.
	LeftoverThaw
	// LeftoverPastFreezerDays is a leftover from the freezer eaten after it
	// stops keeping frozen.
	LeftoverPastFreezerDays
	// FrozenServingsShort is a cook with more servings eaten after its
	// fridge days than it has frozen.
	FrozenServingsShort
	// ComponentAfterCook is a cook whose component is cooked after it.
	ComponentAfterCook
)

// Warning is a problem with a plan.
type Warning struct {
	Kind Kind
	// Info is whether it's only to know about, rather than to change.
	Info bool
	// Uncertain is whether it may be wrong, as some foods lack data.
	Uncertain bool
	// Nutrient is the nutrient of a daily target warning, and of no meaning
	// otherwise.
	Nutrient nutrition.Nutrient
	// Value is the planned amount checked, and Limit the rule's, in the
	// rule's unit: grams, the nutrient's unit, a ratio, days or servings.
	Value float64
	Limit float64
	// Message says what's wrong and what to change.
	Message string
}

// Rules are the rules every eating time of a person is checked against.
// Each nil rule is no rule.
type Rules struct {
	// MaxPortionGrams is the most an eating time may weigh.
	MaxPortionGrams *float64
	// MinProteinGrams is the least protein an eating time should have.
	MinProteinGrams *float64
	// MaxCarbohydrateGrams is the most carbohydrate an eating time may
	// have.
	MaxCarbohydrateGrams *float64
	// MinPairingRatio is the least (protein + fat) / carbohydrate of an
	// eating time with carbohydrate.
	MinPairingRatio *float64
	// MaxSugarsGrams is the most sugars an eating time may have.
	MaxSugarsGrams *float64
	// WarnFastCarbs is whether to warn about fast carb foods.
	WarnFastCarbs bool
}

// EatingTime is what's planned at a person's eating time on a day.
type EatingTime struct {
	// Name is the eating time's name, e.g. "Mid-morning".
	Name string
	// Amount is the sum of its portions.
	Amount nutrition.Amount
	// FastCarbFoods are the names of the fast carb foods in its portions,
	// without repeats.
	FastCarbFoods []string
	// UnweighedNames are the names of the portions that can't be fully
	// weighed, without repeats.
	UnweighedNames []string
}

// CheckEatingTime checks what's planned at an eating time against a
// person's rules. An eating time with nothing planned has no warnings: it's
// still to plan.
func CheckEatingTime(r *Rules, et *EatingTime) []Warning {
	var warnings []Warning
	a := &et.Amount
	if a.Grams == 0 && !a.UnknownWeight {
		return nil
	}
	if limit := r.MaxPortionGrams; limit != nil {
		if a.Grams > *limit+roundingSlack {
			warnings = append(warnings, Warning{
				Kind:  PortionTooLarge,
				Value: a.Grams,
				Limit: *limit,
				Message: fmt.Sprintf("%s is %s g, over the maximum portion of %s g: move %s g or more to another small meal, or plan less.",
					et.Name, format(a.Grams), format(*limit), format(a.Grams-*limit)),
			})
		}
		if a.UnknownWeight {
			warnings = append(warnings, Warning{
				Kind:      WeightUnknown,
				Uncertain: true,
				Value:     a.Grams,
				Limit:     *limit,
				Message: fmt.Sprintf("%s can't be fully weighed, so its maximum portion of %s g can't be checked: set the weight per piece or the density of the foods in %s.",
					et.Name, format(*limit), list(et.UnweighedNames)),
			})
		}
	}
	protein, proteinComplete := a.Value(nutrition.ProteinGrams)
	if limit := r.MinProteinGrams; limit != nil && protein < *limit-roundingSlack {
		w := Warning{Kind: ProteinLow, Uncertain: !proteinComplete, Value: protein, Limit: *limit}
		if proteinComplete {
			w.Message = fmt.Sprintf("%s has %s g of protein, under the minimum of %s g: add a protein food, like tofu, tempeh, soy yogurt or nuts.",
				et.Name, format(protein), format(*limit))
		} else {
			w.Message = fmt.Sprintf("%s may have too little protein: at least %s g, under the minimum of %s g, but some foods lack protein data. Complete them, or add a protein food.",
				et.Name, format(protein), format(*limit))
		}
		warnings = append(warnings, w)
	}
	carbs, _ := a.Value(nutrition.CarbohydrateGrams)
	if limit := r.MaxCarbohydrateGrams; limit != nil && carbs > *limit+roundingSlack {
		warnings = append(warnings, Warning{
			Kind:  CarbohydrateHigh,
			Value: carbs,
			Limit: *limit,
			Message: fmt.Sprintf("%s has %s g of carbohydrate, over the maximum of %s g: plan a smaller portion of the starchy or sweet foods, or swap some for protein or vegetables.",
				et.Name, format(carbs), format(*limit)),
		})
	}
	if limit := r.MinPairingRatio; limit != nil && carbs > 0 {
		fat, fatComplete := a.Value(nutrition.FatGrams)
		ratio := (protein + fat) / carbs
		if ratio < *limit-ratioSlack {
			uncertain := !proteinComplete || !fatComplete
			w := Warning{Kind: CarbohydrateUnpaired, Uncertain: uncertain, Value: ratio, Limit: *limit}
			need := math.Ceil(*limit*carbs - protein - fat)
			w.Message = fmt.Sprintf("%s's carbohydrate isn't balanced: %s g of protein and fat for %s g of carbohydrate is a ratio of %s, under the minimum of %s. Add about %s g of protein or fat, like nuts, seeds, tofu or soy yogurt, or plan less carbohydrate.",
				et.Name, format(protein+fat), format(carbs), formatRatio(ratio), formatRatio(*limit), format(need))
			if uncertain {
				w.Message += " Some foods lack protein or fat data, so it may be balanced already."
			}
			warnings = append(warnings, w)
		}
	}
	if limit := r.MaxSugarsGrams; limit != nil {
		if sugars, _ := a.Value(nutrition.SugarsGrams); sugars > *limit+roundingSlack {
			warnings = append(warnings, Warning{
				Kind:  SugarsHigh,
				Value: sugars,
				Limit: *limit,
				Message: fmt.Sprintf("%s has %s g of sugars, over the maximum of %s g, which risks dumping: swap sweet foods, like fruit, juice, dried fruit or sweetened milks, for less sweet ones, or plan smaller amounts.",
					et.Name, format(sugars), format(*limit)),
			})
		}
	}
	if r.WarnFastCarbs && len(et.FastCarbFoods) > 0 {
		warnings = append(warnings, Warning{
			Kind:  FastCarbs,
			Value: float64(len(et.FastCarbFoods)),
			Message: fmt.Sprintf("%s has fast carbs: %s. Swap them for slower carbohydrates, or balance them with protein and fat.",
				et.Name, strings.Join(et.FastCarbFoods, ", ")),
		})
	}
	return warnings
}

// Target is a person's daily target for a nutrient. A nil bound is none.
type Target struct {
	Nutrient nutrition.Nutrient
	Min      *float64
	Max      *float64
}

// Day is what a person has planned on a day.
type Day struct {
	// Amount is the sum of the day's portions.
	Amount nutrition.Amount
	// Unplanned is how many of the person's eating times have nothing
	// planned.
	Unplanned int
}

// CheckDay checks a person's day against their daily targets. A day with
// nothing planned has no warnings.
func CheckDay(targets []Target, day *Day) []Warning {
	a := &day.Amount
	if a.Grams == 0 && !a.UnknownWeight {
		return nil
	}
	var warnings []Warning
	for _, t := range targets {
		value, complete := a.Value(t.Nutrient)
		label, unit := t.Nutrient.Label(), t.Nutrient.Unit()
		if t.Min != nil && value < *t.Min-roundingSlack {
			uncertain := !complete || day.Unplanned > 0
			w := Warning{Kind: TargetLow, Uncertain: uncertain, Nutrient: t.Nutrient, Value: value, Limit: *t.Min}
			if complete {
				w.Message = fmt.Sprintf("%s is %s %s, under the daily minimum of %s %s: add foods rich in %s.",
					label, format(value), unit, format(*t.Min), unit, lowerFirst(label))
			} else {
				w.Message = fmt.Sprintf("%s may be low: at least %s %s, under the daily minimum of %s %s, but some foods lack %s data. Complete them, or add foods rich in %s.",
					label, format(value), unit, format(*t.Min), unit, lowerFirst(label), lowerFirst(label))
			}
			if day.Unplanned > 0 {
				w.Message += fmt.Sprintf(" %d eating %s still unplanned.", day.Unplanned, plural(day.Unplanned, "time is", "times are"))
			}
			warnings = append(warnings, w)
		}
		if t.Max != nil && value > *t.Max+roundingSlack {
			warnings = append(warnings, Warning{
				Kind:     TargetHigh,
				Nutrient: t.Nutrient,
				Value:    value,
				Limit:    *t.Max,
				Message: fmt.Sprintf("%s is %s %s, over the daily maximum of %s %s: swap foods high in %s for others, or plan smaller portions of them.",
					label, format(value), unit, format(*t.Max), unit, lowerFirst(label)),
			})
		}
	}
	return warnings
}

// Cook is what checking a cook needs.
type Cook struct {
	// RecipeName is the name of its recipe.
	RecipeName string
	// Date is the date it's cooked on, at midnight UTC.
	Date time.Time
	// Servings is how many servings it makes.
	Servings float64
	// FridgeDays is how many days its leftovers keep in the fridge, nil if
	// unknown.
	FridgeDays *int32
	// Freezable is whether its leftovers can be frozen.
	Freezable bool
	// FreezerDays is how many days its frozen leftovers keep, nil if
	// unknown.
	FreezerDays *int32
	// FrozenServings is how many of its servings are frozen on its date,
	// for the portions eaten after its fridge days. A cook with frozen
	// servings counts as frozen even if its recipe isn't marked freezable,
	// as the household froze it.
	FrozenServings float64
	// LateServings is how many servings its portions eat after its fridge
	// days, which CheckServings checks against FrozenServings.
	LateServings float64
	// OffPlanServings is how many servings were taken without a portion,
	// which count as used, from the freezer first.
	OffPlanServings float64
}

// CheckPortion checks when a portion of the cook is eaten: not before it's
// cooked, and while it keeps. date is at midnight UTC. It returns nil if
// the portion is fine.
func CheckPortion(c *Cook, date time.Time) *Warning {
	days := int(math.Round(date.Sub(c.Date).Hours() / 24))
	if days < 0 {
		return &Warning{
			Kind:  PortionBeforeCook,
			Value: float64(days),
			Message: fmt.Sprintf("This portion of %s is planned on %s, before it's cooked on %s: move the portion or the cook.",
				c.RecipeName, formatDate(date), formatDate(c.Date)),
		}
	}
	if c.FridgeDays == nil || days <= int(*c.FridgeDays) {
		return nil
	}
	fridgeDays := int(*c.FridgeDays)
	last := c.Date.AddDate(0, 0, fridgeDays)
	if c.FrozenServings > 0 {
		if c.FreezerDays != nil && days > int(*c.FreezerDays) {
			freezerDays := int(*c.FreezerDays)
			return &Warning{
				Kind:  LeftoverPastFreezerDays,
				Value: float64(days),
				Limit: float64(freezerDays),
				Message: fmt.Sprintf("%s frozen on %s keeps %d %s in the freezer, so it won't keep until %s: eat it by %s, or plan another cook.",
					c.RecipeName, formatDate(c.Date), freezerDays, plural(freezerDays, "day", "days"), formatDate(date),
					formatDate(c.Date.AddDate(0, 0, freezerDays))),
			}
		}
		return &Warning{
			Kind:  LeftoverThaw,
			Info:  true,
			Value: float64(days),
			Limit: float64(fridgeDays),
			Message: fmt.Sprintf("This portion of %s comes from the freezer: thaw it in the fridge on %s.",
				c.RecipeName, formatDate(date.AddDate(0, 0, -1))),
		}
	}
	if c.Freezable {
		return &Warning{
			Kind:  LeftoverFreeze,
			Info:  true,
			Value: float64(days),
			Limit: float64(fridgeDays),
			Message: fmt.Sprintf("%s keeps %d %s in the fridge: freeze servings of it on %s for this portion, and thaw it for %s.",
				c.RecipeName, fridgeDays, plural(fridgeDays, "day", "days"), formatDate(c.Date), formatDate(date)),
		}
	}
	return &Warning{
		Kind:  LeftoverPastFridgeDays,
		Value: float64(days),
		Limit: float64(fridgeDays),
		Message: fmt.Sprintf("%s cooked on %s keeps %d %s in the fridge, so it won't keep until %s: eat it by %s, or plan another cook.",
			c.RecipeName, formatDate(c.Date), fridgeDays, plural(fridgeDays, "day", "days"), formatDate(date), formatDate(last)),
	}
}

// CheckSupply checks that a cook's component is cooked on or before the
// cook, by the cook supplying it. Dates are at midnight UTC. It returns nil
// if it is.
func CheckSupply(recipeName string, date time.Time, componentName string, supplierDate time.Time) *Warning {
	if !supplierDate.After(date) {
		return nil
	}
	return &Warning{
		Kind:  ComponentAfterCook,
		Value: math.Round(supplierDate.Sub(date).Hours() / 24),
		Message: fmt.Sprintf("The %s for %s on %s is cooked on %s, after it: move its cook earlier, or have another cook make it.",
			componentName, recipeName, formatDate(date), formatDate(supplierDate)),
	}
}

// CheckServings checks that a cook's servings match the servings of its
// portions, assigned, and those taken off plan, and that it has frozen
// enough servings for those eaten after its fridge days. Frozen servings
// not planned or taken yet are in the freezer, rather than unassigned. It
// returns nil if all's well.
func CheckServings(c *Cook, assigned float64) *Warning {
	inFreezer := max(c.FrozenServings-c.LateServings-c.OffPlanServings, 0)
	assigned += c.OffPlanServings
	switch {
	case c.FrozenServings > 0 && c.LateServings > c.FrozenServings+servingsSlack:
		return &Warning{
			Kind:  FrozenServingsShort,
			Value: c.LateServings,
			Limit: c.FrozenServings,
			Message: fmt.Sprintf("%s servings of %s are planned after it keeps in the fridge, but only %s are frozen: freeze more, or eat them sooner.",
				format(c.LateServings), c.RecipeName, format(c.FrozenServings)),
		}
	case assigned > c.Servings+servingsSlack:
		return &Warning{
			Kind:  ServingsOverassigned,
			Value: assigned,
			Limit: c.Servings,
			Message: fmt.Sprintf("%s servings of %s are planned, in portions or for other cooks, more than the %s cooked: cook more servings, or plan less of it.",
				format(assigned), c.RecipeName, format(c.Servings)),
		}
	case assigned+inFreezer < c.Servings-servingsSlack:
		left := c.Servings - assigned - inFreezer
		return &Warning{
			Kind:  ServingsUnassigned,
			Info:  true,
			Value: left,
			Limit: c.Servings,
			Message: fmt.Sprintf("%s of the %s servings of %s %s planned for anyone: plan them as leftovers, freeze them, or cook fewer servings.",
				format(left), format(c.Servings), c.RecipeName, plural(int(math.Ceil(left)), "isn't", "aren't")),
		}
	}
	return nil
}

const (
	// roundingSlack keeps amounts that are over or under a limit by less
	// than a rounding error, like 200.0000001 g for 200 g, from warning.
	roundingSlack = 0.05
	// ratioSlack is roundingSlack for pairing ratios, which are rounded to
	// 2 decimals.
	ratioSlack = 0.005
	// servingsSlack is the least difference in servings worth a warning,
	// as portions are split in hundredths of a serving.
	servingsSlack = 0.05
)

// format formats an amount for a message, with fewer decimals the larger it
// is, like the web app: "412", "12.4" or "0.38".
func format(v float64) string {
	abs := math.Abs(v)
	digits := 2
	switch {
	case abs >= 100:
		digits = 0
	case abs >= 10:
		digits = 1
	}
	scale := math.Pow10(digits)
	return strconv.FormatFloat(math.Round(v*scale)/scale, 'f', -1, 64)
}

// formatRatio formats a ratio with 2 decimals.
func formatRatio(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// formatDate formats a date as e.g. "Mon 2 Nov".
func formatDate(t time.Time) string {
	return t.Format("Mon 2 Jan")
}

// list joins names into an English list, e.g. "A, B and C".
func list(names []string) string {
	switch len(names) {
	case 0:
		return "the foods"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// lowerFirst lowercases a nutrient's label for the middle of a sentence,
// keeping the rest as it is, like "vitamin B12".
func lowerFirst(label string) string {
	if label == "" {
		return label
	}
	return strings.ToLower(label[:1]) + label[1:]
}

// plural returns one if n is 1, and other otherwise.
func plural(n int, one, other string) string {
	if n == 1 {
		return one
	}
	return other
}
