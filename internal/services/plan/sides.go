package plan

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/store"
)

// minSideGrams is the least of a side worth adding: less doesn't make a
// side, so a side the maximum portion leaves less room for doesn't fit.
const minSideGrams = 20

// sideCandidate is a side of a meal, with what adding it does.
type sideCandidate struct {
	side     recipes.Side
	grams    float64
	added    nutrition.Amount
	warnings []fixWarning
	// newWarnings are the warnings it adds, which the meal alone doesn't
	// have.
	newWarnings []fixWarning
	badness     float64
	reason      string
	// planned is the portion of it planned at the eating time, if any.
	planned *plans.Portion
}

func (s *Service) SuggestSides(ctx context.Context, req *suggestionsv1.SuggestSidesRequest) (*suggestionsv1.SuggestSidesResponse, error) {
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
	eatingTimeID, err := parseID("eating_time_id", req.GetEatingTimeId(), "find people's eating times with ListPeople")
	if err != nil {
		return nil, err
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
	et := p.EatingTime(eatingTimeID)
	if et == nil {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"eating_time_id isn't one of %s's eating times; find them with ListPeople", p.Name)
	}
	slot := plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date}
	r, main := d.mealWithSides(slot)
	if r == nil {
		return suggestionsv1.SuggestSidesResponse_builder{}.Build(), nil
	}
	candidates := d.sideCandidates(p, et, slot, r, main)
	resp := suggestionsv1.SuggestSidesResponse_builder{
		RecipeId:   store.UUIDToString(r.ID),
		RecipeName: r.Name,
	}
	for _, c := range candidates {
		b := suggestionsv1.SideSuggestion_builder{
			FoodId:            store.UUIDToString(c.side.FoodID),
			FoodName:          c.side.FoodName,
			Grams:             c.grams,
			EnergyKcal:        roundTenth(c.added.Nutrients[nutrition.EnergyKcal]),
			ProteinGrams:      roundTenth(c.added.Nutrients[nutrition.ProteinGrams]),
			CarbohydrateGrams: roundTenth(c.added.Nutrients[nutrition.CarbohydrateGrams]),
			FatGrams:          roundTenth(c.added.Nutrients[nutrition.FatGrams]),
			Reason:            c.reason,
			Planned:           c.planned != nil,
		}
		if c.planned != nil {
			b.PortionId = store.UUIDToString(c.planned.ID)
		}
		for _, w := range c.newWarnings {
			b.Warnings = append(b.Warnings, fixWarningToProto(&w, p.ID, date))
		}
		resp.Sides = append(resp.Sides, b.Build())
	}
	return resp.Build(), nil
}

// mealWithSides returns the recipe with sides of the first share of a
// cook at a slot, with that share, or nil if none has sides.
func (d *planData) mealWithSides(slot plans.Slot) (*recipes.Recipe, *plans.Portion) {
	for _, it := range d.items[slot] {
		if it.portion == nil || !it.portion.CookID.Valid {
			continue
		}
		if r, ok := d.recipes[it.portion.CookRecipeID]; ok && len(r.ServedWith) > 0 {
			return r, it.portion
		}
	}
	return nil, nil
}

// sideCandidates scores each of a recipe's sides at a slot, best first:
// the day is checked with the side added to what's planned there, without
// the recipe's sides already planned, so each is compared with the meal
// on its own.
func (d *planData) sideCandidates(p *people.Person, et *people.EatingTime, slot plans.Slot, r *recipes.Recipe, main *plans.Portion) []*sideCandidate {
	original := d.items[slot]
	defer func() { d.items[slot] = original }()
	var base []*item
	planned := map[[16]byte]*plans.Portion{}
	for _, it := range original {
		if it.portion != nil && it.portion.FoodID.Valid &&
			slices.ContainsFunc(r.ServedWith, func(side recipes.Side) bool { return side.FoodID == it.portion.FoodID }) {
			planned[it.portion.FoodID.Bytes] = it.portion
			continue
		}
		base = append(base, it)
	}
	d.items[slot] = base
	baseWarnings, _ := d.dayWarnings(p, slot.Date)
	room := math.Inf(1)
	if p.Rules.MaxPortionGrams != nil {
		used := 0.0
		if ruleSlot := d.ruleEatingTime(slot, et.Name); ruleSlot != nil {
			used = ruleSlot.Amount.Grams
		}
		room = *p.Rules.MaxPortionGrams - used
	}
	proteinShort := d.proteinShort(p, slot)

	var candidates []*sideCandidate
	for _, side := range r.ServedWith {
		f, ok := d.foods[side.FoodID]
		if !ok {
			continue
		}
		c := &sideCandidate{side: side, planned: planned[side.FoodID.Bytes]}
		c.side.FoodName = f.Name
		c.grams = roundAmount(side.Grams*main.Servings, nutrition.Grams)
		if c.grams > room {
			c.grams = math.Floor(room/5) * 5
		}
		if c.grams < minSideGrams {
			c.grams = 0
			c.badness = math.Inf(1)
			c.reason = fmt.Sprintf("no room: %s is at its %s g maximum portion", lowerFirst(et.Name), formatNumber(*p.Rules.MaxPortionGrams))
			candidates = append(candidates, c)
			continue
		}
		c.added = f.Nutrition().AmountOf(c.grams, nutrition.Grams)
		side := &item{portion: &plans.Portion{
			PersonID: p.ID, EatingTimeID: et.ID, Date: slot.Date,
			FoodID: f.ID, Amount: c.grams, Unit: nutrition.Grams, Name: f.Name,
		}, date: slot.Date}
		d.items[slot] = append(slices.Clone(base), side)
		c.warnings, _ = d.dayWarnings(p, slot.Date)
		c.badness = badness(c.warnings)
		for _, w := range c.warnings {
			if !slices.ContainsFunc(baseWarnings, func(b fixWarning) bool { return b.key() == w.key() }) {
				c.newWarnings = append(c.newWarnings, w)
			}
		}
		c.reason = sideReason(c, baseWarnings, proteinShort)
		candidates = append(candidates, c)
	}
	slices.SortStableFunc(candidates, func(a, b *sideCandidate) int {
		if a, b := a.grams == 0, b.grams == 0; a != b {
			if a {
				return 1
			}
			return -1
		}
		if c := cmp.Compare(len(a.warnings), len(b.warnings)); c != 0 {
			return c
		}
		if c := cmp.Compare(math.Round(a.badness*100), math.Round(b.badness*100)); c != 0 {
			return c
		}
		return cmp.Compare(amountOf(&b.added, nutrition.ProteinGrams), amountOf(&a.added, nutrition.ProteinGrams))
	})
	return candidates
}

// proteinShort returns how far a person's day, as planned, is under
// their daily minimum of protein, 0 if it isn't or they have none.
func (d *planData) proteinShort(p *people.Person, slot plans.Slot) float64 {
	minimum := 0.0
	for _, t := range p.Targets {
		if t.Nutrient == nutrition.ProteinGrams && t.Min != nil {
			minimum = *t.Min
		}
	}
	if minimum == 0 {
		return 0
	}
	total := 0.0
	for _, et := range p.EatingTimes {
		if ruleSlot := d.ruleEatingTime(plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: slot.Date}, et.Name); ruleSlot != nil {
			total += amountOf(&ruleSlot.Amount, nutrition.ProteinGrams)
		}
	}
	return math.Max(minimum-total, 0)
}

// sideReason says why a side fits or not: a warning it adds, the protein
// it adds to a day short of it, the warnings it clears, or what it adds.
func sideReason(c *sideCandidate, base []fixWarning, proteinShort float64) string {
	if len(c.newWarnings) > 0 {
		return c.newWarnings[0].Message
	}
	protein := amountOf(&c.added, nutrition.ProteinGrams)
	if proteinShort >= 1 && protein >= 1 {
		return fmt.Sprintf("adds %s g protein: today is %s g short", formatNumber(math.Round(protein)), formatNumber(math.Round(proteinShort)))
	}
	if cleared := len(base) - len(c.warnings); cleared > 0 {
		return fmt.Sprintf("clears %d of the day's warnings", cleared)
	}
	return fmt.Sprintf("adds %s kcal and %s g protein, within the rules and targets",
		formatNumber(math.Round(amountOf(&c.added, nutrition.EnergyKcal))), formatNumber(math.Round(protein)))
}

// amountOf returns an amount's nutrient, 0 if unknown.
func amountOf(a *nutrition.Amount, n nutrition.Nutrient) float64 {
	if v := a.Nutrients[n]; v != nil {
		return *v
	}
	return 0
}

// roundTenth rounds a nutrient to a tenth, 0 if unknown.
func roundTenth(v *float64) float64 {
	if v == nil {
		return 0
	}
	return math.Round(*v*10) / 10
}

// lowerFirst lowers the first letter of a name, for within a sentence.
func lowerFirst(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}
