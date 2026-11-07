package nutrition

import "strings"

// labels are the nutrients' names, for messages to users.
var labels = [Count]string{
	EnergyKcal:           "Energy",
	ProteinGrams:         "Protein",
	CarbohydrateGrams:    "Carbohydrate",
	SugarsGrams:          "Sugars",
	FatGrams:             "Fat",
	FiberGrams:           "Fiber",
	VitaminB12Micrograms: "Vitamin B12",
	IronMilligrams:       "Iron",
	CalciumMilligrams:    "Calcium",
	VitaminDMicrograms:   "Vitamin D",
	IodineMicrograms:     "Iodine",
	ZincMilligrams:       "Zinc",
	Omega3ALAGrams:       "Omega-3 (ALA)",
	FolateMicrograms:     "Folate",
	VitaminAMicrograms:   "Vitamin A",
	VitaminEMilligrams:   "Vitamin E",
	VitaminKMicrograms:   "Vitamin K",
	CopperMilligrams:     "Copper",
	MagnesiumMilligrams:  "Magnesium",
	SodiumMilligrams:     "Sodium",
	PotassiumMilligrams:  "Potassium",
}

// Label returns the nutrient's name for users, e.g. "Vitamin B12".
func (n Nutrient) Label() string {
	if n < 0 || n >= Count {
		return "Unknown"
	}
	return labels[n]
}

// Unit returns the symbol of the nutrient's unit: "kcal", "g", "mg" or
// "µg".
func (n Nutrient) Unit() string {
	name := n.String()
	switch {
	case n == EnergyKcal:
		return "kcal"
	case strings.HasSuffix(name, "_milligrams"):
		return "mg"
	case strings.HasSuffix(name, "_micrograms"):
		return "µg"
	default:
		return "g"
	}
}

// Amount is the nutrition of some food eaten: a portion, an eating time or
// a day. Amounts add up, keeping track of what's unknown, so that totals
// are lower bounds where data is missing.
type Amount struct {
	// Grams is the weight of what can be weighed.
	Grams float64
	// UnknownWeight is whether some of it can't be weighed, so Grams is a
	// lower bound.
	UnknownWeight bool
	// Nutrients are the amounts of each nutrient, nil if no part has it.
	Nutrients Nutrients
	// Incomplete is whether some part lacks the nutrient, so its amount is a
	// lower bound.
	Incomplete [Count]bool
}

// Add adds b to a.
func (a *Amount) Add(b *Amount) {
	a.Grams += b.Grams
	a.UnknownWeight = a.UnknownWeight || b.UnknownWeight
	for n, v := range b.Nutrients {
		if v != nil {
			sum := *v
			if a.Nutrients[n] != nil {
				sum += *a.Nutrients[n]
			}
			a.Nutrients[n] = &sum
		}
		a.Incomplete[n] = a.Incomplete[n] || b.Incomplete[n]
	}
}

// Value returns the amount of a nutrient, 0 if unknown, and whether it's
// complete. An unknown nutrient is incomplete.
func (a *Amount) Value(n Nutrient) (float64, bool) {
	v := a.Nutrients[n]
	if v == nil {
		return 0, false
	}
	return *v, !a.Incomplete[n]
}

// Servings returns the nutrition of a number of servings of the recipe.
func (r *Recipe) Servings(servings float64) Amount {
	a := Amount{
		Grams:     r.GramsPerServing * servings,
		Nutrients: r.PerServing.Scale(servings),
	}
	for _, n := range r.Incomplete {
		a.Incomplete[n] = true
	}
	for _, issue := range r.Issues {
		if issue.UnknownWeight {
			a.UnknownWeight = true
		}
	}
	// A recipe weighing nothing, as none of its ingredients can be weighed
	// or it has none, has an unknown weight and nutrients.
	if r.TotalGrams == 0 {
		a.UnknownWeight = true
		for n := range Count {
			a.Incomplete[n] = true
		}
	}
	return a
}

// AmountOf returns the nutrition of an amount of the food in a unit. If it
// can't be weighed, its weight and every nutrient are unknown.
func (f *Food) AmountOf(amount float64, unit Unit) Amount {
	grams, ok := f.Grams(amount, unit)
	if !ok {
		a := Amount{UnknownWeight: true}
		for n := range Count {
			a.Incomplete[n] = true
		}
		return a
	}
	a := Amount{Grams: grams, Nutrients: f.Per100Grams.Scale(grams / 100)}
	for n, v := range f.Per100Grams {
		if v == nil {
			a.Incomplete[n] = true
		}
	}
	return a
}
