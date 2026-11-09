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
	// The cook rinses the rice (0–5 min), chops while it simmers (5–15),
	// fries (15–23), the curry simmers (23–38), and serving waits for it.
	want := [][2]int32{{0, 300}, {300, 1500}, {300, 900}, {900, 1380}, {1380, 2280}, {2280, 2580}}
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
