// Package bowls builds bowls: a carbohydrate, like rice or quinoa, a
// protein, like tofu or chickpeas, vegetables and a sauce, from the
// household's foods and recipes, and suggests them from what's on hand.
// A bowl becomes an ordinary recipe to cook or plan, so the rest of the
// app, like the shopping list and the pantry, treats it like any other.
package bowls

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// Role is what a part does in a bowl.
type Role int

const (
	RoleUnspecified Role = iota
	// RoleCarb is its base, like rice, quinoa or noodles.
	RoleCarb
	// RoleProtein is its protein, like tofu, tempeh, beans or lentils.
	RoleProtein
	// RoleVegetable is a vegetable, raw or cooked.
	RoleVegetable
	// RoleSauce is its sauce or dressing, which ties it together.
	RoleSauce
)

// Style is a bowl's cuisine, which its sauce sets and its other parts
// should fit: tortillas don't go with a peanut sauce.
type Style string

const (
	StyleAsian         Style = "Asian"
	StyleMexican       Style = "Mexican"
	StyleMediterranean Style = "Mediterranean"
	StyleIndian        Style = "Indian"
)

// Part is a food or recipe a bowl can have, in its role, with a default
// amount for a serving and whether it's on hand.
type Part struct {
	Role Role
	// Key identifies the part: "food:<id>", "recipe:<id>", or
	// "sauce:<recipe id>:<n>" for the nth sauce of a recipe.
	Key  string
	Name string
	// Detail says more about it, like the recipe a sauce is from.
	Detail string
	// Food is the food of a food part, Recipe the recipe of a recipe part
	// or the recipe a sauce is from, and Sauce the sauce's ingredients.
	Food   *foods.Food
	Recipe *recipes.Recipe
	Sauce  []*recipes.Ingredient
	// SauceSteps are the texts of the recipe's steps making its sauce.
	SauceSteps []string
	// Kind is a sauce's kind, like "sauce" or "dressing".
	Kind string
	// Styles are the styles it fits, all of them if empty.
	Styles []Style
	// Amount is a serving's: grams of a food, servings of a recipe, or for
	// a recipe's sauce, shares of it, one per serving of the recipe.
	Amount float64
	// OnHand is whether the household has enough for a serving: in the
	// pantry, or for a recipe, leftovers already cooked, and OnHandNote
	// says where. UseSoon is whether some should be used soon.
	OnHand     bool
	OnHandNote string
	UseSoon    bool
	// Leftovers is whether it's leftovers already cooked.
	Leftovers bool
}

// Short returns a short name of the part for a bowl's name, like "Tofu"
// for "Tofu, extra firm".
func (p *Part) Short() string {
	name, _, _ := strings.Cut(p.Name, ",")
	name, _, _ = strings.Cut(name, " (")
	return strings.TrimSpace(name)
}

// Leftover is a cook's leftovers on hand, to use as a part.
type Leftover struct {
	RecipeID pgtype.UUID
	Servings float64
	// Note says where they are, like "in the fridge, cooked Wed 18 Nov".
	Note    string
	UseSoon bool
}

// Input is what parts are read from.
type Input struct {
	// Date is the day the bowls are for.
	Date    time.Time
	Foods   map[pgtype.UUID]*foods.Food
	Recipes []*recipes.Recipe
	// Lots are the pantry's lots.
	Lots []*pantry.Item
	// Leftovers are the leftovers already cooked, in the fridge and the
	// freezer.
	Leftovers []*Leftover
}

// useSoonDays is how many days ahead a lot's use-by date makes it one to
// use soon.
const useSoonDays = 3

// Amounts of a serving: a carbohydrate of about carbKcal, a protein of
// about proteinGrams of protein, and vegetables of vegetableGrams each,
// less for leafy greens and avocado; a sauce food of sauceGrams.
const (
	carbKcal         = 260
	maxCarbGrams     = 250
	proteinGrams     = 20
	minProteinWeight = 50
	maxProteinWeight = 200
	vegetableGrams   = 80
	greensGrams      = 40
	avocadoGrams     = 50
	sauceGrams       = 30
)

// Parts returns every part a bowl can have, by role and then name.
func Parts(in *Input) []*Part {
	stock := stockOf(in)
	var parts []*Part
	for _, f := range in.Foods {
		if p := foodPart(f, stock); p != nil {
			parts = append(parts, p)
		}
	}
	sides, components := sidesAndComponents(in.Recipes)
	leftovers := map[pgtype.UUID]*Leftover{}
	for _, l := range in.Leftovers {
		if have := leftovers[l.RecipeID]; have == nil || l.UseSoon && !have.UseSoon {
			leftovers[l.RecipeID] = l
		}
	}
	for _, r := range in.Recipes {
		if r.Draft {
			continue
		}
		if p := recipePart(r, sides, components, leftovers[r.ID]); p != nil {
			parts = append(parts, p)
		}
		parts = append(parts, recipeSauces(r, stock)...)
	}
	parts = dedupeSauces(parts)
	slices.SortStableFunc(parts, func(a, b *Part) int {
		if a.Role != b.Role {
			return int(a.Role) - int(b.Role)
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return parts
}

// stock is what the pantry has of each food, and whether some should be
// used soon; always in stock foods count as plenty.
type stock struct {
	grams   map[pgtype.UUID]float64
	soon    map[pgtype.UUID]time.Time
	always  map[pgtype.UUID]bool
	date    time.Time
	storage map[pgtype.UUID]pantry.Storage
}

func stockOf(in *Input) *stock {
	s := &stock{
		grams: map[pgtype.UUID]float64{}, soon: map[pgtype.UUID]time.Time{},
		always: map[pgtype.UUID]bool{}, date: in.Date, storage: map[pgtype.UUID]pantry.Storage{},
	}
	for _, f := range in.Foods {
		if f.AlwaysInStock {
			s.always[f.ID] = true
		}
	}
	for _, lot := range in.Lots {
		useBy := lot.UseBy()
		if !useBy.IsZero() && useBy.Before(in.Date) {
			continue
		}
		s.grams[lot.FoodID] += lot.Grams
		if _, ok := s.storage[lot.FoodID]; !ok {
			s.storage[lot.FoodID] = lot.Storage
		}
		if !useBy.IsZero() && pantry.DaysTo(in.Date, useBy) <= useSoonDays {
			if have, ok := s.soon[lot.FoodID]; !ok || useBy.Before(have) {
				s.soon[lot.FoodID] = useBy
			}
		}
	}
	return s
}

// has reports whether the pantry has grams of a food.
func (s *stock) has(id pgtype.UUID, grams float64) bool {
	return s.always[id] || (s.grams[id] > 0 && s.grams[id] >= grams-1e-9)
}

// note says where a food on hand is, and when to use it by.
func (s *stock) note(id pgtype.UUID) string {
	if s.always[id] {
		return "always in stock"
	}
	where := "in the pantry"
	switch s.storage[id] {
	case pantry.StorageFridge:
		where = "in the fridge"
	case pantry.StorageFreezer:
		where = "in the freezer"
	}
	if useBy, ok := s.soon[id]; ok {
		return fmt.Sprintf("%s, use by %s", where, useBy.Format("Mon 2 Jan"))
	}
	return where
}

// The words that place a food in a role, matched against whole words of
// its name.
var (
	carbWords = []string{
		"rice", "quinoa", "farro", "couscous", "bulgur", "barley", "millet", "buckwheat", "freekeh",
		"noodles", "noodle", "soba", "udon", "vermicelli", "pasta", "orzo", "potato", "potatoes", "gnocchi",
	}
	proteinWords = []string{
		"tofu", "tempeh", "seitan", "edamame", "chickpeas", "chickpea", "beans", "lentils", "lentil",
		"soy curls", "falafel", "peas",
	}
	// notMain are words of foods that are none of a bowl's parts, though
	// another word matches, like rice vinegar or green beans.
	notMain = []string{
		"flour", "vinegar", "milk", "syrup", "paper", "cake", "cakes", "crackers", "chips", "powder",
		"wine", "starch", "bran", "flakes", "puffs", "pasta sauce", "sauce", "paste", "miso",
		"sprouts", "green beans", "snap peas", "snow peas", "split", "sweet potato", "sweet potatoes",
		"instant", "cup", "soup", "pudding", "drink", "tortilla", "tortillas", "bread", "crispy",
	}
	vegetableWords = []string{
		"broccoli", "broccolini", "cauliflower", "cabbage", "kale", "spinach", "arugula", "lettuce", "romaine",
		"carrot", "carrots", "cucumber", "tomato", "tomatoes", "pepper", "peppers", "zucchini", "eggplant",
		"squash", "sweet potato", "sweet potatoes", "beet", "beets", "corn", "mushroom", "mushrooms",
		"bok choy", "asparagus", "brussels sprouts", "green beans", "snap peas", "snow peas", "peas",
		"radish", "radishes", "avocado", "edamame", "sprouts", "artichoke", "fennel", "red onion", "onion",
		"chard", "collard", "greens", "pumpkin", "olives", "kimchi", "sauerkraut", "pickled", "slaw",
		"mixed vegetables", "bamboo shoots", "water chestnuts", "celery", "leek", "okra", "turnip", "parsnip",
	}
	// notVegetable are produce that's no vegetable of a bowl: herbs,
	// aromatics, chilies and fruit.
	notVegetable = []string{
		"garlic", "ginger", "shallot", "shallots", "scallion", "scallions", "green onion", "chili", "chilies",
		"chile", "jalapeño", "jalapeno", "habanero", "serrano", "basil", "cilantro", "parsley", "dill", "mint",
		"chives", "thyme", "rosemary", "oregano", "sage", "curry leaves", "lemon", "lime", "limes", "orange",
		"apple", "banana", "grapes", "berries", "blueberries", "strawberries", "raspberries", "fruit", "mango",
		"pineapple", "dried", "powder", "sauce", "paste", "soup", "yellow onion", "white onion", "flakes",
		"seasoning", "frozen fruit", "tortilla", "tortillas", "bread", "chips", "flour", "starch", "crushed",
		"diced", "peeled", "puree", "passata", "juice", "milk", "cream",
	}
	// leafy are vegetables a serving has less of by weight.
	leafy = []string{"spinach", "arugula", "lettuce", "romaine", "kale", "greens", "sprouts", "chard", "collard"}
	// sauceWords are ready-made sauces a bowl can have, and notSauce those
	// too strong or not for a bowl, like soy sauce or pasta sauce.
	sauceWords = []string{
		"sauce", "dressing", "salsa", "pesto", "hummus", "tahini", "chili crisp", "teriyaki", "tzatziki",
		"guacamole", "chimichurri", "vinaigrette",
	}
	notSauce = []string{
		"soy sauce", "fish sauce", "hot sauce", "worcestershire", "pasta sauce", "tomato sauce", "apple sauce",
		"applesauce", "cranberry", "chili garlic sauce", "sriracha", "liquid smoke", "marinara",
	}
)

// words returns a lowercased name with its punctuation as spaces, padded,
// to match whole words in.
func words(name string) string {
	var b strings.Builder
	b.WriteByte(' ')
	for _, r := range strings.ToLower(name) {
		switch r {
		case ',', '(', ')', '-', '/', '&', '.', ';', ':':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(' ')
	return b.String()
}

// hasWord reports whether a name, as words returns it, has any of the
// words or phrases.
func hasWord(name string, list []string) bool {
	return slices.ContainsFunc(list, func(w string) bool { return strings.Contains(name, " "+w+" ") })
}

// foodRole returns the role of a food in a bowl, if any.
func foodRole(f *foods.Food) Role {
	switch f.StoreSection {
	case foods.StoreSectionSpices, foods.StoreSectionDrinks, foods.StoreSectionHousehold, foods.StoreSectionOther:
		return RoleUnspecified
	}
	name := words(f.Name)
	kcal := nutrientOf(f.Per100Grams, nutrition.EnergyKcal)
	switch {
	case hasWord(name, sauceWords) && !hasWord(name, notSauce):
		return RoleSauce
	case hasWord(name, proteinWords) && !hasWord(name, notMain) &&
		// Dried beans need soaking, so a bowl takes them cooked or canned;
		// dried lentils cook in 20 minutes.
		!(hasWord(name, []string{"dried", "dry"}) && !hasWord(name, []string{"lentils", "lentil"})):
		return RoleProtein
	case hasWord(name, carbWords) && !hasWord(name, notMain):
		return RoleCarb
	case hasWord(name, vegetableWords) && !hasWord(name, notVegetable) &&
		(kcal == 0 || kcal <= 120 || hasWord(name, []string{"avocado", "olives"})):
		return RoleVegetable
	}
	return RoleUnspecified
}

// nutrientOf returns a nutrient, 0 if unknown.
func nutrientOf(n nutrition.Nutrients, which nutrition.Nutrient) float64 {
	if v := n[which]; v != nil {
		return *v
	}
	return 0
}

// foodPart returns the part a food can be, nil if none.
func foodPart(f *foods.Food, s *stock) *Part {
	role := foodRole(f)
	if role == RoleUnspecified {
		return nil
	}
	p := &Part{
		Role: role, Key: "food:" + store.UUIDToString(f.ID), Name: f.Name, Food: f,
		Styles: stylesOf(role, words(f.Name)),
	}
	p.Amount = defaultFoodGrams(role, f)
	if s.has(f.ID, p.Amount) {
		p.OnHand = true
		p.OnHandNote = s.note(f.ID)
		_, p.UseSoon = s.soon[f.ID]
	}
	return p
}

// defaultFoodGrams returns the grams of a food in a serving of a bowl.
func defaultFoodGrams(role Role, f *foods.Food) float64 {
	name := words(f.Name)
	kcal := nutrientOf(f.Per100Grams, nutrition.EnergyKcal)
	protein := nutrientOf(f.Per100Grams, nutrition.ProteinGrams)
	switch role {
	case RoleCarb:
		if kcal <= 0 {
			return 150
		}
		return roundTo(math.Min(carbKcal/kcal*100, maxCarbGrams), 5)
	case RoleProtein:
		if protein <= 0 {
			return 150
		}
		return roundTo(math.Max(minProteinWeight, math.Min(proteinGrams/protein*100, maxProteinWeight)), 5)
	case RoleVegetable:
		switch {
		case hasWord(name, []string{"avocado"}):
			return avocadoGrams
		case hasWord(name, leafy):
			return greensGrams
		}
		return vegetableGrams
	case RoleSauce:
		return sauceGrams
	}
	return 0
}

// roundTo rounds a number to a multiple of step.
func roundTo(v, step float64) float64 {
	return math.Round(v/step) * step
}

// sidesAndComponents returns the recipes served as sides of others, and
// those that go into others, as components or their alternatives.
func sidesAndComponents(rs []*recipes.Recipe) (sides, components map[pgtype.UUID]bool) {
	sides, components = map[pgtype.UUID]bool{}, map[pgtype.UUID]bool{}
	for _, r := range rs {
		for _, side := range r.ServedWith {
			if side.RecipeID.Valid {
				sides[side.RecipeID] = true
			}
		}
		for _, in := range r.Ingredients {
			if in.ComponentRecipeID.Valid {
				components[in.ComponentRecipeID] = true
			}
			for _, a := range in.Alternatives {
				if a.RecipeID.Valid {
					components[a.RecipeID] = true
				}
			}
		}
	}
	return sides, components
}

// recipePart returns the part a recipe can be, nil if none: a sauce, if
// tagged for one, or a carbohydrate, protein or vegetable made to go with
// or into others, like a pot of rice and quinoa, smoky tofu crumbles or a
// pot of beans, by its main ingredient.
func recipePart(r *recipes.Recipe, sides, components map[pgtype.UUID]bool, leftover *Leftover) *Part {
	tags := words(strings.Join(r.Tags, " "))
	n := r.Nutrition()
	p := &Part{Key: "recipe:" + store.UUIDToString(r.ID), Name: r.Name, Recipe: r, Amount: 1}
	switch {
	case hasWord(tags, []string{"sauce", "dressing", "condiment"}):
		p.Role = RoleSauce
		p.Kind = "sauce"
	case sides[r.ID] || components[r.ID] || hasWord(tags, []string{"side", "basics", "component"}):
		role := mainRole(r)
		if role == RoleUnspecified {
			return nil
		}
		p.Role = role
		perServing := n.PerServing
		switch role {
		case RoleCarb:
			if kcal := nutrientOf(perServing, nutrition.EnergyKcal); kcal > 0 {
				p.Amount = clampServings(carbKcal / kcal)
			}
		case RoleProtein:
			if protein := nutrientOf(perServing, nutrition.ProteinGrams); protein > 0 {
				p.Amount = clampServings(proteinGrams / protein)
			}
		}
	default:
		return nil
	}
	ingredientNames := make([]string, 0, len(r.Ingredients))
	for _, in := range r.Ingredients {
		ingredientNames = append(ingredientNames, in.FoodName)
	}
	p.Styles = stylesOf(p.Role, words(r.Name+" "+strings.Join(ingredientNames, " ")))
	if leftover != nil && leftover.Servings >= p.Amount-1e-9 {
		p.OnHand, p.OnHandNote, p.UseSoon, p.Leftovers = true, leftover.Note, leftover.UseSoon, true
	}
	return p
}

// clampServings rounds servings of a recipe in a bowl to a quarter, from
// half a serving to two.
func clampServings(s float64) float64 {
	return math.Max(0.5, math.Min(2, roundTo(s, 0.25)))
}

// mainRole returns the role of a recipe's main ingredient: the heaviest
// that's a carbohydrate, protein or vegetable, leaving out the water,
// oil and spices it's cooked with, which outweigh it in a pot of rice.
func mainRole(r *recipes.Recipe) Role {
	role, mainGrams := RoleUnspecified, 0.0
	for _, in := range r.Ingredients {
		if !in.FoodID.Valid || in.Optional {
			continue
		}
		// A recipe's food can be dried, like the beans of a pot of beans,
		// which the recipe cooks.
		name := strings.NewReplacer("dried", "", "Dried", "").Replace(in.FoodName)
		inRole := foodRole(&foods.Food{Name: name, Per100Grams: in.Food.Per100Grams, StoreSection: foods.StoreSectionDryGoods})
		if inRole == RoleUnspecified || inRole == RoleSauce {
			continue
		}
		if grams, ok := in.Food.Grams(in.Amount, in.Unit); ok && grams > mainGrams {
			role, mainGrams = inRole, grams
		}
	}
	return role
}

// The words of a sauce in a recipe: its steps' sections, like "Sauce", and
// its ingredients' notes, like "for the dressing".
var (
	sauceSection = regexp.MustCompile(`(?i)\b(sauce|dressing|vinaigrette|crema|glaze|drizzle|pesto|chimichurri|salsa|aioli|mayo|tzatziki|raita)\b`)
	sauceNote    = regexp.MustCompile(`(?i)\bfor the ([^;,]*?\b(sauce|dressing|vinaigrette|crema|glaze|drizzle|pesto|chimichurri|salsa|aioli|mayo|tzatziki|raita))\b`)
)

// recipeSauces returns the sauces made in a recipe, like the peanut sauce
// of a noodle bowl, each its ingredients from the steps of a sauce section,
// or noted as for the sauce.
func recipeSauces(r *recipes.Recipe, s *stock) []*Part {
	type group struct {
		kind    string
		indexes []int
		steps   []string
	}
	var groups []*group
	find := func(kind string) *group {
		for _, g := range groups {
			if g.kind == kind {
				return g
			}
		}
		g := &group{kind: kind}
		groups = append(groups, g)
		return g
	}
	for _, step := range r.Instructions {
		m := sauceSection.FindStringSubmatch(step.Section)
		if m == nil {
			continue
		}
		g := find(strings.ToLower(m[1]))
		g.steps = append(g.steps, step.Text)
		for _, i := range step.Ingredients {
			if !slices.Contains(g.indexes, i) {
				g.indexes = append(g.indexes, i)
			}
		}
	}
	for i, in := range r.Ingredients {
		m := sauceNote.FindStringSubmatch(in.Note)
		if m == nil {
			continue
		}
		g := find(strings.ToLower(m[2]))
		if !slices.Contains(g.indexes, i) {
			g.indexes = append(g.indexes, i)
		}
	}
	var parts []*Part
	for n, g := range groups {
		slices.Sort(g.indexes)
		var ings []*recipes.Ingredient
		for _, i := range g.indexes {
			if i >= 0 && i < len(r.Ingredients) && r.Ingredients[i].FoodID.Valid {
				ings = append(ings, r.Ingredients[i])
			}
		}
		if len(ings) < 2 {
			continue
		}
		var names []string
		onHand, soon := true, false
		for _, in := range ings {
			names = append(names, in.FoodName)
			if in.Optional {
				continue
			}
			grams, _ := in.Food.Grams(in.Amount/float64(max(r.Servings, 1)), in.Unit)
			if !s.has(in.FoodID, grams) {
				onHand = false
			}
			if _, ok := s.soon[in.FoodID]; ok {
				soon = true
			}
		}
		p := &Part{
			Role: RoleSauce, Key: fmt.Sprintf("sauce:%s:%d", store.UUIDToString(r.ID), n),
			Name: sauceName(ings, g.kind), Detail: "from " + r.Name, Recipe: r, Sauce: ings,
			SauceSteps: g.steps, Kind: g.kind, Amount: 1,
			Styles: stylesOf(RoleSauce, words(strings.Join(names, " "))), OnHand: onHand, UseSoon: onHand && soon,
		}
		if onHand {
			p.OnHandNote = "everything for it is in the pantry"
		}
		parts = append(parts, p)
	}
	return parts
}

// plainIngredients are a sauce's ingredients too plain to name it by.
var plainIngredients = []string{
	"water", "salt", "pepper", "oil", "garlic", "sugar", "vinegar", "cornstarch", "starch", "syrup",
	"agave", "maple", "spice", "flakes", "powder", "seeds",
}

// sauceName names a sauce by its main ingredients, heaviest first, like
// "Peanut butter, tamari & lime sauce".
func sauceName(ings []*recipes.Ingredient, kind string) string {
	type weighed struct {
		name  string
		grams float64
	}
	var main []weighed
	for _, in := range ings {
		name, _, _ := strings.Cut(in.FoodName, ",")
		name, _, _ = strings.Cut(name, " (")
		name = strings.ToLower(strings.TrimSpace(name))
		if hasWord(words(name), plainIngredients) {
			continue
		}
		grams, _ := in.Food.Grams(in.Amount, in.Unit)
		main = append(main, weighed{name, grams})
	}
	slices.SortStableFunc(main, func(a, b weighed) int {
		switch {
		case a.grams > b.grams:
			return -1
		case a.grams < b.grams:
			return 1
		}
		return 0
	})
	var names []string
	for _, w := range main {
		if len(names) < 3 && !slices.Contains(names, w.name) {
			names = append(names, w.name)
		}
	}
	var name string
	switch len(names) {
	case 0:
		name = "house"
	case 1:
		name = names[0]
	default:
		name = strings.Join(names[:len(names)-1], ", ") + " & " + names[len(names)-1]
	}
	name += " " + kind
	return strings.ToUpper(name[:1]) + name[1:]
}

// dedupeSauces leaves out sauces made of the same foods as one before
// them, so each sauce is offered once.
func dedupeSauces(parts []*Part) []*Part {
	seen := map[string]bool{}
	return slices.DeleteFunc(parts, func(p *Part) bool {
		if p.Sauce == nil {
			return false
		}
		var ids []string
		for _, in := range p.Sauce {
			ids = append(ids, store.UUIDToString(in.FoodID))
		}
		slices.Sort(ids)
		key := strings.Join(ids, ",")
		if seen[key] {
			return true
		}
		seen[key] = true
		return false
	})
}

// The words of each style's ingredients. A sauce's style is the one most
// of its ingredients have, and other parts fit the styles they have words
// of, or all if none.
var styleWords = map[Style][]string{
	StyleAsian: {
		"soy", "tamari", "miso", "sesame", "ginger", "sriracha", "gochujang", "hoisin", "peanut", "rice vinegar",
		"mirin", "chili crisp", "aminos", "teriyaki", "sambal", "kimchi", "bok choy", "edamame", "soba", "udon",
		"rice noodles", "noodles", "jasmine", "sushi", "nori", "shiitake", "bamboo", "water chestnuts", "snow peas",
		"scallion", "five spice", "lemongrass", "thai", "doenjang", "japanese", "korean",
	},
	StyleMexican: {
		"chipotle", "adobo", "salsa", "jalapeño", "jalapeno", "taco", "tortilla", "tortillas", "black beans",
		"pinto", "corn", "lime", "cilantro", "guacamole", "avocado", "crema", "cumin", "fajita", "enchilada",
		"pico", "poblano",
	},
	StyleMediterranean: {
		"tahini", "lemon", "oregano", "dill", "parsley", "olive", "olives", "feta", "hummus", "tzatziki",
		"za'atar", "sumac", "harissa", "pesto", "basil", "balsamic", "red wine vinegar", "farro", "bulgur",
		"couscous", "chickpeas", "cucumber", "artichoke", "sun dried", "greek", "falafel", "orzo", "pasta",
		"gnocchi", "spaghetti", "penne", "fusilli",
	},
	StyleIndian: {
		"curry", "garam masala", "turmeric", "coriander", "cardamom", "tikka", "masala", "chutney", "raita",
		"dal", "paneer", "basmati", "mustard seeds", "fenugreek", "tamarind", "coconut",
	},
}

// stylesOf returns the styles a part fits by the words of its name and
// ingredients: a sauce has one, the one most of its words are of, and
// other parts those they have any words of. Vegetables, grains and
// proteins with no words of a style fit them all.
func stylesOf(role Role, name string) []Style {
	counts := map[Style]int{}
	for style, ws := range styleWords {
		for _, w := range ws {
			if strings.Contains(name, " "+w+" ") || strings.Contains(name, " "+w) {
				counts[style]++
			}
		}
	}
	var styles []Style
	for _, style := range []Style{StyleAsian, StyleMexican, StyleMediterranean, StyleIndian} {
		if counts[style] > 0 {
			styles = append(styles, style)
		}
	}
	if role == RoleSauce && len(styles) > 1 {
		best := styles[0]
		for _, style := range styles[1:] {
			if counts[style] > counts[best] {
				best = style
			}
		}
		return []Style{best}
	}
	// Grains and proteins fit more than the words say: rice goes with any
	// cuisine, as do tofu and chickpeas.
	if role != RoleSauce && len(styles) > 0 && hasWord(name, []string{"rice", "quinoa", "tofu", "chickpeas", "lentils"}) {
		return nil
	}
	return styles
}

// fits reports whether a part fits a style: a part fits every style if it
// has none, and a bowl with no style fits every part.
func fits(p *Part, style Style) bool {
	return style == "" || len(p.Styles) == 0 || slices.Contains(p.Styles, style)
}
