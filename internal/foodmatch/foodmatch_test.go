package foodmatch

import (
	"slices"
	"testing"
)

func TestMatch(t *testing.T) {
	t.Parallel()
	names := []string{
		"Tofu, firm", "Tofu, silken", "Chickpeas, canned", "Olive oil", "Sesame oil",
		"Onions", "Red onion", "Garlic", "Garlic powder", "Tomatoes, canned diced", "Basmati rice",
		"Blueberries, frozen", "Coconut milk",
	}
	m := New(names, names)
	tests := []struct {
		ingredient string
		// best is the best match, "" for none.
		best string
	}{
		{"firm tofu", "Tofu, firm"},
		{"chickpeas", "Chickpeas, canned"},
		{"extra virgin olive oil", "Olive oil"},
		{"onion", "Onions"},
		{"red onions", "Red onion"},
		{"garlic cloves", "Garlic"},
		{"canned tomatoes", "Tomatoes, canned diced"},
		{"basmatti rice", "Basmati rice"},
		{"blueberries", "Blueberries, frozen"},
		{"light coconut milk", "Coconut milk"},
		{"maple syrup", ""},
	}
	for _, tt := range tests {
		got := m.Match(tt.ingredient, 3)
		best := ""
		if len(got) > 0 {
			best = got[0].Value
		}
		if best != tt.best {
			t.Errorf("%q: got %v, want %q first", tt.ingredient, got, tt.best)
		}
	}
	// The other tofu is offered too, as an alternative.
	var tofus []string
	for _, c := range m.Match("firm tofu", 5) {
		tofus = append(tofus, c.Value)
	}
	if !slices.Contains(tofus, "Tofu, silken") {
		t.Errorf("got %v, want the silken tofu as an alternative", tofus)
	}
}

func TestWords(t *testing.T) {
	t.Parallel()
	if got, want := Words("2 large red onions, finely diced"), []string{"red", "onion"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := Words("Tomatoes & berries"), []string{"tomato", "berry"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
