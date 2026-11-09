package recipeimport

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"testing"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
)

func TestParseIngredient(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line    string
		amount  float64
		unit    nutrition.Unit
		kitchen nutrition.KitchenUnit
		name    string
	}{
		{"1 ½ cups almond milk, unsweetened", 360, nutrition.Milliliters, nutrition.Cups, "almond milk"},
		{"2 tbsp olive oil", 30, nutrition.Milliliters, nutrition.Tablespoons, "olive oil"},
		{"1/2 tsp. ground cumin", 2.5, nutrition.Milliliters, nutrition.Teaspoons, "ground cumin"},
		{"250g firm tofu (pressed)", 250, nutrition.Grams, nutrition.NoKitchenUnit, "firm tofu"},
		{"400 ml coconut milk", 400, nutrition.Milliliters, nutrition.NoKitchenUnit, "coconut milk"},
		{"1 lb potatoes", 453.59237, nutrition.Grams, nutrition.Pounds, "potatoes"},
		{"3 cloves garlic, minced", 3, nutrition.Pieces, nutrition.NoKitchenUnit, "garlic"},
		{"2-3 large onions", 2, nutrition.Pieces, nutrition.NoKitchenUnit, "onions"},
		{"1 1/2 kg flour", 1500, nutrition.Grams, nutrition.Kilograms, "flour"},
		{"½ lemon", 0.5, nutrition.Pieces, nutrition.NoKitchenUnit, "lemon"},
		{"Salt and pepper, to taste", 0, 0, nutrition.NoKitchenUnit, "Salt and pepper"},
		{"1,5 l vegetable stock", 1500, nutrition.Milliliters, nutrition.Liters, "vegetable stock"},
	}
	for _, tt := range tests {
		in, name := ParseIngredient(tt.line)
		if in == nil {
			t.Errorf("%q: got nil", tt.line)
			continue
		}
		if math.Abs(in.Amount-tt.amount) > 1e-6 || in.Unit != tt.unit || in.KitchenUnit != tt.kitchen || name != tt.name {
			t.Errorf("%q: got %v %v %q, name %q; want %v %v %q, name %q",
				tt.line, in.Amount, in.Unit, in.KitchenUnit, name, tt.amount, tt.unit, tt.kitchen, tt.name)
		}
	}
	if in, _ := ParseIngredient("  <b></b> "); in != nil {
		t.Errorf("got %v for an empty line, want nil", in)
	}
}

func TestParse(t *testing.T) {
	t.Parallel()
	page := `<html><head>
<script type="application/ld+json">{"@context":"https://schema.org","@type":"WebSite","name":"Blog"}</script>
<script type='application/ld+json' class='yoast-schema-graph'>{"@context":"https://schema.org","@graph":[
 {"@type":"Article","headline":"Best chili"},
 {"@type":["Recipe"],"name":"Smoky Pinto Bean Chili &amp; Rice","description":"<p>A <b>cozy</b> chili.</p>",
  "recipeYield":["6","6 servings"],"prepTime":"PT15M","cookTime":"PT1H10M",
  "recipeIngredient":["2 tbsp olive oil","1 large onion, diced","800 g cooked pinto beans"],
  "recipeInstructions":[
   {"@type":"HowToSection","name":"For the chili","itemListElement":[
     {"@type":"HowToStep","text":"Fry the onion in the oil."},
     {"@type":"HowToStep","text":"Add the beans &amp; simmer."}]},
   {"@type":"HowToStep","name":"Serve with rice."}]}
]}</script></head><body></body></html>`
	got, err := Parse([]byte(page), "https://example.com/chili")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := got.Recipe
	if r.Name != "Smoky Pinto Bean Chili & Rice" || r.Description != "A cozy chili." || r.Servings != 6 ||
		r.PrepSeconds != 900 || r.CookSeconds != 4200 || !r.Draft || r.SourceURL != "https://example.com/chili" {
		t.Errorf("got %+v", r)
	}
	if len(r.Ingredients) != 3 || r.Ingredients[2].Amount != 800 || got.FoodNames[1] != "onion" {
		t.Errorf("got ingredients %+v, names %v", r.Ingredients, got.FoodNames)
	}
	if len(r.Instructions) != 3 || r.Instructions[0].Section != "For the chili" ||
		r.Instructions[1].Text != "Add the beans & simmer." || r.Instructions[2].Text != "Serve with rice." || r.Instructions[2].Section != "" {
		t.Errorf("got steps %+v %+v %+v", r.Instructions[0], r.Instructions[1], r.Instructions[2])
	}

	// Instructions can be a text, a step a line.
	page = `<script type="application/ld+json">[{"@type":"Recipe","name":"Toast","recipeYield":2,
	"recipeIngredient":"2 slices bread","recipeInstructions":"Toast the bread.<br>Butter it.\nEat."}]</script>`
	got, err = Parse([]byte(page), "https://example.com/toast")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Recipe.Servings != 2 || len(got.Recipe.Instructions) != 3 || len(got.Recipe.Ingredients) != 1 {
		t.Errorf("got %+v", got.Recipe)
	}

	if _, err := Parse([]byte(`<script type="application/ld+json">{"@type":"Article"}</script>`), ""); err != ErrNoRecipe {
		t.Errorf("got %v for a page without a recipe, want ErrNoRecipe", err)
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]int32{"PT20M": 1200, "PT1H": 3600, "P0DT1H30M": 5400, "PT45S": 45, "20 minutes": 0, "P1D": 86400} {
		if got := duration(s); got != want {
			t.Errorf("duration(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestIsPublic(t *testing.T) {
	t.Parallel()
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700::1111": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.0.1": false,
		"169.254.169.254": false, "::1": false, "fd00::1": false, "0.0.0.0": false, "::ffff:127.0.0.1": false, "100.100.1.1": false,
	} {
		if got := isPublic(net.ParseIP(addr)); got != want {
			t.Errorf("isPublic(%s) = %v, want %v", addr, got, want)
		}
	}
	for _, u := range []string{"ftp://example.com", "example.com/recipe", "https://"} {
		if _, err := CheckURL(u); err == nil {
			t.Errorf("CheckURL(%q) succeeded, want an error", u)
		}
	}
}

func TestDescribe(t *testing.T) {
	t.Parallel()
	for err, want := range map[error]string{
		ErrNotPublic: "the address isn't a public web site",
		fmt.Errorf("Get %q: %w", "https://x", &net.DNSError{Err: "no such host", Name: "x"}): "the site couldn't be found",
		context.DeadlineExceeded:                      "the site took too long to answer",
		errors.New("the site answered 404 Not Found"): "the site answered 404 Not Found",
		errors.New("connection reset"):                "the site couldn't be reached",
	} {
		if got := Describe(err); got != want {
			t.Errorf("Describe(%v) = %q, want %q", err, got, want)
		}
	}
}
