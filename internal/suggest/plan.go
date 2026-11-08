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
// it has any.
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

// PlanSlot is a shared meal on a date to plan.
type PlanSlot struct {
	// Date is at midnight UTC.
	Date time.Time
	Meal people.Meal
	// People are those who eat the meal, each with an eating time for it.
	People []*people.Person
	// Fixed is the recipe the user chose for the slot, nil to pick one.
	Fixed *recipes.Recipe
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
	p := &planner{in: in, nutrition: map[pgtype.UUID]*nutrition.Recipe{}}
	for _, r := range in.Recipes {
		p.nutrition[r.ID] = r.Nutrition()
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
}

// rollout plans the slots in order, drawing each slot's recipe if draw,
// and otherwise taking the best. It returns the suggestions picked, nil
// for a slot without any, and the plan's score.
func (p *planner) rollout(draw bool) ([]*Suggestion, float64) {
	st := p.newState()
	picks := make([]*Suggestion, len(p.in.Slots))
	var total float64
	for i, slot := range p.in.Slots {
		ranked := Suggest(st.input(slot, p.candidates(slot)))
		sg := p.pick(ranked, draw)
		if sg == nil {
			continue
		}
		picks[i] = sg
		total += sg.Score
		st.add(slot, sg)
	}
	return picks, total
}

// candidates returns the recipes a slot picks from: its fixed recipe, or
// the input's candidates.
func (p *planner) candidates(slot *PlanSlot) []*recipes.Recipe {
	if slot.Fixed != nil {
		return []*recipes.Recipe{slot.Fixed}
	}
	if p.in.Candidates != nil {
		return p.in.Candidates
	}
	return p.in.Recipes
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
		ranked := Suggest(st.input(slot, p.candidates(slot)))
		planned := &PlannedSlot{Slot: slot}
		for _, sg := range ranked {
			if sg.FitsRules {
				planned.Fitting++
			}
		}
		slots[i] = planned
		var current *Suggestion
		for _, sg := range ranked {
			if picks[i] != nil && sg.Recipe == picks[i].Recipe {
				current = sg
			}
		}
		// A better recipe that breaks no more rules replaces the pick,
		// like a final greedy step.
		if best := p.pick(ranked, false); best != nil && (current == nil || (best.Score > current.Score && (best.FitsRules || !current.FitsRules))) {
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
		Date:       slot.Date,
		Meal:       slot.Meal,
		People:     slot.People,
		ShopDate:   st.p.in.ShopDate(slot.Date),
		Week:       week,
		Cooks:      st.cooks,
		Planned:    st.planned,
		Recipes:    st.p.in.Recipes,
		Candidates: candidates,
		Foods:      st.p.in.Foods,
		Lots:       st.p.in.Lots,
		Weights:    st.p.in.Weights,
	}
}

// add plans a suggestion at a slot: its cook, its portions, and what they
// add to their eating times.
func (st *state) add(slot *PlanSlot, sg *Suggestion) {
	r := sg.Recipe
	cook := &plans.Cook{
		ID:         store.NewUUID(),
		RecipeID:   r.ID,
		RecipeName: r.Name,
		Date:       slot.Date,
		Servings:   sg.CookServings,
		Meal:       slot.Meal,
	}
	st.cooks = append(st.cooks, cook)
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
			CookDate:     slot.Date,
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
