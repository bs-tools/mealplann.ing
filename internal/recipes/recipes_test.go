package recipes

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// food returns an ingredient of grams of a food with energy and protein
// per 100 g.
func food(name string, grams, kcal, protein float64) *Ingredient {
	in := &Ingredient{FoodID: store.NewUUID(), FoodName: name, Amount: grams, Unit: nutrition.Grams}
	in.Food.Per100Grams[nutrition.EnergyKcal] = new(kcal)
	in.Food.Per100Grams[nutrition.ProteinGrams] = new(protein)
	return in
}

// component returns an ingredient of servings of a recipe.
func component(r *Recipe, servings float64) *Ingredient {
	return &Ingredient{ComponentRecipeID: r.ID, Component: r, Amount: servings}
}

func describe(ingredients []*Ingredient) []string {
	var lines []string
	for _, in := range ingredients {
		lines = append(lines, fmt.Sprintf("%s %v", in.FoodName, math.Round(in.Amount*100)/100))
	}
	return lines
}

// chili returns a chili of 4 servings with 2 servings of a component of
// cooked chickpeas, which 200 g of dried chickpeas make 4 servings of.
func chili() (*Recipe, *Recipe) {
	chickpeas := &Recipe{
		ID: store.NewUUID(), Name: "Cooked chickpeas", Servings: 4,
		Ingredients: []*Ingredient{food("Dried chickpeas", 200, 380, 20)},
	}
	chili := &Recipe{ID: store.NewUUID(), Name: "Chili", Servings: 4, Ingredients: []*Ingredient{
		food("Tomatoes", 400, 20, 1),
		component(chickpeas, 2),
	}}
	return chili, chickpeas
}

func TestComponents(t *testing.T) {
	t.Parallel()
	chili, chickpeas := chili()

	// The nutrition counts half the pot of chickpeas: 100 g of dried, 380
	// kcal, with the tomatoes' 80, over 4 servings.
	n := chili.Nutrition()
	if got := *n.PerServing[nutrition.EnergyKcal]; math.Abs(got-(380+80)/4.0) > 1e-9 {
		t.Errorf("got %v kcal a serving, want %v", got, (380+80)/4.0)
	}
	if got := n.TotalGrams; math.Abs(got-500) > 1e-9 {
		t.Errorf("got %v g, want the tomatoes' 400 and the chickpeas' 100", got)
	}
	// A nutrient the chickpeas lack is incomplete in the chili too.
	if !slices.Contains(n.Incomplete, nutrition.FiberGrams) {
		t.Errorf("got incomplete %v, want fiber, which nothing has", n.Incomplete)
	}

	// Its ingredients for 8 servings are made of the chickpeas' own,
	// scaled: 4 servings of chickpeas is 200 g of dried.
	if got, want := describe(chili.IngredientsFor(8)), []string{"Tomatoes 800", "Dried chickpeas 200"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Supplied by another cook, the chickpeas aren't among them.
	supplied := map[pgtype.UUID]bool{chickpeas.ID: true}
	if got, want := describe(chili.IngredientsWithout(8, supplied)), []string{"Tomatoes 800"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got := chili.ComponentsFor(8); len(got) != 1 || got[0].Recipe != chickpeas || got[0].Servings != 4 {
		t.Errorf("got %+v, want 4 servings of chickpeas", got)
	}
	if !chili.UsesRecipe(chickpeas.ID) || chickpeas.UsesRecipe(chili.ID) || chili.Depth() != 1 || chickpeas.Depth() != 0 {
		t.Errorf("got uses %v and depths %d and %d, want the chili using the chickpeas, 1 deep", chili.UsesRecipe(chickpeas.ID), chili.Depth(), chickpeas.Depth())
	}
}

func TestComponentsCycle(t *testing.T) {
	t.Parallel()
	// Two recipes using each other by mistake, like concurrent updates
	// could make, don't recurse forever.
	a := &Recipe{ID: store.NewUUID(), Name: "A", Servings: 1, Ingredients: []*Ingredient{food("Salt", 1, 0, 0)}}
	b := &Recipe{ID: store.NewUUID(), Name: "B", Servings: 1, Ingredients: []*Ingredient{component(a, 1)}}
	a.Ingredients = append(a.Ingredients, component(b, 1))
	_ = a.Nutrition()
	_ = a.IngredientsFor(1)
	if a.Depth() <= MaxComponentDepth {
		t.Errorf("got depth %d, want deeper than %d", a.Depth(), MaxComponentDepth)
	}
	if !a.UsesRecipe(a.ID) {
		t.Error("got A not using itself, want the cycle found")
	}
}
