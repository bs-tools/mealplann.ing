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
	// of the three shared meals and a few eating times of one person each
	// day, like snacks.
	maxMealsToPlan = 150
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

// slotKey identifies a slot of a plan: a shared meal on a date, or one
// person's eating time on a date, with meal MealNone.
type slotKey struct {
	date         time.Time
	meal         people.Meal
	personID     pgtype.UUID
	eatingTimeID pgtype.UUID
}

// personal reports whether the slot is one person's eating time.
func (k slotKey) personal() bool {
	return k.eatingTimeID.Valid
}

// personalSlot returns the key of one person's eating time on a date.
func personalSlot(date time.Time, personID, eatingTimeID pgtype.UUID) slotKey {
	return slotKey{date: date, personID: personID, eatingTimeID: eatingTimeID}
}

// personalEatingTime returns a person of the household and their eating
// time that eats from no shared meal, or INVALID_ARGUMENT naming the field
// if there's none.
func personalEatingTime(all []*people.Person, field, rawPersonID, rawEatingTimeID string) (*people.Person, *people.EatingTime, error) {
	personID, err := parseID(field+".person_id", rawPersonID, "find people with ListPeople")
	if err != nil {
		return nil, nil, err
	}
	eatingTimeID, err := parseID(field+".eating_time_id", rawEatingTimeID, "find people's eating times with ListPeople")
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(all, func(p *people.Person) bool { return p.ID == personID })
	if i < 0 {
		return nil, nil, connect.Errorf(connect.CodeInvalidArgument, "%s.person_id isn't in your household; find people with ListPeople", field)
	}
	et := all[i].EatingTime(eatingTimeID)
	switch {
	case et == nil:
		return nil, nil, connect.Errorf(connect.CodeInvalidArgument,
			"%s.eating_time_id isn't one of %s's eating times; find them with ListPeople", field, all[i].Name)
	case et.Meal != people.MealNone:
		return nil, nil, connect.Errorf(connect.CodeInvalidArgument,
			"%s's %s eats from the shared %s: give meal instead of the eating time", all[i].Name, et.Name, et.Meal)
	}
	return all[i], et, nil
}

// inDayOrder returns slot keys by date, and each day's in the order its
// people eat them, as the plan shows days, rather than in the order they're
// planned in, shared meals first: a person's snack between breakfast and
// lunch is between them. A slot's place is where its eating time is in
// its person's day, as a fraction of their eating times, and a shared
// meal's the earliest of its eaters'. Ties keep the planning order, shared
// meals first.
func inDayOrder(keys []slotKey, all []*people.Person, eaters map[people.Meal][]*people.Person) []slotKey {
	// place is where an eating time is in its person's day, from 0 to 1.
	place := func(p *people.Person, match func(*people.EatingTime) bool) (float64, bool) {
		for i, et := range p.EatingTimes {
			if match(et) {
				return float64(i) / float64(len(p.EatingTimes)), true
			}
		}
		return 0, false
	}
	places := map[slotKey]float64{}
	for _, key := range keys {
		best, found := 1.0, false
		if key.personal() {
			if i := slices.IndexFunc(all, func(p *people.Person) bool { return p.ID == key.personID }); i >= 0 {
				best, found = place(all[i], func(et *people.EatingTime) bool { return et.ID == key.eatingTimeID })
			}
		} else {
			for _, p := range eaters[key.meal] {
				if at, ok := place(p, func(et *people.EatingTime) bool { return et.Meal == key.meal }); ok && (!found || at < best) {
					best, found = at, true
				}
			}
		}
		if !found {
			// A meal nobody eats at an eating time goes by its own order.
			best = float64(slices.Index(mealOrder, key.meal)) / float64(len(mealOrder))
		}
		places[key] = best
	}
	sorted := slices.Clone(keys)
	slices.SortStableFunc(sorted, func(a, b slotKey) int {
		return cmp.Or(a.date.Compare(b.date), cmp.Compare(places[a], places[b]))
	})
	return sorted
}

// personalSlots returns the eating times of one person that eat from no
// shared meal, by person and in the order of their day.
func personalSlots(all []*people.Person) []struct {
	person     *people.Person
	eatingTime *people.EatingTime
} {
	var slots []struct {
		person     *people.Person
		eatingTime *people.EatingTime
	}
	for _, p := range all {
		for _, et := range p.EatingTimes {
			if et.Meal == people.MealNone {
				slots = append(slots, struct {
					person     *people.Person
					eatingTime *people.EatingTime
				}{p, et})
			}
		}
	}
	return slots
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
	leftoverChoices := map[slotKey]*suggest.LeftoverRef{}
	var personalChoices []*suggestionsv1.PlanChoice
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
		var key slotKey
		if c.GetEatingTimeId() != "" || c.GetPersonId() != "" {
			// The person and eating time are checked once the people are
			// read.
			personID, err := parseID(field+".person_id", c.GetPersonId(), "find people with ListPeople")
			if err != nil {
				return nil, err
			}
			eatingTimeID, err := parseID(field+".eating_time_id", c.GetEatingTimeId(), "find people's eating times with ListPeople")
			if err != nil {
				return nil, err
			}
			key = personalSlot(date, personID, eatingTimeID)
			personalChoices = append(personalChoices, c)
		} else {
			meal, err := mealFromProto(c.GetMeal())
			if err != nil {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.%s, or person_id and eating_time_id", field, mealRequired)
			}
			key = slotKey{date: date, meal: meal}
		}
		if _, ok := choices[key]; ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s is the second choice for its slot on %s: give at most one per slot",
				field, store.FormatDate(date))
		}
		var id pgtype.UUID
		if c.GetRecipeId() != "" {
			id, err = parseID(field+".recipe_id", c.GetRecipeId(), "find recipes with ListRecipes, or leave it empty to leave the slot empty")
			if err != nil {
				return nil, err
			}
		}
		choices[key] = id
		if c.HasLeftoverOf() {
			if !key.personal() || !id.Valid {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.leftover_of is only for one person's eating time, with recipe_id: set them, or leave it out", field)
			}
			leftoverChoices[key], err = leftoverFromProto(field+".leftover_of", c.GetLeftoverOf(), id, date)
			if err != nil {
				return nil, err
			}
		}
	}

	d, lists, lots, err := s.loadSuggestionData(ctx, user.HouseholdID(), start, end)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	for key, id := range choices {
		if _, ok := d.recipes[id]; id.Valid && !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"the recipe chosen for a slot on %s isn't in your household; find recipes with ListRecipes", store.FormatDate(key.date))
		}
	}
	for _, c := range personalChoices {
		if _, _, err := personalEatingTime(d.people, "choices", c.GetPersonId(), c.GetEatingTimeId()); err != nil {
			return nil, err
		}
	}
	var personal []struct {
		person     *people.Person
		eatingTime *people.EatingTime
	}
	if !req.GetSkipPersonalEatingTimes() {
		personal = personalSlots(d.people)
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
		if len(meals) == 0 && len(personal) == 0 {
			return nil, connect.NewError(connect.CodeFailedPrecondition,
				"nobody eats from a shared meal, nor has eating times of their own: set people's eating times with UpdatePerson")
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
		if c.Meal == people.MealNone {
			continue
		}
		key := slotKey{date: c.Date, meal: c.Meal}
		plannedNames[key] = append(plannedNames[key], c.RecipeName)
	}
	// One person's eating time is planned if it has portions or staples.
	for slot, items := range d.items {
		key := personalSlot(slot.Date, slot.PersonID, slot.EatingTimeID)
		for _, it := range items {
			plannedNames[key] = append(plannedNames[key], it.name())
		}
	}
	toChoose := 0
	slotOf := map[slotKey]*suggest.PlanSlot{}
	addSlot := func(key slotKey, slot *suggest.PlanSlot) {
		keys = append(keys, key)
		id, chosen := choices[key]
		switch {
		case len(plannedNames[key]) > 0:
			states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_PLANNED
		case chosen && !id.Valid:
			states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SKIPPED
		case chosen:
			states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_KEPT
			if ref, ok := leftoverChoices[key]; ok {
				slot.FixedLeftover = ref
			} else {
				slot.Fixed = d.recipes[id]
			}
			in.Slots = append(in.Slots, slot)
		default:
			states[key] = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SUGGESTED
			in.Slots = append(in.Slots, slot)
			toChoose++
		}
		slotOf[key] = slot
	}
	// Each day's shared meals come first, then each person's own eating
	// times, so those are planned given the meals' portions, which spill
	// into them when they're more than the person's maximum portion.
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		for _, meal := range meals {
			// People with a staple at the meal eat it instead; with
			// everyone's, the meal is planned already.
			key := slotKey{date: date, meal: meal}
			eating, staples := withoutStapled(eaters[meal], d.plan.Staples, meal, date)
			if len(eating) == 0 {
				plannedNames[key] = append(plannedNames[key], staples...)
			}
			addSlot(key, &suggest.PlanSlot{Date: date, Meal: meal, People: eating})
		}
		for _, ps := range personal {
			addSlot(personalSlot(date, ps.person.ID, ps.eatingTime.ID),
				&suggest.PlanSlot{Date: date, People: []*people.Person{ps.person}, EatingTimeID: ps.eatingTime.ID})
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

	planned := map[*suggest.PlanSlot]*suggest.PlannedSlot{}
	for _, ps := range result.Slots {
		planned[ps.Slot] = ps
	}
	slots := make([]*suggestionsv1.PlanSlot, 0, len(keys))
	for _, key := range inDayOrder(keys, d.people, eaters) {
		b := suggestionsv1.PlanSlot_builder{
			Date:  store.FormatDate(key.date),
			Meal:  person.MealsToProto[key.meal],
			State: states[key],
		}
		if key.personal() {
			p := slotOf[key].People[0]
			b.PersonId = store.UUIDToString(p.ID)
			b.PersonName = p.Name
			b.EatingTimeId = store.UUIDToString(key.eatingTimeID)
			b.EatingTimeName = p.EatingTime(key.eatingTimeID).Name
		}
		switch states[key] {
		case suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_PLANNED:
			b.PlannedRecipeNames = plannedNames[key]
		case suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_SUGGESTED, suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_KEPT:
			ps := planned[slotOf[key]]
			if ps.FilledByMeal {
				b.State = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_FILLED_BY_MEAL
				break
			}
			if ps.Suggestion == nil {
				b.State = suggestionsv1.PlanSlotState_PLAN_SLOT_STATE_UNFILLED
				b.UnfilledReason = unfilledReason(ps)
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

// leftoverFromProto converts and validates the earlier meal of a portion
// of recipeID on date.
func leftoverFromProto(field string, p *plansv1.LeftoverOf, recipeID pgtype.UUID, date time.Time) (*suggest.LeftoverRef, error) {
	from, err := parseDate(field+".date", p.GetDate())
	if err != nil {
		return nil, err
	}
	if from.After(date) {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.date must be on or before the portion's date, %s",
			field, store.FormatDate(date))
	}
	meal, err := mealFromProto(p.GetMeal())
	if err != nil {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.%s", field, mealRequired)
	}
	return &suggest.LeftoverRef{Date: from, Meal: meal, RecipeID: recipeID}, nil
}

// leftoverCook returns the cook a leftover of the i-th meal to plan is
// eaten from: one planned by the request before it, which is grown, or
// else one planned already, as a new entry to grow it, which isn't in
// planned yet.
func leftoverCook(planned []*plans.CookWithPortions, existing []*plans.Cook, ref *suggest.LeftoverRef, i int) (*plans.CookWithPortions, error) {
	for _, cp := range planned {
		if ref.Matches(cp.Cook) {
			return cp, nil
		}
	}
	for _, c := range existing {
		if ref.Matches(c) {
			return &plans.CookWithPortions{
				Cook:     &plans.Cook{ID: c.ID, HouseholdID: c.HouseholdID, RecipeID: c.RecipeID, Date: c.Date, Meal: c.Meal},
				Existing: true,
			}, nil
		}
	}
	return nil, connect.Errorf(connect.CodeFailedPrecondition,
		"meals[%d].leftover_of: there's no cook of its recipe at the %s on %s to eat the rest of: plan that meal first, or leave leftover_of out to cook the recipe",
		i, ref.Meal, store.FormatDate(ref.Date))
}

// unfilledReason says why a slot got no recipe.
func unfilledReason(ps *suggest.PlannedSlot) string {
	switch {
	case ps.Slot.FixedLeftover != nil:
		return "The meal whose rest was kept here isn't in the plan anymore: choose another."
	case ps.Eligible > 0:
		return "No recipe could be planned here."
	}
	return `No recipe meeting the criteria is tagged "breakfast": tag the recipes that make a good breakfast.`
}

// boolInt returns 1 for true and 0 for false, to sort by.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// planWeights returns the weights of the scores' components for plan
// criteria. The rules, meal tags and leftovers always count fully.
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
		// leftover is the earlier meal it's the rest of, nil for a new
		// cook.
		leftover *suggest.LeftoverRef
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
		var key slotKey
		if m.GetPersonId() != "" || m.GetEatingTimeId() != "" {
			// The person and eating time are checked once the people are
			// read.
			personID, err := parseID(field+".person_id", m.GetPersonId(), "find people with ListPeople")
			if err != nil {
				return nil, err
			}
			eatingTimeID, err := parseID(field+".eating_time_id", m.GetEatingTimeId(), "find people's eating times with ListPeople")
			if err != nil {
				return nil, err
			}
			key = personalSlot(date, personID, eatingTimeID)
		} else {
			meal, err := mealFromProto(m.GetMeal())
			if err != nil {
				return nil, connect.Errorf(connect.CodeInvalidArgument, "%s.%s, or person_id and eating_time_id", field, mealRequired)
			}
			key = slotKey{date: date, meal: meal}
		}
		if seen[key] {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s is a second meal at its slot on %s: plan at most one per date and meal or eating time",
				field, store.FormatDate(date))
		}
		seen[key] = true
		var leftover *suggest.LeftoverRef
		if m.HasLeftoverOf() {
			if !key.personal() {
				return nil, connect.Errorf(connect.CodeInvalidArgument,
					"%s.leftover_of is only for one person's eating time: set person_id and eating_time_id, or leave it out", field)
			}
			leftover, err = leftoverFromProto(field+".leftover_of", m.GetLeftoverOf(), recipeID, date)
			if err != nil {
				return nil, err
			}
		}
		list = append(list, mealToPlan{recipeID, key, leftover})
		recipeIDs = append(recipeIDs, recipeID)
	}
	// The meals are split in order, each given the portions of those
	// before it, as if planned one by one with PlanMeal, and each day's
	// eating times of one person after its shared meals, as SuggestPlan
	// plans them.
	slices.SortStableFunc(list, func(a, b mealToPlan) int {
		return cmp.Or(a.date.Compare(b.date), cmp.Compare(boolInt(a.personal()), boolInt(b.personal())), compareMeals(a.meal, b.meal))
	})
	from, to := list[0].date, list[len(list)-1].date.AddDate(0, 0, 1)
	// The plan read has the cooks of the earlier meals eaten as leftovers.
	for _, m := range list {
		if m.leftover != nil && m.leftover.Date.Before(from) {
			from = m.leftover.Date
		}
	}
	d, err := s.loadPlan(ctx, user.HouseholdID(), from, to, unique(recipeIDs)...)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	planned := d.plannedGrams()
	eaters := map[people.Meal][]*people.Person{}
	toCreate := make([]*plans.CookWithPortions, 0, len(list))
	for i, m := range list {
		if _, ok := d.recipes[m.recipeID]; !ok {
			return nil, connect.Errorf(connect.CodeNotFound, "the recipe for a meal on %s doesn't exist in your household; find recipes with ListRecipes",
				store.FormatDate(m.date))
		}
		gramsPerServing := d.recipeNutrition[m.recipeID].GramsPerServing
		if m.personal() {
			p, _, err := personalEatingTime(d.people, fmt.Sprintf("meals[%d]", i), store.UUIDToString(m.personID), store.UUIDToString(m.eatingTimeID))
			if err != nil {
				return nil, err
			}
			slot := plans.Slot{PersonID: p.ID, EatingTimeID: m.eatingTimeID, Date: m.date}
			servings, cookServings := plans.EatingTimeServings(p, gramsPerServing, planned[slot])
			planned[slot] += servings * gramsPerServing
			portion := &plans.Portion{PersonID: p.ID, EatingTimeID: m.eatingTimeID, Date: m.date, Servings: servings}
			if m.leftover != nil {
				cp, err := leftoverCook(toCreate, d.plan.Cooks, m.leftover, i)
				if err != nil {
					return nil, err
				}
				if cp.Existing {
					toCreate = append(toCreate, cp)
				}
				// The cook grows by the portion.
				cp.Cook.Servings += servings
				cp.Portions = append(cp.Portions, portion)
				continue
			}
			toCreate = append(toCreate, &plans.CookWithPortions{
				Cook: &plans.Cook{
					HouseholdID: user.HouseholdID(),
					RecipeID:    m.recipeID,
					Date:        m.date,
					Servings:    cookServings,
				},
				Portions: []*plans.Portion{portion},
			})
			continue
		}
		if _, ok := eaters[m.meal]; !ok {
			eaters[m.meal], err = mealEaters(d.people, m.meal, nil)
			if err != nil {
				return nil, err
			}
		}
		eating, _ := withoutStapled(eaters[m.meal], d.plan.Staples, m.meal, m.date)
		if len(eating) == 0 {
			return nil, allStapled(m.meal, m.date)
		}
		split, err := plans.SplitMeal(&plans.SplitInput{
			Date:            m.date,
			Meal:            m.meal,
			People:          eating,
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
	// The cooks of the recipes' components are planned with them.
	cooks, created, err := s.plans.CreateCooksWithPortions(ctx, withSuppliers(toCreate, d.recipes))
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
