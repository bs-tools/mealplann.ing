// Package recipeimport reads recipes from web pages, from the schema.org
// Recipe in their JSON-LD, which most recipe sites have for search
// engines, like WordPress's recipe plugins. Its ingredients are read as
// written, with their amounts and units parsed where they can be, for
// matching to the household's foods.
package recipeimport

import (
	"encoding/json"
	"errors"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bs-tools/mealplann.ing/internal/recipes"
)

// Limits on what's read, the recipe service's own.
const (
	maxIngredients    = 100
	maxSteps          = 100
	maxNameLength     = 200
	maxDescription    = 2000
	maxStepLength     = 2000
	maxOriginalLength = 500
	maxSeconds        = 7 * 24 * 3600
	maxServings       = 100
	defaultServings   = 4
)

// ErrNoRecipe is returned for a page without a recipe in its JSON-LD.
var ErrNoRecipe = errors.New("the page has no recipe data (schema.org Recipe in JSON-LD)")

// Imported is a recipe read from a page, as a draft whose ingredients
// aren't matched to foods yet.
type Imported struct {
	Recipe *recipes.Recipe
	// FoodNames are the likely food names of the ingredients, in their
	// order, like "almond milk" of "1 cup almond milk, unsweetened", to
	// match to the household's foods.
	FoodNames []string
}

// jsonLD matches the JSON-LD scripts of a page.
var jsonLD = regexp.MustCompile(`(?is)<script[^>]*type\s*=\s*["']?application/ld\+json["']?[^>]*>(.*?)</script>`)

// Parse reads the recipe of a page, at pageURL, failing with ErrNoRecipe if
// it has none.
func Parse(page []byte, pageURL string) (*Imported, error) {
	for _, m := range jsonLD.FindAllSubmatch(page, -1) {
		var v any
		if err := json.Unmarshal(m[1], &v); err != nil {
			// Some sites' JSON-LD is broken; another script may do.
			continue
		}
		if r := findRecipe(v, 0); r != nil {
			return fromSchema(r, pageURL), nil
		}
	}
	return nil, ErrNoRecipe
}

// findRecipe returns the first object of type Recipe in JSON-LD, which can
// be the object itself, in an array, or in its @graph.
func findRecipe(v any, depth int) map[string]any {
	if depth > 5 {
		return nil
	}
	switch v := v.(type) {
	case []any:
		for _, item := range v {
			if r := findRecipe(item, depth+1); r != nil {
				return r
			}
		}
	case map[string]any:
		if isType(v["@type"], "Recipe") {
			return v
		}
		if g, ok := v["@graph"]; ok {
			return findRecipe(g, depth+1)
		}
	}
	return nil
}

// isType reports whether an @type, a string or an array of them, is name.
func isType(t any, name string) bool {
	switch t := t.(type) {
	case string:
		return t == name
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok && s == name {
				return true
			}
		}
	}
	return false
}

func fromSchema(r map[string]any, pageURL string) *Imported {
	recipe := &recipes.Recipe{
		Name:        truncate(text(r["name"]), maxNameLength),
		Description: truncate(text(r["description"]), maxDescription),
		Servings:    servings(r["recipeYield"]),
		PrepSeconds: duration(r["prepTime"]),
		CookSeconds: duration(r["cookTime"]),
		SourceURL:   pageURL,
		Draft:       true,
	}
	if recipe.Name == "" {
		recipe.Name = "Imported recipe"
	}
	imported := &Imported{Recipe: recipe}
	for _, line := range strings_(r["recipeIngredient"]) {
		if len(recipe.Ingredients) == maxIngredients {
			break
		}
		in, name := ParseIngredient(line)
		if in == nil {
			continue
		}
		recipe.Ingredients = append(recipe.Ingredients, in)
		imported.FoodNames = append(imported.FoodNames, name)
	}
	for _, s := range steps(r["recipeInstructions"], "", 0) {
		if len(recipe.Instructions) == maxSteps {
			break
		}
		recipe.Instructions = append(recipe.Instructions, s)
	}
	return imported
}

// text returns a JSON-LD value as plain text: HTML entities decoded, tags
// removed and spaces collapsed.
func text(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return cleanText(s)
}

var (
	tags   = regexp.MustCompile(`<[^>]*>`)
	spaces = regexp.MustCompile(`\s+`)
)

func cleanText(s string) string {
	// Entities are decoded twice, as some sites encode them twice, like
	// "&amp;#8217;".
	s = html.UnescapeString(html.UnescapeString(tags.ReplaceAllString(s, " ")))
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// truncate shortens text to at most n characters.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n]))
}

// strings_ returns a JSON-LD value that's a string or an array of them as
// texts, without empty ones.
func strings_(v any) []string {
	var out []string
	switch v := v.(type) {
	case string:
		if t := cleanText(v); t != "" {
			out = append(out, t)
		}
	case []any:
		for _, item := range v {
			out = append(out, strings_(item)...)
		}
	}
	return out
}

var leadingInt = regexp.MustCompile(`\d+`)

// servings returns the servings of a recipeYield, like 4, "4", "4
// servings" or ["4", "4 servings"], or defaultServings if unknown.
func servings(v any) int32 {
	if n, ok := parseServings(v); ok {
		return n
	}
	return defaultServings
}

func parseServings(v any) (int32, bool) {
	switch v := v.(type) {
	case float64:
		if v >= 1 {
			return int32(min(math.Round(v), maxServings)), true
		}
	case string:
		if m := leadingInt.FindString(v); m != "" {
			if n, err := strconv.Atoi(m); err == nil && n >= 1 {
				return int32(min(n, maxServings)), true
			}
		}
	case []any:
		for _, item := range v {
			if n, ok := parseServings(item); ok {
				return n, true
			}
		}
	}
	return 0, false
}

// lineBreaks are the HTML line and paragraph breaks of a text.
var lineBreaks = regexp.MustCompile(`(?i)<br\s*/?>|</p>`)

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

// duration returns the seconds of an ISO 8601 duration, like "PT1H30M", 0
// if it isn't one.
func duration(v any) int32 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	m := isoDuration.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return 0
	}
	var total float64
	for i, unit := range []float64{86400, 3600, 60, 1} {
		if m[i+1] != "" {
			n, _ := strconv.ParseFloat(m[i+1], 64)
			total += n * unit
		}
	}
	return int32(min(total, maxSeconds))
}

// steps returns the steps of recipeInstructions: a text of lines, texts,
// HowToSteps, or HowToSections of them, whose names are the steps'
// sections.
func steps(v any, section string, depth int) []*recipes.Step {
	if depth > 4 {
		return nil
	}
	var out []*recipes.Step
	add := func(t string) {
		if t = truncate(cleanText(t), maxStepLength); t != "" {
			out = append(out, &recipes.Step{Text: t, Section: truncate(section, 100)})
		}
	}
	switch v := v.(type) {
	case string:
		// A text has a step per line, or per paragraph.
		for _, line := range strings.Split(lineBreaks.ReplaceAllString(v, "\n"), "\n") {
			add(line)
		}
	case []any:
		for _, item := range v {
			out = append(out, steps(item, section, depth+1)...)
		}
	case map[string]any:
		switch {
		case isType(v["@type"], "HowToSection"):
			out = append(out, steps(v["itemListElement"], text(v["name"]), depth+1)...)
		case v["text"] != nil:
			add(text(v["text"]))
		default:
			add(text(v["name"]))
		}
	}
	return out
}
