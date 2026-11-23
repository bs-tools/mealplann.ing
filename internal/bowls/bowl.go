package bowls

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
)

// Bowl is a carbohydrate, a protein, vegetables and a sauce, in amounts
// for a serving, with the nutrients of a serving.
type Bowl struct {
	Name  string
	Style Style
	// Parts are in order of role.
	Parts []*Part
	// PerServing are the nutrients of a serving.
	PerServing nutrition.Nutrients
	// Reasons say why a suggested bowl was chosen.
	Reasons []string
	// Score is how good a suggested bowl is.
	Score float64
}

// OnHand reports whether all the bowl's parts are on hand.
func (b *Bowl) OnHand() bool {
	return !slices.ContainsFunc(b.Parts, func(p *Part) bool { return !p.OnHand })
}

// ErrIncomplete is returned for a bowl without each of its parts.
var ErrIncomplete = errors.New("a bowl has at least one carbohydrate, one protein and one vegetable, and exactly one sauce")

// Compose makes a bowl of parts, in their amounts, checking it has each
// of its parts: at least one carbohydrate, protein and vegetable, and one
// sauce.
func Compose(parts []*Part) (*Bowl, error) {
	counts := map[Role]int{}
	for _, p := range parts {
		counts[p.Role]++
	}
	if counts[RoleCarb] < 1 || counts[RoleProtein] < 1 || counts[RoleVegetable] < 1 || counts[RoleSauce] != 1 {
		return nil, ErrIncomplete
	}
	b := &Bowl{Parts: slices.Clone(parts)}
	slices.SortStableFunc(b.Parts, func(a, c *Part) int { return int(a.Role) - int(c.Role) })
	for _, p := range b.Parts {
		if p.Role == RoleSauce {
			if len(p.Styles) == 1 {
				b.Style = p.Styles[0]
			}
		}
		b.PerServing = add(b.PerServing, nutrientsOf(p))
	}
	b.Name = nameOf(b)
	return b, nil
}

// nutrientsOf returns the nutrients of a part in a serving of a bowl.
func nutrientsOf(p *Part) nutrition.Nutrients {
	switch {
	case p.Food != nil:
		return p.Food.Per100Grams.Scale(p.Amount / 100)
	case p.Sauce != nil:
		var total nutrition.Nutrients
		for _, in := range p.Sauce {
			grams, ok := in.Food.Grams(in.Amount, in.Unit)
			if !ok {
				continue
			}
			total = add(total, in.Food.Per100Grams.Scale(grams/100))
		}
		return total.Scale(p.Amount / float64(max(p.Recipe.Servings, 1)))
	case p.Recipe != nil:
		per := p.Recipe.Nutrition().PerServing
		return per.Scale(p.Amount)
	}
	return nutrition.Nutrients{}
}

// add adds nutrients, a nutrient known if either has it.
func add(a, b nutrition.Nutrients) nutrition.Nutrients {
	var sum nutrition.Nutrients
	for i := range sum {
		if a[i] == nil && b[i] == nil {
			continue
		}
		v := 0.0
		if a[i] != nil {
			v += *a[i]
		}
		if b[i] != nil {
			v += *b[i]
		}
		sum[i] = &v
	}
	return sum
}

// nameOf names a bowl by its protein, carbohydrate and sauce, like "Tofu &
// quinoa bowl with peanut sauce".
func nameOf(b *Bowl) string {
	var protein, carb, sauce string
	for _, p := range b.Parts {
		switch {
		case p.Role == RoleProtein && protein == "":
			protein = p.Short()
		case p.Role == RoleCarb && carb == "":
			carb = strings.ToLower(p.Short())
		case p.Role == RoleSauce:
			sauce = strings.ToLower(p.Name)
		}
	}
	return fmt.Sprintf("%s & %s bowl with %s", protein, carb, sauce)
}

// Points of a part of a suggested bowl, for being on hand, to use soon,
// cooked already, and of the bowl's style, rather than fitting any.
const (
	useSoonPoints   = 4
	leftoverPoints  = 5
	styleMatchPoint = 2
	anyStylePoint   = 1
	// repeatPenalty is for each part a suggestion shares with one before
	// it, so the suggestions differ.
	repeatPenalty = 4
	// noise is the most a seed changes a part's points by, to shuffle
	// between parts that are about as good.
	noise = 1.5
	// maxVegetables is how many vegetables a suggested bowl has.
	maxVegetables = 3
)

// Suggest suggests up to n bowls from the parts on hand: for each sauce
// on hand, the carbohydrate, protein and vegetables that fit its style
// best, preferring leftovers already cooked and food to use soon, best
// first, sharing as few parts as they can. A seed shuffles parts about as
// good.
func Suggest(parts []*Part, n int, seed uint64) []*Bowl {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	byRole := map[Role][]*Part{}
	jitter := map[string]float64{}
	for _, p := range parts {
		if !p.OnHand {
			continue
		}
		byRole[p.Role] = append(byRole[p.Role], p)
		jitter[p.Key] = rng.Float64() * noise
	}
	score := func(p *Part, style Style) float64 {
		s := jitter[p.Key]
		if p.UseSoon {
			s += useSoonPoints
		}
		if p.Leftovers {
			s += leftoverPoints
		}
		if style != "" && slices.Contains(p.Styles, style) {
			s += styleMatchPoint
		} else {
			s += anyStylePoint
		}
		return s
	}
	used := map[string]int{}
	best := func(role Role, style Style, k int) []*Part {
		var fitting []*Part
		for _, p := range byRole[role] {
			if fits(p, style) {
				fitting = append(fitting, p)
			}
		}
		slices.SortStableFunc(fitting, func(a, b *Part) int {
			return cmp.Compare(score(b, style)-float64(used[b.Key]*repeatPenalty), score(a, style)-float64(used[a.Key]*repeatPenalty))
		})
		return fitting[:min(k, len(fitting))]
	}
	var bowls []*Bowl
	sauces := slices.Clone(byRole[RoleSauce])
	for len(bowls) < n && len(sauces) > 0 {
		var top *Bowl
		topIndex := -1
		for i, sauce := range sauces {
			style := Style("")
			if len(sauce.Styles) == 1 {
				style = sauce.Styles[0]
			}
			carbs, proteins, vegetables := best(RoleCarb, style, 1), best(RoleProtein, style, 1), best(RoleVegetable, style, maxVegetables)
			if len(carbs) == 0 || len(proteins) == 0 || len(vegetables) == 0 {
				continue
			}
			chosen := append(append([]*Part{carbs[0], proteins[0]}, vegetables...), sauce)
			b, err := Compose(chosen)
			if err != nil {
				continue
			}
			for _, p := range chosen {
				b.Score += score(p, style) - float64(used[p.Key]*repeatPenalty)
			}
			if top == nil || b.Score > top.Score {
				top, topIndex = b, i
			}
		}
		if top == nil {
			break
		}
		top.Reasons = reasonsOf(top)
		bowls = append(bowls, top)
		for _, p := range top.Parts {
			used[p.Key]++
		}
		sauces = slices.Delete(sauces, topIndex, topIndex+1)
	}
	return bowls
}

// reasonsOf says why a bowl was suggested: what of it is cooked already,
// and what should be used soon.
func reasonsOf(b *Bowl) []string {
	var reasons []string
	for _, p := range b.Parts {
		switch {
		case p.Leftovers:
			reasons = append(reasons, fmt.Sprintf("%s is cooked already: %s", p.Name, p.OnHandNote))
		case p.UseSoon:
			reasons = append(reasons, fmt.Sprintf("uses %s: %s", strings.ToLower(p.Short()), p.OnHandNote))
		}
	}
	if b.Style != "" {
		reasons = append(reasons, fmt.Sprintf("%s-style: its parts go with the %s", b.Style, strings.ToLower(b.Parts[len(b.Parts)-1].Kind)))
	}
	return reasons
}

// Recipe returns the recipe of a bowl, for servings: its parts as
// ingredients, a food in grams, a recipe in servings as a component, and
// a recipe's sauce as its own ingredients, scaled, with steps to make it.
func (b *Bowl) Recipe(servings int32) *recipes.Recipe {
	r := &recipes.Recipe{
		Name:       b.Name,
		Servings:   servings,
		Tags:       []string{"bowl", "bowl-builder", "lunch", "dinner"},
		FridgeDays: new(int32(3)),
		Notes:      "Made with the bowl builder.",
	}
	var descriptions []string
	n := float64(servings)
	indexes := map[Role][]int{}
	names := map[Role][]string{}
	var sauce *Part
	for _, p := range b.Parts {
		names[p.Role] = append(names[p.Role], strings.ToLower(p.Short()))
		descriptions = append(descriptions, strings.ToLower(p.Short()))
		switch {
		case p.Food != nil:
			indexes[p.Role] = append(indexes[p.Role], len(r.Ingredients))
			r.Ingredients = append(r.Ingredients, &recipes.Ingredient{
				FoodID: p.Food.ID, Amount: roundTo(p.Amount*n, 1), Unit: nutrition.Grams, Note: noteOf(p),
			})
		case p.Sauce != nil:
			sauce = p
			scale := p.Amount * n / float64(max(p.Recipe.Servings, 1))
			for _, in := range p.Sauce {
				indexes[RoleSauce] = append(indexes[RoleSauce], len(r.Ingredients))
				r.Ingredients = append(r.Ingredients, &recipes.Ingredient{
					FoodID: in.FoodID, Amount: in.Amount * scale, Unit: in.Unit, KitchenUnit: in.KitchenUnit,
					Note: "for the " + p.Kind, Optional: in.Optional,
				})
			}
		case p.Recipe != nil:
			indexes[p.Role] = append(indexes[p.Role], len(r.Ingredients))
			r.Ingredients = append(r.Ingredients, &recipes.Ingredient{
				ComponentRecipeID: p.Recipe.ID, Amount: p.Amount * n, Note: noteOf(p),
			})
		}
	}
	r.Description = "A bowl of " + list(descriptions) + "."
	r.Instructions = stepsOf(b, indexes, names, sauce)
	for _, step := range r.Instructions {
		if step.Waiting {
			r.CookSeconds = max(r.CookSeconds, step.DurationSeconds)
		} else {
			r.PrepSeconds += step.DurationSeconds
		}
	}
	return r
}

// noteOf returns how a part is prepared for a bowl, as its ingredient's
// note.
func noteOf(p *Part) string {
	if p.Role != RoleVegetable || p.Food == nil {
		return ""
	}
	name := words(p.Food.Name)
	switch {
	case hasWord(name, roasted):
		return "roasted"
	case hasWord(name, sauteed):
		return "sautéed"
	}
	return "sliced"
}

// The vegetables a bowl roasts, and those it sautés; the others are raw.
var (
	roasted = []string{
		"sweet potato", "sweet potatoes", "squash", "pumpkin", "cauliflower", "broccoli", "brussels sprouts",
		"beet", "beets", "eggplant", "zucchini", "parsnip", "turnip", "carrot", "carrots", "fennel",
	}
	sauteed = []string{
		"spinach", "kale", "chard", "bok choy", "mushroom", "mushrooms", "green beans", "snap peas", "snow peas",
		"corn", "peas", "mixed vegetables", "broccolini", "asparagus", "collard", "edamame", "frozen",
	}
)

// stepsOf returns a bowl recipe's steps: the sauce, the carbohydrate,
// protein and vegetables prepared side by side, and then the bowls built.
func stepsOf(b *Bowl, indexes map[Role][]int, names map[Role][]string, sauce *Part) []*recipes.Step {
	var steps []*recipes.Step
	if sauce != nil && len(indexes[RoleSauce]) > 0 {
		text := "Whisk the " + sauce.Kind + " ingredients together."
		if len(sauce.SauceSteps) > 0 {
			text = strings.Join(sauce.SauceSteps, " ")
		}
		steps = append(steps, &recipes.Step{
			Text: text, Section: strings.ToUpper(sauce.Kind[:1]) + sauce.Kind[1:], Ingredients: indexes[RoleSauce],
			DurationSeconds: 300, FromStart: true,
		})
	}
	partIndex := map[*Part]int{}
	i := map[Role]int{}
	for _, p := range b.Parts {
		if p.Sauce != nil {
			continue
		}
		if len(indexes[p.Role]) > i[p.Role] {
			partIndex[p] = indexes[p.Role][i[p.Role]]
			i[p.Role]++
		}
	}
	var roast, saute, raw []*Part
	for _, p := range b.Parts {
		at, ok := partIndex[p]
		if !ok {
			continue
		}
		switch p.Role {
		case RoleCarb:
			steps = append(steps, carbStep(p, at))
		case RoleProtein:
			steps = append(steps, proteinStep(p, at))
		case RoleVegetable:
			switch noteOf(p) {
			case "roasted":
				roast = append(roast, p)
			case "sautéed":
				saute = append(saute, p)
			default:
				raw = append(raw, p)
			}
		}
	}
	group := func(ps []*Part) ([]int, string) {
		var at []int
		var ns []string
		for _, p := range ps {
			at = append(at, partIndex[p])
			ns = append(ns, strings.ToLower(p.Short()))
		}
		return at, list(ns)
	}
	if len(roast) > 0 {
		at, ns := group(roast)
		steps = append(steps, &recipes.Step{
			Text:        fmt.Sprintf("Cut the %s into bite-sized pieces, toss with a little oil and salt, and roast at 220°C for 20 to 25 minutes, until browned.", ns),
			Ingredients: at, DurationSeconds: 1500, Waiting: true, FromStart: true,
		})
	}
	if len(saute) > 0 {
		at, ns := group(saute)
		steps = append(steps, &recipes.Step{
			Text:        fmt.Sprintf("Sauté the %s in a little oil until just tender.", ns),
			Ingredients: at, DurationSeconds: 360, FromStart: true,
		})
	}
	if len(raw) > 0 {
		at, ns := group(raw)
		steps = append(steps, &recipes.Step{
			Text:        fmt.Sprintf("Slice the %s.", ns),
			Ingredients: at, DurationSeconds: 300, FromStart: true,
		})
	}
	after := make([]int, len(steps))
	for k := range steps {
		after[k] = k
	}
	steps = append(steps, &recipes.Step{
		Text: fmt.Sprintf("Build the bowls: the %s first, then the %s and the %s, with the %s on top.",
			list(names[RoleCarb]), list(names[RoleProtein]), list(names[RoleVegetable]), strings.ToLower(kindOf(sauce))),
		DurationSeconds: 180, After: after,
	})
	return steps
}

// kindOf returns a sauce's kind, "sauce" if it has none.
func kindOf(sauce *Part) string {
	if sauce == nil || sauce.Kind == "" {
		return "sauce"
	}
	return sauce.Kind
}

// carbStep returns the step preparing a bowl's carbohydrate.
func carbStep(p *Part, at int) *recipes.Step {
	name := strings.ToLower(p.Short())
	step := &recipes.Step{Ingredients: []int{at}, FromStart: true}
	switch {
	case p.Recipe != nil && p.Leftovers:
		step.Text, step.DurationSeconds = fmt.Sprintf("Reheat the %s.", name), 300
	case p.Recipe != nil:
		step.Text, step.DurationSeconds, step.Waiting = fmt.Sprintf("Make the %s, or reheat it if it's cooked already.", name), 1200, true
	case hasWord(words(name), []string{"couscous"}):
		step.Text, step.DurationSeconds = "Pour boiling water over the couscous, cover, and leave it for 5 minutes, then fluff it with a fork.", 300
	case hasWord(words(name), []string{"noodles", "soba", "udon", "pasta", "vermicelli", "orzo"}):
		step.Text, step.DurationSeconds, step.Waiting = fmt.Sprintf("Cook the %s as the packet says, then drain and rinse.", name), 600, true
	case hasWord(words(name), []string{"potato", "potatoes", "gnocchi"}):
		step.Text, step.DurationSeconds, step.Waiting = fmt.Sprintf("Cut the %s into chunks and roast at 220°C for 30 minutes, until crisp.", name), 1800, true
	default:
		step.Text, step.DurationSeconds, step.Waiting = fmt.Sprintf("Cook the %s as the packet says, then fluff it with a fork.", name), 1200, true
	}
	return step
}

// proteinStep returns the step preparing a bowl's protein.
func proteinStep(p *Part, at int) *recipes.Step {
	name := strings.ToLower(p.Short())
	step := &recipes.Step{Ingredients: []int{at}, FromStart: true}
	w := words(name)
	switch {
	case p.Recipe != nil && p.Leftovers:
		step.Text, step.DurationSeconds = fmt.Sprintf("Reheat the %s.", name), 300
	case p.Recipe != nil:
		step.Text, step.DurationSeconds = fmt.Sprintf("Make the %s, or reheat it if it's cooked already.", name), 1200
	case hasWord(w, []string{"tofu", "tempeh", "seitan"}):
		step.Text, step.DurationSeconds = fmt.Sprintf("Pat the %s dry, cut it into cubes and pan-fry it in a little oil until golden, about 10 minutes.", name), 900
	case hasWord(w, []string{"edamame", "peas"}):
		step.Text, step.DurationSeconds = fmt.Sprintf("Cook the %s in boiling water for 3 to 4 minutes, then drain.", name), 300
	case hasWord(w, []string{"lentils", "lentil"}) && !strings.Contains(w, "canned"):
		step.Text, step.DurationSeconds, step.Waiting = fmt.Sprintf("Simmer the %s in plenty of water until tender, 15 to 20 minutes, then drain.", name), 1200, true
	case hasWord(w, []string{"beans", "chickpeas", "chickpea", "lentils"}):
		step.Text, step.DurationSeconds = fmt.Sprintf("Drain and rinse the %s, and warm them in a pan if you like.", name), 300
	default:
		step.Text, step.DurationSeconds = fmt.Sprintf("Warm the %s.", name), 300
	}
	return step
}

// list joins names as "a, b and c".
func list(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// Scaled returns a part's amount for servings, rounded to what reads
// well: grams to 5, servings to a quarter.
func Scaled(p *Part, servings int32) float64 {
	v := p.Amount * float64(servings)
	if p.Food != nil {
		return roundTo(v, 5)
	}
	return math.Round(v*4) / 4
}
