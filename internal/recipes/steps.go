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

// PlainSteps returns the steps of a recipe in the old format, plain texts
// done one after the other, as instructions, so what reads instructions
// works for every recipe.
func PlainSteps(texts []string) []*Step {
	steps := make([]*Step, 0, len(texts))
	for _, t := range texts {
		steps = append(steps, &Step{Text: t})
	}
	return steps
}

// StepsOf returns a recipe's instructions, or its plain steps as
// instructions if it has none.
func (r *Recipe) StepsOf() []*Step {
	if r.Instructions != nil {
		return r.Instructions
	}
	return PlainSteps(r.Steps)
}

// instructionsToDB returns instructions as JSON, nil for none.
func instructionsToDB(steps []*Step) (json.RawMessage, error) {
	if steps == nil {
		return nil, nil
	}
	data, err := json.Marshal(steps)
	if err != nil {
		return nil, fmt.Errorf("failed to encode instructions: %w", err)
	}
	return data, nil
}

// instructionsFromDB returns instructions from their JSON, nil for none.
func instructionsFromDB(data json.RawMessage) ([]*Step, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
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

// Timeline schedules the steps for one cook, in order, each as soon as
// the steps it starts after are done. Hands-on steps also wait for the
// cook to be free, but waiting steps, like simmering, don't keep the cook
// busy, so hands-on steps overlap them.
func (r *Recipe) Timeline() *Timeline {
	steps := r.StepsOf()
	t := &Timeline{Steps: make([]TimedStep, 0, len(steps))}
	var cookFree int32
	for i, s := range steps {
		var start int32
		for _, dep := range s.Dependencies(i) {
			if dep >= 0 && dep < i {
				start = max(start, t.Steps[dep].EndSeconds)
			}
		}
		if !s.Waiting {
			start = max(start, cookFree)
		}
		duration, estimated := s.DurationSeconds, false
		if duration <= 0 {
			duration, estimated = DefaultStepSeconds, true
		}
		end := start + duration
		if !s.Waiting {
			cookFree = end
			t.HandsOnSeconds += duration
		}
		t.Steps = append(t.Steps, TimedStep{Index: i, StartSeconds: start, EndSeconds: end, Estimated: estimated})
		t.TotalSeconds = max(t.TotalSeconds, end)
	}
	return t
}

// UsesIngredient reports whether a step uses the ingredient at a position.
func (s *Step) UsesIngredient(position int) bool {
	return slices.Contains(s.Ingredients, position)
}
