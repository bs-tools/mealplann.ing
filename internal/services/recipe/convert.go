package recipe

import (
	"fmt"
	"math"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bs-tools/mealplann.ing/internal/services/amounts"

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

func recipeToProto(r *recipes.Recipe) *recipesv1.Recipe {
	ingredients := make([]*recipesv1.Ingredient, 0, len(r.Ingredients))
	for _, in := range r.Ingredients {
		b := recipesv1.Ingredient_builder{
			Amount:       in.Amount,
			Note:         in.Note,
			OriginalText: in.OriginalText,
			Optional:     in.Optional,
		}
		switch {
		case in.IsComponent():
			b.RecipeId = store.UUIDToString(in.ComponentRecipeID)
			b.RecipeName = in.Name()
			b.Amount, b.Unit = amounts.ToProto(in.Amount, in.Unit, in.KitchenUnit)
			b.ComponentServings = round(in.ComponentServings())
		case !in.IsMatched():
			b.Amount, b.Unit = amounts.ToProto(in.Amount, in.Unit, in.KitchenUnit)
		default:
			b.FoodId = store.UUIDToString(in.FoodID)
			b.FoodName = in.FoodName
			b.Amount, b.Unit = amounts.ToProto(in.Amount, in.Unit, in.KitchenUnit)
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
		Steps:         recipes.StepTexts(r.Instructions),
		Instructions:  instructionsToProto(r.Instructions, r.Ingredients),
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
		Draft:         r.Draft,
		YieldAmount:   r.YieldAmount,
		YieldUnit:     yieldUnitToProto(r),
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
			UnknownWeight:    issue.UnknownWeight && in.IsMatched(),
			UnknownNutrients: unknown,
			Unmatched:        !in.IsMatched(),
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
		CostCentsPerServing:         costPerServing(r),
		Draft:                       r.Draft,
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
	ingredients, err := ingredientsFromProto(p.GetIngredients(), p.GetDraft())
	if err != nil {
		return nil, err
	}
	// The steps are the instructions' texts, so the steps given are
	// ignored, being what was read, unless they're all there is: then
	// they'd be lost, so it fails, saying what to set.
	if len(p.GetInstructions()) == 0 && len(p.GetSteps()) > 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"steps are read only, the texts of instructions: give the steps as instructions, with each one's text, ingredients and duration")
	}
	instructions, err := instructionsFromProto(p.GetInstructions(), ingredients)
	if err != nil {
		return nil, err
	}
	tags, err := normalizeTags(p.GetTags())
	if err != nil {
		return nil, err
	}
	sourceURL, err := sourceURLFromProto(p.GetSourceUrl())
	if err != nil {
		return nil, err
	}
	yieldAmount, yieldUnit, err := yieldFromProto(p)
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
		YieldAmount:   yieldAmount,
		YieldUnit:     yieldUnit,
		Draft:         p.GetDraft(),
	}, nil
}

// yieldUnitToProto returns the unit of what a recipe makes, unspecified if
// it isn't given.
func yieldUnitToProto(r *recipes.Recipe) foodsv1.AmountUnit {
	if r.YieldAmount <= 0 {
		return foodsv1.AmountUnit_AMOUNT_UNIT_UNSPECIFIED
	}
	_, unit := amounts.ToProto(r.YieldAmount, r.YieldUnit, nutrition.NoKitchenUnit)
	return unit
}

// yieldFromProto converts and checks what a recipe makes, in its base
// unit.
func yieldFromProto(p *recipesv1.Recipe) (float64, nutrition.Unit, error) {
	if p.GetYieldAmount() == 0 && p.GetYieldUnit() == foodsv1.AmountUnit_AMOUNT_UNIT_UNSPECIFIED {
		return 0, 0, nil
	}
	amount, unit, _, ok := amounts.FromProto(p.GetYieldAmount(), p.GetYieldUnit())
	switch {
	case !ok:
		return 0, 0, connect.Errorf(connect.CodeInvalidArgument, "yield_unit is required with yield_amount: %s", amounts.UnitsHelp)
	case math.IsNaN(amount) || amount <= 0 || amount > maxAmount:
		return 0, 0, connect.Errorf(connect.CodeInvalidArgument,
			"yield_amount must be more than 0 and at most %d g, ml or pieces, or 0 with no yield_unit if unknown", maxAmount)
	}
	return amount, unit, nil
}

// ingredientsFromProto converts and checks ingredients, which a draft's
// can leave unmatched.
func ingredientsFromProto(list []*recipesv1.Ingredient, draft bool) ([]*recipes.Ingredient, error) {
	if len(list) > maxIngredients {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "a recipe can have at most %d ingredients", maxIngredients)
	}
	ingredients := make([]*recipes.Ingredient, 0, len(list))
	for i, p := range list {
		field := fmt.Sprintf("ingredients[%d]", i)
		var foodID, componentID pgtype.UUID
		var unit nutrition.Unit
		var kitchen nutrition.KitchenUnit
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
				var ok bool
				if _, unit, kitchen, ok = amounts.FromProto(0, p.GetUnit()); !ok {
					return nil, connect.Errorf(connect.CodeInvalidArgument,
						"%s.unit is unknown: leave it unspecified for servings of the recipe, or give %s", field, amounts.UnitsHelp)
				}
			}
		case p.GetFoodId() == "" && draft:
			// An unmatched ingredient of a draft, with its unit if known.
			if strings.TrimSpace(p.GetOriginalText()) == "" {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s has no food_id, recipe_id nor original_text: give the ingredient as the recipe writes it in original_text, to match it later", field)
			}
			if p.GetUnit() != foodsv1.AmountUnit_AMOUNT_UNIT_UNSPECIFIED {
				var ok bool
				if _, unit, kitchen, ok = amounts.FromProto(0, p.GetUnit()); !ok {
					return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.unit is unknown: give %s", field, amounts.UnitsHelp)
				}
			}
		case p.GetFoodId() == "":
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s has no food_id: choose its food (find foods with ListFoods), set recipe_id for a component, or save the recipe as a draft to match it later", field)
		default:
			foodID, err = store.StringToUUID(p.GetFoodId())
			if err != nil {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.food_id is invalid: %v; find the household's foods with ListFoods, or set recipe_id for a component", field, err).WithCause(err)
			}
			var ok bool
			if _, unit, kitchen, ok = amounts.FromProto(0, p.GetUnit()); !ok {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.unit is required: %s", field, amounts.UnitsHelp)
			}
		}
		amount := p.GetAmount()
		unmatched := !foodID.Valid && !componentID.Valid
		switch {
		case unmatched && amount == 0:
			// The amount isn't known yet.
		case math.IsNaN(amount) || amount <= 0 || amount > maxAmount:
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.amount must be more than 0 and at most %d", field, maxAmount)
		}
		if kitchen != nutrition.NoKitchenUnit {
			amount, _, _, _ = amounts.FromProto(amount, p.GetUnit())
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
			KitchenUnit:       kitchen,
			Note:              note,
			OriginalText:      original,
			Optional:          p.GetOptional(),
		})
	}
	return ingredients, nil
}

// instructionsToProto converts instructions, nil for none, with the
// amounts of ingredients in the units they were entered in.
func instructionsToProto(steps []*recipes.Step, ingredients []*recipes.Ingredient) []*recipesv1.Step {
	if steps == nil {
		return nil
	}
	out := make([]*recipesv1.Step, 0, len(steps))
	for _, s := range steps {
		b := recipesv1.Step_builder{
			Text:               s.Text,
			Section:            s.Section,
			DurationSeconds:    s.DurationSeconds,
			Waiting:            s.Waiting,
			FromStart:          s.FromStart,
			StartOffsetSeconds: s.StartOffsetSeconds,
			EndBeforeSeconds:   s.EndBeforeSeconds,
		}
		for k, a := range s.Amounts {
			if k < len(s.Ingredients) && s.Ingredients[k] < len(ingredients) {
				a /= kitchenSize(ingredients[s.Ingredients[k]])
			}
			b.IngredientAmounts = append(b.IngredientAmounts, a)
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

// instructionsFromProto converts and checks instructions, given the
// recipe's ingredients.
func instructionsFromProto(list []*recipesv1.Step, ingredients []*recipes.Ingredient) ([]*recipes.Step, error) {
	ingredientCount := len(ingredients)
	// used is how much of each ingredient the steps use, by position.
	used := make([]float64, ingredientCount)
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
		case p.GetStartOffsetSeconds() < 0 || p.GetStartOffsetSeconds() > maxStepSeconds:
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s.start_offset_seconds must be from 0 to %d, with 0 to start once the steps it follows are done", field, maxStepSeconds)
		case p.HasEndBeforeSeconds() && (p.GetEndBeforeSeconds() < 0 || p.GetEndBeforeSeconds() > maxStepSeconds):
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s.end_before_seconds must be from 0 to %d, with 0 to end as the steps it follows do; leave it unset to start after them", field, maxStepSeconds)
		case p.HasEndBeforeSeconds() && p.GetStartOffsetSeconds() > 0:
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s has both start_offset_seconds and end_before_seconds: time it by when the steps it follows start, or by when they end", field)
		case p.HasEndBeforeSeconds() && (i == 0 || p.GetFromStart()):
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s.end_before_seconds needs steps to end before: set after_step_indexes, or leave from_start unset, for a step after the first", field)
		case len(p.GetIngredientAmounts()) > 0 && len(p.GetIngredientAmounts()) != len(p.GetIngredientIndexes()):
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"%s has %d ingredient_amounts for %d ingredient_indexes: give one for each, 0 for all of it, or none",
				field, len(p.GetIngredientAmounts()), len(p.GetIngredientIndexes()))
		}
		s := &recipes.Step{
			Text:               text,
			Section:            section,
			DurationSeconds:    p.GetDurationSeconds(),
			Waiting:            p.GetWaiting(),
			FromStart:          p.GetFromStart(),
			StartOffsetSeconds: p.GetStartOffsetSeconds(),
		}
		if p.HasEndBeforeSeconds() {
			s.EndBeforeSeconds = new(p.GetEndBeforeSeconds())
		}
		var amounts []float64
		for k, in := range p.GetIngredientIndexes() {
			if in < 0 || int(in) >= ingredientCount {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.ingredient_indexes has %d, but the recipe's ingredients are indexed from 0 to %d", field, in, ingredientCount-1)
			}
			var amount float64
			if k < len(p.GetIngredientAmounts()) {
				amount = p.GetIngredientAmounts()[k]
				if math.IsNaN(amount) || amount < 0 {
					return nil, connect.Errorf(connect.CodeInvalidArgument,
						"%s.ingredient_amounts[%d] must be at least 0, with 0 for all of it or the rest", field, k)
				}
				// It's in the unit the ingredient's amount was given in.
				amount *= kitchenSize(ingredients[in])
			}
			// An ingredient given twice uses both amounts.
			if j := slices.Index(s.Ingredients, int(in)); j >= 0 {
				amounts[j] += amount
			} else {
				s.Ingredients = append(s.Ingredients, int(in))
				amounts = append(amounts, amount)
			}
			used[in] += amount
			if have := ingredients[in].Amount; used[in] > have*(1+1e-9) {
				size := kitchenSize(ingredients[in])
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"the steps use %v of %s, but the recipe has %v: lower their ingredient_amounts, or raise the ingredient's amount",
					used[in]/size, ingredients[in].Name(), have/size)
			}
		}
		if slices.ContainsFunc(amounts, func(a float64) bool { return a > 0 }) {
			s.Amounts = amounts
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

// kitchenSize returns how many of its base unit one of the kitchen unit an
// ingredient was entered in is, like 15 for tablespoons, or 1.
func kitchenSize(in *recipes.Ingredient) float64 {
	if _, size := in.KitchenUnit.Base(); size > 0 {
		return size
	}
	return 1
}
