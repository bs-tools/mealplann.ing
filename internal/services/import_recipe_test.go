package services_test

import (
	"context"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
)

func TestImportRecipe(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	// Not concurrent with the import, so the map isn't raced.
	e.pages["https://blog.example/overnight-oats/"] = `<html><head><script type="application/ld+json">
{"@context":"https://schema.org","@graph":[{"@type":"Recipe","name":"Overnight Oats","recipeYield":"2 servings",
"prepTime":"PT5M","recipeIngredient":["1 cup rolled oats","1 ½ cups almond milk","2 tbsp chia seeds","Pinch of salt"],
"recipeInstructions":[{"@type":"HowToStep","text":"Stir everything together."},{"@type":"HowToStep","text":"Chill overnight."}]}]}
</script></head></html>`
	oats := customFood(t, e, "Rolled oats", 380, 13, 60, 1, 7, func(f *foodsv1.Food) { f.SetGramsPerMilliliter(0.4) })
	customFood(t, e, "Chia seed", 490, 17, 42, 0, 31, nil)

	resp, err := e.recipes.ImportRecipe(ctx, recipesv1.ImportRecipeRequest_builder{Url: "https://blog.example/overnight-oats/#recipe"}.Build())
	if err != nil {
		t.Fatalf("ImportRecipe: %v", err)
	}
	r := resp.GetRecipe()
	if !r.GetDraft() || r.GetName() != "Overnight Oats" || r.GetServings() != 2 || r.GetPrepSeconds() != 300 ||
		r.GetSourceUrl() != "https://blog.example/overnight-oats/" || len(r.GetInstructions()) != 2 {
		t.Errorf("got %v", r)
	}
	// The oats and chia seeds, singular, are matched, in their units.
	ins := r.GetIngredients()
	if resp.GetMatchedIngredients() != 2 || len(ins) != 4 {
		t.Fatalf("got %d matched of %v, want 2 of 4", resp.GetMatchedIngredients(), ins)
	}
	if ins[0].GetFoodId() != oats.GetId() || ins[0].GetAmount() != 1 || ins[0].GetUnit() != foodsv1.AmountUnit_AMOUNT_UNIT_CUPS {
		t.Errorf("got oats %v", ins[0])
	}
	if ins[1].GetFoodId() != "" || ins[1].GetAmount() != 1.5 || ins[1].GetOriginalText() != "1 ½ cups almond milk" {
		t.Errorf("got almond milk %v, want it unmatched, 1.5 cups", ins[1])
	}
	if ins[2].GetFoodName() != "Chia seed" || ins[3].GetFoodId() != "" || ins[3].GetAmount() != 0 {
		t.Errorf("got %v and %v", ins[2], ins[3])
	}

	// Pages without a recipe, or that can't be fetched, fail saying so.
	e.pages["https://blog.example/about/"] = `<html><body>About me</body></html>`
	_, err = e.recipes.ImportRecipe(ctx, recipesv1.ImportRecipeRequest_builder{Url: "https://blog.example/about/"}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.recipes.ImportRecipe(ctx, recipesv1.ImportRecipeRequest_builder{Url: "https://blog.example/missing/"}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.recipes.ImportRecipe(ctx, recipesv1.ImportRecipeRequest_builder{Url: "blog.example/about"}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
}
