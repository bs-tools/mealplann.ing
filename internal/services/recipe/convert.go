package recipe

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/services/food"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Limits of a recipe's fields.
const (
	maxNameLength        = 200
	maxDescriptionLength = 2000
	maxServings          = 100
	// maxSeconds is a week, longer than any recipe takes, even with
	// fermenting or sprouting.
	maxSeconds        = 7 * 24 * 60 * 60
	maxIngredients    = 100
	maxAmount         = 100_000
	maxNoteLength     = 200
	maxOriginalLength = 500
	maxSteps          = 100
	maxStepLength     = 2000
	maxSectionLength  = 100
	// maxStepSeconds is two days: longer steps, like soaking, start the
	// days before with prep_ahead_days.
	maxStepSeconds     = 2 * 24 * 60 * 60
	maxTags            = 20
	maxTagLength       = 40
	maxSourceURLLength = 2000
	// maxFridgeDays is longer than any cooked food keeps in a fridge.
	maxFridgeDays = 14
	// maxFreezerDays is a year, longer than cooked food keeps its quality
	// frozen.
	maxFreezerDays = 365
	// maxPrepAheadDays is a week: longer than any soaking, sprouting or
	// marinating ahead.
	maxPrepAheadDays       = 7
	maxPrepAheadNoteLength = 500
	maxNotesLength         = 5000
	maxQueryLength         = 200
	// maxFilterIDs is how many IDs a list can be filtered by.
	maxFilterIDs = 100
)

// nutritionDigits are the decimals computed nutrients are rounded to, which
// is enough for micrograms.
const nutritionDigits = 3

var unitsToProto = map[nutrition.Unit]foodsv1.AmountUnit{
	nutrition.Grams:       foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS,
	nutrition.Milliliters: foodsv1.AmountUnit_AMOUNT_UNIT_MILLILITERS,
	nutrition.Pieces:      foodsv1.AmountUnit_AMOUNT_UNIT_PIECES,
}

var unitsFromProto = map[foodsv1.AmountUnit]nutrition.Unit{
	foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS:       nutrition.Grams,
	foodsv1.AmountUnit_AMOUNT_UNIT_MILLILITERS: nutrition.Milliliters,
	foodsv1.AmountUnit_AMOUNT_UNIT_PIECES:      nutrition.Pieces,
}

func recipeToProto(r *recipes.Recipe) *recipesv1.Recipe {
	ingredients := make([]*recipesv1.Ingredient, 0, len(r.Ingredients))
	for _, in := range r.Ingredients {
		b := recipesv1.Ingredient_builder{
			Amount:       in.Amount,
			Note:         in.Note,
			OriginalText: in.OriginalText,
			Optional:     in.Optional,
		}
		if in.IsComponent() {
			b.RecipeId = store.UUIDToString(in.ComponentRecipeID)
			b.RecipeName = in.Name()
		} else {
			b.FoodId = store.UUIDToString(in.FoodID)
			b.FoodName = in.FoodName
			b.Unit = unitsToProto[in.Unit]
			b.FoodMeasure = food.MeasuresToProto[in.FoodMeasure]
			b.FoodGramsPerMilliliter = in.Food.GramsPerMilliliter
			b.FoodGramsPerPiece = in.Food.GramsPerPiece
		}
		ingredients = append(ingredients, b.Build())
	}
	return recipesv1.Recipe_builder{
		Id:            store.UUIDToString(r.ID),
		Name:          r.Name,
		Description:   r.Description,
		Servings:      r.Servings,
		PrepSeconds:   r.PrepSeconds,
		CookSeconds:   r.CookSeconds,
		Ingredients:   ingredients,
		Steps:         r.Steps,
		Instructions:  instructionsToProto(r.Instructions),
		Cost:          costToProto(r),
		Tags:          r.Tags,
		SourceUrl:     r.SourceURL,
		FridgeDays:    r.FridgeDays,
		Freezable:     r.Freezable,
		FreezerDays:   r.FreezerDays,
		PrepAheadDays: r.PrepAheadDays,
		PrepAheadNote: r.PrepAheadNote,
		Notes:         r.Notes,
		Favorite:      r.Favorite,
		Nutrition:     nutritionToProto(r),
		CreateTime:    timestamppb.New(r.CreateTime),
		UpdateTime:    timestamppb.New(r.UpdateTime),
	}.Build()
}

func nutritionToProto(r *recipes.Recipe) *recipesv1.RecipeNutrition {
	n := r.Nutrition()
	incomplete := make([]string, 0, len(n.Incomplete))
	for _, nutrient := range n.Incomplete {
		incomplete = append(incomplete, nutrient.String())
	}
	issues := make([]*recipesv1.IngredientIssue, 0, len(n.Issues))
	for _, issue := range n.Issues {
		in := r.Ingredients[issue.Index]
		unknown := make([]string, 0, len(issue.UnknownNutrients))
		for _, nutrient := range issue.UnknownNutrients {
			unknown = append(unknown, nutrient.String())
		}
		issues = append(issues, recipesv1.IngredientIssue_builder{
			IngredientIndex:  int32(issue.Index),
			FoodId:           store.UUIDToString(in.FoodID),
			FoodName:         in.Name(),
			UnknownWeight:    issue.UnknownWeight,
			UnknownNutrients: unknown,
		}.Build())
	}
	return recipesv1.RecipeNutrition_builder{
		TotalGrams:          round(n.TotalGrams),
		GramsPerServing:     round(n.GramsPerServing),
		PerServing:          food.NutrientsToProto(&n.PerServing, nutritionDigits),
		PerHundredGrams:     food.NutrientsToProto(&n.Per100Grams, nutritionDigits),
		IncompleteNutrients: incomplete,
		Issues:              issues,
	}.Build()
}

func summaryToProto(r *recipes.Recipe) *recipesv1.RecipeSummary {
	n := r.Nutrition()
	perServing := func(nutrient nutrition.Nutrient) *float64 {
		if v := n.PerServing[nutrient]; v != nil {
			return new(round(*v))
		}
		return nil
	}
	return recipesv1.RecipeSummary_builder{
		Id:                          store.UUIDToString(r.ID),
		Name:                        r.Name,
		Description:                 r.Description,
		Servings:                    r.Servings,
		PrepSeconds:                 r.PrepSeconds,
		CookSeconds:                 r.CookSeconds,
		Tags:                        r.Tags,
		EnergyKcalPerServing:        perServing(nutrition.EnergyKcal),
		ProteinGramsPerServing:      perServing(nutrition.ProteinGrams),
		CarbohydrateGramsPerServing: perServing(nutrition.CarbohydrateGrams),
		GramsPerServing:             round(n.GramsPerServing),
		HasIssues:                   len(n.Issues) > 0,
		UpdateTime:                  timestamppb.New(r.UpdateTime),
		Favorite:                    r.Favorite,
		HasInstructions:             r.Instructions != nil,
		CostCentsPerServing:         costPerServing(r),
	}.Build()
}

// costToProto returns what a recipe's ingredients cost.
func costToProto(r *recipes.Recipe) *recipesv1.RecipeCost {
	c := r.Cost()
	return recipesv1.RecipeCost_builder{
		TotalCents:        int32(math.Round(c.Cents)),
		CentsPerServing:   int32(math.Round(c.Cents / float64(max(r.Servings, 1)))),
		UnpricedFoodNames: c.Unpriced,
	}.Build()
}

// costPerServing returns what a serving of a recipe costs, nil if none of
// its foods have a price.
func costPerServing(r *recipes.Recipe) *int32 {
	c := r.Cost()
	if c.Cents == 0 {
		return nil
	}
	return new(int32(math.Round(c.Cents / float64(max(r.Servings, 1)))))
}

// round rounds a computed amount to nutritionDigits decimals.
func round(v float64) float64 {
	scale := math.Pow10(nutritionDigits)
	return math.Round(v*scale) / scale
}

// recipeFromProto converts and validates a recipe. Its ID, household and
// times are left for the caller to set.
func recipeFromProto(p *recipesv1.Recipe) (*recipes.Recipe, error) {
	name := strings.TrimSpace(p.GetName())
	description := strings.TrimSpace(p.GetDescription())
	notes := strings.TrimSpace(p.GetNotes())
	prepNote := strings.TrimSpace(p.GetPrepAheadNote())
	switch {
	case name == "":
		return nil, connect.NewError(connect.CodeInvalidArgument, `name is required: give the recipe a name, e.g. "Chana masala"`)
	case utf8.RuneCountInString(name) > maxNameLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "name must be at most %d characters", maxNameLength)
	case utf8.RuneCountInString(description) > maxDescriptionLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "description must be at most %d characters", maxDescriptionLength)
	case utf8.RuneCountInString(notes) > maxNotesLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "notes must be at most %d characters", maxNotesLength)
	case p.GetServings() < 1 || p.GetServings() > maxServings:
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"servings must be from 1 to %d: set it to the number of servings the recipe makes", maxServings)
	case p.GetPrepSeconds() < 0 || p.GetPrepSeconds() > maxSeconds:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "prep_seconds must be from 0 to %d, with 0 for unknown", maxSeconds)
	case p.GetCookSeconds() < 0 || p.GetCookSeconds() > maxSeconds:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "cook_seconds must be from 0 to %d, with 0 for unknown", maxSeconds)
	case p.HasFridgeDays() && (p.GetFridgeDays() < 0 || p.GetFridgeDays() > maxFridgeDays):
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"fridge_days must be from 0 to %d; leave it unset if unknown", maxFridgeDays)
	case p.HasFreezerDays() && (p.GetFreezerDays() < 1 || p.GetFreezerDays() > maxFreezerDays):
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"freezer_days must be from 1 to %d; leave it unset if unknown", maxFreezerDays)
	case p.GetPrepAheadDays() < 0 || p.GetPrepAheadDays() > maxPrepAheadDays:
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"prep_ahead_days must be from 0 to %d, with 0 for a recipe started the day it's cooked", maxPrepAheadDays)
	case utf8.RuneCountInString(prepNote) > maxPrepAheadNoteLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "prep_ahead_note must be at most %d characters", maxPrepAheadNoteLength)
	}
	ingredients, err := ingredientsFromProto(p.GetIngredients())
	if err != nil {
		return nil, err
	}
	// With instructions, the steps are their texts, so the steps given
	// are ignored: they're what was read, or stale.
	var steps []string
	var instructions []*recipes.Step
	if len(p.GetInstructions()) > 0 {
		instructions, err = instructionsFromProto(p.GetInstructions(), len(ingredients))
		if err != nil {
			return nil, err
		}
		steps = recipes.StepTexts(instructions)
	} else {
		steps, err = stepsFromProto(p.GetSteps())
		if err != nil {
			return nil, err
		}
	}
	tags, err := normalizeTags(p.GetTags())
	if err != nil {
		return nil, err
	}
	sourceURL, err := sourceURLFromProto(p.GetSourceUrl())
	if err != nil {
		return nil, err
	}
	var fridgeDays, freezerDays *int32
	if p.HasFridgeDays() {
		fridgeDays = new(p.GetFridgeDays())
	}
	if p.HasFreezerDays() {
		freezerDays = new(p.GetFreezerDays())
	}
	return &recipes.Recipe{
		Name:          name,
		Description:   description,
		Servings:      p.GetServings(),
		PrepSeconds:   p.GetPrepSeconds(),
		CookSeconds:   p.GetCookSeconds(),
		Ingredients:   ingredients,
		Steps:         steps,
		Instructions:  instructions,
		Tags:          tags,
		SourceURL:     sourceURL,
		FridgeDays:    fridgeDays,
		Freezable:     p.GetFreezable(),
		FreezerDays:   freezerDays,
		PrepAheadDays: p.GetPrepAheadDays(),
		PrepAheadNote: prepNote,
		Notes:         notes,
		Favorite:      p.GetFavorite(),
	}, nil
}

func ingredientsFromProto(list []*recipesv1.Ingredient) ([]*recipes.Ingredient, error) {
	if len(list) > maxIngredients {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "a recipe can have at most %d ingredients", maxIngredients)
	}
	ingredients := make([]*recipes.Ingredient, 0, len(list))
	for i, p := range list {
		field := fmt.Sprintf("ingredients[%d]", i)
		var foodID, componentID pgtype.UUID
		var unit nutrition.Unit
		var err error
		switch {
		case p.GetRecipeId() != "" && p.GetFoodId() != "":
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s has both food_id and recipe_id: set food_id for a food, or recipe_id for a component", field)
		case p.GetRecipeId() != "":
			componentID, err = store.StringToUUID(p.GetRecipeId())
			if err != nil {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.recipe_id is invalid: %v; find the household's recipes with ListRecipes", field, err).WithCause(err)
			}
			if p.GetUnit() != foodsv1.AmountUnit_AMOUNT_UNIT_UNSPECIFIED {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.unit must be unspecified for a component, whose amount is in servings of its recipe", field)
			}
		default:
			foodID, err = store.StringToUUID(p.GetFoodId())
			if err != nil {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.food_id is invalid: %v; find the household's foods with ListFoods, or set recipe_id for a component", field, err).WithCause(err)
			}
			var ok bool
			unit, ok = unitsFromProto[p.GetUnit()]
			if !ok {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.unit is required: AMOUNT_UNIT_GRAMS, AMOUNT_UNIT_MILLILITERS or AMOUNT_UNIT_PIECES", field)
			}
		}
		amount := p.GetAmount()
		if math.IsNaN(amount) || amount <= 0 || amount > maxAmount {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.amount must be more than 0 and at most %d", field, maxAmount)
		}
		note := strings.TrimSpace(p.GetNote())
		if utf8.RuneCountInString(note) > maxNoteLength {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.note must be at most %d characters", field, maxNoteLength)
		}
		original := strings.TrimSpace(p.GetOriginalText())
		if utf8.RuneCountInString(original) > maxOriginalLength {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.original_text must be at most %d characters", field, maxOriginalLength)
		}
		ingredients = append(ingredients, &recipes.Ingredient{
			FoodID:            foodID,
			ComponentRecipeID: componentID,
			Amount:            amount,
			Unit:              unit,
			Note:              note,
			OriginalText:      original,
			Optional:          p.GetOptional(),
		})
	}
	return ingredients, nil
}

// instructionsToProto converts instructions, nil for none.
func instructionsToProto(steps []*recipes.Step) []*recipesv1.Step {
	if steps == nil {
		return nil
	}
	out := make([]*recipesv1.Step, 0, len(steps))
	for _, s := range steps {
		b := recipesv1.Step_builder{
			Text:            s.Text,
			Section:         s.Section,
			DurationSeconds: s.DurationSeconds,
			Waiting:         s.Waiting,
			FromStart:       s.FromStart,
		}
		for _, i := range s.Ingredients {
			b.IngredientIndexes = append(b.IngredientIndexes, int32(i))
		}
		for _, i := range s.After {
			b.AfterStepIndexes = append(b.AfterStepIndexes, int32(i))
		}
		out = append(out, b.Build())
	}
	return out
}

// instructionsFromProto converts and checks instructions, given how many
// ingredients the recipe has.
func instructionsFromProto(list []*recipesv1.Step, ingredientCount int) ([]*recipes.Step, error) {
	if len(list) > maxSteps {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "a recipe can have at most %d steps", maxSteps)
	}
	steps := make([]*recipes.Step, 0, len(list))
	for i, p := range list {
		field := fmt.Sprintf("instructions[%d]", i)
		text := strings.TrimSpace(p.GetText())
		section := strings.TrimSpace(p.GetSection())
		switch {
		case text == "":
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.text is empty: say what to do, or remove the step", field)
		case utf8.RuneCountInString(text) > maxStepLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.text must be at most %d characters; split it into several steps", field, maxStepLength)
		case utf8.RuneCountInString(section) > maxSectionLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.section must be at most %d characters", field, maxSectionLength)
		case p.GetDurationSeconds() < 0 || p.GetDurationSeconds() > maxStepSeconds:
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s.duration_seconds must be from 0 to %d, with 0 for unknown; start longer steps, like soaking, days ahead with prep_ahead_days", field, maxStepSeconds)
		case p.GetFromStart() && len(p.GetAfterStepIndexes()) > 0:
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s has both from_start and after_step_indexes: a step either starts right away or after others", field)
		}
		s := &recipes.Step{
			Text:            text,
			Section:         section,
			DurationSeconds: p.GetDurationSeconds(),
			Waiting:         p.GetWaiting(),
			FromStart:       p.GetFromStart(),
		}
		for _, in := range p.GetIngredientIndexes() {
			if in < 0 || int(in) >= ingredientCount {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.ingredient_indexes has %d, but the recipe's ingredients are indexed from 0 to %d", field, in, ingredientCount-1)
			}
			if !slices.Contains(s.Ingredients, int(in)) {
				s.Ingredients = append(s.Ingredients, int(in))
			}
		}
		for _, after := range p.GetAfterStepIndexes() {
			if after < 0 || int(after) >= i {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.after_step_indexes has %d, but a step can only start after the steps before it, from 0 to %d", field, after, i-1)
			}
			if !slices.Contains(s.After, int(after)) {
				s.After = append(s.After, int(after))
			}
		}
		steps = append(steps, s)
	}
	return steps, nil
}

func stepsFromProto(list []string) ([]string, error) {
	if len(list) > maxSteps {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "a recipe can have at most %d steps", maxSteps)
	}
	steps := make([]string, 0, len(list))
	for i, step := range list {
		step = strings.TrimSpace(step)
		switch {
		case step == "":
			return nil, connect.Errorf(connect.CodeInvalidArgument, "steps[%d] is empty; remove it", i)
		case utf8.RuneCountInString(step) > maxStepLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "steps[%d] must be at most %d characters; split it into several steps", i, maxStepLength)
		}
		steps = append(steps, step)
	}
	return steps, nil
}

// normalizeTags trims and lowercases tags, and removes duplicates, keeping
// their order.
func normalizeTags(list []string) ([]string, error) {
	tags := make([]string, 0, len(list))
	for i, tag := range list {
		tag = strings.ToLower(strings.TrimSpace(tag))
		switch {
		case tag == "":
			return nil, connect.Errorf(connect.CodeInvalidArgument, "tags[%d] is empty; remove it", i)
		case utf8.RuneCountInString(tag) > maxTagLength:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "tags[%d] must be at most %d characters", i, maxTagLength)
		}
		if !slices.Contains(tags, tag) {
			tags = append(tags, tag)
		}
	}
	if len(tags) > maxTags {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "a recipe can have at most %d tags", maxTags)
	}
	return tags, nil
}

func sourceURLFromProto(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(s) > maxSourceURLLength {
		return "", connect.Errorf(connect.CodeInvalidArgument,
			"source_url must be an http or https URL of at most %d characters, or empty", maxSourceURLLength)
	}
	return s, nil
}
