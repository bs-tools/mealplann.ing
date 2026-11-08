package plan

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/services/person"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/suggest"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// maxPlanDays is the most days SuggestPlan plans: two weeks, which is
	// as far ahead as households shop, and which the search still covers
	// with enough tries in its budget.
	maxPlanDays = 14
	// maxMealsToPlan is the most meals PlanMeals plans at once: two weeks
	// of the three shared meals, rounded up.
	maxMealsToPlan = 60
	// maxPlanSeconds is the longest max_total_seconds of plan criteria: a
	// day.
	maxPlanSeconds = 24 * 60 * 60
	// maxPlanExclusions is the most tags or recipes a plan can exclude.
	maxPlanExclusions = 100
)

// mealRequired says a meal is required, after its field's name.
const mealRequired = "meal is required: HOUSEHOLD_MEAL_BREAKFAST, HOUSEHOLD_MEAL_LUNCH or HOUSEHOLD_MEAL_DINNER"

// mealOrder is the order of the shared meals in a day.
var mealOrder = []people.Meal{people.MealBreakfast, people.MealLunch, people.MealDinner}

// compareMeals orders shared meals as in a day.
func compareMeals(a, b people.Meal) int {
	return cmp.Compare(slices.Index(mealOrder, a), slices.Index(mealOrder, b))
}

// emphasisWeights are the weights of the criteria's emphases.
var emphasisWeights = map[suggestionsv1.Emphasis]float64{
	suggestionsv1.Emphasis_EMPHASIS_UNSPECIFIED: 1,
	suggestionsv1.Emphasis_EMPHASIS_OFF:         0,
	suggestionsv1.Emphasis_EMPHASIS_LOW:         0.5,
	suggestionsv1.Emphasis_EMPHASIS_NORMAL:      1,
	suggestionsv1.Emphasis_EMPHASIS_HIGH:        2,
}

// slotKey identifies a slot of a plan: a shared meal on a date.
type slotKey struct {
	date time.Time
	meal people.Meal
}

func (s *Service) SuggestPlan(ctx context.Context, req *suggestionsv1.SuggestPlanRequest) (*suggestionsv1.SuggestPlanResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	start, err := parseDate("start_date", req.GetStartDate())
	if err != nil {
		return nil, err
	}
	days := int(req.GetDays())
	if days < 1 || days > maxPlanDays {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "days must be from 1 to %d", maxPlanDays)
	}
	end := start.AddDate(0, 0, days-1)
	var meals []people.Meal
	for _, m := range req.GetMeals() {
		meal, err := mealFromProto(m)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				"meals must be HOUSEHOLD_MEAL_BREAKFAST, HOUSEHOLD_MEAL_LUNCH or HOUSEHOLD_MEAL_DINNER, or empty for every meal someone eats")
		}
		if !slices.Contains(meals, meal) {
			meals = append(meals, meal)
		}
	}
	slices.SortFunc(meals, compareMeals)
	criteria := req.GetCriteria()
	weights, err := planWeights(criteria)
	if err != nil {
		return nil, err
	}
	if v := criteria.GetMaxTotalSeconds(); v < 0 || v > maxPlanSeconds {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"criteria.max_total_seconds must be from 0 to %d, with 0 for any time", maxPlanSeconds)
	}
	if len(criteria.GetExcludeTags()) > maxPlanExclusions || len(criteria.GetExcludeRecipeIds()) > maxPlanExclusions {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"criteria can exclude at most %d tags and %d recipes", maxPlanExclusions, maxPlanExclusions)
	}
	var excludeTags []string
	for _, tag := range criteria.GetExcludeTags() {
		excludeTags = append(excludeTags, strings.ToLower(strings.TrimSpace(tag)))
	}
	var excludeIDs []pgtype.UUID
	for _, raw := range criteria.GetExcludeRecipeIds() {
		id, err := parseID("criteria.exclude_recipe_ids", raw, "find recipes with ListRecipes")
		if err != nil {
			return nil, err
		}
		excludeIDs = append(excludeIDs, id)
	}
	choices := map[slotKey]pgtype.UUID{}
	for i, c := range req.GetChoices() {
		field := fmt.Sprintf("choices[%d]", i)
		date, err := parseDate(field+".date", c.GetDate())
		if err != nil {
			return nil, err
		}
		if date.Before(start) || date.After(end) {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.date must be one of the days planned, from %s to %s",
				field, store.FormatDate(start), store.FormatDate(end))
		}
		meal, err := mealFromProto(c.GetMeal())
		if err != nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.%s", field, mealRequired)
		}
		key := slotKey{date, meal}
		if _, ok := choices[key]; ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s is the second choice for %s on %s: give at most one per slot",
				field, meal, store.FormatDate(date))
		}
		var id pgtype.UUID
		if c.GetRecipeId() != "" {
			id, err = parseID(field+".recipe_id", c.GetRecipeId(), "find recipes with ListRecipes, or leave it empty to leave the slot empty")
			if err != nil {
				return nil, err
			}
		}
		choices[key] = id
	}

	d, lists, lots, err := s.loadSuggestionData(ctx, user.HouseholdID(), start, end)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	for key, id := range choices {
		if _, ok := d.recipes[id]; id.Valid && !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"the recipe chosen for %s on %s isn't in your household; find recipes with ListRecipes", key.meal, store.FormatDate(key.date))
		}
	}

	// The meals are those asked for, each of which someone must eat, or
	// every meal someone eats.
	eaters := map[people.Meal][]*people.Person{}
	if len(meals) == 0 {
		for _, meal := range mealOrder {
			if list, err := mealEaters(d.people, meal, nil); err == nil {
				meals = append(meals, meal)
				eaters[meal] = list
			}
		}
		if len(meals) == 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				"nobody eats from a shared meal: set the meal of people's eating times with UpdatePerson")
		}
	}
	for _, meal := range meals {
		if _, ok := eaters[meal]; ok {
			continue
		}
		eaters[meal], err = mealEaters(d.people, meal, nil)
		if err != nil {
			return nil, err
		}
	}

	candidates := planCandidates(d, criteria.GetMaxTotalSeconds(), excludeTags, excludeIDs)
	in := &suggest.PlanInput{
		Plan:       d.plan,
		Planned:    d.allPlanned(),
		Recipes:    d.recipeList(),
		Candidates: candidates,
		Foods:      d.foods,
		Lots:       lots,
		ShopDate:   func(date time.Time) time.Time { return s.shopDate(lists, date) },
		Weights:    weights,
		Done:       ctx.Done(),
	}
	// Each slot is planned, kept, skipped or planned already.
	var keys []slotKey
	states := map[slotKey]suggestionsv1.PlanSlotState{}
	plannedNames := map[slotKey][]string{}
	for _, c := range d.plan.Cooks {
		key := slotKey{c.Date, c.Meal}
		plannedNames[key] = append(plannedNames[key], c.RecipeName)
	}
	toChoose := 0
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		for _, meal := range meals {
			key := slotKey{date, meal}
			keys = append(keys, key)
			id, chosen := choices[key]
			switch {
			case len(plannedNames[key]) > 0:
				states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_PLANNED
			case chosen && !id.Valid:
				states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SKIPPED
			case chosen:
				states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_KEPT
				in.Slots = append(in.Slots, &suggest.PlanSlot{Date: date, Meal: meal, People: eaters[meal], Fixed: d.recipes[id]})
			default:
				states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SUGGESTED
				in.Slots = append(in.Slots, &suggest.PlanSlot{Date: date, Meal: meal, People: eaters[meal]})
				toChoose++
			}
		}
	}
	if toChoose > 0 && len(candidates) == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition,
			"no recipe meets the criteria: allow a longer time, exclude fewer tags or recipes, or add recipes with RecipeService.CreateRecipe")
	}

	seed := req.GetSeed()
	for seed == 0 {
		seed = rand.Uint64()
	}
	in.Rand = rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	result := suggest.Plan(in)

	planned := map[slotKey]*suggest.PlannedSlot{}
	for _, ps := range result.Slots {
		planned[slotKey{ps.Slot.Date, ps.Slot.Meal}] = ps
	}
	slots := make([]*suggestionsv1.PlanSlot, 0, len(keys))
	for _, key := range keys {
		b := suggestionsv1.PlanSlot_builder{
			Date:  store.FormatDate(key.date),
			Meal:  person.MealsToProto[key.meal],
			State: states[key],
		}
		switch states[key] {
		case suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_PLANNED:
			b.PlannedRecipeNames = plannedNames[key]
		case suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SUGGESTED, suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_KEPT:
			ps := planned[key]
			if ps.Suggestion == nil {
				b.State = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_UNFILLED
				break
			}
			b.Suggestion = suggestionToProto(ps.Suggestion)
			b.FittingRecipeCount = int32(ps.Fitting)
			for _, alt := range ps.Alternatives {
				b.Alternatives = append(b.Alternatives, suggestionToProto(alt))
			}
		}
		slots = append(slots, b.Build())
	}
	return suggestionsv1.SuggestPlanResponse_builder{
		Slots:       slots,
		Score:       result.Score,
		PlansTried:  int32(result.Rollouts),
		Seed:        seed,
		RecipeCount: int32(len(candidates)),
	}.Build(), nil
}

// planWeights returns the weights of the scores' components for plan
// criteria. The rules, flavor and meal tags always count fully.
func planWeights(c *suggestionsv1.PlanCriteria) (map[suggest.Kind]float64, error) {
	weights := map[suggest.Kind]float64{}
	for _, e := range []struct {
		field    string
		emphasis suggestionsv1.Emphasis
		kinds    []suggest.Kind
	}{
		{"variety", c.GetVariety(), []suggest.Kind{suggest.Variety}},
		{"favorites", c.GetFavorites(), []suggest.Kind{suggest.Favorite}},
		{"pantry", c.GetPantry(), []suggest.Kind{suggest.Pantry, suggest.PackLeftovers}},
		{"shopping", c.GetShopping(), []suggest.Kind{suggest.SharedIngredients, suggest.Keeping}},
		{"nutrition", c.GetNutrition(), []suggest.Kind{suggest.Nutrition}},
	} {
		w, ok := emphasisWeights[e.emphasis]
		if !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"criteria.%s must be EMPHASIS_OFF, EMPHASIS_LOW, EMPHASIS_NORMAL or EMPHASIS_HIGH", e.field)
		}
		for _, k := range e.kinds {
			weights[k] = w
		}
	}
	return weights, nil
}

// planCandidates returns the recipes a plan picks from: those that take at
// most maxSeconds, if not 0, with none of the tags, and not excluded. They
// are ordered by name and ID, so the search's draws are reproducible.
func planCandidates(d *planData, maxSeconds int32, excludeTags []string, excludeIDs []pgtype.UUID) []*recipes.Recipe {
	candidates := []*recipes.Recipe{}
	for _, r := range d.recipeList() {
		total := r.PrepSeconds + r.CookSeconds
		switch {
		case maxSeconds > 0 && total > maxSeconds:
		case slices.Contains(excludeIDs, r.ID):
		case slices.ContainsFunc(r.Tags, func(tag string) bool { return slices.Contains(excludeTags, strings.ToLower(tag)) }):
		default:
			candidates = append(candidates, r)
		}
	}
	return candidates
}

// recipeList returns the recipes read, ordered by name and then ID, so
// that scores and draws over them are reproducible.
func (d *planData) recipeList() []*recipes.Recipe {
	list := make([]*recipes.Recipe, 0, len(d.recipes))
	for _, r := range d.recipes {
		list = append(list, r)
	}
	slices.SortFunc(list, func(a, b *recipes.Recipe) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(store.UUIDToString(a.ID), store.UUIDToString(b.ID)))
	})
	return list
}

// allPlanned returns what's planned at every slot of the plan read, as the
// rules check it.
func (d *planData) allPlanned() map[plans.Slot]*rules.EatingTime {
	names := map[pgtype.UUID]string{}
	for _, p := range d.people {
		for _, et := range p.EatingTimes {
			names[et.ID] = et.Name
		}
	}
	planned := map[plans.Slot]*rules.EatingTime{}
	for slot := range d.items {
		if et := d.ruleEatingTime(slot, names[slot.EatingTimeID]); et != nil {
			planned[slot] = et
		}
	}
	return planned
}

func (s *Service) PlanMeals(ctx context.Context, req *plansv1.PlanMealsRequest) (*plansv1.PlanMealsResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	if n := len(req.GetMeals()); n == 0 || n > maxMealsToPlan {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "meals must have from 1 to %d meals", maxMealsToPlan)
	}
	type mealToPlan struct {
		recipeID pgtype.UUID
		slotKey
	}
	var list []mealToPlan
	seen := map[slotKey]bool{}
	var recipeIDs []pgtype.UUID
	for i, m := range req.GetMeals() {
		field := fmt.Sprintf("meals[%d]", i)
		recipeID, err := parseID(field+".recipe_id", m.GetRecipeId(), "find recipes with ListRecipes")
		if err != nil {
			return nil, err
		}
		date, err := parseDate(field+".date", m.GetDate())
		if err != nil {
			return nil, err
		}
		meal, err := mealFromProto(m.GetMeal())
		if err != nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.%s", field, mealRequired)
		}
		key := slotKey{date, meal}
		if seen[key] {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s is a second %s on %s: plan at most one per date and meal",
				field, meal, store.FormatDate(date))
		}
		seen[key] = true
		list = append(list, mealToPlan{recipeID, key})
		recipeIDs = append(recipeIDs, recipeID)
	}
	// The meals are split in order, each given the portions of those
	// before it, as if planned one by one with PlanMeal.
	slices.SortFunc(list, func(a, b mealToPlan) int {
		return cmp.Or(a.date.Compare(b.date), compareMeals(a.meal, b.meal))
	})
	from, to := list[0].date, list[len(list)-1].date.AddDate(0, 0, 1)
	d, err := s.loadPlan(ctx, user.HouseholdID(), from, to, unique(recipeIDs)...)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	planned := d.plannedGrams()
	eaters := map[people.Meal][]*people.Person{}
	toCreate := make([]*plans.CookWithPortions, 0, len(list))
	for _, m := range list {
		if _, ok := d.recipes[m.recipeID]; !ok {
			return nil, connect.Errorf(connect.CodeNotFound, "the recipe for %s on %s doesn't exist in your household; find recipes with ListRecipes",
				m.meal, store.FormatDate(m.date))
		}
		if _, ok := eaters[m.meal]; !ok {
			eaters[m.meal], err = mealEaters(d.people, m.meal, nil)
			if err != nil {
				return nil, err
			}
		}
		gramsPerServing := d.recipeNutrition[m.recipeID].GramsPerServing
		split, err := plans.SplitMeal(&plans.SplitInput{
			Date:            m.date,
			Meal:            m.meal,
			People:          eaters[m.meal],
			GramsPerServing: gramsPerServing,
			PlannedGrams:    planned,
		})
		if err != nil {
			return nil, s.toConnectError(ctx, err, "failed to split meal", "")
		}
		portions := make([]*plans.Portion, 0, len(split.Shares))
		for _, share := range split.Shares {
			portions = append(portions, &plans.Portion{
				PersonID:     share.PersonID,
				EatingTimeID: share.EatingTimeID,
				Date:         share.Date,
				Servings:     share.Servings,
			})
			planned[share.Slot] += share.Servings * gramsPerServing
		}
		toCreate = append(toCreate, &plans.CookWithPortions{
			Cook: &plans.Cook{
				HouseholdID: user.HouseholdID(),
				RecipeID:    m.recipeID,
				Date:        m.date,
				Servings:    split.Servings,
				Meal:        m.meal,
			},
			Portions: portions,
		})
	}
	cooks, created, err := s.plans.CreateCooksWithPortions(ctx, toCreate)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to plan meals", recipeNotFound)
	}
	result := make([]*plansv1.Cook, 0, len(cooks))
	count := 0
	for i, c := range cooks {
		result = append(result, cookToProto(c))
		count += len(created[i])
	}
	return plansv1.PlanMealsResponse_builder{Cooks: result, PortionCount: int32(count)}.Build(), nil
}
