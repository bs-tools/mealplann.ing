package recipes

import "slices"

// Cost is what a recipe's ingredients cost, from their foods' prices.
type Cost struct {
	// Cents is the cost of the ingredients that have prices and can be
	// weighed, a lower bound if Unpriced isn't empty.
	Cents float64
	// Unpriced are the names of the foods without a price, or that can't
	// be weighed, in order.
	Unpriced []string
}

// CostOf returns what ingredients cost, pro rata to the grams used, not
// whole packs: what a meal costs, rather than what its shop does.
func CostOf(ingredients []*Ingredient) Cost {
	var c Cost
	for _, in := range ingredients {
		grams, ok := in.Food.Grams(in.Amount, in.Unit)
		if !ok || in.PriceCentsPerKilogram <= 0 {
			if !slices.Contains(c.Unpriced, in.FoodName) {
				c.Unpriced = append(c.Unpriced, in.FoodName)
			}
			continue
		}
		c.Cents += grams / 1000 * float64(in.PriceCentsPerKilogram)
	}
	return c
}

// Cost returns what the recipe's servings cost, with its components made
// of their own ingredients, and without the optional ones.
func (r *Recipe) Cost() Cost {
	return CostOf(r.IngredientsFor(float64(max(r.Servings, 1))))
}
