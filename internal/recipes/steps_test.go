package recipes

import (
	"slices"
	"testing"
)

func TestTimeline(t *testing.T) {
	t.Parallel()
	// Rice simmers while the vegetables are chopped and fried; the curry
	// simmers once both are done.
	r := &Recipe{Instructions: []*Step{
		{Text: "Rinse the rice and bring it to a boil", DurationSeconds: 300},
		{Text: "Simmer the rice", DurationSeconds: 1200, Waiting: true},
		{Text: "Chop the onions", DurationSeconds: 600, FromStart: true},
		{Text: "Fry the onions", DurationSeconds: 480, After: []int{2}},
		{Text: "Simmer the curry", DurationSeconds: 900, Waiting: true, After: []int{3}},
		{Text: "Serve", After: []int{1, 4}},
	}}
	tl := r.Timeline()
	var got [][2]int32
	for _, s := range tl.Steps {
		got = append(got, [2]int32{s.StartSeconds, s.EndSeconds})
	}
	// The curry's chain is the longest, so the cook chops first (0–10
	// min), rinses the rice (10–15), which simmers while the onions fry
	// (15–23), the curry simmers (23–38), and serving waits for it.
	want := [][2]int32{{600, 900}, {900, 2100}, {0, 600}, {900, 1380}, {1380, 2280}, {2280, 2580}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if tl.TotalSeconds != 2580 || tl.HandsOnSeconds != 300+600+480+300 {
		t.Errorf("got %d s in all and %d hands-on, want 2580 and %d", tl.TotalSeconds, tl.HandsOnSeconds, 300+600+480+300)
	}
	if !tl.Steps[5].Estimated || tl.Steps[0].Estimated {
		t.Errorf("got %+v, want only serving estimated", tl.Steps)
	}

	// Steps of unknown duration run one after the other, each estimated.
	plain := (&Recipe{Instructions: []*Step{{Text: "Mix"}, {Text: "Bake"}}}).Timeline()
	if plain.TotalSeconds != 2*DefaultStepSeconds || !plain.Steps[1].Estimated || plain.Steps[1].StartSeconds != DefaultStepSeconds {
		t.Errorf("got %+v, want two estimated steps in a row", plain)
	}
}

// TestTimelineFillsGaps checks that a hands-on step that can start right
// away isn't stuck behind earlier hands-on steps, but fills the cook's
// time while something waits.
func TestTimelineFillsGaps(t *testing.T) {
	t.Parallel()
	r := &Recipe{Instructions: []*Step{
		{Text: "Fry the onions", DurationSeconds: 300},
		{Text: "Simmer the sauce", DurationSeconds: 1800, Waiting: true},
		{Text: "Stir in the spinach", DurationSeconds: 120},
		{Text: "Boil water for the pasta", DurationSeconds: 120, FromStart: true},
		{Text: "Bring it to a boil", DurationSeconds: 600, Waiting: true},
		{Text: "Cook the pasta", DurationSeconds: 600, Waiting: true},
		{Text: "Serve", After: []int{2, 5}},
	}}
	tl := r.Timeline()
	// Frying leads the longest chain, so it's first (0–5 min), then the
	// cook puts the water on (5–7), not after the spinach at 35.
	if s := tl.Steps[3]; s.StartSeconds != 300 {
		t.Errorf("got the water on at %d s, want 300, while the sauce simmers", s.StartSeconds)
	}
	if tl.TotalSeconds != 300+1800+120+300 {
		t.Errorf("got %d s in all, want %d", tl.TotalSeconds, 300+1800+120+300)
	}
}

// TestTimelineOffset checks that a step can start partway through the step
// it follows.
func TestTimelineOffset(t *testing.T) {
	t.Parallel()
	r := &Recipe{Instructions: []*Step{
		{Text: "Simmer the stew", DurationSeconds: 1800, Waiting: true},
		{Text: "Add the dumplings", DurationSeconds: 120, StartOffsetSeconds: 1200},
		{Text: "Toast the spices", DurationSeconds: 60, FromStart: true, StartOffsetSeconds: 300},
		{Text: "Serve", After: []int{0, 1, 2}},
	}}
	tl := r.Timeline()
	var got [][2]int32
	for _, s := range tl.Steps {
		got = append(got, [2]int32{s.StartSeconds, s.EndSeconds})
	}
	want := [][2]int32{{0, 1800}, {1200, 1320}, {300, 360}, {1800, 2100}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestTimelineAnchors checks what offsets count from: start_offset_seconds
// from the latest start of the steps it follows, and end_before_seconds
// from their latest end, as in the agents' tofu recipe.
func TestTimelineAnchors(t *testing.T) {
	t.Parallel()
	before := int32(180)
	r := &Recipe{Instructions: []*Step{
		{Text: "Press the tofu", DurationSeconds: 300},
		{Text: "Blend the marinade", DurationSeconds: 180, FromStart: true},
		{Text: "Marinate", DurationSeconds: 900, Waiting: true, After: []int{0, 1}},
		{Text: "Preheat the oven", DurationSeconds: 600, Waiting: true, After: []int{2}, EndBeforeSeconds: &before},
		{Text: "Set the table", DurationSeconds: 60, After: []int{0, 1}, StartOffsetSeconds: 720},
		{Text: "Bake", DurationSeconds: 1500, Waiting: true, After: []int{2, 3}},
	}}
	tl := r.Timeline()
	var got [][2]int32
	for _, s := range tl.Steps {
		got = append(got, [2]int32{s.StartSeconds, s.EndSeconds})
	}
	// The cook presses (0–5 min) then blends (5–8), the tofu marinates
	// (8–23), the oven preheats to be ready 3 minutes before (10–20), the
	// table is set 12 minutes after blending starts (17–18), and baking
	// follows the marinating (23–48).
	want := [][2]int32{{0, 300}, {300, 480}, {480, 1380}, {600, 1200}, {1020, 1080}, {1380, 2880}}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestInstructionsJSON(t *testing.T) {
	t.Parallel()
	steps := []*Step{{Text: "Soak", DurationSeconds: 3600, Waiting: true, Ingredients: []int{0}}, {Text: "Cook", After: []int{0}}}
	data, err := instructionsToDB(steps)
	if err != nil {
		t.Fatal(err)
	}
	back, err := instructionsFromDB(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[0].DurationSeconds != 3600 || !back[0].UsesIngredient(0) || back[1].After[0] != 0 {
		t.Errorf("got %+v back from %s", back, data)
	}
	// No instructions are an empty array, as the column is required.
	if none, _ := instructionsToDB(nil); string(none) != "[]" {
		t.Errorf("got %s for no instructions, want []", none)
	}
	if empty, _ := instructionsFromDB([]byte("[]")); empty == nil {
		t.Error("got nil for empty instructions, want empty")
	}
}

// TestDeriveTimes checks that a recipe whose steps say how long they take
// gets its prep and cook times from its timeline, whatever it was given,
// and that one whose steps don't keeps its own.
func TestDeriveTimes(t *testing.T) {
	t.Parallel()
	r := &Recipe{PrepSeconds: 60, CookSeconds: 60, Instructions: []*Step{
		{Text: "Rinse the rice", DurationSeconds: 120},
		{Text: "Simmer the rice", DurationSeconds: 1500, Waiting: true},
	}}
	r.DeriveTimes()
	if r.PrepSeconds != 120 || r.CookSeconds != 1500 {
		t.Errorf("got %d s prep and %d s cook, want 120 and 1500, the timeline's", r.PrepSeconds, r.CookSeconds)
	}
	plain := &Recipe{PrepSeconds: 600, CookSeconds: 900, Instructions: []*Step{{Text: "Mix"}}}
	plain.DeriveTimes()
	if plain.PrepSeconds != 600 || plain.CookSeconds != 900 {
		t.Errorf("got %d s prep and %d s cook, want the 600 and 900 given", plain.PrepSeconds, plain.CookSeconds)
	}
}
