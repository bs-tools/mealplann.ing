package plan

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/services/food"
	"github.com/bs-tools/mealplann.ing/internal/services/person"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// nutritionDigits are the decimals computed amounts are rounded to, like
// the recipes'.
const nutritionDigits = 3

// planData is a household's plan of a range of dates, with the people,
// recipes and foods computing it needs.
type planData struct {
	plan   *plans.Plan
	people []*people.Person
	// recipes and foods are those the plan uses, by ID, and recipeNutrition
	// the nutrition of each recipe.
	recipes         map[pgtype.UUID]*recipes.Recipe
	recipeNutrition map[pgtype.UUID]*nutrition.Recipe
	foods           map[pgtype.UUID]*foods.Food
	// items are the portions and staples planned, by slot, staples first.
	items map[plans.Slot][]*item
	// history is how many cooks of each recipe were done in the
	// suggest.HistoryDays before the dates suggested for, read only for
	// suggestions.
	history map[pgtype.UUID]int
}

// item is a portion or a staple on a date, planned at a slot.
type item struct {
	portion *plans.Portion
	staple  *plans.Staple
	date    time.Time
}

// loadPlan reads a household's plan from one date to another, with the
// people and what nutrition needs of the recipes and foods it uses, and of
// the extra recipes. It reads the plan and the people together, and then
// the recipes and foods together: two round trips.
func (s *Service) loadPlan(ctx context.Context, householdID pgtype.UUID, from, to time.Time, extraRecipes ...pgtype.UUID) (*planData, error) {
	d := &planData{}
	var planErr, peopleErr error
	var wg sync.WaitGroup
	wg.Go(func() { d.plan, planErr = s.plans.GetPlan(ctx, householdID, from, to) })
	wg.Go(func() { d.people, peopleErr = s.people.ListPeople(ctx, householdID) })
	wg.Wait()
	if planErr != nil {
		return nil, planErr
	}
	if peopleErr != nil {
		return nil, peopleErr
	}

	recipeIDs := slices.Clone(extraRecipes)
	var foodIDs []pgtype.UUID
	for _, c := range d.plan.Cooks {
		recipeIDs = append(recipeIDs, c.RecipeID)
	}
	for _, p := range d.plan.Portions {
		if p.CookID.Valid {
			recipeIDs = append(recipeIDs, p.CookRecipeID)
		} else {
			foodIDs = append(foodIDs, p.FoodID)
		}
	}
	for _, st := range d.plan.Staples {
		if st.RecipeID.Valid {
			recipeIDs = append(recipeIDs, st.RecipeID)
		} else {
			foodIDs = append(foodIDs, st.FoodID)
		}
	}
	var recipeList []*recipes.Recipe
	var foodList []*foods.Food
	var recipesErr, foodsErr error
	wg.Go(func() { recipeList, recipesErr = s.recipes.ListRecipesByID(ctx, householdID, unique(recipeIDs)) })
	wg.Go(func() { foodList, foodsErr = s.foods.ListFoodsByID(ctx, householdID, unique(foodIDs)) })
	wg.Wait()
	if recipesErr != nil {
		return nil, recipesErr
	}
	if foodsErr != nil {
		return nil, foodsErr
	}
	d.index(recipeList, foodList)
	return d, nil
}

// index indexes the recipes and foods by ID, and the plan's portions and
// staples by slot.
func (d *planData) index(recipeList []*recipes.Recipe, foodList []*foods.Food) {
	d.recipes = map[pgtype.UUID]*recipes.Recipe{}
	d.recipeNutrition = map[pgtype.UUID]*nutrition.Recipe{}
	// The recipes' components are indexed too, as cooks of them are planned
	// with the cooks using them.
	var add func(r *recipes.Recipe, depth int)
	add = func(r *recipes.Recipe, depth int) {
		if _, ok := d.recipes[r.ID]; ok || depth > recipes.MaxComponentDepth {
			return
		}
		d.recipes[r.ID] = r
		d.recipeNutrition[r.ID] = r.Nutrition()
		for _, in := range r.Ingredients {
			if in.Component != nil {
				add(in.Component, depth+1)
			}
		}
	}
	for _, r := range recipeList {
		add(r, 0)
	}
	d.foods = map[pgtype.UUID]*foods.Food{}
	for _, f := range foodList {
		d.foods[f.ID] = f
	}
	d.items = map[plans.Slot][]*item{}
	for _, o := range d.plan.StapleOccurrences() {
		d.addItem(&item{staple: o.Staple, date: o.Date})
	}
	for _, p := range d.plan.Portions {
		d.addItem(&item{portion: p, date: p.Date})
	}
}

// addItem adds a portion or staple to the slot it's planned at.
func (d *planData) addItem(it *item) {
	slot := plans.Slot{PersonID: it.personID(), EatingTimeID: it.eatingTimeID(), Date: it.date}
	d.items[slot] = append(d.items[slot], it)
}

func (it *item) personID() pgtype.UUID {
	if it.portion != nil {
		return it.portion.PersonID
	}
	return it.staple.PersonID
}

func (it *item) eatingTimeID() pgtype.UUID {
	if it.portion != nil {
		return it.portion.EatingTimeID
	}
	return it.staple.EatingTimeID
}

// recipeID returns the recipe the item is servings of, invalid for a food.
func (it *item) recipeID() pgtype.UUID {
	if it.portion != nil {
		return it.portion.CookRecipeID
	}
	return it.staple.RecipeID
}

// eaten is the nutrition of an item, with what the rules need of it.
type eaten struct {
	amount    nutrition.Amount
	fastCarbs []string
}

// eaten computes the nutrition of an item. A recipe or food missing from
// the data, like one created since it was read, counts as unknown.
func (d *planData) eaten(it *item) eaten {
	var e eaten
	if recipeID := it.recipeID(); recipeID.Valid {
		servings := 0.0
		if it.portion != nil {
			servings = it.portion.Servings
		} else {
			servings = it.staple.Servings
		}
		n, ok := d.recipeNutrition[recipeID]
		if !ok {
			return unknownEaten()
		}
		e.amount = n.Servings(servings)
		r := d.recipes[recipeID]
		for _, in := range r.IngredientsFor(float64(r.Servings)) {
			if in.FastCarb && !slices.Contains(e.fastCarbs, in.FoodName) {
				e.fastCarbs = append(e.fastCarbs, in.FoodName)
			}
		}
		return e
	}
	var foodID pgtype.UUID
	var amount float64
	var unit nutrition.Unit
	if it.portion != nil {
		foodID, amount, unit = it.portion.FoodID, it.portion.Amount, it.portion.Unit
	} else {
		foodID, amount, unit = it.staple.FoodID, it.staple.Amount, it.staple.Unit
	}
	f, ok := d.foods[foodID]
	if !ok {
		return unknownEaten()
	}
	e.amount = f.Nutrition().AmountOf(amount, unit)
	if f.FastCarb {
		e.fastCarbs = []string{f.Name}
	}
	return e
}

func unknownEaten() eaten {
	e := eaten{amount: nutrition.Amount{UnknownWeight: true}}
	for n := range nutrition.Count {
		e.amount.Incomplete[n] = true
	}
	return e
}

// name returns the name of an item's recipe or food.
func (it *item) name() string {
	if it.portion != nil {
		return it.portion.Name
	}
	return it.staple.Name
}

// cookOf returns what checking a portion's leftovers needs of its cook, or
// nil if it isn't a share of a cook.
func (d *planData) cookOf(p *plans.Portion) *rules.Cook {
	if !p.CookID.Valid {
		return nil
	}
	c := &rules.Cook{RecipeName: p.Name, Date: p.CookDate, FrozenServings: p.CookFrozenServings}
	if r, ok := d.recipes[p.CookRecipeID]; ok {
		c.FridgeDays = r.FridgeDays
		c.Freezable = r.Freezable
		c.FreezerDays = r.FreezerDays
	}
	return c
}

// slotPlan is what's planned at a person's eating time on a date, checked.
type slotPlan struct {
	proto  *plansv1.PlannedEatingTime
	amount nutrition.Amount
	empty  bool
	// eatingOut is whether the person eats out there.
	eatingOut bool
}

// slot computes and checks what's planned at a person's eating time on a
// date.
func (d *planData) slot(p *people.Person, et *people.EatingTime, date time.Time) *slotPlan {
	slot := plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date}
	sp := &slotPlan{empty: len(d.items[slot]) == 0}
	ruleSlot := &rules.EatingTime{Name: et.Name}
	var portions []*plansv1.PlannedPortion
	var warnings []*plansv1.Warning
	wc := warningContext{personID: p.ID, date: date, eatingTimeID: et.ID}
	for _, it := range d.items[slot] {
		e := d.eaten(it)
		addEaten(ruleSlot, it, &e)
		portions = append(portions, d.plannedPortion(it, &e))
		if it.portion != nil {
			if c := d.cookOf(it.portion); c != nil {
				if w := rules.CheckPortion(c, date); w != nil {
					cc := wc
					cc.cookID = it.portion.CookID
					warnings = append(warnings, warningToProto(w, cc))
				}
			}
		}
	}
	for _, w := range rules.CheckEatingTime(&p.Rules, ruleSlot) {
		warnings = append(warnings, warningToProto(&w, wc))
	}
	// Eating times show their energy and macronutrients only: the rest are
	// in the day's totals, which keeps a week small enough for agents.
	macros := ruleSlot.Amount.Nutrients
	for n := nutrition.VitaminB12Micrograms; n < nutrition.Count; n++ {
		macros[n] = nil
	}
	sp.amount = ruleSlot.Amount
	b := plansv1.PlannedEatingTime_builder{
		EatingTime: person.EatingTimeToProto(et),
		Portions:   portions,
		Grams:      round(ruleSlot.Amount.Grams),
		Nutrients:  food.NutrientsToProto(&macros, nutritionDigits),
		Warnings:   warnings,
	}
	if out := d.plan.EatingOutAt(slot); out != nil {
		sp.eatingOut = true
		b.EatingOut = true
		b.EatingOutNotes = out.Notes
	}
	sp.proto = b.Build()
	return sp
}

// addEaten adds an item eaten at an eating time to what its rules check.
func addEaten(et *rules.EatingTime, it *item, e *eaten) {
	et.Amount.Add(&e.amount)
	for _, name := range e.fastCarbs {
		if !slices.Contains(et.FastCarbFoods, name) {
			et.FastCarbFoods = append(et.FastCarbFoods, name)
		}
	}
	if e.amount.UnknownWeight && !slices.Contains(et.UnweighedNames, it.name()) {
		et.UnweighedNames = append(et.UnweighedNames, it.name())
	}
}

// ruleEatingTime returns what's planned at a slot, as its rules check it,
// or nil if nothing is.
func (d *planData) ruleEatingTime(slot plans.Slot, name string) *rules.EatingTime {
	items := d.items[slot]
	if len(items) == 0 {
		return nil
	}
	et := &rules.EatingTime{Name: name}
	for _, it := range items {
		e := d.eaten(it)
		addEaten(et, it, &e)
	}
	return et
}

// plannedPortion converts an item to the API.
func (d *planData) plannedPortion(it *item, e *eaten) *plansv1.PlannedPortion {
	b := plansv1.PlannedPortion_builder{
		Name:          it.name(),
		Grams:         round(e.amount.Grams),
		UnknownWeight: e.amount.UnknownWeight,
	}
	if p := it.portion; p != nil {
		b.PortionId = store.UUIDToString(p.ID)
		b.Notes = p.Notes
		b.Eaten = p.Eaten
		if p.CookID.Valid {
			b.CookId = store.UUIDToString(p.CookID)
			b.RecipeId = store.UUIDToString(p.CookRecipeID)
			b.Servings = p.Servings
			b.CookDate = store.FormatDate(p.CookDate)
			b.Leftover = p.Date.After(p.CookDate)
		} else {
			b.FoodId = store.UUIDToString(p.FoodID)
			b.Amount = p.Amount
			b.Unit = unitsToProto[p.Unit]
		}
	} else {
		st := it.staple
		b.StapleId = store.UUIDToString(st.ID)
		b.Eaten = st.EatenOn(it.date)
		b.Notes = st.Notes
		if st.RecipeID.Valid {
			b.RecipeId = store.UUIDToString(st.RecipeID)
			b.Servings = st.Servings
		} else {
			b.FoodId = store.UUIDToString(st.FoodID)
			b.Amount = st.Amount
			b.Unit = unitsToProto[st.Unit]
		}
	}
	return b.Build()
}

// personDay computes and checks a person's day.
func (d *planData) personDay(p *people.Person, date time.Time) *plansv1.PersonDay {
	day := &rules.Day{}
	eatingTimes := make([]*plansv1.PlannedEatingTime, 0, len(p.EatingTimes))
	for _, et := range p.EatingTimes {
		sp := d.slot(p, et, date)
		eatingTimes = append(eatingTimes, sp.proto)
		day.Amount.Add(&sp.amount)
		switch {
		case sp.eatingOut:
			day.EatingOut++
		case sp.empty:
			day.Unplanned++
		}
	}
	var warnings []*plansv1.Warning
	for _, w := range rules.CheckDay(p.Targets, day) {
		warnings = append(warnings, warningToProto(&w, warningContext{personID: p.ID, date: date}))
	}
	var incomplete []string
	if day.Amount.Grams > 0 || day.Amount.UnknownWeight {
		for n, ok := range day.Amount.Incomplete {
			if ok {
				incomplete = append(incomplete, nutrition.Nutrient(n).String())
			}
		}
	}
	return plansv1.PersonDay_builder{
		PersonId:            store.UUIDToString(p.ID),
		PersonName:          p.Name,
		EatingTimes:         eatingTimes,
		Grams:               round(day.Amount.Grams),
		Nutrients:           food.NutrientsToProto(&day.Amount.Nutrients, nutritionDigits),
		IncompleteNutrients: incomplete,
		Warnings:            warnings,
	}.Build()
}

// days computes and checks every person's days from one date to another.
func (d *planData) days(from, to time.Time) []*plansv1.PlanDay {
	var days []*plansv1.PlanDay
	for date := from; !date.After(to); date = date.AddDate(0, 0, 1) {
		peopleDays := make([]*plansv1.PersonDay, 0, len(d.people))
		for _, p := range d.people {
			peopleDays = append(peopleDays, d.personDay(p, date))
		}
		days = append(days, plansv1.PlanDay_builder{
			Date:      store.FormatDate(date),
			People:    peopleDays,
			PrepTasks: d.prepTasks(date),
		}.Build())
	}
	return days
}

// prepTasks returns what to start on a date for the cooks of the plan
// read, by their dates: those whose recipes start prep_ahead_days before
// they're cooked, and aren't cooked yet, then the components to take out
// of the freezer the day before the cooks they supply.
func (d *planData) prepTasks(date time.Time) []*plansv1.PrepTask {
	var tasks []*plansv1.PrepTask
	for _, c := range d.plan.Cooks {
		r, ok := d.recipes[c.RecipeID]
		if !ok || r.PrepAheadDays <= 0 || c.Done || !c.Date.AddDate(0, 0, -int(r.PrepAheadDays)).Equal(date) {
			continue
		}
		note := r.PrepAheadNote
		if note == "" {
			note = "Start " + r.Name
		}
		var forNames []string
		for _, supply := range c.Supplying {
			if !slices.Contains(forNames, supply.ConsumerRecipeName) {
				forNames = append(forNames, supply.ConsumerRecipeName)
			}
		}
		tasks = append(tasks, plansv1.PrepTask_builder{
			CookId:         store.UUIDToString(c.ID),
			RecipeName:     r.Name,
			CookDate:       store.FormatDate(c.Date),
			Note:           note,
			ForRecipeNames: forNames,
		}.Build())
	}
	// Components from the freezer are thawed the day before.
	for _, c := range d.plan.Cooks {
		if c.Done || !c.Date.AddDate(0, 0, -1).Equal(date) {
			continue
		}
		for _, supply := range c.Supplies {
			if !supply.FromFreezer {
				continue
			}
			tasks = append(tasks, plansv1.PrepTask_builder{
				CookId:         store.UUIDToString(c.ID),
				RecipeName:     supply.SupplierRecipeName,
				CookDate:       store.FormatDate(c.Date),
				Note:           fmt.Sprintf("Take the %s out of the freezer to thaw", strings.ToLower(supply.SupplierRecipeName)),
				ForRecipeNames: []string{c.RecipeName},
			}.Build())
		}
	}
	return tasks
}

// plannedCook checks a cook's servings.
func (d *planData) plannedCook(c *plans.Cook) *plansv1.PlannedCook {
	var warnings []*plansv1.Warning
	check := &rules.Cook{
		RecipeName:      c.RecipeName,
		Date:            c.Date,
		Servings:        c.Servings,
		FrozenServings:  c.FrozenServings,
		LateServings:    c.LateServings,
		OffPlanServings: c.OffPlanServings,
		RemovedServings: c.RemovedServings,
		Done:            c.Done,
	}
	// What it makes for other cooks counts as planned, like portions.
	assigned := c.AssignedServings + c.SuppliedServings
	if w := rules.CheckServings(check, assigned); w != nil {
		warnings = append(warnings, warningToProto(w, warningContext{cookID: c.ID}))
	}
	for _, supply := range c.Supplies {
		if w := rules.CheckSupply(c.RecipeName, c.Date, supply.SupplierRecipeName, supply.SupplierDate); w != nil {
			warnings = append(warnings, warningToProto(w, warningContext{cookID: c.ID}))
		}
	}
	return plansv1.PlannedCook_builder{
		Cook:             cookToProto(c),
		AssignedServings: round(assigned),
		Warnings:         warnings,
		LeftServings:     round(max(leftServings(c), 0)),
	}.Build()
}

// warningContext is what a warning is about.
type warningContext struct {
	personID     pgtype.UUID
	date         time.Time
	eatingTimeID pgtype.UUID
	cookID       pgtype.UUID
}

var kindsToProto = map[rules.Kind]plansv1.WarningKind{
	rules.PortionTooLarge:         plansv1.WarningKind_WARNING_KIND_PORTION_TOO_LARGE,
	rules.ProteinLow:              plansv1.WarningKind_WARNING_KIND_PROTEIN_LOW,
	rules.CarbohydrateHigh:        plansv1.WarningKind_WARNING_KIND_CARBOHYDRATE_HIGH,
	rules.CarbohydrateUnpaired:    plansv1.WarningKind_WARNING_KIND_CARBOHYDRATE_UNPAIRED,
	rules.SugarsHigh:              plansv1.WarningKind_WARNING_KIND_SUGARS_HIGH,
	rules.FastCarbs:               plansv1.WarningKind_WARNING_KIND_FAST_CARBS,
	rules.WeightUnknown:           plansv1.WarningKind_WARNING_KIND_WEIGHT_UNKNOWN,
	rules.TargetLow:               plansv1.WarningKind_WARNING_KIND_TARGET_LOW,
	rules.TargetHigh:              plansv1.WarningKind_WARNING_KIND_TARGET_HIGH,
	rules.LeftoverPastFridgeDays:  plansv1.WarningKind_WARNING_KIND_LEFTOVER_PAST_FRIDGE_DAYS,
	rules.LeftoverFreeze:          plansv1.WarningKind_WARNING_KIND_LEFTOVER_FREEZE,
	rules.PortionBeforeCook:       plansv1.WarningKind_WARNING_KIND_PORTION_BEFORE_COOK,
	rules.ServingsUnassigned:      plansv1.WarningKind_WARNING_KIND_SERVINGS_UNASSIGNED,
	rules.ServingsOverassigned:    plansv1.WarningKind_WARNING_KIND_SERVINGS_OVERASSIGNED,
	rules.LeftoverThaw:            plansv1.WarningKind_WARNING_KIND_LEFTOVER_THAW,
	rules.LeftoverPastFreezerDays: plansv1.WarningKind_WARNING_KIND_LEFTOVER_PAST_FREEZER_DAYS,
	rules.FrozenServingsShort:     plansv1.WarningKind_WARNING_KIND_FROZEN_SERVINGS_SHORT,
	rules.ComponentAfterCook:      plansv1.WarningKind_WARNING_KIND_COMPONENT_AFTER_COOK,
}

func warningToProto(w *rules.Warning, c warningContext) *plansv1.Warning {
	severity := plansv1.Severity_SEVERITY_WARNING
	if w.Info {
		severity = plansv1.Severity_SEVERITY_INFO
	}
	nutrient := foodsv1.Nutrient_NUTRIENT_UNSPECIFIED
	if w.Kind == rules.TargetLow || w.Kind == rules.TargetHigh {
		nutrient = food.NutrientToProto(w.Nutrient)
	}
	return plansv1.Warning_builder{
		Kind:         kindsToProto[w.Kind],
		Severity:     severity,
		Message:      w.Message,
		Uncertain:    w.Uncertain,
		PersonId:     optionalID(c.personID),
		Date:         store.FormatDate(c.date),
		EatingTimeId: optionalID(c.eatingTimeID),
		CookId:       optionalID(c.cookID),
		Nutrient:     nutrient,
		Value:        round(w.Value),
		Limit:        round(w.Limit),
	}.Build()
}

// round rounds a computed amount to nutritionDigits decimals.
func round(v float64) float64 {
	scale := math.Pow10(nutritionDigits)
	return math.Round(v*scale) / scale
}

// unique returns the valid IDs, without repeats.
func unique(ids []pgtype.UUID) []pgtype.UUID {
	seen := map[pgtype.UUID]bool{}
	var list []pgtype.UUID
	for _, id := range ids {
		if id.Valid && !seen[id] {
			seen[id] = true
			list = append(list, id)
		}
	}
	return list
}
