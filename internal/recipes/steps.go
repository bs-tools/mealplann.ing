package recipes

import (
	"encoding/json"
	"fmt"
	"slices"
)

// Step is a step of a recipe's instructions, stored as JSON.
type Step struct {
	// Text says what to do.
	Text string `json:"text"`
	// Section is the part of the recipe it's in, e.g. "For the sauce",
	// empty if none.
	Section string `json:"section,omitempty"`
	// Ingredients are the ingredients it uses, by their position in the
	// recipe's ingredients.
	Ingredients []int `json:"ingredients,omitempty"`
	// Amounts are how much of each of Ingredients it uses, in the
	// ingredient's unit, 0 for all of it, or the rest after other steps'
	// amounts. Empty for all of each.
	Amounts []float64 `json:"amounts,omitempty"`
	// DurationSeconds is how long it takes, 0 if unknown.
	DurationSeconds int32 `json:"duration_seconds,omitempty"`
	// Waiting is whether it's waiting time, when the cook is free, like
	// simmering or baking, rather than hands-on work.
	Waiting bool `json:"waiting,omitempty"`
	// After are the steps it starts after, by index, all before it. Empty
	// for the step just before it, unless FromStart.
	After []int `json:"after,omitempty"`
	// FromStart is whether it starts right away, after no other step.
	FromStart bool `json:"from_start,omitempty"`
	// StartOffsetSeconds, if more than 0, is how long after the steps it
	// follows start it starts, from the latest start of them, rather than
	// once they're done, like pasta 10 minutes into a simmer; with no steps
	// to follow, after the recipe starts.
	StartOffsetSeconds int32 `json:"start_offset_seconds,omitempty"`
	// EndBeforeSeconds, if not nil, is how long before the latest end of
	// the steps it follows it ends, rather than starting after them, like
	// the oven preheated 3 minutes before the marinating is done.
	EndBeforeSeconds *int32 `json:"end_before_seconds,omitempty"`
}

// Amount returns how much of the ingredient at a position the step uses,
// 0 for all of it, or the rest.
func (s *Step) Amount(position int) float64 {
	i := slices.Index(s.Ingredients, position)
	if i < 0 || i >= len(s.Amounts) {
		return 0
	}
	return s.Amounts[i]
}

// Dependencies returns the steps a step starts after, by index: its After,
// or the step just before it, or none if it's the first or FromStart.
func (s *Step) Dependencies(index int) []int {
	switch {
	case s.FromStart || index == 0:
		return nil
	case len(s.After) > 0:
		return s.After
	default:
		return []int{index - 1}
	}
}

// StepTexts returns the texts of the steps.
func StepTexts(steps []*Step) []string {
	texts := make([]string, 0, len(steps))
	for _, s := range steps {
		texts = append(texts, s.Text)
	}
	return texts
}

// instructionsToDB returns instructions as JSON.
func instructionsToDB(steps []*Step) (json.RawMessage, error) {
	if steps == nil {
		steps = []*Step{}
	}
	data, err := json.Marshal(steps)
	if err != nil {
		return nil, fmt.Errorf("failed to encode instructions: %w", err)
	}
	return data, nil
}

// instructionsFromDB returns instructions from their JSON, empty for
// none.
func instructionsFromDB(data json.RawMessage) ([]*Step, error) {
	var steps []*Step
	if err := json.Unmarshal(data, &steps); err != nil {
		return nil, fmt.Errorf("failed to decode instructions: %w", err)
	}
	if steps == nil {
		steps = []*Step{}
	}
	return steps, nil
}

// DefaultStepSeconds is how long a timeline assumes a step of unknown
// duration takes: about what reading it and doing a simple task does.
const DefaultStepSeconds = 5 * 60

// Timed reports whether any of the recipe's steps says how long it takes,
// so its timeline, not its own prep and cook times, says how long it
// takes.
func (r *Recipe) Timed() bool {
	for _, s := range r.Instructions {
		if s.DurationSeconds > 0 {
			return true
		}
	}
	return false
}

// DeriveTimes sets a timed recipe's prep and cook times from its
// timeline: the hands-on time, and the waiting time, so they add up to its
// total and can't disagree with its steps. A recipe whose steps don't say
// how long they take keeps the times it was given.
func (r *Recipe) DeriveTimes() {
	if !r.Timed() {
		return
	}
	t := r.Timeline()
	r.PrepSeconds = t.HandsOnSeconds
	r.CookSeconds = t.TotalSeconds - t.HandsOnSeconds
}

// TimedStep is a step on a recipe's timeline.
type TimedStep struct {
	Index int
	// StartSeconds and EndSeconds are from the start of the recipe.
	StartSeconds, EndSeconds int32
	// Estimated is whether its duration is unknown, and assumed to be
	// DefaultStepSeconds.
	Estimated bool
}

// Timeline is when a recipe's steps happen, for one cook.
type Timeline struct {
	Steps []TimedStep
	// TotalSeconds is from the first step's start to the last step's end,
	// and HandsOnSeconds the time the cook works, not waiting.
	TotalSeconds, HandsOnSeconds int32
}

// Timeline schedules the steps for one cook. Each starts once the steps it
// follows are done, or StartOffsetSeconds after they start. Waiting steps,
// like simmering, start as soon as they can, as they don't keep the cook
// busy. Hands-on steps need the cook: whenever the cook is free, the ready
// step on the longest chain to the end goes first, so a step that starts
// long waiting, like boiling water, isn't stuck behind chopping that can
// wait, and hands-on work fills the waiting time. Steps on chains equally
// long go in the recipe's order.
func (r *Recipe) Timeline() *Timeline {
	steps := r.Instructions
	n := len(steps)
	t := &Timeline{Steps: make([]TimedStep, n)}
	durations := make([]int32, n)
	deps := make([][]int, n)
	next := make([][]int, n)
	for i, s := range steps {
		durations[i], t.Steps[i].Estimated = s.DurationSeconds, false
		if durations[i] <= 0 {
			durations[i], t.Steps[i].Estimated = DefaultStepSeconds, true
		}
		t.Steps[i].Index = i
		for _, dep := range s.Dependencies(i) {
			if dep >= 0 && dep < i {
				deps[i] = append(deps[i], dep)
				next[dep] = append(next[dep], i)
			}
		}
	}
	// A step's chain is the longest time from its start to the end of the
	// recipe through the steps that follow it. link is how long after a
	// step starts one that follows it starts.
	link := func(i, j int) int32 {
		switch {
		case steps[j].EndBeforeSeconds != nil:
			return durations[i] - *steps[j].EndBeforeSeconds - durations[j]
		case steps[j].StartOffsetSeconds > 0:
			return steps[j].StartOffsetSeconds
		default:
			return durations[i]
		}
	}
	chain := make([]int32, n)
	for i := n - 1; i >= 0; i-- {
		chain[i] = durations[i]
		for _, j := range next[i] {
			chain[i] = max(chain[i], link(i, j)+chain[j])
		}
	}
	scheduled := make([]bool, n)
	// ready returns when a step can start, given the steps it follows, and
	// whether they're all scheduled.
	ready := func(i int) (int32, bool) {
		offset := steps[i].StartOffsetSeconds
		at := max(offset, 0)
		var lastEnd int32
		for _, dep := range deps[i] {
			if !scheduled[dep] {
				return 0, false
			}
			lastEnd = max(lastEnd, t.Steps[dep].EndSeconds)
			if offset > 0 {
				at = max(at, t.Steps[dep].StartSeconds+offset)
			} else {
				at = max(at, t.Steps[dep].EndSeconds)
			}
		}
		// A step ending before the steps it follows end starts its
		// duration before that, but not before the recipe starts.
		if before := steps[i].EndBeforeSeconds; before != nil && len(deps[i]) > 0 {
			at = max(lastEnd-*before-durations[i], 0)
		}
		return at, true
	}
	schedule := func(i int, start int32) {
		scheduled[i] = true
		t.Steps[i].StartSeconds = start
		t.Steps[i].EndSeconds = start + durations[i]
		t.TotalSeconds = max(t.TotalSeconds, start+durations[i])
	}
	var cookFree int32
	for left := n; left > 0; {
		// Waiting steps start as soon as they can, which may let others.
		for progress := true; progress; {
			progress = false
			for i, s := range steps {
				if scheduled[i] || !s.Waiting {
					continue
				}
				if at, ok := ready(i); ok {
					schedule(i, at)
					left--
					progress = true
				}
			}
		}
		// Then the cook does the ready hands-on step that can start
		// first, on the longest chain if several can.
		best, bestStart := -1, int32(0)
		for i, s := range steps {
			if scheduled[i] || s.Waiting {
				continue
			}
			at, ok := ready(i)
			if !ok {
				continue
			}
			start := max(at, cookFree)
			if best < 0 || start < bestStart || start == bestStart && chain[i] > chain[best] {
				best, bestStart = i, start
			}
		}
		if best < 0 {
			break
		}
		schedule(best, bestStart)
		cookFree = bestStart + durations[best]
		t.HandsOnSeconds += durations[best]
		left--
	}
	return t
}

// UsesIngredient reports whether a step uses the ingredient at a position.
func (s *Step) UsesIngredient(position int) bool {
	return slices.Contains(s.Ingredients, position)
}
