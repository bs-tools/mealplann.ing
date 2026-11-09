package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestCookDoneFoodUses checks that marking a cook done can say what was
// actually used, and preview what it takes out of the pantry first.
func TestCookDoneFoodUses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	tofu := shopFood(t, e, "Tofu, super-firm", func(f *foodsv1.Food) { f.SetGramsPerPiece(454) })
	firm := shopFood(t, e, "Tofu, extra-firm", func(f *foodsv1.Food) { f.SetGramsPerPiece(350) })
	onions := shopFood(t, e, "Onions", func(f *foodsv1.Food) { f.SetGramsPerPiece(150) })
	ginger := shopFood(t, e, "Ginger", nil)
	soy := shopFood(t, e, "Soy sauce", nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Tofu stir-fry", Servings: 2,
		Ingredients: []*recipesv1.Ingredient{
			ingredient(tofu.GetId(), 794, grams), ingredient(onions.GetId(), 1, pieces), ingredient(ginger.GetId(), 20, grams),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	for _, lot := range []struct {
		food  *foodsv1.Food
		grams float64
	}{{tofu, 1000}, {firm, 700}, {onions, 300}, {soy, 500}} {
		addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: lot.food.GetId(), Amount: lot.grams, Unit: grams}.Build())
	}
	newCook := func() string {
		t.Helper()
		resp, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
			Cook: plansv1.Cook_builder{RecipeId: created.GetRecipe().GetId(), Date: monday, Servings: 2}.Build(),
		}.Build())
		if err != nil {
			t.Fatalf("CreateCook: %v", err)
		}
		return resp.GetCook().GetId()
	}
	markDone := func(id string, uses []*plansv1.FoodUse, preview bool) (*plansv1.UpdateCookResponse, error) {
		return e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
			Cook:       plansv1.Cook_builder{Id: id, Done: true}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
			FoodUses:   uses,
			Preview:    preview,
		}.Build())
	}
	changes := func(resp *plansv1.UpdateCookResponse) []string {
		var got []string
		for _, c := range resp.GetPantryChanges() {
			got = append(got, c.GetFoodName()+" "+formatGrams(c.GetGrams()))
		}
		return got
	}

	// Two whole blocks of tofu were used, the onion was taken out by hand,
	// there was no ginger, and soy sauce went in too.
	uses := []*plansv1.FoodUse{
		plansv1.FoodUse_builder{FoodId: tofu.GetId(), Amount: new(2.0), Unit: pieces}.Build(),
		plansv1.FoodUse_builder{FoodId: onions.GetId(), Skipped: true}.Build(),
		plansv1.FoodUse_builder{FoodId: ginger.GetId(), Skipped: true}.Build(),
		plansv1.FoodUse_builder{FoodId: soy.GetId(), Amount: new(30.0), Unit: grams}.Build(),
	}
	want := []string{"Soy sauce 30", "Tofu, super-firm 908"}
	id := newCook()
	preview, err := markDone(id, uses, true)
	if err != nil {
		t.Fatalf("UpdateCook preview: %v", err)
	}
	if got := changes(preview); !slices.Equal(got, want) || !preview.GetPreviewed() || preview.GetCook().GetDone() != true {
		t.Errorf("got preview %v, want %v", got, want)
	}
	if got := stock(listPantry(t, e)); !slices.Contains(got, "Tofu, super-firm 1000 g") {
		t.Errorf("got %v after a preview, want the pantry unchanged", got)
	}
	done, err := markDone(id, uses, false)
	if err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	if got := changes(done); !slices.Equal(got, want) || done.GetPreviewed() {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := stock(listPantry(t, e)), []string{"Onions 300 g", "Soy sauce 470 g", "Tofu, extra-firm 700 g", "Tofu, super-firm 92 g"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Extra-firm tofu used instead takes the recipe's weight of it.
	done, err = markDone(newCook(), []*plansv1.FoodUse{
		plansv1.FoodUse_builder{FoodId: tofu.GetId(), SubstituteFoodId: firm.GetId()}.Build(),
	}, false)
	if err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	if got, want := changes(done), []string{"Onions 150", "Tofu, extra-firm 700"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v, all the extra-firm there is", got, want)
	}

	// Mistakes change nothing, and say what to do.
	id = newCook()
	for name, uses := range map[string][]*plansv1.FoodUse{
		"extra without amount": {plansv1.FoodUse_builder{FoodId: soy.GetId()}.Build()},
		"skipped with amount":  {plansv1.FoodUse_builder{FoodId: tofu.GetId(), Skipped: true, Amount: new(1.0), Unit: pieces}.Build()},
		"amount without unit":  {plansv1.FoodUse_builder{FoodId: tofu.GetId(), Amount: new(1.0)}.Build()},
		"twice":                {plansv1.FoodUse_builder{FoodId: tofu.GetId(), Skipped: true}.Build(), plansv1.FoodUse_builder{FoodId: tofu.GetId(), Skipped: true}.Build()},
		"unweighable":          {plansv1.FoodUse_builder{FoodId: ginger.GetId(), Amount: new(1.0), Unit: pieces}.Build()},
	} {
		_, err := markDone(id, uses, false)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid argument", name, err)
		}
	}
	got, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: id, Notes: "spicy"}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"notes"}},
		Preview:    true,
	}.Build())
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("got %v, %v previewing without marking done, want invalid argument", got, err)
	}
	if got := stock(listPantry(t, e)); !slices.Contains(got, "Onions 150 g") {
		t.Errorf("got %v, want the pantry unchanged by the mistakes", got)
	}
}
