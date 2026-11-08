package foods

import (
	"slices"
	"strings"
	"unicode"
)

// synonyms are names of foods the reference foods call otherwise, by the
// words of a name. FDC and CNF name foods in American and Canadian English,
// often by their botanical or trade name, so a search for "nori" or
// "garbanzo" would otherwise find nothing, or only branded products. Each
// group is searched as one: a query with any of its names matches foods
// with any of them. The names are stemmed like the descriptions, so
// singular names match plural ones.
var synonyms = [][]string{
	{"chickpea", "garbanzo"},
	{"nori", "laver", "seaweed"},
	{"kombu", "kelp"},
	{"wakame", "seaweed"},
	{"doenjang", "soybean paste", "miso"},
	{"gochujang", "chili paste"},
	{"ciabatta", "italian bread"},
	{"cilantro", "coriander"},
	{"scallion", "green onion", "spring onion"},
	{"eggplant", "aubergine"},
	{"zucchini", "courgette", "summer squash"},
	{"arugula", "rocket"},
	{"rutabaga", "swede"},
	{"bell pepper", "sweet pepper"},
	{"chili", "chile"},
	{"edamame", "green soybean"},
	{"navy bean", "haricot"},
	{"black eyed pea", "cowpea"},
	{"seitan", "wheat gluten"},
	{"tamari", "soy sauce"},
	{"panko", "bread crumbs"},
	{"soy milk", "soymilk", "soy beverage"},
	{"oat milk", "oat beverage"},
	{"almond milk", "almond beverage"},
	{"rice milk", "rice beverage"},
	{"coconut milk", "coconut beverage"},
}

// textQuery returns the Postgres full text query, for to_tsquery, matching
// descriptions with all of the query's words, where a word or words with
// synonyms match any of them. Only letters and digits are kept, so the
// result is always a valid query, and empty if the query has no words.
func textQuery(query string) string {
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var terms []string
	for i := 0; i < len(words); {
		group, n := synonymsAt(words[i:])
		if n == 0 {
			terms = append(terms, words[i])
			i++
			continue
		}
		alternatives := make([]string, 0, len(group))
		for _, name := range group {
			// The words of a name match in any order, as the sources put
			// the food first: "Bread, italian" for "italian bread".
			alternative := strings.Join(strings.Fields(name), " & ")
			if strings.Contains(alternative, " ") {
				alternative = "(" + alternative + ")"
			}
			alternatives = append(alternatives, alternative)
		}
		terms = append(terms, "("+strings.Join(alternatives, " | ")+")")
		i += n
	}
	return strings.Join(terms, " & ")
}

// synonymsAt returns the synonym group of the longest name the words
// start with, and the number of words of that name, or 0 if they start
// with none.
func synonymsAt(words []string) ([]string, int) {
	var best []string
	bestLength := 0
	for _, group := range synonyms {
		for _, name := range group {
			nameWords := strings.Fields(name)
			if len(nameWords) <= bestLength || len(nameWords) > len(words) {
				continue
			}
			if slices.EqualFunc(nameWords, words[:len(nameWords)], sameWord) {
				best, bestLength = group, len(nameWords)
			}
		}
	}
	return best, bestLength
}

// sameWord reports whether a query word is a synonym's word, singular or
// plural.
func sameWord(name, word string) bool {
	return word == name || word == name+"s" || word == name+"es"
}
