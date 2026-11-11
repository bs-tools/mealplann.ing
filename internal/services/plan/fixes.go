package plan

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// maxFixes is the most fixes returned, and maxRemovals the most that
	// leave a portion out, so resizes, which keep what's planned, come
	// first.
	maxFixes    = 6
	maxRemovals = 2
	// maxWarningDistance caps how far past its limit a warning counts, so
	// one far-off value doesn't outweigh the rest.
	maxWarningDistance = 3
	// resizeStep is the step of the factors portions are tried resized by:
	// one portion from minPortionFactor to maxPortionFactor of its size,
	// or all of them together, from minScaleFactor to maxScaleFactor. All
	// together changes each less, so it's kept closer to as planned.
	resizeStep       = 0.05
	minPortionFactor = 0.4
	maxPortionFactor = 2
	minScaleFactor   = 0.5
	maxScaleFactor   = 1.5
)

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
	// changes are what it plans at each eating time it changes, after it.
	changes map[plans.Slot][]*item
	// portion is the portion it removes, and resizes the portions it
	// resizes, at their new sizes.
	portion *plans.Portion
	resizes []*plans.Portion
	// eatingTimeID is the eating time it changes, if only one.
	eatingTimeID pgtype.UUID
	// gramsChanged is how much it changes, for ties.
	gramsChanged float64
	// deleteCook is the cook to delete with the portion it removes.
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
	d, _, _, err := s.loadSuggestionData(ctx, user.HouseholdID(), date, date)
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
	fixes := d.fixCandidates(p, date, eatingTimeID, planned, base)
	sortFixes(fixes)
	removals := 0
	for _, f := range fixes {
		if len(resp.Fixes) == maxFixes {
			break
		}
		if f.kind == suggestionsv1.PlanFixKind_PLAN_FIX_KIND_REMOVE {
			if removals == maxRemovals {
				continue
			}
			removals++
		}
		resp.Fixes = append(resp.Fixes, f.toProto(planned, p.ID, date, plannedEnergy))
	}
	return resp.Build(), nil
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

// lowWarning reports whether a warning wants more food: a minimum, or
// carbohydrate needing protein or fat with it.
func lowWarning(w *fixWarning) bool {
	switch w.Kind {
	case rules.ProteinLow, rules.CarbohydrateUnpaired, rules.TargetLow:
		return true
	default:
		return false
	}
}

// sortFixes sorts fixes best first: those leaving the fewest warnings,
// then resizes before leaving portions out, as they keep what's planned,
// then closest to the rules and targets, then the smallest.
func sortFixes(fixes []*candidateFix) {
	removes := func(f *candidateFix) bool { return f.kind == suggestionsv1.PlanFixKind_PLAN_FIX_KIND_REMOVE }
	slices.SortStableFunc(fixes, func(a, b *candidateFix) int {
		if c := cmp.Compare(len(a.warnings), len(b.warnings)); c != 0 {
			return c
		}
		if a, b := removes(a), removes(b); a != b {
			if a {
				return 1
			}
			return -1
		}
		if c := cmp.Compare(math.Round(a.badness*100), math.Round(b.badness*100)); c != 0 {
			return c
		}
		return cmp.Compare(a.gramsChanged, b.gramsChanged)
	})
}

// try checks a person's day with a candidate's changes, and reports
// whether it's better than base.
func (d *planData) try(p *people.Person, date time.Time, c *candidateFix, base float64) bool {
	original := make(map[plans.Slot][]*item, len(c.changes))
	for slot, items := range c.changes {
		original[slot] = d.items[slot]
		d.items[slot] = items
	}
	c.warnings, c.energy = d.dayWarnings(p, date)
	for slot, items := range original {
		d.items[slot] = items
	}
	c.badness = badness(c.warnings)
	return c.badness < base-1e-6
}

// best returns the best of candidates better than base, or nil.
func (d *planData) best(p *people.Person, date time.Time, candidates []*candidateFix, base float64) *candidateFix {
	var better []*candidateFix
	for _, c := range candidates {
		if d.try(p, date, c, base) {
			better = append(better, c)
		}
	}
	if len(better) == 0 {
		return nil
	}
	sortFixes(better)
	return better[0]
}

// fixCandidates returns the changes better than base at a person's eating
// time, or at the day's eating times with portions: for each portion, the
// best resize of it and leaving it out, and the best scaling of all of
// them together, smaller or larger. Scaling leaves out the eating times
// whose own warnings want the other way, like a snack short of protein
// when the day is over its energy maximum. Only portion sizes change: swapping in other recipes or
// foods suggested things nobody would eat at that meal, like a pot of
// beans for breakfast.
func (d *planData) fixCandidates(p *people.Person, date time.Time, eatingTimeID pgtype.UUID, planned []fixWarning, base float64) []*candidateFix {
	var fixes []*candidateFix
	// scalable are the portions to scale together, by eating time.
	type scalable struct {
		et   *people.EatingTime
		slot plans.Slot
		// more and less are whether its warnings want more or less food.
		more, less bool
	}
	var all []scalable
	portions := 0
	for _, et := range p.EatingTimes {
		if eatingTimeID.Valid && et.ID != eatingTimeID {
			continue
		}
		slot := plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date}
		items := d.items[slot]
		has := false
		for i, it := range items {
			portion := it.portion
			if portion == nil || portion.Eaten {
				continue
			}
			has = true
			portions++
			var resizes []*candidateFix
			for factor := minPortionFactor; factor <= maxPortionFactor+1e-9; factor += resizeStep {
				if c := d.resize(et, slot, items, i, factor); c != nil {
					resizes = append(resizes, c)
				}
			}
			if c := d.best(p, date, resizes, base); c != nil {
				fixes = append(fixes, c)
			}
			without := slices.Delete(slices.Clone(items), i, i+1)
			if len(without) == 0 {
				continue
			}
			c := &candidateFix{
				kind:         suggestionsv1.PlanFixKind_PLAN_FIX_KIND_REMOVE,
				description:  fmt.Sprintf("Leave out %s at %s", portion.Name, et.Name),
				changes:      map[plans.Slot][]*item{slot: without},
				portion:      portion,
				eatingTimeID: et.ID,
				gramsChanged: d.eaten(it).amount.Grams,
				deleteCook:   d.soleCook(portion),
			}
			if d.try(p, date, c, base) {
				fixes = append(fixes, c)
			}
		}
		if has {
			sc := scalable{et: et, slot: slot}
			for _, w := range planned {
				if w.eatingTimeID == et.ID {
					sc.more = sc.more || lowWarning(&w)
					sc.less = sc.less || !lowWarning(&w)
				}
			}
			all = append(all, sc)
		}
	}
	if portions < 2 {
		return fixes
	}
	var scales []*candidateFix
	for factor := minScaleFactor; factor <= maxScaleFactor+1e-9; factor += resizeStep {
		if math.Abs(factor-1) < 1e-9 {
			continue
		}
		c := &candidateFix{kind: suggestionsv1.PlanFixKind_PLAN_FIX_KIND_RESIZE, changes: map[plans.Slot][]*item{}}
		var parts []string
		for _, s := range all {
			if (factor < 1 && s.more) || (factor > 1 && s.less) {
				continue
			}
			items := slices.Clone(d.items[s.slot])
			for i, it := range items {
				if it.portion == nil || it.portion.Eaten {
					continue
				}
				resized, text, ok := resizePortion(it.portion, factor)
				if !ok {
					continue
				}
				items[i] = &item{portion: resized, date: it.date}
				c.resizes = append(c.resizes, resized)
				c.gramsChanged += math.Abs(d.eaten(items[i]).amount.Grams - d.eaten(it).amount.Grams)
				parts = append(parts, fmt.Sprintf("%s of %s at %s", text, it.portion.Name, s.et.Name))
			}
			c.changes[s.slot] = items
		}
		if len(c.resizes) < 2 {
			continue
		}
		size := "smaller"
		if factor > 1 {
			size = "larger"
		}
		c.description = fmt.Sprintf("Have each portion %s, at %s%%: %s",
			size, formatNumber(factor*100), strings.Join(parts, ", "))
		scales = append(scales, c)
	}
	if c := d.best(p, date, scales, base); c != nil {
		fixes = append(fixes, c)
	}
	return fixes
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

// resizePortion returns a portion resized by a factor, rounded to what
// people measure, with its new size said, or false if rounding leaves it
// as it was.
func resizePortion(portion *plans.Portion, factor float64) (*plans.Portion, string, bool) {
	resized := *portion
	if portion.CookID.Valid {
		resized.Servings = math.Max(math.Round(portion.Servings*factor*20)/20, 0.05)
		if resized.Servings == portion.Servings {
			return nil, "", false
		}
		return &resized, servingsText(resized.Servings), true
	}
	resized.Amount = roundAmount(portion.Amount*factor, portion.Unit)
	if resized.Amount <= 0 || resized.Amount == portion.Amount {
		return nil, "", false
	}
	return &resized, amountText(resized.Amount, resized.Unit), true
}

// resize returns a portion's resize by a factor, or nil if rounding
// leaves it as it was.
func (d *planData) resize(et *people.EatingTime, slot plans.Slot, items []*item, i int, factor float64) *candidateFix {
	portion := items[i].portion
	resized, text, ok := resizePortion(portion, factor)
	if !ok {
		return nil
	}
	was := amountText(portion.Amount, portion.Unit)
	if portion.CookID.Valid {
		was = servingsText(portion.Servings)
	}
	it := &item{portion: resized, date: items[i].date}
	changed := slices.Clone(items)
	changed[i] = it
	return &candidateFix{
		kind:         suggestionsv1.PlanFixKind_PLAN_FIX_KIND_RESIZE,
		description:  fmt.Sprintf("Have %s of %s at %s, not %s", text, portion.Name, et.Name, was),
		changes:      map[plans.Slot][]*item{slot: changed},
		resizes:      []*plans.Portion{resized},
		eatingTimeID: et.ID,
		gramsChanged: math.Abs(d.eaten(it).amount.Grams - d.eaten(items[i]).amount.Grams),
	}
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
		EatingTimeId:      optionalID(c.eatingTimeID),
		EnergyKcal:        math.Round(c.energy),
		PlannedEnergyKcal: math.Round(plannedEnergy),
	}
	if c.portion != nil {
		b.PortionId = store.UUIDToString(c.portion.ID)
	}
	for _, r := range c.resizes {
		rb := suggestionsv1.PortionResize_builder{PortionId: store.UUIDToString(r.ID)}
		if r.CookID.Valid {
			rb.Servings = r.Servings
		} else {
			rb.Amount = r.Amount
			rb.Unit = unitsToProto[r.Unit]
		}
		b.Resizes = append(b.Resizes, rb.Build())
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
