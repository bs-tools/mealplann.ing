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

// TestPreviewImport checks that previewing an import offers the foods most
// like each ingredient, and that importing only matches clear ones.
func TestPreviewImport(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	e.pages["https://blog.example/scramble/"] = `<html><head><script type="application/ld+json">
{"@type":"Recipe","name":"Tofu scramble","recipeYield":"2",
"recipeIngredient":["1 block extra firm tofu, crumbled","2 tbsp olive oil","1 tsp turmeric"]}
</script></head></html>`
	firm := customFood(t, e, "Tofu, extra-firm", 144, 15, 3, 0.6, 9, nil)
	customFood(t, e, "Tofu, firm", 144, 15, 3, 0.6, 9, nil)
	oil := customFood(t, e, "Olive oil", 884, 0, 0, 0, 100, nil)

	resp, err := e.recipes.PreviewImport(ctx, recipesv1.PreviewImportRequest_builder{Url: "https://blog.example/scramble/"}.Build())
	if err != nil {
		t.Fatalf("PreviewImport: %v", err)
	}
	ins := resp.GetIngredients()
	if len(ins) != 3 || resp.GetRecipe().GetName() != "Tofu scramble" {
		t.Fatalf("got %v, want the scramble's 3 ingredients", resp)
	}
	// The extra-firm tofu comes first, with the firm one as an alternative.
	tofu := ins[0].GetMatches()
	if len(tofu) < 2 || tofu[0].GetFoodId() != firm.GetId() || tofu[1].GetFoodName() != "Tofu, firm" {
		t.Errorf("got %v for the tofu, want the extra-firm tofu, then the firm one", tofu)
	}
	if m := ins[1].GetMatches(); len(m) == 0 || m[0].GetFoodId() != oil.GetId() || m[0].GetScore() != 1 {
		t.Errorf("got %v for the oil, want the olive oil", m)
	}
	if m := ins[2].GetMatches(); len(m) != 0 {
		t.Errorf("got %v for the turmeric, want no match", m)
	}
	// Nothing is saved.
	list, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if len(list.GetRecipes()) != 0 {
		t.Errorf("got %d recipes, want none saved", len(list.GetRecipes()))
	}

	// Importing matches the oil, and the tofu as it's well ahead of the
	// firm one.
	imported, err := e.recipes.ImportRecipe(ctx, recipesv1.ImportRecipeRequest_builder{Url: "https://blog.example/scramble/"}.Build())
	if err != nil {
		t.Fatalf("ImportRecipe: %v", err)
	}
	got := imported.GetRecipe().GetIngredients()
	if got[0].GetFoodId() != firm.GetId() || got[1].GetFoodId() != oil.GetId() || got[2].GetFoodId() != "" {
		t.Errorf("got %v, want the tofu and oil matched, and the turmeric not", got)
	}
}
