package services_test

import (
	"context"
	"math"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

const (
	grams       = foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS
	milliliters = foodsv1.AmountUnit_AMOUNT_UNIT_MILLILITERS
	pieces      = foodsv1.AmountUnit_AMOUNT_UNIT_PIECES
)

func ingredient(foodID string, amount float64, unit foodsv1.AmountUnit) *recipesv1.Ingredient {
	return recipesv1.Ingredient_builder{FoodId: foodID, Amount: amount, Unit: unit}.Build()
}

// near reports whether a computed amount, rounded to 3 decimals, is v.
func near(got, want float64) bool {
	return math.Abs(got-want) < 0.0006
}

func recipeNames(list []*recipesv1.RecipeSummary) []string {
	var names []string
	for _, r := range list {
		names = append(names, r.GetName())
	}
	return names
}

func TestRecipes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.importFDC()
	ctx := context.Background()

	// The foods: chickpeas weighed in grams, onions in pieces of 110 g and
	// soy milk in milliliters of 1.027 g, all from FDC, and garlic, which
	// can't be weighed in pieces yet.
	chickpeas := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{FdcId: 173756, Food: foodsv1.Food_builder{Name: "Chickpeas"}.Build()}.Build())
	onions := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{FdcId: 170000, Food: foodsv1.Food_builder{Name: "Onions"}.Build()}.Build())
	soyMilk := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{FdcId: 2257046, Food: foodsv1.Food_builder{Name: "Soy milk"}.Build()}.Build())
	lentils := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{FdcId: 172420, Food: foodsv1.Food_builder{Name: "Red lentils"}.Build()}.Build())
	garlic := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{
		Name:      "Garlic",
		Nutrients: foodsv1.Nutrients_builder{EnergyKcal: new(149.0), ProteinGrams: new(6.4)}.Build(),
	}.Build()}.Build())

	// Create a recipe.
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Name:        "Chana masala",
			Description: "Chickpeas in a creamy onion sauce.",
			Servings:    4,
			PrepSeconds: 600,
			CookSeconds: 1800,
			Ingredients: []*recipesv1.Ingredient{
				recipesv1.Ingredient_builder{
					FoodId:       chickpeas.GetId(),
					Amount:       400,
					Unit:         grams,
					Note:         " drained ",
					OriginalText: "1 can (400 g) chickpeas, drained",
				}.Build(),
				ingredient(onions.GetId(), 2, pieces),
				ingredient(soyMilk.GetId(), 200, milliliters),
				ingredient(garlic.GetId(), 3, pieces),
				recipesv1.Ingredient_builder{FoodId: soyMilk.GetId(), Amount: 50, Unit: milliliters, Optional: true}.Build(),
			},
			Instructions: []*recipesv1.Step{
				recipesv1.Step_builder{Text: " Fry the onions. "}.Build(),
				recipesv1.Step_builder{Text: "Add the rest and simmer."}.Build(),
			},
			Tags:       []string{"Dinner", " batch-friendly", "dinner"},
			SourceUrl:  "https://example.com/chana-masala",
			FridgeDays: new(int32(3)),
			Freezable:  true,
			Notes:      "Serve with rice.",
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	masala := created.GetRecipe()
	if masala.GetId() == "" || masala.GetName() != "Chana masala" || masala.GetServings() != 4 ||
		!slices.Equal(masala.GetSteps(), []string{"Fry the onions.", "Add the rest and simmer."}) ||
		!slices.Equal(masala.GetTags(), []string{"dinner", "batch-friendly"}) ||
		masala.GetFridgeDays() != 3 || !masala.GetFreezable() || masala.GetSourceUrl() != "https://example.com/chana-masala" ||
		len(masala.GetIngredients()) != 5 || !masala.GetCreateTime().IsValid() {
		t.Errorf("got %v, want the chana masala", masala)
	}
	if in := masala.GetIngredients()[0]; in.GetFoodName() != "Chickpeas" || in.GetNote() != "drained" ||
		in.GetOriginalText() != "1 can (400 g) chickpeas, drained" || in.GetUnit() != grams {
		t.Errorf("got ingredient %v, want the chickpeas", in)
	}
	if in := masala.GetIngredients()[4]; !in.GetOptional() || in.GetFoodName() != "Soy milk" {
		t.Errorf("got ingredient %v, want the optional soy milk", in)
	}

	// Its nutrition weighs the onions and soy milk, leaves out the garlic,
	// which can't be weighed, and the optional soy milk.
	n := masala.GetNutrition()
	totalGrams := 400 + 2*110 + 200*1.027
	if !near(n.GetTotalGrams(), totalGrams) || !near(n.GetGramsPerServing(), totalGrams/4) {
		t.Errorf("got %v g, %v g per serving, want %v g in 4 servings", n.GetTotalGrams(), n.GetGramsPerServing(), totalGrams)
	}
	energy := (378*400 + 40*220 + 32.3*200*1.027) / 100
	if got := n.GetPerServing().GetEnergyKcal(); !near(got, energy/4) {
		t.Errorf("got %v kcal per serving, want %v", got, energy/4)
	}
	if got := n.GetPerHundredGrams().GetEnergyKcal(); !near(got, energy/totalGrams*100) {
		t.Errorf("got %v kcal per 100 g, want %v", got, energy/totalGrams*100)
	}
	// Protein comes from all three foods weighed.
	protein := (20.47*400 + 1.1*220 + 3.55*200*1.027) / 100
	if got := n.GetPerServing().GetProteinGrams(); !near(got, protein/4) {
		t.Errorf("got %v g protein per serving, want %v", got, protein/4)
	}
	// No food has iodine, so it's unknown.
	if n.GetPerServing().HasIodineMicrograms() {
		t.Errorf("got iodine %v, want unknown", n.GetPerServing().GetIodineMicrograms())
	}
	// The garlic makes every nutrient incomplete.
	if len(n.GetIncompleteNutrients()) != 21 || n.GetIncompleteNutrients()[0] != "energy_kcal" {
		t.Errorf("got incomplete nutrients %v, want all", n.GetIncompleteNutrients())
	}
	var garlicIssue *recipesv1.IngredientIssue
	for _, issue := range n.GetIssues() {
		if issue.GetFoodId() == garlic.GetId() {
			garlicIssue = issue
		}
		if issue.GetIngredientIndex() == 4 {
			t.Error("the optional soy milk has an issue")
		}
	}
	if garlicIssue == nil || garlicIssue.GetIngredientIndex() != 3 || !garlicIssue.GetUnknownWeight() ||
		garlicIssue.GetFoodName() != "Garlic" || len(garlicIssue.GetUnknownNutrients()) != 0 {
		t.Errorf("got issue %v, want the garlic's unknown weight", garlicIssue)
	}
	// No food has iodine, so it's no ingredient's issue, but the soy milk
	// lacks nutrients the others have.
	for _, issue := range n.GetIssues() {
		if slices.Contains(issue.GetUnknownNutrients(), "iodine_micrograms") || slices.Contains(issue.GetUnknownNutrients(), "protein_grams") {
			t.Errorf("got issue %v, want iodine left out, as no food has it", issue)
		}
	}
	if soyIssue := n.GetIssues()[0]; soyIssue.GetIngredientIndex() != 2 || !slices.Contains(soyIssue.GetUnknownNutrients(), "fiber_grams") {
		t.Errorf("got issue %v, want the soy milk lacking fiber", soyIssue)
	}

	// Weighing the garlic fixes its issue: the recipe's nutrition follows
	// its foods.
	if _, err := e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: garlic.GetId(), GramsPerPiece: 5}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"grams_per_piece"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateFood: %v", err)
	}
	got, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: masala.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	n = got.GetRecipe().GetNutrition()
	if !near(n.GetTotalGrams(), totalGrams+15) {
		t.Errorf("got %v g, want the garlic's 15 g added", n.GetTotalGrams())
	}
	if slices.Contains(n.GetIncompleteNutrients(), "energy_kcal") || !slices.Contains(n.GetIncompleteNutrients(), "fiber_grams") {
		t.Errorf("got incomplete nutrients %v, want energy complete and fiber not, as the soy milk lacks it", n.GetIncompleteNutrients())
	}
	for _, issue := range n.GetIssues() {
		if issue.GetUnknownWeight() {
			t.Errorf("got issue %v, want every ingredient weighed", issue)
		}
	}

	// A second recipe.
	created, err = e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Name:        "Lentil soup",
			Servings:    2,
			Ingredients: []*recipesv1.Ingredient{ingredient(lentils.GetId(), 200, grams), ingredient(onions.GetId(), 1, pieces)},
			Tags:        []string{"lunch"},
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	soup := created.GetRecipe()
	if soup.HasFridgeDays() || soup.GetFreezable() {
		t.Errorf("got %v, want unknown fridge days", soup)
	}

	// List the recipes, with their main nutrients per serving.
	list, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if !slices.Equal(recipeNames(list.GetRecipes()), []string{"Chana masala", "Lentil soup"}) ||
		!slices.Equal(list.GetTags(), []string{"batch-friendly", "dinner", "lunch"}) {
		t.Errorf("got recipes %v and tags %v", recipeNames(list.GetRecipes()), list.GetTags())
	}
	summary := list.GetRecipes()[1]
	soupEnergy := (352*200 + 40*110) / 100.0 / 2
	if !near(summary.GetEnergyKcalPerServing(), soupEnergy) || !near(summary.GetProteinGramsPerServing(), (24.63*200+1.1*110)/100/2) ||
		!near(summary.GetCarbohydrateGramsPerServing(), (63.35*200+9.34*110)/100/2) || !near(summary.GetGramsPerServing(), 155) ||
		summary.GetHasIssues() || summary.GetServings() != 2 || !slices.Equal(summary.GetTags(), []string{"lunch"}) {
		t.Errorf("got summary %v, want the soup's %v kcal per serving, with no issues, as only iodine is missing, from every food", summary, soupEnergy)
	}
	for name, tt := range map[string]struct {
		query string
		tags  []string
		want  []string
	}{
		"by name":        {query: "MASALA", want: []string{"Chana masala"}},
		"by description": {query: "creamy", want: []string{"Chana masala"}},
		"by tag":         {tags: []string{"Lunch"}, want: []string{"Lentil soup"}},
		"by all tags":    {tags: []string{"dinner", "batch-friendly"}, want: []string{"Chana masala"}},
		"by both":        {query: "soup", tags: []string{"dinner"}},
		"no match":       {query: "pancakes"},
	} {
		list, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{Query: tt.query, Tags: tt.tags}.Build())
		if err != nil {
			t.Fatalf("%s: ListRecipes: %v", name, err)
		}
		if got := recipeNames(list.GetRecipes()); !slices.Equal(got, tt.want) || len(list.GetTags()) != 3 {
			t.Errorf("%s: got %v and tags %v, want %v and all tags", name, got, list.GetTags(), tt.want)
		}
	}

	// Update only the fields in the mask: doubling the servings halves the
	// nutrition per serving.
	updated, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: soup.GetId(), Name: "Ignored", Servings: 4}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"servings", "fridge_days"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if r := updated.GetRecipe(); r.GetName() != "Lentil soup" || r.GetServings() != 4 || len(r.GetIngredients()) != 2 ||
		!near(r.GetNutrition().GetPerServing().GetEnergyKcal(), soupEnergy/2) {
		t.Errorf("got %v, want 4 servings of the same soup", r)
	}
	// Replacing the ingredients replaces them all, in order.
	updated, err = e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Id:          soup.GetId(),
			Ingredients: []*recipesv1.Ingredient{ingredient(onions.GetId(), 2, pieces), ingredient(lentils.GetId(), 250, grams), ingredient(garlic.GetId(), 2, pieces)},
			FridgeDays:  new(int32(0)),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"ingredients", "fridge_days"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if r := updated.GetRecipe(); len(r.GetIngredients()) != 3 || r.GetIngredients()[0].GetFoodName() != "Onions" ||
		r.GetIngredients()[1].GetAmount() != 250 || r.GetIngredients()[2].GetFoodName() != "Garlic" ||
		!r.HasFridgeDays() || r.GetFridgeDays() != 0 || !near(r.GetNutrition().GetTotalGrams(), 220+250+10) {
		t.Errorf("got %v, want the new ingredients, and fridge days 0", r)
	}
	// Mark the chana masala as a favorite, leaving the rest as it is.
	updated, err = e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: masala.GetId(), Favorite: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"favorite"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if r := updated.GetRecipe(); !r.GetFavorite() || r.GetName() != "Chana masala" || len(r.GetIngredients()) != 5 {
		t.Errorf("got %v, want the same chana masala, as a favorite", r)
	}
	// Only it is listed among the favorites, as one.
	favorites, err := e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{FavoritesOnly: true}.Build())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if got := recipeNames(favorites.GetRecipes()); !slices.Equal(got, []string{"Chana masala"}) || !favorites.GetRecipes()[0].GetFavorite() {
		t.Errorf("got favorites %v, want the chana masala", favorites.GetRecipes())
	}
	// The other household has no favorites.
	otherFavorites, err := e.otherRecipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{FavoritesOnly: true}.Build())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if len(otherFavorites.GetRecipes()) != 0 {
		t.Errorf("got the other household's favorites %v, want none", recipeNames(otherFavorites.GetRecipes()))
	}
	// Unmark it.
	updated, err = e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: masala.GetId()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"favorite"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if updated.GetRecipe().GetFavorite() {
		t.Error("got a favorite, want it unmarked")
	}

	// The output only fields and unknown fields can't be updated.
	for _, path := range []string{"nutrition", "nutrition.total_grams", "id", "update_time", "ingredient"} {
		_, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
			Recipe:     recipesv1.Recipe_builder{Id: soup.GetId()}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{path}},
		}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("updating %s: got %v, want invalid argument", path, err)
		}
	}

	// Invalid recipes are refused.
	valid := func() *recipesv1.Recipe {
		return recipesv1.Recipe_builder{
			Name:        "Hummus",
			Servings:    6,
			Ingredients: []*recipesv1.Ingredient{ingredient(chickpeas.GetId(), 250, grams)},
		}.Build()
	}
	for name, change := range map[string]func(r *recipesv1.Recipe){
		"no name":       func(r *recipesv1.Recipe) { r.SetName(" ") },
		"no servings":   func(r *recipesv1.Recipe) { r.SetServings(0) },
		"too many":      func(r *recipesv1.Recipe) { r.SetServings(101) },
		"negative time": func(r *recipesv1.Recipe) { r.SetCookSeconds(-1) },
		"no unit":       func(r *recipesv1.Recipe) { r.GetIngredients()[0].SetUnit(foodsv1.AmountUnit_AMOUNT_UNIT_UNSPECIFIED) },
		"no amount":     func(r *recipesv1.Recipe) { r.GetIngredients()[0].SetAmount(0) },
		"invalid food":  func(r *recipesv1.Recipe) { r.GetIngredients()[0].SetFoodId("chickpeas") },
		"empty step": func(r *recipesv1.Recipe) {
			r.SetInstructions([]*recipesv1.Step{recipesv1.Step_builder{Text: "Blend."}.Build(), recipesv1.Step_builder{Text: " "}.Build()})
		},
		"only steps":       func(r *recipesv1.Recipe) { r.SetInstructions(nil); r.SetSteps([]string{"Blend."}) },
		"empty tag":        func(r *recipesv1.Recipe) { r.SetTags([]string{""}) },
		"long tag":         func(r *recipesv1.Recipe) { r.SetTags([]string{strings.Repeat("a", 41)}) },
		"not a web URL":    func(r *recipesv1.Recipe) { r.SetSourceUrl("ftp://example.com/hummus") },
		"long fridge days": func(r *recipesv1.Recipe) { r.SetFridgeDays(30) },
	} {
		r := valid()
		change(r)
		_, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: r}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid argument", name, err)
		}
	}

	// Another household sees none of it, and can't change it.
	otherList, err := e.otherRecipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListRecipes of the other household: %v", err)
	}
	if len(otherList.GetRecipes()) != 0 || len(otherList.GetTags()) != 0 {
		t.Errorf("the other household sees %v and tags %v", recipeNames(otherList.GetRecipes()), otherList.GetTags())
	}
	_, err = e.otherRecipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: masala.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherRecipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{Recipe: valid()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	other := valid()
	other.SetId(masala.GetId())
	_, err = e.otherRecipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{Recipe: other}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherRecipes.DeleteRecipe(ctx, recipesv1.DeleteRecipeRequest_builder{Id: masala.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	// Nor use the household's foods in its recipes, or the reverse.
	_, err = e.otherRecipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: valid()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	if !strings.Contains(err.Error(), chickpeas.GetId()) {
		t.Errorf("got error %v, want it to name the food", err)
	}
	otherFood := createFood(t, e.otherFoods, foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{Name: "Tahini"}.Build()}.Build())
	withOther := valid()
	withOther.SetIngredients(append(withOther.GetIngredients(), ingredient(otherFood.GetId(), 30, grams)))
	_, err = e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: withOther}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	withOther.SetId(soup.GetId())
	_, err = e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     withOther,
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"ingredients"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	// The failed update left the soup as it was.
	got, err = e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: soup.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	if len(got.GetRecipe().GetIngredients()) != 3 {
		t.Errorf("got ingredients %v, want the soup's 3", got.GetRecipe().GetIngredients())
	}

	// Foods used by recipes can't be deleted.
	_, err = e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: onions.GetId()}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	if !strings.Contains(err.Error(), `"Chana masala" and "Lentil soup"`) {
		t.Errorf("got error %v, want it to name the recipes", err)
	}
	_, err = e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: chickpeas.GetId()}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)

	// Delete a recipe, after which its foods can be deleted.
	if _, err := e.recipes.DeleteRecipe(ctx, recipesv1.DeleteRecipeRequest_builder{Id: masala.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	_, err = e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: masala.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.recipes.DeleteRecipe(ctx, recipesv1.DeleteRecipeRequest_builder{Id: masala.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	if _, err := e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: chickpeas.GetId()}.Build()); err != nil {
		t.Errorf("DeleteFood of the chickpeas no recipe uses: %v", err)
	}
	list, err = e.recipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if !slices.Equal(recipeNames(list.GetRecipes()), []string{"Lentil soup"}) || !slices.Equal(list.GetTags(), []string{"lunch"}) {
		t.Errorf("got recipes %v and tags %v, want only the soup's", recipeNames(list.GetRecipes()), list.GetTags())
	}

	// Signing in is required.
	_, err = e.anonRecipes.ListRecipes(ctx, recipesv1.ListRecipesRequest_builder{}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = e.anonRecipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: valid()}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = e.anonRecipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: soup.GetId()}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
}
