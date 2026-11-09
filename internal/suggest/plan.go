package suggest

import (
	"cmp"
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// Planning a range of days scores whole plans rather than single slots,
// and there are far too many plans to score them all: 30 recipes for 14
// dinners is 30^14. So it searches probabilistically, with the scoring of
// single slots as its guide:
//
//  1. A rollout fills the open slots in order, each with a recipe drawn
//     from the best few for that slot given what the rollout has planned
//     so far, more likely the higher it scores. The first rollout always
//     takes the best, so the result is never worse than planning greedily.
//     Each pick is added to the rollout's plan, so later slots share its
//     ingredients and packs, and avoid repeating it.
//  2. Rollouts run until the time budget or the most rollouts, and the plan
//     with the highest total score wins.
//  3. A polishing pass scores each slot of the winner again against all
//     the others, before and after it, and swaps in a better recipe if
//     there is one. That pass also gives each slot's final reasons and its
//     alternatives.
//
// The rules of the eating times aren't hard constraints, as in
// SuggestRecipes, but a slot only draws from recipes that break no rule if
// it has any. Breakfast is a hard constraint: it only draws from the
// recipes FitsMeal allows, those tagged "breakfast", so it's never a
// chili. A small meal draws from the recipes tagged "snack" that break no
// rule, if there are any, and otherwise from the rest of the plan's
// earlier meals, eaten reheated (see Input.Leftovers), so snacks come
// first even when repeating one costs variety.
//
// The slots are shared meals, and the eating times of one person that eat
// from no shared meal, like a snack mid-morning. A shared meal's portion
// that's more than a person's maximum portion spills into their next small
// eating times (see plans.SplitMeal), so such a slot that a planned meal
// fills already isn't planned: it's eating the rest of that meal.
const (
	// PlanTemperature is how much a rollout's picks favor the best
	// recipes, in points: a recipe scoring PlanTemperature less than the
	// best is drawn e (2.7) times less often. Lower is greedier, higher
	// more varied.
	PlanTemperature = 2.5
	// PlanChoices is how many of a slot's best recipes a rollout draws
	// from, so a poor recipe is never picked by chance.
	PlanChoices = 5
	// DefaultPlanBudget is how long the rollouts run for, by default: long
	// enough for dozens of rollouts of two weeks, short enough to feel
	// instant.
	DefaultPlanBudget = 1500 * time.Millisecond
	// DefaultPlanRollouts is the most rollouts, by default, after which
	// more rarely find a better plan.
	DefaultPlanRollouts = 200
	// DefaultAlternatives is how many alternatives each slot offers.
	DefaultAlternatives = 4
	// PlanPatience is how many rollouts in a row may find no better plan
	// before the search stops early: by then, more rarely do.
	PlanPatience = 40
)

// PlanSlot is a shared meal, or one person's eating time, on a date to
// plan.
type PlanSlot struct {
	// Date is at midnight UTC.
	Date time.Time
	// Meal is the shared meal, MealNone for one person's eating time.
	Meal people.Meal
	// People are those who eat the meal, each with an eating time for it,
	// or the one person whose eating time it is.
	People []*people.Person
	// EatingTimeID is the person's eating time, invalid for a shared meal.
	EatingTimeID pgtype.UUID
	// Fixed is the recipe the user chose for the slot, nil to pick one.
	Fixed *recipes.Recipe
	// FixedLeftover is the leftover the user chose for a small meal of
	// one person, nil to pick one. It's planned if the plan has its cook.
	FixedLeftover *LeftoverRef
}

// PlanInput is what planning a range of days needs.
type PlanInput struct {
	// Slots are the slots to plan, by date and then meal, so that each
	// slot's portions are split given those planned before it.
	Slots []*PlanSlot
	// Plan is what's planned already, from VarietyDays before the first
	// slot's week to the end of the last slot's week and the day after.
	Plan *plans.Plan
	// Planned is what's planned at each slot of Plan, as the rules check
	// it.
	Planned map[plans.Slot]*rules.EatingTime
	// Recipes, Foods and Lots are as in Input.
	Recipes []*recipes.Recipe
	Foods   map[pgtype.UUID]*foods.Food
	Lots    []*pantry.Item
	// Candidates are the recipes to pick from, nil for all of Recipes.
	Candidates []*recipes.Recipe
	// ShopDate returns the date of the shop of a date's fresh foods.
	ShopDate func(date time.Time) time.Time
	// Weights are as in Input.
	Weights map[Kind]float64
	// Rand draws the rollouts' picks.
	Rand *rand.Rand
	// Budget is how long the rollouts run for, and Rollouts the most of
	// them; DefaultPlanBudget and DefaultPlanRollouts if 0.
	Budget   time.Duration
	Rollouts int
	// Alternatives is how many alternatives each slot offers,
	// DefaultAlternatives if 0.
	Alternatives int
	// Now returns the time, for the budget. time.Now if nil.
	Now func() time.Time
	// Done stops the rollouts when closed, like a request's context's when
	// the user changes their mind, so a search nobody waits for doesn't
	// keep the server's only CPU busy. Nil never stops them.
	Done <-chan struct{}
}

// PlannedSlot is a slot of a suggested plan.
type PlannedSlot struct {
	Slot *PlanSlot
	// Suggestion is the recipe planned, scored against the rest of the
	// plan, nil if no recipe could be planned.
	Suggestion *Suggestion
	// Alternatives are the next best recipes for the slot, given the rest
	// of the plan, best first.
	Alternatives []*Suggestion
	// Fitting is how many of the slot's candidates break no rule, given
	// the rest of the plan. Fewer than the slots of its meal means the plan
	// has to repeat them, or break a rule.
	Fitting int
	// Eligible is how many of the candidates its meal tag allows (see
	// FitsMeal): with none, the slot can't be planned.
	Eligible int
	// FilledByMeal is whether one person's eating time isn't planned, as a
	// shared meal of the plan spills into it.
	FilledByMeal bool
}

// personal reports whether the slot is one person's eating time.
func (s *PlanSlot) personal() bool {
	return s.Meal == people.MealNone
}

// key returns the eating time slot of a personal slot.
func (s *PlanSlot) key() plans.Slot {
	return plans.Slot{PersonID: s.People[0].ID, EatingTimeID: s.EatingTimeID, Date: s.Date}
}

// Tag returns the slot's meal tag, like "breakfast" or "snack" (see
// FitsMeal).
func (s *PlanSlot) Tag() string {
	var et *people.EatingTime
	if s.personal() {
		et = s.People[0].EatingTime(s.EatingTimeID)
	}
	return slotTag(s.Meal, et)
}

// PlanResult is a suggested plan.
type PlanResult struct {
	// Slots are the planned slots, in the order of the input's.
	Slots []*PlannedSlot
	// Score is the sum of the slots' scores.
	Score float64
	// Rollouts is how many plans were tried.
	Rollouts int
}

// Plan suggests recipes for the slots, as a whole plan. See the comment
// above for how.
func Plan(in *PlanInput) *PlanResult {
	now := in.Now
	if now == nil {
		now = time.Now
	}
	budget := cmp.Or(in.Budget, DefaultPlanBudget)
	maxRollouts := cmp.Or(in.Rollouts, DefaultPlanRollouts)
	p := &planner{in: in, nutrition: map[pgtype.UUID]*nutrition.Recipe{}, eligible: map[string][]*recipes.Recipe{}}
	for _, r := range in.Recipes {
		p.nutrition[r.ID] = r.Nutrition()
	}
	all := in.Candidates
	if all == nil {
		all = in.Recipes
	}
	for _, slot := range in.Slots {
		tag := slot.Tag()
		if _, ok := p.eligible[tag]; ok {
			continue
		}
		p.eligible[tag] = []*recipes.Recipe{}
		for _, r := range all {
			if FitsMeal(r, tag) {
				p.eligible[tag] = append(p.eligible[tag], r)
			}
		}
		// A small meal takes the snack recipes, if there are any, as the
		// rest of an earlier meal is the other choice there.
		if tag == "snack" {
			var snacks []*recipes.Recipe
			for _, r := range p.eligible[tag] {
				if slices.Contains(recipeMealTags(r), "snack") {
					snacks = append(snacks, r)
				}
			}
			if len(snacks) > 0 {
				p.eligible[tag] = snacks
			}
		}
	}

	deadline := now().Add(budget)
	var best []*Suggestion
	bestScore := math.Inf(-1)
	result := &PlanResult{}
	sinceBetter := 0
	for result.Rollouts < maxRollouts && sinceBetter < PlanPatience {
		// The first rollout is greedy, the rest draw.
		picks, score := p.rollout(result.Rollouts > 0)
		result.Rollouts++
		sinceBetter++
		if score > bestScore {
			best, bestScore = picks, score
			sinceBetter = 0
		}
		if !now().Before(deadline) || p.stopped() {
			break
		}
	}
	result.Slots, result.Score = p.polish(best)
	return result
}

// stopped reports whether the input's Done is closed.
func (p *planner) stopped() bool {
	select {
	case <-p.in.Done:
		return true
	default:
		return false
	}
}

// planner plans the slots, with what's computed once for all rollouts.
type planner struct {
	in *PlanInput
	// nutrition is each recipe's nutrition, by ID.
	nutrition map[pgtype.UUID]*nutrition.Recipe
	// eligible are the candidates each meal tag allows.
	eligible map[string][]*recipes.Recipe
}

// rollout plans the slots in order, drawing each slot's recipe if draw,
// and otherwise taking the best. It returns the suggestions picked, nil
// for a slot without any, and the plan's score.
func (p *planner) rollout(draw bool) ([]*Suggestion, float64) {
	st := p.newState()
	picks := make([]*Suggestion, len(p.in.Slots))
	var total float64
	for i, slot := range p.in.Slots {
		if st.filled(slot) {
			continue
		}
		ranked := Suggest(st.input(slot, p.candidates(slot)))
		sg := p.pick(preferred(slot, ranked), draw)
		if sg == nil {
			continue
		}
		picks[i] = sg
		total += sg.Score
		st.add(slot, sg)
	}
	return picks, total
}

// candidates returns the recipes a slot picks from: its fixed recipe, none
// for a fixed leftover, or the input's candidates that its meal tag
// allows.
func (p *planner) candidates(slot *PlanSlot) []*recipes.Recipe {
	switch {
	case slot.Fixed != nil:
		return []*recipes.Recipe{slot.Fixed}
	case slot.FixedLeftover != nil:
		return []*recipes.Recipe{}
	}
	return p.eligible[slot.Tag()]
}

// pick picks a slot's recipe from its suggestions, best first: one that
// breaks no rule if there is any, the best if not draw, and otherwise one
// of the best PlanChoices, drawn by score.
func (p *planner) pick(ranked []*Suggestion, draw bool) *Suggestion {
	choices := fitting(ranked)
	if len(choices) == 0 {
		return nil
	}
	if !draw || len(choices) == 1 {
		return choices[0]
	}
	choices = choices[:min(len(choices), PlanChoices)]
	weights := make([]float64, len(choices))
	var sum float64
	for i, sg := range choices {
		weights[i] = math.Exp((sg.Score - choices[0].Score) / PlanTemperature)
		sum += weights[i]
	}
	x := p.in.Rand.Float64() * sum
	for i, w := range weights {
		if x < w {
			return choices[i]
		}
		x -= w
	}
	return choices[len(choices)-1]
}

// preferred returns the suggestions a slot picks from: at a small meal,
// those of recipes tagged "snack", if any break no rule, and otherwise
// all of them, leftovers too.
func preferred(slot *PlanSlot, ranked []*Suggestion) []*Suggestion {
	if slot.Tag() != "snack" {
		return ranked
	}
	var snacks []*Suggestion
	fits := false
	for _, sg := range ranked {
		if sg.Leftover == nil && slices.Contains(recipeMealTags(sg.Recipe), "snack") {
			snacks = append(snacks, sg)
			fits = fits || sg.FitsRules
		}
	}
	if !fits {
		return ranked
	}
	return snacks
}

// fitting returns the suggestions that break no rule, or all of them if
// none do.
func fitting(ranked []*Suggestion) []*Suggestion {
	var fits []*Suggestion
	for _, sg := range ranked {
		if sg.FitsRules {
			fits = append(fits, sg)
		}
	}
	if len(fits) == 0 {
		return ranked
	}
	return fits
}

// polish scores each slot of a plan against all its other slots, and
// swaps in a better recipe where there's one, in a single pass. It returns
// the planned slots, with their alternatives, and the plan's score.
func (p *planner) polish(picks []*Suggestion) ([]*PlannedSlot, float64) {
	alternatives := cmp.Or(p.in.Alternatives, DefaultAlternatives)
	slots := make([]*PlannedSlot, len(p.in.Slots))
	var total float64
	for i, slot := range p.in.Slots {
		// The other slots are planned with their picks' portions, so the
		// slot is scored against them all, later ones too, without scoring
		// them again.
		st := p.newState()
		for j, other := range p.in.Slots {
			if j != i && picks[j] != nil {
				st.add(other, picks[j])
			}
		}
		planned := &PlannedSlot{Slot: slot, Eligible: len(p.candidates(slot))}
		if slot.FixedLeftover != nil {
			planned.Eligible = 1
		}
		slots[i] = planned
		if st.filled(slot) {
			planned.FilledByMeal = true
			picks[i] = nil
			continue
		}
		ranked := Suggest(st.input(slot, p.candidates(slot)))
		for _, sg := range ranked {
			if sg.FitsRules {
				planned.Fitting++
			}
		}
		// The pick is kept if it's still among the slot's preferred
		// suggestions, and a better one that breaks no more rules replaces
		// it, like a final greedy step.
		choices := preferred(slot, ranked)
		var current *Suggestion
		for _, sg := range choices {
			if picks[i] != nil && sg.Same(picks[i]) {
				current = sg
			}
		}
		if best := p.pick(choices, false); best != nil && (current == nil || (best.Score > current.Score && (best.FitsRules || !current.FitsRules))) {
			current = best
		}
		if current == nil {
			continue
		}
		picks[i] = current
		planned.Suggestion = current
		total += current.Score
		for _, sg := range fitting(ranked) {
			if len(planned.Alternatives) == alternatives {
				break
			}
			if sg != current {
				planned.Alternatives = append(planned.Alternatives, sg)
			}
		}
	}
	return slots, math.Round(total*10) / 10
}

// state is a plan being made: the input's plan with the cooks and
// portions a rollout added.
type state struct {
	p        *planner
	cooks    []*plans.Cook
	portions []*plans.Portion
	// planned is what's planned at each slot, and owned whether it's the
	// state's own copy rather than the input's, which every rollout
	// shares.
	planned map[plans.Slot]*rules.EatingTime
	owned   bool
}

func (p *planner) newState() *state {
	return &state{
		p:        p,
		cooks:    slices.Clone(p.in.Plan.Cooks),
		portions: slices.Clone(p.in.Plan.Portions),
		planned:  p.in.Planned,
	}
}

// input returns the input of scoring the candidates for a slot, given the
// state's plan.
func (st *state) input(slot *PlanSlot, candidates []*recipes.Recipe) *Input {
	monday := slot.Date.AddDate(0, 0, -int(slot.Date.Weekday()+6)%7)
	sunday := monday.AddDate(0, 0, 6)
	week := &plans.Plan{From: monday, To: sunday, Staples: st.p.in.Plan.Staples}
	for _, c := range st.cooks {
		if !c.Date.Before(monday) && !c.Date.After(sunday) {
			week.Cooks = append(week.Cooks, c)
		}
	}
	for _, portion := range st.portions {
		if !portion.Date.Before(monday) && !portion.Date.After(sunday) {
			week.Portions = append(week.Portions, portion)
		}
	}
	return &Input{
		Date:         slot.Date,
		Meal:         slot.Meal,
		People:       slot.People,
		EatingTimeID: slot.EatingTimeID,
		ShopDate:     st.p.in.ShopDate(slot.Date),
		Week:         week,
		Cooks:        st.cooks,
		Planned:      st.planned,
		Recipes:      st.p.in.Recipes,
		Candidates:   candidates,
		Foods:        st.p.in.Foods,
		Lots:         st.p.in.Lots,
		Weights:      st.p.in.Weights,
		// A slot of a recipe the user chose takes no leftover.
		Leftovers:  slot.personal() && slot.Fixed == nil,
		LeftoverOf: slot.FixedLeftover,
	}
}

// filled reports whether a person's eating time is filled by a shared meal
// the state planned, which spills into it. What the plan had there before
// doesn't count, as the slot is then planned already, and not planned
// again.
func (st *state) filled(slot *PlanSlot) bool {
	if !slot.personal() || !st.owned {
		return false
	}
	et, ok := st.planned[slot.key()]
	if !ok {
		return false
	}
	before, had := st.p.in.Planned[slot.key()]
	return !had || et.Amount.Grams > before.Amount.Grams
}

// add plans a suggestion at a slot: its cook, or for a leftover, the
// servings it adds to the state's cook of it, its portions, and what they
// add to their eating times. A leftover of a cook the state doesn't have,
// as another recipe replaced it, isn't planned.
func (st *state) add(slot *PlanSlot, sg *Suggestion) {
	r := sg.Recipe
	var cook *plans.Cook
	if ref := sg.LeftoverRef(); ref != nil {
		i := slices.IndexFunc(st.cooks, ref.Matches)
		if i < 0 {
			return
		}
		// The cook is copied, as the input's and other states share it;
		// the slice is the state's own.
		grown := *st.cooks[i]
		grown.Servings += sg.CookServings
		st.cooks[i] = &grown
		cook = &grown
	} else {
		cook = &plans.Cook{
			ID:         store.NewUUID(),
			RecipeID:   r.ID,
			RecipeName: r.Name,
			Date:       slot.Date,
			Servings:   sg.CookServings,
			Meal:       slot.Meal,
		}
		st.cooks = append(st.cooks, cook)
	}
	// The planned eating times are copied before the first change, once
	// per state.
	if !st.owned {
		st.planned = maps.Clone(st.planned)
		if st.planned == nil {
			st.planned = map[plans.Slot]*rules.EatingTime{}
		}
		st.owned = true
	}
	planned := st.planned
	n := st.p.nutrition[r.ID]
	for _, portion := range sg.Portions {
		st.portions = append(st.portions, &plans.Portion{
			ID:           store.NewUUID(),
			PersonID:     portion.PersonID,
			EatingTimeID: portion.EatingTimeID,
			Date:         portion.Date,
			CookID:       cook.ID,
			Servings:     portion.Servings,
			Name:         r.Name,
			CookDate:     cook.Date,
			CookRecipeID: r.ID,
		})
		et := &rules.EatingTime{}
		if portion.EatingTime != nil {
			et.Name = portion.EatingTime.Name
		}
		if old, ok := planned[portion.Slot]; ok {
			*et = *old
			et.FastCarbFoods = slices.Clone(old.FastCarbFoods)
			et.UnweighedNames = slices.Clone(old.UnweighedNames)
		}
		if n != nil {
			amount := n.Servings(portion.Servings)
			et.Amount.Add(&amount)
			if amount.UnknownWeight && !slices.Contains(et.UnweighedNames, r.Name) {
				et.UnweighedNames = append(et.UnweighedNames, r.Name)
			}
		}
		for _, in := range r.IngredientsFor(float64(r.Servings)) {
			if in.FastCarb && !slices.Contains(et.FastCarbFoods, in.FoodName) {
				et.FastCarbFoods = append(et.FastCarbFoods, in.FoodName)
			}
		}
		planned[portion.Slot] = et
	}
}
