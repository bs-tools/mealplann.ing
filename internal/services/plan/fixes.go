package plan

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// maxFixes is the most fixes returned, and maxFixesPerKind the most of
	// one kind, so a few resizes don't crowd out a replacement.
	maxFixes        = 6
	maxFixesPerKind = 2
	// maxFixFoods is the most pantry foods tried as additions, those to
	// use first.
	maxFixFoods = 40
	// fixGrams are the amounts of a pantry food tried as an addition.
	fixGramsSmall  = 50
	fixGramsMedium = 100
	fixGramsLarge  = 150
	// maxWarningDistance caps how far past its limit a warning counts, so
	// one far-off value doesn't outweigh the rest.
	maxWarningDistance = 3
)

// fixResizes are the factors portions are tried resized by.
var fixResizes = []float64{0.5, 2.0 / 3, 0.75, 1.25, 1.5}

// fixWarning is a warning of a person's day, with the eating time it's
// about, invalid for the day's targets.
type fixWarning struct {
	rules.Warning
	eatingTimeID pgtype.UUID
}

// key identifies a warning across changes to the day.
func (w *fixWarning) key() string {
	return fmt.Sprintf("%d|%x|%d", w.Kind, w.eatingTimeID.Bytes, w.Nutrient)
}

// candidateFix is a change to a person's day, with what applying it does.
type candidateFix struct {
	kind        suggestionsv1.PlanFixKind
	description string
	// slot is the eating time it changes, and items what's planned there
	// after it.
	slot  plans.Slot
	items []*item
	// portion is the portion it removes, resizes or replaces.
	portion *plans.Portion
	// recipe or food, with servings or amount and unit, is what it adds,
	// replaces with, or resizes to.
	recipe   pgtype.UUID
	food     pgtype.UUID
	servings float64
	amount   float64
	unit     nutrition.Unit
	// gramsChanged is how much it changes, for ties.
	gramsChanged float64
	// deleteCook is the cook to delete with the portion it removes or
	// replaces.
	deleteCook pgtype.UUID

	warnings []fixWarning
	badness  float64
	energy   float64
}

func (s *Service) SuggestFixes(ctx context.Context, req *suggestionsv1.SuggestFixesRequest) (*suggestionsv1.SuggestFixesResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	personID, err := parseID("person_id", req.GetPersonId(), "find people with ListPeople")
	if err != nil {
		return nil, err
	}
	date, err := parseDate("date", req.GetDate())
	if err != nil {
		return nil, err
	}
	var eatingTimeID pgtype.UUID
	if req.GetEatingTimeId() != "" {
		if eatingTimeID, err = parseID("eating_time_id", req.GetEatingTimeId(), "find people's eating times with ListPeople"); err != nil {
			return nil, err
		}
	}
	d, _, lots, err := s.loadSuggestionData(ctx, user.HouseholdID(), date, date)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	i := slices.IndexFunc(d.people, func(p *people.Person) bool { return p.ID == personID })
	if i < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "person_id isn't in your household; find people with ListPeople")
	}
	p := d.people[i]
	if eatingTimeID.Valid && p.EatingTime(eatingTimeID) == nil {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"eating_time_id isn't one of %s's eating times; find them with ListPeople", p.Name)
	}
	pantryFoods, err := s.fixFoods(ctx, d, user.HouseholdID(), lots)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read foods", "")
	}

	planned, plannedEnergy := d.dayWarnings(p, date)
	resp := suggestionsv1.SuggestFixesResponse_builder{}
	for _, w := range planned {
		if !eatingTimeID.Valid || !w.eatingTimeID.Valid || w.eatingTimeID == eatingTimeID {
			resp.Warnings = append(resp.Warnings, fixWarningToProto(&w, p.ID, date))
		}
	}
	if len(resp.Warnings) == 0 {
		return resp.Build(), nil
	}
	base := badness(planned)
	var fixes []*candidateFix
	for _, cand := range d.fixCandidates(p, date, eatingTimeID, planned, pantryFoods) {
		original := d.items[cand.slot]
		d.items[cand.slot] = cand.items
		cand.warnings, cand.energy = d.dayWarnings(p, date)
		d.items[cand.slot] = original
		cand.badness = badness(cand.warnings)
		if cand.badness < base-1e-6 {
			fixes = append(fixes, cand)
		}
	}
	slices.SortStableFunc(fixes, func(a, b *candidateFix) int {
		if c := cmp.Compare(len(a.warnings), len(b.warnings)); c != 0 {
			return c
		}
		if c := cmp.Compare(math.Round(a.badness*100), math.Round(b.badness*100)); c != 0 {
			return c
		}
		return cmp.Compare(a.gramsChanged, b.gramsChanged)
	})
	perKind := map[suggestionsv1.PlanFixKind]int{}
	for _, f := range fixes {
		if len(resp.Fixes) == maxFixes {
			break
		}
		if perKind[f.kind] == maxFixesPerKind {
			continue
		}
		perKind[f.kind]++
		resp.Fixes = append(resp.Fixes, f.toProto(planned, p.ID, date, plannedEnergy))
	}
	return resp.Build(), nil
}

// fixFoods returns the pantry's foods to try adding, those to use first
// first, reading the foods the plan read doesn't have.
func (s *Service) fixFoods(ctx context.Context, d *planData, householdID pgtype.UUID, lots []*pantry.Item) ([]*foods.Food, error) {
	sorted := slices.Clone(lots)
	pantry.SortByUseBy(sorted)
	var ids, missing []pgtype.UUID
	for _, lot := range sorted {
		if slices.Contains(ids, lot.FoodID) {
			continue
		}
		ids = append(ids, lot.FoodID)
		if _, ok := d.foods[lot.FoodID]; !ok {
			missing = append(missing, lot.FoodID)
		}
		if len(ids) == maxFixFoods {
			break
		}
	}
	if len(missing) > 0 {
		found, err := s.foods.ListFoodsByID(ctx, householdID, missing)
		if err != nil {
			return nil, err
		}
		for _, f := range found {
			d.foods[f.ID] = f
		}
	}
	list := make([]*foods.Food, 0, len(ids))
	for _, id := range ids {
		if f, ok := d.foods[id]; ok {
			list = append(list, f)
		}
	}
	return list, nil
}

// dayWarnings checks a person's day as planned in d.items: each eating
// time's rules, and the day's targets, leaving out those only to know
// about. A daily minimum isn't counted while some eating times are
// unplanned, as planning them is the fix. It also returns the day's
// energy, in kcal.
func (d *planData) dayWarnings(p *people.Person, date time.Time) ([]fixWarning, float64) {
	day := &rules.Day{}
	var warnings []fixWarning
	for _, et := range p.EatingTimes {
		slot := plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date}
		items := d.items[slot]
		switch {
		case d.plan.EatingOutAt(slot) != nil:
			day.EatingOut++
		case len(items) == 0:
			day.Unplanned++
		}
		ruleSlot := &rules.EatingTime{Name: et.Name}
		for _, it := range items {
			e := d.eaten(it)
			addEaten(ruleSlot, it, &e)
		}
		day.Amount.Add(&ruleSlot.Amount)
		for _, w := range rules.CheckEatingTime(&p.Rules, ruleSlot) {
			if !w.Info && w.Kind != rules.WeightUnknown {
				warnings = append(warnings, fixWarning{Warning: w, eatingTimeID: et.ID})
			}
		}
	}
	for _, w := range rules.CheckDay(p.Targets, day) {
		if w.Info || (w.Kind == rules.TargetLow && day.Unplanned > 0) {
			continue
		}
		warnings = append(warnings, fixWarning{Warning: w})
	}
	energy := 0.0
	if v := day.Amount.Nutrients[nutrition.EnergyKcal]; v != nil {
		energy = *v
	}
	return warnings, energy
}

// badness is how far a day is from its rules and targets: each warning
// counts one, plus how far past its limit it is, relative to the limit.
func badness(warnings []fixWarning) float64 {
	total := 0.0
	for _, w := range warnings {
		distance := 0.0
		if w.Limit != 0 {
			distance = math.Min(math.Abs(w.Value-w.Limit)/math.Abs(w.Limit), maxWarningDistance)
		}
		total += 1 + distance
	}
	return total
}

// lowWarning reports whether a warning is fixed by eating more: a
// minimum, or carbohydrate needing protein or fat with it.
func lowWarning(w *fixWarning) bool {
	switch w.Kind {
	case rules.ProteinLow, rules.CarbohydrateUnpaired, rules.TargetLow:
		return true
	default:
		return false
	}
}

// fixCandidates returns the changes to try at a person's eating time, or
// at each of the day's eating times with portions: leaving out, resizing
// or replacing each portion, and if some warning wants more food, adding
// a recipe or a pantry food.
func (d *planData) fixCandidates(p *people.Person, date time.Time, eatingTimeID pgtype.UUID, planned []fixWarning, pantryFoods []*foods.Food) []*candidateFix {
	wantsMore := slices.ContainsFunc(planned, func(w fixWarning) bool {
		return lowWarning(&w) && (!w.eatingTimeID.Valid || !eatingTimeID.Valid || w.eatingTimeID == eatingTimeID)
	})
	var candidates []*candidateFix
	for _, et := range p.EatingTimes {
		if eatingTimeID.Valid && et.ID != eatingTimeID {
			continue
		}
		slot := plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date}
		items := d.items[slot]
		if len(items) == 0 {
			continue
		}
		// Recipes at the eating time already aren't added again.
		var there []pgtype.UUID
		for _, it := range items {
			there = append(there, it.recipeID())
		}
		for i, it := range items {
			portion := it.portion
			if portion == nil {
				continue
			}
			without := slices.Delete(slices.Clone(items), i, i+1)
			grams := d.eaten(it).amount.Grams
			deleteCook := d.soleCook(portion)
			if len(without) > 0 {
				candidates = append(candidates, &candidateFix{
					kind:         suggestionsv1.PlanFixKind_PLAN_FIX_KIND_REMOVE,
					description:  fmt.Sprintf("Leave out %s at %s", portion.Name, et.Name),
					slot:         slot,
					items:        without,
					portion:      portion,
					gramsChanged: grams,
					deleteCook:   deleteCook,
				})
			}
			candidates = append(candidates, d.resizes(et, slot, items, i)...)
			for _, r := range d.recipes {
				if r.Draft || slices.Contains(there, r.ID) || r.Servings <= 0 {
					continue
				}
				added := &item{portion: &plans.Portion{
					PersonID: p.ID, EatingTimeID: et.ID, Date: date,
					Servings: 1, CookRecipeID: r.ID, Name: r.Name,
				}, date: date}
				candidates = append(candidates, &candidateFix{
					kind:         suggestionsv1.PlanFixKind_PLAN_FIX_KIND_REPLACE,
					description:  fmt.Sprintf("Replace %s with %s (1 serving) at %s", portion.Name, r.Name, et.Name),
					slot:         slot,
					items:        append(slices.Clone(without), added),
					portion:      portion,
					recipe:       r.ID,
					servings:     1,
					gramsChanged: math.Abs(d.eaten(added).amount.Grams - grams),
					deleteCook:   deleteCook,
				})
			}
		}
		if wantsMore {
			candidates = append(candidates, d.additions(p, et, slot, items, pantryFoods)...)
		}
	}
	return candidates
}

// soleCook returns a portion's cook if the portion is all that's planned
// of it, it isn't cooked, and supplies no other cook, so removing the
// portion would leave the whole cook over; or an invalid ID.
func (d *planData) soleCook(portion *plans.Portion) pgtype.UUID {
	if !portion.CookID.Valid {
		return pgtype.UUID{}
	}
	i := slices.IndexFunc(d.plan.Cooks, func(c *plans.Cook) bool { return c.ID == portion.CookID })
	if i < 0 {
		return pgtype.UUID{}
	}
	c := d.plan.Cooks[i]
	if c.Done || c.FrozenServings > 0 || c.OffPlanServings > 0 || c.SuppliedServings > 0 ||
		math.Abs(c.AssignedServings-portion.Servings) > 1e-9 {
		return pgtype.UUID{}
	}
	return c.ID
}

// resizes returns a portion's resizes, by each of fixResizes.
func (d *planData) resizes(et *people.EatingTime, slot plans.Slot, items []*item, i int) []*candidateFix {
	portion := items[i].portion
	grams := d.eaten(items[i]).amount.Grams
	var candidates []*candidateFix
	for _, factor := range fixResizes {
		resized := *portion
		var description string
		if portion.CookID.Valid {
			resized.Servings = math.Max(math.Round(portion.Servings*factor*20)/20, 0.1)
			if resized.Servings == portion.Servings {
				continue
			}
			description = fmt.Sprintf("Have %s of %s at %s, not %s",
				servingsText(resized.Servings), portion.Name, et.Name, servingsText(portion.Servings))
		} else {
			resized.Amount = roundAmount(portion.Amount*factor, portion.Unit)
			if resized.Amount <= 0 || resized.Amount == portion.Amount {
				continue
			}
			description = fmt.Sprintf("Have %s of %s at %s, not %s",
				amountText(resized.Amount, resized.Unit), portion.Name, et.Name, amountText(portion.Amount, portion.Unit))
		}
		it := &item{portion: &resized, date: items[i].date}
		changed := slices.Clone(items)
		changed[i] = it
		candidates = append(candidates, &candidateFix{
			kind:         suggestionsv1.PlanFixKind_PLAN_FIX_KIND_RESIZE,
			description:  description,
			slot:         slot,
			items:        changed,
			portion:      portion,
			servings:     resized.Servings,
			amount:       resized.Amount,
			unit:         resized.Unit,
			gramsChanged: math.Abs(d.eaten(it).amount.Grams - grams),
		})
	}
	return candidates
}

// additions returns the recipes and pantry foods to try adding at an
// eating time.
func (d *planData) additions(p *people.Person, et *people.EatingTime, slot plans.Slot, items []*item, pantryFoods []*foods.Food) []*candidateFix {
	var candidates []*candidateFix
	add := func(c *candidateFix, it *item) {
		c.kind = suggestionsv1.PlanFixKind_PLAN_FIX_KIND_ADD
		c.slot = slot
		c.items = append(slices.Clone(items), it)
		c.gramsChanged = d.eaten(it).amount.Grams
		candidates = append(candidates, c)
	}
	for _, r := range d.recipes {
		if r.Draft || r.Servings <= 0 {
			continue
		}
		for _, servings := range []float64{0.5, 1} {
			add(&candidateFix{
				description: fmt.Sprintf("Add %s of %s at %s", servingsText(servings), r.Name, et.Name),
				recipe:      r.ID,
				servings:    servings,
			}, &item{portion: &plans.Portion{
				PersonID: p.ID, EatingTimeID: et.ID, Date: slot.Date,
				Servings: servings, CookRecipeID: r.ID, Name: r.Name,
			}, date: slot.Date})
		}
	}
	for _, f := range pantryFoods {
		for _, grams := range []float64{fixGramsSmall, fixGramsMedium, fixGramsLarge} {
			add(&candidateFix{
				description: fmt.Sprintf("Add %s of %s at %s", amountText(grams, nutrition.Grams), f.Name, et.Name),
				food:        f.ID,
				amount:      grams,
				unit:        nutrition.Grams,
			}, &item{portion: &plans.Portion{
				PersonID: p.ID, EatingTimeID: et.ID, Date: slot.Date,
				FoodID: f.ID, Amount: grams, Unit: nutrition.Grams, Name: f.Name,
			}, date: slot.Date})
		}
	}
	return candidates
}

// roundAmount rounds a resized amount to what people measure: whole grams
// or milliliters to 5, pieces to halves, packages to quarters.
func roundAmount(amount float64, unit nutrition.Unit) float64 {
	switch unit {
	case nutrition.Pieces:
		return math.Round(amount*2) / 2
	case nutrition.Packages:
		return math.Round(amount*4) / 4
	default:
		return math.Round(amount/5) * 5
	}
}

// servingsText says servings, e.g. "1 serving" or "0.5 servings".
func servingsText(servings float64) string {
	if servings == 1 {
		return "1 serving"
	}
	return fmt.Sprintf("%s servings", formatNumber(servings))
}

// amountText says an amount in a unit, e.g. "100 g" or "2 pieces".
func amountText(amount float64, unit nutrition.Unit) string {
	switch unit {
	case nutrition.Milliliters:
		return formatNumber(amount) + " ml"
	case nutrition.Pieces:
		if amount == 1 {
			return "1 piece"
		}
		return formatNumber(amount) + " pieces"
	case nutrition.Packages:
		if amount == 1 {
			return "1 package"
		}
		return formatNumber(amount) + " packages"
	default:
		return formatNumber(amount) + " g"
	}
}

// formatNumber formats a number with at most 2 decimals.
func formatNumber(v float64) string {
	return fmt.Sprintf("%g", math.Round(v*100)/100)
}

// fixWarningToProto converts a warning of a person's day.
func fixWarningToProto(w *fixWarning, personID pgtype.UUID, date time.Time) *plansv1.Warning {
	return warningToProto(&w.Warning, warningContext{personID: personID, date: date, eatingTimeID: w.eatingTimeID})
}

// toProto converts a fix, with the warnings as planned it fixes.
func (c *candidateFix) toProto(planned []fixWarning, personID pgtype.UUID, date time.Time, plannedEnergy float64) *suggestionsv1.PlanFix {
	left := map[string]bool{}
	for _, w := range c.warnings {
		left[w.key()] = true
	}
	b := suggestionsv1.PlanFix_builder{
		Kind:              c.kind,
		Description:       c.description,
		EatingTimeId:      store.UUIDToString(c.slot.EatingTimeID),
		RecipeId:          optionalID(c.recipe),
		FoodId:            optionalID(c.food),
		Servings:          c.servings,
		Amount:            c.amount,
		EnergyKcal:        math.Round(c.energy),
		PlannedEnergyKcal: math.Round(plannedEnergy),
	}
	if c.amount > 0 {
		b.Unit = unitsToProto[c.unit]
	}
	if c.portion != nil {
		b.PortionId = store.UUIDToString(c.portion.ID)
	}
	b.DeleteCookId = optionalID(c.deleteCook)
	for _, w := range planned {
		if !left[w.key()] {
			b.FixedWarnings = append(b.FixedWarnings, w.Message)
		}
	}
	for _, w := range c.warnings {
		b.Warnings = append(b.Warnings, fixWarningToProto(&w, personID, date))
	}
	return b.Build()
}
