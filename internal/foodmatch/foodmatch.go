// Package foodmatch matches ingredient names, like "firm tofu, pressed", to
// the household's foods, like "Tofu, firm", by how alike their words are,
// for importing recipes: word order, plurals, punctuation and small typos
// don't matter.
package foodmatch

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// Candidate is a food an ingredient may be.
type Candidate[T any] struct {
	// Name is the food's name.
	Name string
	// Value is what the caller identifies the food with.
	Value T
	// Score is how alike the names are, from 0 to 1 for the same words.
	Score float64
}

// MinScore is the lowest score worth offering as a match.
const MinScore = 0.35

// Matcher matches names to a set of foods, whose words it prepares once.
type Matcher[T any] struct {
	foods []food[T]
}

type food[T any] struct {
	name     string
	value    T
	words    []string
	trigrams map[string]bool
}

// New returns a matcher of the foods named, with their values.
func New[T any](names []string, values []T) *Matcher[T] {
	m := &Matcher[T]{foods: make([]food[T], 0, len(names))}
	for i, name := range names {
		words := Words(name)
		m.foods = append(m.foods, food[T]{name: name, value: values[i], words: words, trigrams: trigrams(words)})
	}
	return m
}

// Match returns the foods most like a name, best first, at most max, with
// a score of at least MinScore.
func (m *Matcher[T]) Match(name string, max int) []Candidate[T] {
	words := Words(name)
	if len(words) == 0 {
		return nil
	}
	grams := trigrams(words)
	var found []Candidate[T]
	for _, f := range m.foods {
		if s := score(words, grams, f.words, f.trigrams); s >= MinScore {
			found = append(found, Candidate[T]{Name: f.name, Value: f.value, Score: s})
		}
	}
	slices.SortStableFunc(found, func(a, b Candidate[T]) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(len(a.Name), len(b.Name)), strings.Compare(a.Name, b.Name))
	})
	return found[:min(len(found), max)]
}

// score is how alike two names' words are: mostly how many words one
// shares with the shorter, like "chickpeas" with "Chickpeas, canned",
// then all their words in common, with their letters for typos.
func score(a []string, aGrams map[string]bool, b []string, bGrams map[string]bool) float64 {
	shared := 0
	for _, w := range a {
		if slices.Contains(b, w) {
			shared++
		}
	}
	union := len(a) + len(b) - shared
	overlap := float64(shared) / float64(min(len(a), len(b)))
	jaccard := float64(shared) / float64(union)
	words := 0.8*overlap + 0.2*jaccard
	return max(words, 0.9*dice(aGrams, bGrams))
}

// dice is how many trigrams two sets share, from 0 to 1.
func dice(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for g := range a {
		if b[g] {
			shared++
		}
	}
	return 2 * float64(shared) / float64(len(a)+len(b))
}

// trigrams returns the letter trigrams of words, each padded with spaces.
func trigrams(words []string) map[string]bool {
	grams := map[string]bool{}
	for _, w := range words {
		padded := " " + w + " "
		runes := []rune(padded)
		for i := 0; i+3 <= len(runes); i++ {
			grams[string(runes[i:i+3])] = true
		}
	}
	return grams
}

// fillers are words of ingredients that don't say what food it is.
var fillers = map[string]bool{
	"a": true, "an": true, "and": true, "of": true, "the": true, "or": true, "to": true, "for": true,
	"fresh": true, "freshly": true, "large": true, "small": true, "medium": true, "chopped": true,
	"diced": true, "minced": true, "sliced": true, "grated": true, "finely": true, "roughly": true,
	"peeled": true, "drained": true, "rinsed": true, "optional": true, "taste": true, "about": true,
	"cup": true, "cups": true, "tbsp": true, "tsp": true,
}

// Words returns a name's words, lowercase, singular, and without fillers,
// like "onion" and "red" for "2 large red onions, diced".
func Words(name string) []string {
	fields := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	var words []string
	for _, f := range fields {
		if len(f) < 2 || fillers[f] {
			continue
		}
		w := singular(f)
		if !slices.Contains(words, w) {
			words = append(words, w)
		}
	}
	return words
}

// singular returns a word's likely singular, like "tomato" for
// "tomatoes" and "berry" for "berries", or the word.
func singular(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 4 && (strings.HasSuffix(w, "oes") || strings.HasSuffix(w, "shes") || strings.HasSuffix(w, "ches")):
		return w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us"):
		return w[:len(w)-1]
	default:
		return w
	}
}
