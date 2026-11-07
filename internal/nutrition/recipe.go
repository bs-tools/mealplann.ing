package nutrition

// Ingredient is an amount of a food in a recipe.
type Ingredient struct {
	Food   *Food
	Amount float64
	Unit   Unit
	// Optional ingredients, like garnishes, aren't counted.
	Optional bool
}

// Issue is an ingredient whose food lacks data the recipe's nutrition
// needs.
type Issue struct {
	// Index is the ingredient's index in the recipe.
	Index int
	// UnknownWeight is whether the ingredient's amount can't be weighed, so
	// the ingredient is left out of the nutrition entirely.
	UnknownWeight bool
	// UnknownNutrients are the nutrients the food lacks, in order. Empty if
	// UnknownWeight.
	UnknownNutrients []Nutrient
}

// Recipe is the nutrition of a recipe.
type Recipe struct {
	// TotalGrams is the weight of the ingredients that can be weighed.
	TotalGrams float64
	// GramsPerServing is TotalGrams divided by the servings.
	GramsPerServing float64
	// PerServing are the nutrients in a serving. A nutrient is nil if no
	// ingredient has it, and a lower bound if it's in Incomplete.
	PerServing Nutrients
	// Per100Grams are the nutrients in 100 g of the recipe, nil if it
	// weighs nothing.
	Per100Grams Nutrients
	// Incomplete are the nutrients some counted ingredients lack, in order,
	// including all of them if an ingredient can't be weighed.
	Incomplete []Nutrient
	// Issues are the ingredients lacking data, in the recipe's order.
	Issues []Issue
}

// ForRecipe computes the nutrition of a recipe making a number of servings
// from its ingredients, as weighed raw: the water lost or absorbed in
// cooking isn't known. Servings below 1 count as 1.
func ForRecipe(ingredients []Ingredient, servings int) *Recipe {
	servings = max(servings, 1)
	r := &Recipe{}
	var total Nutrients
	var incomplete [Count]bool
	for i, in := range ingredients {
		if in.Optional {
			continue
		}
		grams, ok := in.Food.Grams(in.Amount, in.Unit)
		if !ok {
			r.Issues = append(r.Issues, Issue{Index: i, UnknownWeight: true})
			for n := range Count {
				incomplete[n] = true
			}
			continue
		}
		r.TotalGrams += grams
		var unknown []Nutrient
		for n, per100 := range in.Food.Per100Grams {
			if per100 == nil {
				unknown = append(unknown, Nutrient(n))
				incomplete[n] = true
				continue
			}
			sum := *per100 * grams / 100
			if total[n] != nil {
				sum += *total[n]
			}
			total[n] = &sum
		}
		if len(unknown) > 0 {
			r.Issues = append(r.Issues, Issue{Index: i, UnknownNutrients: unknown})
		}
	}
	for n, ok := range incomplete {
		if ok {
			r.Incomplete = append(r.Incomplete, Nutrient(n))
		}
	}
	r.GramsPerServing = r.TotalGrams / float64(servings)
	r.PerServing = total.Scale(1 / float64(servings))
	if r.TotalGrams > 0 {
		r.Per100Grams = total.Scale(100 / r.TotalGrams)
	}
	return r
}
