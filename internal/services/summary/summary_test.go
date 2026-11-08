package summary

import (
	"testing"

	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSummarize(t *testing.T) {
	t.Parallel()
	nutrients := func() *foodsv1.Nutrients {
		return foodsv1.Nutrients_builder{EnergyKcal: new(100.0), ProteinGrams: new(5.0), CarbohydrateGrams: new(20.0), FatGrams: new(2.0), IodineMicrograms: new(1.0)}.Build()
	}
	r := recipesv1.Recipe_builder{
		Name:  "Dal",
		Steps: []string{"Simmer."},
		Ingredients: []*recipesv1.Ingredient{
			recipesv1.Ingredient_builder{FoodName: "Lentils", Amount: 200, OriginalText: "1 cup lentils"}.Build(),
		},
		Nutrition: recipesv1.RecipeNutrition_builder{
			TotalGrams:          800,
			PerServing:          nutrients(),
			PerHundredGrams:     nutrients(),
			IncompleteNutrients: []string{"iodine_micrograms"},
			Issues:              []*recipesv1.IngredientIssue{recipesv1.IngredientIssue_builder{FoodName: "Lentils"}.Build()},
		}.Build(),
		CreateTime: timestamppb.Now(),
	}.Build()
	Summarize(r)

	n := r.GetNutrition()
	if !n.GetPerServing().HasEnergyKcal() || !n.GetPerServing().HasProteinGrams() || !n.GetPerServing().HasCarbohydrateGrams() ||
		n.GetPerServing().HasFatGrams() || n.GetPerServing().HasIodineMicrograms() {
		t.Errorf("got per serving %v, want only energy, protein and carbohydrate", n.GetPerServing())
	}
	if n.HasPerHundredGrams() || len(n.GetIncompleteNutrients()) != 0 || len(n.GetIssues()) != 0 || n.GetTotalGrams() != 800 {
		t.Errorf("got nutrition %v, want only the total and per serving", n)
	}
	if len(r.GetSteps()) != 0 || r.HasCreateTime() || r.GetName() != "Dal" {
		t.Errorf("got %v, want no steps or times", r)
	}
	if in := r.GetIngredients()[0]; in.GetOriginalText() != "" || in.GetFoodName() != "Lentils" || in.GetAmount() != 200 {
		t.Errorf("got ingredient %v, want it without its original text", in)
	}
}
