// Package suggest scores a household's recipes for an open slot of its
// plan, a shared meal or one person's eating time, and explains each score
// with reasons for people and AI agents. It reuses what the rest of the
// app computes with: the plan's split of a shared meal into portions, the
// shopping list's needs, pantry and pack rounding, the pantry's use-by
// dates, and the rules' checks of eating times. It has no database or API,
// so it's tested on its own.
//
// A score is the sum of components, each worth points (see the constants
// below for the weights) with a reason. Positive points make a recipe a
// better fit; penalties are negative. Recipes breaking someone's rules
// are penalized rather than left out, so the user can still choose them.
package suggest

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/jackc/pgx/v5/pgtype"
)

// Kind is what a component of a score is about.
type Kind int

// The kinds of components.
const (
	// SharedIngredients is ingredients shared with the week's other cooks,
	// and how many new items the recipe adds to the shop.
	SharedIngredients Kind = iota + 1
	// PackLeftovers is the rest of packs the week's plan buys.
	PackLeftovers
	// Pantry is foods in stock, especially those to use soon.
	Pantry
	// Nutrition is the eaters' daily targets.
	Nutrition
	// Rules is the eaters' rules for each eating time.
	Rules
	// Keeping is whether what's bought keeps from the shop until the slot,
	// and the leftovers until they're eaten.
	Keeping
	// Flavor is ingredients that go well together.
	Flavor
	// Variety is the same recipe planned on nearby days.
	Variety
	// MealTag is the recipe's tags for meals.
	MealTag
	// Favorite is the household's favorite recipes.
	Favorite
)

// The weights of the components, in points. A typical good suggestion
// scores 10 to 25 points; the weights are set so that what saves a trip or
// waste (the pantry, the rest of a pack, keeping until the slot) and
// breaking a rule weigh most, nutrition about as much, and flavor least.
const (
	// SharedIngredientPoints is for each ingredient the week's other cooks
	// also use, which makes the shop shorter, and FreshSharedPoints more
	// for a fresh one, bought once for both instead of going off.
	SharedIngredientPoints = 1.5
	FreshSharedPoints      = 1.0
	// MaxSharedPoints caps the shared ingredients, so a long recipe doesn't
	// win on overlap alone.
	MaxSharedPoints = 6.0
	// FreeNewItems is how many new items to buy are neutral: fewer earn
	// NewItemPoints each, more cost them, down to MinNewItemPoints.
	FreeNewItems     = 3
	NewItemPoints    = 1.0
	MinNewItemPoints = -5.0

	// PackLeftoverPoints is for using all of what's left of a pack the
	// week buys, in proportion for part of it, up to MaxPackLeftoverPoints.
	PackLeftoverPoints    = 6.0
	MaxPackLeftoverPoints = 9.0
	// MinLeftoverGrams is the least left of a pack worth using: less is
	// rounding.
	MinLeftoverGrams = 10.0

	// PantryPoints is for each food the pantry covers, in proportion to how
	// much of the recipe's need it covers, and UseSoonPoints more if its
	// lot is to be used within UseSoonDays of the slot.
	PantryPoints    = 2.0
	UseSoonPoints   = 4.0
	UseSoonDays     = 3
	MaxPantryPoints = 12.0

	// NutritionMinPoints is for filling a whole daily minimum, in
	// proportion to how much of it the portion fills, up to
	// MaxNutritionPoints for all of them.
	NutritionMinPoints = 12.0
	MaxNutritionPoints = 15.0
	// NutritionMaxPenalty is for going over a daily maximum, plus
	// NutritionMinPoints in proportion to how far over.
	NutritionMaxPenalty = 4.0

	// RuleWarningPenalty is for each rule a portion breaks, and
	// UncertainRulePenalty for one it may break, as some foods lack data.
	RuleWarningPenalty   = 8.0
	UncertainRulePenalty = 3.0
	// SpilledProteinPenalty is for too little protein in the part of a
	// serving moved to a later small meal.
	SpilledProteinPenalty = 3.0
	// ProteinMetPoints is for a portion meeting a person's minimum protein
	// at their eating time.
	ProteinMetPoints = 2.0

	// TopUpPenalty is for each fresh food to buy that won't keep from the
	// shop until the slot, so it needs another trip.
	TopUpPenalty = 4.0
	// KeepsPoints is for a recipe whose fresh foods to buy all keep from
	// the shop until a slot at least KeepsMinDays after it.
	KeepsPoints  = 2.0
	KeepsMinDays = 3
	// FreshShelfLifeDays is the longest shelf life of a fresh food.
	FreshShelfLifeDays = 7
	// LeftoverKeepsPoints is for leftovers on later days within the
	// recipe's fridge days, LeftoverFreezePenalty for those to freeze, and
	// LeftoverSpoilsPenalty for those that won't keep.
	LeftoverKeepsPoints   = 1.0
	LeftoverFreezePenalty = 1.0
	LeftoverSpoilsPenalty = 5.0

	// FlavorPoints is for each pairing, up to MaxFlavorPoints.
	FlavorPoints    = 1.5
	MaxFlavorPoints = 3.0

	// VarietyPenalty is for each cook of the recipe within VarietyDays of
	// the slot, and RecentPenalty more if within RecentDays.
	VarietyPenalty = 6.0
	VarietyDays    = 6
	RecentPenalty  = 3.0
	RecentDays     = 2

	// MealTagPoints is for a recipe tagged for the slot's meal, and
	// OtherMealTagPenalty for one tagged only for another, like breakfast
	// oats for dinner, which is rarely what the household wants.
	MealTagPoints       = 2.0
	OtherMealTagPenalty = 6.0

	// FavoritePoints is for a recipe the household marked as a favorite.
	// It's about what using the rest of a pack earns, so favorites come up
	// more often, but less than VarietyPenalty plus RecentPenalty, so a
	// favorite cooked recently still makes way for others, and less than
	// RuleWarningPenalty, so it doesn't hide a broken rule.
	FavoritePoints = 5.0
)

const (
	// crumbGrams is the least amount to buy that counts, as amounts are
	// scaled floats.
	crumbGrams = 0.5
	// slack keeps amounts within a rounding error of a limit from counting
	// as over or under it, like the rules'.
	slack = 0.05
)

// Input is what scoring the recipes for a slot needs.
type Input struct {
	// Date is the slot's date, at midnight UTC.
	Date time.Time
	// Meal is the slot's shared meal, MealNone for one person's eating
	// time.
	Meal people.Meal
	// People are those who eat at the slot: the people of the shared meal,
	// each with an eating time for it, or the one person whose eating time
	// it is.
	People []*people.Person
	// EatingTimeID is the eating time of a slot of one person, invalid for a
	// shared meal.
	EatingTimeID pgtype.UUID
	// ShopDate is the date of the week's shop, at midnight UTC, which
	// shelf lives count from.
	ShopDate time.Time
	// Week is the plan of the slot's week, from Monday to Sunday, whose
	// shopping and cooks the recipes share with.
	Week *plans.Plan
	// Cooks are the cooks within VarietyDays of the slot, for variety.
	Cooks []*plans.Cook
	// Planned is what's planned at the people's eating times on the slot's
	// date and the next, by slot, for the day's totals and the eating
	// times' rules. A slot that's missing has nothing planned.
	Planned map[plans.Slot]*rules.EatingTime
	// Recipes are the household's recipes, which include those the week's
	// plan uses.
	Recipes []*recipes.Recipe
	// Candidates are the recipes to score, nil for all of Recipes. They're
	// separate so that a recipe left out of the candidates, like one the
	// user excluded, still counts in the week's shopping if it's planned.
	Candidates []*recipes.Recipe
	// Weights scale the components' points by kind, 1 for a kind missing,
	// so the user can say what matters most, like variety or the pantry.
	Weights map[Kind]float64
	// Foods are the foods of the recipes and of the week's plan, by ID.
	Foods map[pgtype.UUID]*foods.Food
	// Lots are the household's pantry.
	Lots []*pantry.Item
	// Max is the most suggestions to return.
	Max int
}

// Component is a part of a score.
type Component struct {
	Kind Kind
	// Points are what it adds to the score, negative for a penalty.
	Points float64
	// Reason says why, for people.
	Reason string
}

// Portion is a portion a suggestion would plan.
type Portion struct {
	plans.Share
	Person     *people.Person
	EatingTime *people.EatingTime
	// Grams is its weight, 0 if the recipe can't be weighed.
	Grams float64
}

// Warning is a rule a suggestion's portion would break.
type Warning struct {
	rules.Warning
	PersonID     pgtype.UUID
	EatingTimeID pgtype.UUID
	// Date is at midnight UTC.
	Date time.Time
}

// ItemToBuy is a food a suggestion needs bought beyond the week's
// shopping.
type ItemToBuy struct {
	Food *foods.Food
	// AmountText is how much more, e.g. "1 × 400 g".
	AmountText string
	// AlreadyListed is whether the week buys some of it already.
	AlreadyListed bool
}

// Suggestion is a recipe scored for a slot.
type Suggestion struct {
	Recipe *recipes.Recipe
	// Score is the sum of the components' points.
	Score float64
	// Components are the parts of the score, highest points first.
	Components []Component
	// FitsRules is whether no portion breaks a rule for certain.
	FitsRules bool
	// Warnings are the rules the portions would or may break.
	Warnings []Warning
	// Portions are the portions it would plan, by person and date.
	Portions []Portion
	// CookServings is how many servings to cook.
	CookServings float64
	// ItemsToBuy are what it needs bought beyond the pantry and the week's
	// shopping, by name, and NewItemCount how many of them the week
	// doesn't buy already.
	ItemsToBuy   []ItemToBuy
	NewItemCount int
}

// Suggest scores the recipes for the slot, and returns the best in.Max,
// highest score first, then by name.
func Suggest(in *Input) []*Suggestion {
	s := newScorer(in)
	candidates := in.Candidates
	if candidates == nil {
		candidates = in.Recipes
	}
	suggestions := make([]*Suggestion, 0, len(candidates))
	for _, r := range candidates {
		suggestions = append(suggestions, s.score(r))
	}
	slices.SortStableFunc(suggestions, func(a, b *Suggestion) int {
		if c := cmp.Compare(b.Score, a.Score); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Recipe.Name), strings.ToLower(b.Recipe.Name))
	})
	if in.Max > 0 && len(suggestions) > in.Max {
		suggestions = suggestions[:in.Max]
	}
	return suggestions
}

// bought is what the shopping list buys of a food, and takes from the
// pantry.
type bought struct {
	// listed is whether the list has an item of the food.
	listed bool
	// grams is the weight bought, rounded to whole packs or pieces, and
	// short what the needs lack from the pantry, before rounding.
	grams float64
	short float64
	// packs is the number of packs bought.
	packs int32
	// texts are the amounts of its items, e.g. "1 × 400 g".
	texts []string
}

// source is a cook of the week that uses a food.
type source struct {
	recipeID pgtype.UUID
	name     string
	date     time.Time
}

// scorer scores recipes for a slot, with what's computed once for all of
// them.
type scorer struct {
	in        *Input
	recipes   map[pgtype.UUID]*recipes.Recipe
	weekNeeds []shopping.Need
	// base is what the week's shopping buys, by food, and basePantry what
	// it takes from the pantry.
	base       map[pgtype.UUID]*bought
	basePantry map[pgtype.UUID]float64
	// sources are the cooks of the week using each food, by date.
	sources map[pgtype.UUID][]source
	// plannedGrams are the grams planned at each slot.
	plannedGrams map[plans.Slot]float64
}

func newScorer(in *Input) *scorer {
	s := &scorer{
		in:           in,
		recipes:      map[pgtype.UUID]*recipes.Recipe{},
		sources:      map[pgtype.UUID][]source{},
		plannedGrams: map[plans.Slot]float64{},
	}
	for _, r := range in.Recipes {
		s.recipes[r.ID] = r
	}
	s.weekNeeds = shopping.NeedsOf(in.Week, s.recipes)
	s.base = s.shop(s.weekNeeds)
	s.basePantry = shopping.PantryGrams(s.shopInput(s.weekNeeds))
	for _, c := range in.Week.Cooks {
		r, ok := s.recipes[c.RecipeID]
		if !ok {
			continue
		}
		// A component another cook supplies is that cook's ingredients.
		for _, ing := range r.IngredientsWithout(c.Servings, shopping.SuppliedComponents(c)) {
			f, ok := in.Foods[ing.FoodID]
			if !ok || f.AlwaysInStock || slices.ContainsFunc(s.sources[ing.FoodID], func(o source) bool { return o.recipeID == c.RecipeID }) {
				continue
			}
			s.sources[ing.FoodID] = append(s.sources[ing.FoodID], source{recipeID: c.RecipeID, name: c.RecipeName, date: c.Date})
		}
	}
	for slot, et := range in.Planned {
		s.plannedGrams[slot] = et.Amount.Grams
	}
	return s
}

// shop generates the shopping of needs, and returns what it buys of each
// food.
func (s *scorer) shop(needs []shopping.Need) map[pgtype.UUID]*bought {
	items := shopping.Generate(s.shopInput(needs))
	result := map[pgtype.UUID]*bought{}
	for _, it := range items {
		b, ok := result[it.FoodID]
		if !ok {
			b = &bought{listed: true}
			result[it.FoodID] = b
		}
		b.grams += it.Grams
		b.short += it.NeededGrams
		b.packs += it.Packages
		b.texts = append(b.texts, it.AmountText)
	}
	return result
}

// shopInput returns the input of the shopping of needs.
func (s *scorer) shopInput(needs []shopping.Need) *shopping.Input {
	return &shopping.Input{ShopDate: s.in.ShopDate, Needs: needs, Foods: s.in.Foods, Lots: s.in.Lots}
}

// boughtOf returns what's bought of a food, a zero value if nothing.
func boughtOf(m map[pgtype.UUID]*bought, id pgtype.UUID) *bought {
	if b, ok := m[id]; ok {
		return b
	}
	return &bought{}
}

// candidate is a recipe being scored.
type candidate struct {
	recipe     *recipes.Recipe
	nutrition  *nutrition.Recipe
	suggestion *Suggestion
	// foods are the foods of the ingredients to cook it, in order, without
	// optional ones and foods missing from the input; grams are the grams
	// of each, and needed the amounts as the shopping list's needs.
	foods  []pgtype.UUID
	grams  map[pgtype.UUID]float64
	needed []shopping.Need
	// with is what the week's shopping buys with the recipe, and
	// withPantry what it takes from the pantry.
	with       map[pgtype.UUID]*bought
	withPantry map[pgtype.UUID]float64
}

// add adds a component, unless it's worth nothing.
func (c *candidate) add(kind Kind, points float64, format string, args ...any) {
	points = math.Round(points*10) / 10
	if points == 0 {
		return
	}
	c.suggestion.Components = append(c.suggestion.Components, Component{Kind: kind, Points: points, Reason: fmt.Sprintf(format, args...)})
}

// score scores a recipe for the slot.
func (s *scorer) score(r *recipes.Recipe) *Suggestion {
	c := &candidate{
		recipe:     r,
		nutrition:  r.Nutrition(),
		suggestion: &Suggestion{Recipe: r, FitsRules: true},
		grams:      map[pgtype.UUID]float64{},
	}
	s.portion(c)
	for _, ing := range r.IngredientsFor(c.suggestion.CookServings) {
		f, ok := s.in.Foods[ing.FoodID]
		if !ok {
			continue
		}
		c.needed = append(c.needed, shopping.Need{FoodID: ing.FoodID, Date: s.in.Date, Amount: ing.Amount, Unit: ing.Unit})
		if _, seen := c.grams[ing.FoodID]; !seen {
			c.foods = append(c.foods, ing.FoodID)
		}
		grams, _ := f.Nutrition().Grams(ing.Amount, ing.Unit)
		c.grams[ing.FoodID] += grams
	}
	needs := append(slices.Clone(s.weekNeeds), c.needed...)
	c.with = s.shop(needs)
	c.withPantry = shopping.PantryGrams(s.shopInput(needs))

	s.sharedIngredients(c)
	s.packLeftovers(c)
	s.pantry(c)
	s.nutrition(c)
	s.rules(c)
	s.keeping(c)
	s.flavor(c)
	s.variety(c)
	s.mealTag(c)
	if r.Favorite {
		c.add(Favorite, FavoritePoints, "a household favorite")
	}

	sg := c.suggestion
	if s.in.Weights != nil {
		weighted := sg.Components[:0]
		for _, comp := range sg.Components {
			if w, ok := s.in.Weights[comp.Kind]; ok {
				comp.Points = math.Round(comp.Points*w*10) / 10
			}
			// A kind weighted to nothing doesn't count, so it isn't shown.
			if comp.Points != 0 {
				weighted = append(weighted, comp)
			}
		}
		sg.Components = weighted
	}
	slices.SortStableFunc(sg.Components, func(a, b Component) int { return cmp.Compare(b.Points, a.Points) })
	for _, comp := range sg.Components {
		sg.Score += comp.Points
	}
	sg.Score = math.Round(sg.Score*10) / 10
	return sg
}

// portion splits the recipe into the portions it would plan: like PlanMeal
// for a shared meal, or a serving within the person's maximum portion for
// an eating time.
func (s *scorer) portion(c *candidate) {
	gramsPerServing := c.nutrition.GramsPerServing
	sg := c.suggestion
	if s.in.Meal != people.MealNone {
		split, err := plans.SplitMeal(&plans.SplitInput{
			Date:            s.in.Date,
			Meal:            s.in.Meal,
			People:          s.in.People,
			GramsPerServing: gramsPerServing,
			PlannedGrams:    s.plannedGrams,
		})
		if err != nil {
			// The people are checked to eat from the meal, so this is a
			// recipe that can't be split: score it without portions.
			sg.CookServings = 1
			return
		}
		sg.CookServings = split.Servings
		for _, share := range split.Shares {
			p := s.person(share.PersonID)
			sg.Portions = append(sg.Portions, Portion{
				Share:      share,
				Person:     p,
				EatingTime: p.EatingTime(share.EatingTimeID),
				Grams:      share.Servings * gramsPerServing,
			})
		}
		return
	}
	p := s.in.People[0]
	slot := plans.Slot{PersonID: p.ID, EatingTimeID: s.in.EatingTimeID, Date: s.in.Date}
	servings, cookServings := plans.EatingTimeServings(p, gramsPerServing, s.plannedGrams[slot])
	sg.CookServings = cookServings
	sg.Portions = []Portion{{
		Share:      plans.Share{Slot: slot, Servings: servings},
		Person:     p,
		EatingTime: p.EatingTime(s.in.EatingTimeID),
		Grams:      servings * gramsPerServing,
	}}
}

// person returns the eater with the ID.
func (s *scorer) person(id pgtype.UUID) *people.Person {
	for _, p := range s.in.People {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// sharedIngredients scores the ingredients the week's other cooks use, and
// the new items the recipe adds to the shop.
func (s *scorer) sharedIngredients(c *candidate) {
	// Shared foods are grouped by the first other cook using them, for one
	// reason each.
	type group struct {
		source source
		names  []string
		points float64
	}
	var groups []*group
	var total float64
	for _, id := range c.foods {
		f := s.in.Foods[id]
		if f.AlwaysInStock {
			continue
		}
		i := slices.IndexFunc(s.sources[id], func(o source) bool { return o.recipeID != c.recipe.ID })
		if i < 0 {
			continue
		}
		src := s.sources[id][i]
		points := SharedIngredientPoints
		if isFresh(f) {
			points += FreshSharedPoints
		}
		points = min(points, MaxSharedPoints-total)
		if points <= 0 {
			break
		}
		total += points
		j := slices.IndexFunc(groups, func(g *group) bool { return g.source == src })
		if j < 0 {
			groups = append(groups, &group{source: src})
			j = len(groups) - 1
		}
		groups[j].names = append(groups[j].names, foodName(f))
		groups[j].points += points
	}
	for _, g := range groups {
		c.add(SharedIngredients, g.points, "shares %s with %s's %s", list(g.names), g.source.date.Weekday(), g.source.name)
	}

	// The items to buy are those whose purchase grows with the recipe.
	sg := c.suggestion
	for _, id := range c.foods {
		f := s.in.Foods[id]
		base, with := boughtOf(s.base, id), boughtOf(c.with, id)
		switch {
		case !with.listed:
			continue
		case !base.listed:
			sg.ItemsToBuy = append(sg.ItemsToBuy, ItemToBuy{Food: f, AmountText: strings.Join(with.texts, " + ")})
			sg.NewItemCount++
		case with.grams-base.grams > crumbGrams:
			text := shopping.FormatGrams(with.grams-base.grams) + " more"
			if packs := with.packs - base.packs; packs > 0 && f.PackageGrams > 0 {
				text = fmt.Sprintf("%d more × %s", packs, shopping.FormatGrams(f.PackageGrams))
			}
			sg.ItemsToBuy = append(sg.ItemsToBuy, ItemToBuy{Food: f, AmountText: text, AlreadyListed: true})
		}
	}
	slices.SortStableFunc(sg.ItemsToBuy, func(a, b ItemToBuy) int {
		return strings.Compare(strings.ToLower(a.Food.Name), strings.ToLower(b.Food.Name))
	})
	n := sg.NewItemCount
	points := max(float64(FreeNewItems-n)*NewItemPoints, MinNewItemPoints)
	switch {
	case n == 0:
		c.add(SharedIngredients, points, "nothing new to buy")
	case n < FreeNewItems:
		c.add(SharedIngredients, points, "only %s to buy", count(n, "new item", "new items"))
	default:
		c.add(SharedIngredients, points, "%s to buy", count(n, "new item", "new items"))
	}
}

// packLeftovers scores using what's left of packs the week's shopping
// buys.
func (s *scorer) packLeftovers(c *candidate) {
	var total float64
	for _, id := range c.foods {
		f := s.in.Foods[id]
		base, with := boughtOf(s.base, id), boughtOf(c.with, id)
		left := base.grams - base.short
		if base.grams <= 0 || left < MinLeftoverGrams {
			continue
		}
		used := min(with.short-base.short, left)
		if used < MinLeftoverGrams {
			continue
		}
		points := min(PackLeftoverPoints*used/left, MaxPackLeftoverPoints-total)
		if points <= 0 {
			break
		}
		total += points
		// The cook it's bought for is named when there's just one.
		boughtFor := "this week"
		if srcs := s.sources[id]; len(srcs) == 1 {
			boughtFor = fmt.Sprintf("for %s's %s", srcs[0].date.Weekday(), srcs[0].name)
		}
		all := used >= left-crumbGrams
		switch {
		case f.PackageGrams > 0 && base.packs == 1 && all:
			c.add(PackLeftovers, points, "uses the other %s of the %s pack of %s bought %s",
				shopping.FormatGrams(left), shopping.FormatGrams(f.PackageGrams), foodName(f), boughtFor)
		case all:
			c.add(PackLeftovers, points, "uses the %s of %s left over from what's bought %s",
				shopping.FormatGrams(left), foodName(f), boughtFor)
		default:
			c.add(PackLeftovers, points, "uses %s of the %s of %s left over from what's bought %s",
				shopping.FormatGrams(used), shopping.FormatGrams(left), foodName(f), boughtFor)
		}
	}
}

// pantry scores using what's in the pantry, more for lots to use soon.
func (s *scorer) pantry(c *candidate) {
	var total float64
	for _, id := range c.foods {
		f := s.in.Foods[id]
		used := c.withPantry[id] - s.basePantry[id]
		if used < crumbGrams || c.grams[id] <= 0 {
			continue
		}
		points := PantryPoints * min(used/c.grams[id], 1)
		// The lot it's used from is the first of those usable on the date,
		// like the shopping list's.
		var useBy time.Time
		var lots []*pantry.Item
		for _, lot := range s.in.Lots {
			if lot.FoodID == id {
				lots = append(lots, lot)
			}
		}
		pantry.SortByUseBy(lots)
		for _, lot := range lots {
			if u := lot.UseBy(); u.IsZero() || !u.Before(s.in.Date) {
				useBy = u
				break
			}
		}
		soon := !useBy.IsZero() && pantry.DaysTo(s.in.Date, useBy) <= UseSoonDays
		if soon {
			points += UseSoonPoints
		}
		points = min(points, MaxPantryPoints-total)
		if points <= 0 {
			break
		}
		total += points
		if soon {
			c.add(Pantry, points, "uses %s of %s from the pantry, best used by %s", shopping.FormatGrams(used), foodName(f), formatDate(useBy))
		} else {
			c.add(Pantry, points, "uses %s of %s from the pantry", shopping.FormatGrams(used), foodName(f))
		}
	}
}

// nutrition scores how the portions move each eater's day toward their
// daily minimums, and over their maximums.
func (s *scorer) nutrition(c *candidate) {
	type day struct {
		person *people.Person
		date   time.Time
		added  nutrition.Amount
	}
	var days []*day
	for _, p := range c.suggestion.Portions {
		i := slices.IndexFunc(days, func(d *day) bool { return d.person == p.Person && d.date.Equal(p.Date) })
		if i < 0 {
			days = append(days, &day{person: p.Person, date: p.Date})
			i = len(days) - 1
		}
		a := c.nutrition.Servings(p.Servings)
		days[i].added.Add(&a)
	}
	first := len(c.suggestion.Components)
	var positive float64
	for _, d := range days {
		before := s.dayTotal(d.person, d.date)
		for _, t := range d.person.Targets {
			added := d.added.Nutrients[t.Nutrient]
			if added == nil || *added <= 0 {
				continue
			}
			value, _ := before.Value(t.Nutrient)
			after := value + *added
			label, unit := strings.ToLower(t.Nutrient.Label()[:1])+t.Nutrient.Label()[1:], t.Nutrient.Unit()
			if t.Min != nil && *t.Min > 0 {
				filled := max(*t.Min-value, 0) - max(*t.Min-after, 0)
				if filled > 0 {
					points := NutritionMinPoints * filled / *t.Min
					positive += math.Round(points*10) / 10
					c.add(Nutrition, points, "adds %s %s of %s toward %s's daily minimum of %s %s",
						format(filled), unit, label, d.person.Name, format(*t.Min), unit)
				}
			}
			if t.Max != nil && after > *t.Max+slack {
				over := after - max(value, *t.Max)
				c.add(Nutrition, -(NutritionMaxPenalty + NutritionMinPoints*over / *t.Max),
					"takes %s's %s to %s %s, over the daily maximum of %s %s",
					d.person.Name, label, format(after), unit, format(*t.Max), unit)
			}
		}
	}
	// Many targets filled at once are capped, scaling each down, so that
	// nutrition doesn't outweigh everything else.
	if positive > MaxNutritionPoints {
		for i := first; i < len(c.suggestion.Components); i++ {
			comp := &c.suggestion.Components[i]
			if comp.Points > 0 {
				comp.Points = math.Round(comp.Points*MaxNutritionPoints/positive*10) / 10
			}
		}
	}
}

// dayTotal returns what's planned for a person on a date.
func (s *scorer) dayTotal(p *people.Person, date time.Time) nutrition.Amount {
	var total nutrition.Amount
	for _, et := range p.EatingTimes {
		if planned, ok := s.in.Planned[plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: date}]; ok {
			total.Add(&planned.Amount)
		}
	}
	return total
}

// checkedKinds are the rules checked for each portion. Weight unknown is
// left out: it's about the foods' data, not the recipe.
var checkedKinds = []rules.Kind{
	rules.PortionTooLarge, rules.ProteinLow, rules.CarbohydrateHigh,
	rules.CarbohydrateUnpaired, rules.SugarsHigh, rules.FastCarbs,
}

// rules scores the eating times of the portions against their people's
// rules: a penalty for each rule a portion would break that wasn't broken
// already, and points for meeting a minimum of protein.
func (s *scorer) rules(c *candidate) {
	var fastCarbs []string
	for _, ing := range c.recipe.IngredientsFor(float64(c.recipe.Servings)) {
		if ing.FastCarb && !slices.Contains(fastCarbs, ing.FoodName) {
			fastCarbs = append(fastCarbs, ing.FoodName)
		}
	}
	met := map[*people.Person]bool{}
	for _, p := range c.suggestion.Portions {
		if p.EatingTime == nil {
			continue
		}
		before := &rules.EatingTime{Name: p.EatingTime.Name}
		if planned, ok := s.in.Planned[p.Slot]; ok {
			before = planned
		}
		after := &rules.EatingTime{
			Name:           p.EatingTime.Name,
			Amount:         before.Amount,
			FastCarbFoods:  slices.Clone(before.FastCarbFoods),
			UnweighedNames: before.UnweighedNames,
		}
		a := c.nutrition.Servings(p.Servings)
		after.Amount.Add(&a)
		for _, name := range fastCarbs {
			if !slices.Contains(after.FastCarbFoods, name) {
				after.FastCarbFoods = append(after.FastCarbFoods, name)
			}
		}
		broken := map[rules.Kind]bool{}
		for _, w := range rules.CheckEatingTime(&p.Person.Rules, before) {
			broken[w.Kind] = true
		}
		for _, w := range rules.CheckEatingTime(&p.Person.Rules, after) {
			if broken[w.Kind] || !slices.Contains(checkedKinds, w.Kind) {
				continue
			}
			// Protein low where the rest of a serving is moved to a later
			// small meal is easy to fix by adding a protein food there, so
			// it weighs less, and doesn't break the rules by itself.
			spilled := w.Kind == rules.ProteinLow && (p.EatingTimeID != s.mealEatingTime(p.Person) || !p.Date.Equal(s.in.Date))
			penalty := RuleWarningPenalty
			switch {
			case w.Uncertain:
				penalty = UncertainRulePenalty
			case spilled:
				penalty = SpilledProteinPenalty
			default:
				c.suggestion.FitsRules = false
			}
			c.add(Rules, -penalty, "%s", ruleReason(p.Person.Name, p.EatingTime.Name, &w, after))
			w.Message = p.Person.Name + ": " + w.Message
			c.suggestion.Warnings = append(c.suggestion.Warnings, Warning{
				Warning:      w,
				PersonID:     p.PersonID,
				EatingTimeID: p.EatingTimeID,
				Date:         p.Date,
			})
		}
		// A minimum of protein met at the person's first portion is worth
		// points, as it's the hardest rule to meet with small portions.
		limit := p.Person.Rules.MinProteinGrams
		if limit == nil || met[p.Person] {
			continue
		}
		met[p.Person] = true
		protein, _ := after.Amount.Value(nutrition.ProteinGrams)
		previous, _ := before.Amount.Value(nutrition.ProteinGrams)
		if protein >= *limit-slack && previous < *limit-slack {
			c.add(Rules, ProteinMetPoints, "gives %s %s g of protein at %s, meeting the minimum of %s g",
				p.Person.Name, format(protein), p.EatingTime.Name, format(*limit))
		}
	}
}

// mealEatingTime returns the eating time of a person at the slot: theirs for
// the shared meal, or the slot's eating time.
func (s *scorer) mealEatingTime(p *people.Person) pgtype.UUID {
	if s.in.Meal == people.MealNone {
		return s.in.EatingTimeID
	}
	for _, et := range p.EatingTimes {
		if et.Meal == s.in.Meal {
			return et.ID
		}
	}
	return pgtype.UUID{}
}

// ruleReason says briefly which rule a portion breaks.
func ruleReason(person, eatingTime string, w *rules.Warning, et *rules.EatingTime) string {
	at := person + "'s " + eatingTime
	may := "would"
	if w.Uncertain {
		may = "may"
	}
	switch w.Kind {
	case rules.PortionTooLarge:
		return fmt.Sprintf("%s %s weigh %s g, over the maximum portion of %s g", at, may, format(w.Value), format(w.Limit))
	case rules.ProteinLow:
		return fmt.Sprintf("%s %s have %s g of protein, under the minimum of %s g", at, may, format(w.Value), format(w.Limit))
	case rules.CarbohydrateHigh:
		return fmt.Sprintf("%s %s have %s g of carbohydrate, over the maximum of %s g", at, may, format(w.Value), format(w.Limit))
	case rules.CarbohydrateUnpaired:
		return fmt.Sprintf("%s %s have a protein and fat to carbohydrate ratio of %.2f, under the minimum of %.2f", at, may, w.Value, w.Limit)
	case rules.SugarsHigh:
		return fmt.Sprintf("%s %s have %s g of sugars, over the maximum of %s g", at, may, format(w.Value), format(w.Limit))
	case rules.FastCarbs:
		return fmt.Sprintf("%s %s have fast carbs: %s", at, may, list(et.FastCarbFoods))
	default:
		return at + ": " + w.Message
	}
}

// keeping scores whether the fresh foods to buy keep from the shop until
// the slot, and the leftovers until they're eaten.
func (s *scorer) keeping(c *candidate) {
	days := pantry.DaysTo(s.in.ShopDate, s.in.Date)
	freshKeeps := false
	topUps := 0
	for _, id := range c.foods {
		f := s.in.Foods[id]
		if f.AlwaysInStock || f.ShelfLifeDays <= 0 || boughtOf(c.with, id).short-boughtOf(s.base, id).short <= crumbGrams {
			continue
		}
		if days > int(f.ShelfLifeDays) {
			topUps++
			c.add(Keeping, -TopUpPenalty, "%s keeps %s, so it won't keep from the shop on %s until %s: buy it on a second trip",
				foodName(f), count(int(f.ShelfLifeDays), "day", "days"), formatDate(s.in.ShopDate), formatDate(s.in.Date))
		} else if isFresh(f) {
			freshKeeps = true
		}
	}
	if freshKeeps && topUps == 0 && days >= KeepsMinDays {
		c.add(Keeping, KeepsPoints, "its fresh foods keep from the shop on %s until %s", formatDate(s.in.ShopDate), formatDate(s.in.Date))
	}

	// Leftovers are the portions on later days than the cook.
	cook := &rules.Cook{RecipeName: c.recipe.Name, Date: s.in.Date, FridgeDays: c.recipe.FridgeDays, Freezable: c.recipe.Freezable}
	var last time.Time
	var who []string
	for _, p := range c.suggestion.Portions {
		if !p.Date.After(s.in.Date) {
			continue
		}
		if p.Date.After(last) {
			last = p.Date
		}
		if !slices.Contains(who, p.Person.Name) {
			who = append(who, p.Person.Name)
		}
	}
	if last.IsZero() {
		return
	}
	switch w := rules.CheckPortion(cook, last); {
	case w == nil && cook.FridgeDays != nil:
		c.add(Keeping, LeftoverKeepsPoints, "keeps %s in the fridge, for %s's leftovers on %s",
			count(int(*cook.FridgeDays), "day", "days"), list(who), formatDate(last))
	case w != nil && w.Kind == rules.LeftoverFreeze:
		c.add(Keeping, -LeftoverFreezePenalty, "keeps %s in the fridge: freeze %s's leftovers for %s",
			count(int(*cook.FridgeDays), "day", "days"), list(who), formatDate(last))
	case w != nil && w.Kind == rules.LeftoverPastFridgeDays:
		c.add(Keeping, -LeftoverSpoilsPenalty, "keeps %s in the fridge, not until %s's leftovers on %s",
			count(int(*cook.FridgeDays), "day", "days"), list(who), formatDate(last))
	}
}

// flavor scores the pairings of the recipe's ingredients.
func (s *scorer) flavor(c *candidate) {
	ingredients := c.recipe.IngredientsFor(float64(c.recipe.Servings))
	names := make([]string, 0, len(ingredients))
	for _, ing := range ingredients {
		names = append(names, ing.FoodName)
	}
	var total float64
	for _, p := range matchPairings(names) {
		points := min(FlavorPoints, MaxFlavorPoints-total)
		if points <= 0 {
			break
		}
		total += points
		c.add(Flavor, points, "%s go well together", p.Name)
	}
}

// variety penalizes the recipe's cooks on nearby days, and its staples of
// the eaters on the slot's date.
func (s *scorer) variety(c *candidate) {
	for _, cook := range s.in.Cooks {
		if cook.RecipeID != c.recipe.ID {
			continue
		}
		days := pantry.DaysTo(s.in.Date, cook.Date)
		if days < 0 {
			days = -days
		}
		if days > VarietyDays {
			continue
		}
		penalty := VarietyPenalty
		if days <= RecentDays {
			penalty += RecentPenalty
		}
		c.add(Variety, -penalty, "already planned on %s", formatDate(cook.Date))
	}
	// A recipe an eater has as a staple that day, like their usual
	// breakfast, is eaten often enough already.
	for _, st := range s.in.Week.Staples {
		if st.RecipeID != c.recipe.ID || !st.OccursOn(s.in.Date) {
			continue
		}
		p := s.person(st.PersonID)
		if p == nil {
			continue
		}
		name := "a meal"
		if et := p.EatingTime(st.EatingTimeID); et != nil {
			name = et.Name
		}
		c.add(Variety, -(VarietyPenalty + RecentPenalty), "already %s's staple at %s", p.Name, name)
	}
}

// mealTags are the tags that say which meal a recipe is for. "snack" is
// for small eating times.
var mealTags = []string{"breakfast", "lunch", "dinner", "snack"}

// mealTag scores the recipe's tags for meals: points if it's tagged for
// the slot's meal, a penalty if it's tagged only for others. Lunch and
// dinner are interchangeable, so neither counts against the other, and any
// recipe can make a small meal.
func (s *scorer) mealTag(c *candidate) {
	var et *people.EatingTime
	if s.in.Meal == people.MealNone {
		et = s.in.People[0].EatingTime(s.in.EatingTimeID)
	}
	want := slotTag(s.in.Meal, et)
	if want == "" {
		return
	}
	tagged := recipeMealTags(c.recipe)
	switch {
	case len(tagged) == 0:
	case slices.Contains(tagged, want):
		c.add(MealTag, MealTagPoints, "tagged %s", want)
	case want == "snack":
		// Small meals are often a part of a main meal's recipe, so a recipe
		// for another meal isn't out of place at one.
	case (want == "lunch" && slices.Contains(tagged, "dinner")) || (want == "dinner" && slices.Contains(tagged, "lunch")):
	default:
		c.add(MealTag, -OtherMealTagPenalty, "tagged %s, not %s", list(tagged), want)
	}
}

// slotTag returns the meal tag of a slot: its shared meal's, or for one
// person's eating time, its meal's, or "snack" for a small one. It's ""
// for a main eating time of no shared meal.
func slotTag(meal people.Meal, et *people.EatingTime) string {
	switch {
	case meal != people.MealNone:
		return string(meal)
	case et == nil:
		return ""
	case et.Meal != people.MealNone:
		return string(et.Meal)
	case et.Kind == people.KindSmall:
		return "snack"
	default:
		return ""
	}
}

// recipeMealTags returns the meal tags of a recipe, in lower case.
func recipeMealTags(r *recipes.Recipe) []string {
	var tagged []string
	for _, tag := range r.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if slices.Contains(mealTags, tag) && !slices.Contains(tagged, tag) {
			tagged = append(tagged, tag)
		}
	}
	return tagged
}

// FitsMeal reports whether a plan may put a recipe at a slot of a meal
// tag, as returned by slotTag. Breakfast is its own kind of food, so it
// takes only recipes tagged "breakfast": a chili isn't breakfast, and a
// recipe without meal tags is most likely a main meal. Every other slot
// takes any recipe, a breakfast for lunch too, with the meal tags only
// counting in its score.
func FitsMeal(r *recipes.Recipe, tag string) bool {
	return tag != "breakfast" || slices.Contains(recipeMealTags(r), "breakfast")
}

// isFresh reports whether a food is fresh: it keeps a week or less, or is
// produce of unknown shelf life.
func isFresh(f *foods.Food) bool {
	if f.ShelfLifeDays > 0 {
		return f.ShelfLifeDays <= FreshShelfLifeDays
	}
	return f.StoreSection == foods.StoreSectionProduce
}

// foodName returns a food's name for the middle of a sentence. A name
// with a short qualifier after a comma, the way food databases name foods,
// reads in order: "Chickpeas, canned" is "canned chickpeas". Names with
// more qualifiers are kept, as reordering them would garble them.
func foodName(f *foods.Food) string {
	name := f.Name
	if base, qualifier, ok := strings.Cut(name, ", "); ok && !strings.Contains(qualifier, ",") && len(strings.Fields(qualifier)) <= 2 {
		name = qualifier + " " + lowerFirst(base)
	}
	return lowerFirst(name)
}

// lowerFirst lowercases the first letter of a name, unless it starts an
// acronym like "TVP".
func lowerFirst(name string) string {
	if name == "" || (len(name) >= 2 && name[1] >= 'A' && name[1] <= 'Z') {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}

// format formats an amount, with fewer decimals the larger it is: "412",
// "6.4" or "0.38".
func format(v float64) string {
	digits := 2
	switch abs := math.Abs(v); {
	case abs >= 100:
		digits = 0
	case abs >= 1:
		digits = 1
	}
	scale := math.Pow10(digits)
	return fmt.Sprint(math.Round(v*scale) / scale)
}

// formatDate formats a date as e.g. "Tue 3 Nov".
func formatDate(t time.Time) string {
	return t.Format("Mon 2 Jan")
}

// count formats a count with its noun, e.g. "1 new item" or "3 days".
func count(n int, one, other string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, other)
}

// list joins names into an English list, e.g. "A, B and C".
func list(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
