package suggest

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Pairing is a combination of foods that go well together, matched on the
// names of a recipe's ingredients.
type Pairing struct {
	// Name says what goes together, for the reason, e.g. "chickpeas and
	// cumin".
	Name string
	// Foods are the foods that make up the pairing, each a list of
	// alternative terms, like "chickpea" or "garbanzo". A recipe has the
	// pairing when each food matches one of its ingredients.
	Foods [][]string
}

// pairings is a small curated list of vegan flavor pairings, classics of
// the cuisines vegan home cooking borrows from. It's deliberately short:
// flavor is a minor bonus, as household taste matters more, and an AI agent
// can reason about flavor far beyond it.
//
// Terms are lowercase and match the start of a word in an ingredient's
// name, so "lentil" matches "Lentils, red" and "Green lentils", and "lime"
// matches "Limes" but not "Slime". Prefer the singular, and list
// alternative names, like British and American ones, as alternatives.
var pairings = []Pairing{
	{Name: "chickpeas and cumin", Foods: [][]string{{"chickpea", "garbanzo"}, {"cumin"}}},
	{Name: "chickpeas and tahini", Foods: [][]string{{"chickpea", "garbanzo"}, {"tahini", "sesame paste"}}},
	{Name: "chickpeas and spinach", Foods: [][]string{{"chickpea", "garbanzo"}, {"spinach"}}},
	{Name: "tofu, ginger and soy sauce", Foods: [][]string{{"tofu"}, {"ginger"}, {"soy sauce", "tamari", "shoyu"}}},
	{Name: "tofu and sesame", Foods: [][]string{{"tofu"}, {"sesame"}}},
	{Name: "tempeh and soy sauce", Foods: [][]string{{"tempeh"}, {"soy sauce", "tamari", "shoyu"}}},
	{Name: "lentils and tomato", Foods: [][]string{{"lentil"}, {"tomato"}}},
	{Name: "lentils and cumin", Foods: [][]string{{"lentil"}, {"cumin"}}},
	{Name: "coconut milk and curry", Foods: [][]string{{"coconut milk", "coconut cream"}, {"curry"}}},
	{Name: "coconut milk and lime", Foods: [][]string{{"coconut milk", "coconut cream"}, {"lime"}}},
	{Name: "coconut milk and ginger", Foods: [][]string{{"coconut milk", "coconut cream"}, {"ginger"}}},
	{Name: "spinach and garlic", Foods: [][]string{{"spinach"}, {"garlic"}}},
	{Name: "black beans and lime", Foods: [][]string{{"black bean"}, {"lime"}}},
	{Name: "black beans and cumin", Foods: [][]string{{"black bean"}, {"cumin"}}},
	{Name: "sweet potato and black beans", Foods: [][]string{{"sweet potato"}, {"black bean"}}},
	{Name: "kidney beans and chili", Foods: [][]string{{"kidney bean"}, {"chili", "chilli"}}},
	{Name: "tomato and basil", Foods: [][]string{{"tomato"}, {"basil"}}},
	{Name: "tomato and garlic", Foods: [][]string{{"tomato"}, {"garlic"}}},
	{Name: "mushrooms and thyme", Foods: [][]string{{"mushroom"}, {"thyme"}}},
	{Name: "mushrooms and garlic", Foods: [][]string{{"mushroom"}, {"garlic"}}},
	{Name: "peanuts and lime", Foods: [][]string{{"peanut"}, {"lime"}}},
	{Name: "peanuts and soy sauce", Foods: [][]string{{"peanut"}, {"soy sauce", "tamari", "shoyu"}}},
	{Name: "broccoli and garlic", Foods: [][]string{{"broccoli"}, {"garlic"}}},
	{Name: "cauliflower and turmeric", Foods: [][]string{{"cauliflower"}, {"turmeric"}}},
	{Name: "eggplant and miso", Foods: [][]string{{"eggplant", "aubergine"}, {"miso"}}},
	{Name: "eggplant and tomato", Foods: [][]string{{"eggplant", "aubergine"}, {"tomato"}}},
	{Name: "potatoes and rosemary", Foods: [][]string{{"potato"}, {"rosemary"}}},
	{Name: "carrots and ginger", Foods: [][]string{{"carrot"}, {"ginger"}}},
	{Name: "beets and walnuts", Foods: [][]string{{"beet"}, {"walnut"}}},
	{Name: "kale and lemon", Foods: [][]string{{"kale"}, {"lemon"}}},
	{Name: "oats and cinnamon", Foods: [][]string{{"oat"}, {"cinnamon"}}},
	{Name: "apple and cinnamon", Foods: [][]string{{"apple"}, {"cinnamon"}}},
	{Name: "banana and peanut butter", Foods: [][]string{{"banana"}, {"peanut butter"}}},
	{Name: "white beans and rosemary", Foods: [][]string{{"white bean", "cannellini", "butter bean"}, {"rosemary"}}},
}

// matchPairings returns the pairings of a recipe with ingredients of the
// names, in the list's order. Each food of a pairing must match an
// ingredient, and the matches must take at least two ingredients, so that
// a "coconut curry paste" alone isn't coconut milk and curry.
func matchPairings(names []string) []Pairing {
	lower := make([]string, len(names))
	for i, name := range names {
		lower[i] = strings.ToLower(name)
	}
	var matched []Pairing
	for _, p := range pairings {
		used := map[int]bool{}
		ok := true
		for _, alternatives := range p.Foods {
			found := false
			for i, name := range lower {
				if matchesAny(name, alternatives) {
					used[i] = true
					found = true
				}
			}
			if !found {
				ok = false
				break
			}
		}
		if ok && len(used) >= 2 {
			matched = append(matched, p)
		}
	}
	return matched
}

// matchesAny reports whether any of the terms starts a word of the
// lowercase name.
func matchesAny(name string, terms []string) bool {
	for _, term := range terms {
		if hasWordPrefix(name, term) {
			return true
		}
	}
	return false
}

// hasWordPrefix reports whether term occurs in name at the start of a word:
// at the start of the name, or after a character that isn't a letter.
func hasWordPrefix(name, term string) bool {
	for i := 0; i+len(term) <= len(name); {
		j := strings.Index(name[i:], term)
		if j < 0 {
			return false
		}
		at := i + j
		if before, _ := utf8.DecodeLastRuneInString(name[:at]); at == 0 || !unicode.IsLetter(before) {
			return true
		}
		i = at + 1
	}
	return false
}
