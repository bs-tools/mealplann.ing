package services_test

import (
	"context"
	"slices"
	"testing"

	"connectrpc.com/connect/v2"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestInstructions(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	rice := customFood(t, e, "Rice", 360, 8, 76, 1, 3, nil)
	lentils := customFood(t, e, "Red lentils", 350, 25, 60, 2, 1, nil)
	onions := customFood(t, e, "Onions", 40, 1, 9, 4, 0.1, nil)

	ingredients := []*recipesv1.Ingredient{
		ingredient(rice.GetId(), 300, grams), ingredient(lentils.GetId(), 250, grams), ingredient(onions.GetId(), 150, grams),
	}
	// Steps alone are refused, as they're read from the instructions.
	_, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Dal and rice", Servings: 4, Ingredients: ingredients,
		Steps: []string{"Cook the rice.", "Chop and fry the onions, add the lentils and simmer for 20 minutes."},
	}.Build()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	// A dal of two steps of unknown duration runs them in a row,
	// estimated.
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Dal and rice", Servings: 4, Ingredients: ingredients,
		Instructions: []*recipesv1.Step{
			recipesv1.Step_builder{Text: "Cook the rice."}.Build(),
			recipesv1.Step_builder{Text: "Chop and fry the onions, add the lentils and simmer."}.Build(),
		},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	dal := created.GetRecipe()
	if !slices.Equal(dal.GetSteps(), []string{"Cook the rice.", "Chop and fry the onions, add the lentils and simmer."}) {
		t.Errorf("got steps %v, want the instructions' texts", dal.GetSteps())
	}
	tl, err := e.recipes.GetRecipeTimeline(ctx, recipesv1.GetRecipeTimelineRequest_builder{Id: dal.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetRecipeTimeline: %v", err)
	}
	if s := tl.GetSteps(); len(s) != 2 || !s[1].GetEstimated() || s[1].GetStartSeconds() != s[0].GetEndSeconds() {
		t.Errorf("got %v, want two estimated steps in a row", s)
	}

	// Rewritten with structure: the rice simmers while the onions are
	// chopped and fried, and the steps follow from them.
	step := func(text string, ingredients []int32, seconds int32, waiting bool) *recipesv1.Step {
		return recipesv1.Step_builder{Text: text, IngredientIndexes: ingredients, DurationSeconds: seconds, Waiting: waiting}.Build()
	}
	instructions := []*recipesv1.Step{
		step("Rinse the rice and bring it to a boil", []int32{0}, 300, false),
		step("Simmer the rice, covered", nil, 1080, true),
		recipesv1.Step_builder{Text: "Chop the onions", IngredientIndexes: []int32{2}, DurationSeconds: 300, FromStart: true, Section: "For the dal"}.Build(),
		recipesv1.Step_builder{Text: "Fry the onions", DurationSeconds: 480, Section: "For the dal"}.Build(),
		recipesv1.Step_builder{Text: "Add the lentils and simmer", IngredientIndexes: []int32{1}, DurationSeconds: 1200, Waiting: true, Section: "For the dal"}.Build(),
		recipesv1.Step_builder{Text: "Serve the dal over the rice", AfterStepIndexes: []int32{1, 4}}.Build(),
	}
	updated, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
		Recipe:     recipesv1.Recipe_builder{Id: dal.GetId(), Instructions: instructions}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"instructions"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	got := updated.GetRecipe()
	if len(got.GetInstructions()) != 6 || got.GetInstructions()[4].GetDurationSeconds() != 1200 || !got.GetInstructions()[2].GetFromStart() ||
		!slices.Equal(got.GetInstructions()[5].GetAfterStepIndexes(), []int32{1, 4}) || got.GetInstructions()[2].GetSection() != "For the dal" {
		t.Errorf("got %v, want the instructions back", got.GetInstructions())
	}
	if steps := got.GetSteps(); len(steps) != 6 || steps[0] != "Rinse the rice and bring it to a boil" {
		t.Errorf("got steps %v, want the instructions' texts", steps)
	}
	tl, err = e.recipes.GetRecipeTimeline(ctx, recipesv1.GetRecipeTimelineRequest_builder{Id: dal.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetRecipeTimeline: %v", err)
	}
	// Rinse 0–5, chop 5–10 while the rice simmers 5–23, fry 10–18, the
	// dal simmers 18–38, and serving waits for both.
	var spans [][2]int32
	for _, s := range tl.GetSteps() {
		spans = append(spans, [2]int32{s.GetStartSeconds() / 60, s.GetEndSeconds() / 60})
	}
	if want := [][2]int32{{0, 5}, {5, 23}, {5, 10}, {10, 18}, {18, 38}, {38, 43}}; !slices.Equal(spans, want) {
		t.Errorf("got %v minutes, want %v", spans, want)
	}
	if tl.GetHandsOnSeconds() != (5+5+8+5)*60 || tl.GetTotalSeconds() != 43*60 || tl.GetSteps()[2].GetSection() != "For the dal" {
		t.Errorf("got %d s hands-on of %d, want %d of %d", tl.GetHandsOnSeconds(), tl.GetTotalSeconds(), (5+5+8+5)*60, 43*60)
	}

	// A full update with the recipe as read keeps the instructions, and its
	// stale steps are ignored.
	read := updated.GetRecipe()
	read.SetSteps([]string{"Something else"})
	again, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{Recipe: read}.Build())
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if steps := again.GetRecipe().GetSteps(); steps[0] != "Rinse the rice and bring it to a boil" {
		t.Errorf("got steps %v, want the instructions' texts", steps)
	}

	// Mistakes are refused, saying what to do.
	bad := func(name string, mask []string, r *recipesv1.Recipe) {
		t.Helper()
		_, err := e.recipes.UpdateRecipe(ctx, recipesv1.UpdateRecipeRequest_builder{
			Recipe: r, UpdateMask: &fieldmaskpb.FieldMask{Paths: mask},
		}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid argument", name, err)
		}
	}
	with := func(s *recipesv1.Step) *recipesv1.Recipe {
		return recipesv1.Recipe_builder{Id: dal.GetId(), Instructions: []*recipesv1.Step{step("First", nil, 0, false), s}}.Build()
	}
	bad("only steps", []string{"steps"}, recipesv1.Recipe_builder{Id: dal.GetId(), Steps: []string{"Just cook it"}}.Build())
	bad("unknown ingredient", []string{"instructions"}, with(step("Add it", []int32{3}, 0, false)))
	bad("a later step", []string{"instructions"}, with(recipesv1.Step_builder{Text: "Wait", AfterStepIndexes: []int32{1}}.Build()))
	bad("both starts", []string{"instructions"}, with(recipesv1.Step_builder{Text: "Go", FromStart: true, AfterStepIndexes: []int32{0}}.Build()))
	bad("too long", []string{"instructions"}, with(step("Soak", nil, 3*24*3600, true)))
	bad("empty", []string{"instructions"}, with(step(" ", nil, 0, false)))

	// Replacing a food keeps the instructions.
	brown := customFood(t, e, "Brown rice", 360, 8, 76, 1, 3, nil)
	if _, err := e.recipes.ReplaceFood(ctx, recipesv1.ReplaceFoodRequest_builder{FoodId: rice.GetId(), NewFoodId: brown.GetId()}.Build()); err != nil {
		t.Fatalf("ReplaceFood: %v", err)
	}
	after, err := e.recipes.GetRecipe(ctx, recipesv1.GetRecipeRequest_builder{Id: dal.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetRecipe: %v", err)
	}
	if len(after.GetRecipe().GetInstructions()) != 6 {
		t.Errorf("got %v, want the instructions kept", after.GetRecipe().GetInstructions())
	}
}
