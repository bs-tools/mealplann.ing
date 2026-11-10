package recipeimport

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
)

// vulgarFractions are the fraction characters recipes write.
var vulgarFractions = map[rune]float64{
	'¼': 0.25, '½': 0.5, '¾': 0.75, '⅓': 1.0 / 3, '⅔': 2.0 / 3, '⅛': 0.125,
	'⅜': 0.375, '⅝': 0.625, '⅞': 0.875, '⅕': 0.2,
}

// quantity matches an amount at the start of an ingredient: a whole or
// decimal number, a fraction, or a mixed number, like "2", "1.5", "1/2",
// "1 1/2", "½" or "1½", then optionally the top of a range, like "2-3",
// which is left out.
var quantity = regexp.MustCompile(`^(\d+(?:[.,]\d+)?(?:\s*[¼½¾⅓⅔⅛⅜⅝⅞⅕]|\s+\d+/\d+|/\d+)?|[¼½¾⅓⅔⅛⅜⅝⅞⅕])(?:\s*(?:-|–|to)\s*(?:\d+(?:[.,]\d+)?(?:/\d+)?|[¼½¾⅓⅔⅛⅜⅝⅞⅕]))?\s*`)

// unitWords are the units recipes write, by their lowercase spellings.
// The base units are given as kitchen units of size 1, so both are one
// lookup.
var unitWords = map[string]unitWord{}

// unitWord is a unit an ingredient is written in.
type unitWord struct {
	base    nutrition.Unit
	kitchen nutrition.KitchenUnit
}

func init() {
	add := func(u unitWord, words ...string) {
		for _, w := range words {
			unitWords[w] = u
		}
	}
	add(unitWord{base: nutrition.Grams}, "g", "gr", "gram", "grams", "gramme", "grammes")
	add(unitWord{base: nutrition.Milliliters}, "ml", "milliliter", "milliliters", "millilitre", "millilitres")
	add(unitWord{base: nutrition.Packages}, "block", "blocks", "package", "packages", "pkg", "pack", "packs", "can", "cans", "tin", "tins")
	add(unitWord{kitchen: nutrition.Kilograms}, "kg", "kilogram", "kilograms", "kilo", "kilos")
	add(unitWord{kitchen: nutrition.Liters}, "l", "liter", "liters", "litre", "litres")
	add(unitWord{kitchen: nutrition.Teaspoons}, "tsp", "tsps", "teaspoon", "teaspoons", "tsp.")
	add(unitWord{kitchen: nutrition.Tablespoons}, "tbsp", "tbsps", "tbs", "tablespoon", "tablespoons", "tbsp.", "tbl")
	add(unitWord{kitchen: nutrition.Cups}, "cup", "cups", "c.")
	add(unitWord{kitchen: nutrition.Ounces}, "oz", "ounce", "ounces", "oz.")
	add(unitWord{kitchen: nutrition.Pounds}, "lb", "lbs", "pound", "pounds", "lb.", "lbs.")
}

// unitPrefix matches a unit word at the start of the rest of an
// ingredient, directly after its amount too, like "250g".
var unitPrefix = regexp.MustCompile(`^([a-zA-Z]+\.?)(?:\s+|$|[,(])`)

// ParseIngredient reads an ingredient line, like "1 ½ cups almond milk,
// unsweetened", as an unmatched ingredient of a draft, with its amount
// and unit if it has them, and returns the name of its likely food, like
// "almond milk". It returns nil for an empty line.
func ParseIngredient(line string) (*recipes.Ingredient, string) {
	line = truncate(cleanText(line), maxOriginalLength)
	if line == "" {
		return nil, ""
	}
	in := &recipes.Ingredient{OriginalText: line}
	rest := line
	if m := quantity.FindStringSubmatch(rest); m != nil {
		if amount, ok := parseQuantity(m[1]); ok && amount > 0 {
			rest = rest[len(m[0]):]
			in.Amount, in.Unit = amount, nutrition.Pieces
			if u := unitPrefix.FindStringSubmatch(rest); u != nil {
				if w, ok := unitWords[strings.ToLower(u[1])]; ok {
					rest = strings.TrimLeft(rest[len(u[1]):], " ")
					if w.kitchen != nutrition.NoKitchenUnit {
						base, size := w.kitchen.Base()
						in.Amount, in.Unit, in.KitchenUnit = amount*size, base, w.kitchen
					} else {
						in.Unit = w.base
					}
				}
			}
		}
	}
	return in, foodName(rest)
}

// parseQuantity parses an amount: a number, with a comma as the decimal
// point too, a fraction, a mixed number, or a fraction character.
func parseQuantity(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	var total float64
	var digits strings.Builder
	flush := func() bool {
		if digits.Len() == 0 {
			return true
		}
		part := digits.String()
		digits.Reset()
		if num, den, ok := strings.Cut(part, "/"); ok {
			n, err1 := strconv.ParseFloat(num, 64)
			d, err2 := strconv.ParseFloat(den, 64)
			if err1 != nil || err2 != nil || d == 0 {
				return false
			}
			total += n / d
			return true
		}
		n, err := strconv.ParseFloat(part, 64)
		total += n
		return err == nil
	}
	for _, r := range s {
		switch {
		case vulgarFractions[r] > 0:
			if !flush() {
				return 0, false
			}
			total += vulgarFractions[r]
		case r == ' ':
			if !flush() {
				return 0, false
			}
		default:
			digits.WriteRune(r)
		}
	}
	return total, flush()
}

// countWords are words between an ingredient's amount and its food that
// aren't the food, like "cloves" of "3 cloves garlic" or "large" of "1
// large onion".
var countWords = map[string]bool{
	"bunch": true, "bunches": true, "can": true, "cans": true, "clove": true, "cloves": true,
	"handful": true, "handfuls": true, "heaped": true, "heaping": true, "large": true, "level": true,
	"medium": true, "piece": true, "pieces": true, "pinch": true, "pinches": true, "scant": true,
	"slice": true, "slices": true, "small": true, "sprig": true, "sprigs": true, "stalk": true,
	"stalks": true, "of": true,
}

// foodName returns the likely food of the rest of an ingredient, after
// its amount and unit: before a comma or parentheses, without words like
// "cloves" or "of" leading it, like "almond milk" of "of almond milk
// (unsweetened), cold".
func foodName(rest string) string {
	if i := strings.IndexAny(rest, ",("); i >= 0 {
		rest = rest[:i]
	}
	words := strings.Fields(rest)
	for len(words) > 1 && countWords[strings.ToLower(words[0])] {
		words = words[1:]
	}
	return strings.Join(words, " ")
}
