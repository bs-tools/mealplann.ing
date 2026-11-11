package suggest

import (
	"strings"
	"unicode"

	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/people"
)

// The weights of the eaters' tastes. A dislike weighs about as much as a
// broken rule, as a meal someone won't eat is wasted however well it fits;
// a like, a sweet or savory small meal and the spice weigh less, as they
// say what someone prefers, not what they won't eat.
const (
	// DislikePenalty is for each eater who dislikes something in the
	// recipe, and LikePoints for each who likes something in it.
	DislikePenalty = 8.0
	LikePoints     = 2.0
	// SmallMealTastePoints is for a small meal that's sweet or savory as
	// its eater prefers, and SmallMealTastePenalty for one that's not.
	SmallMealTastePoints  = 2.0
	SmallMealTastePenalty = 3.0
	// TooSpicyPenalty is for a spicy recipe for an eater who likes it
	// mild, and SpicyPoints for one who likes it hot.
	TooSpicyPenalty = 4.0
	SpicyPoints     = 1.0
	// sweetSugarsShare is the share of a recipe's carbohydrate in sugars
	// from which it counts as sweet, without a "sweet" tag.
	sweetSugarsShare = 0.5
)

// tastes scores the recipe against its eaters' tastes: what they like and
// dislike, whether they prefer a small meal sweet or savory, and how spicy
// they like food.
func (s *scorer) tastes(c *candidate) {
	terms := c.recipe.Name + "\n" + strings.Join(c.recipe.Tags, "\n")
	for _, id := range c.foods {
		terms += "\n" + foodName(s.in.Foods[id])
	}
	terms = strings.ToLower(terms)
	spicy := hasTag(c.recipe.Tags, "spicy", "hot")
	sweet := hasTag(c.recipe.Tags, "sweet", "dessert") || sugarsShare(c.nutrition) >= sweetSugarsShare
	seen := map[*people.Person]bool{}
	for _, p := range c.suggestion.Portions {
		person := p.Person
		if person == nil || seen[person] {
			continue
		}
		seen[person] = true
		taste := &person.Profile
		if item, ok := matchTaste(terms, taste.Dislikes); ok {
			c.add(Tastes, -DislikePenalty, "%s dislikes %s", person.Name, item)
		} else if item, ok := matchTaste(terms, taste.Likes); ok {
			c.add(Tastes, LikePoints, "%s likes %s", person.Name, item)
		}
		if p.EatingTime != nil && p.EatingTime.Kind == people.KindSmall && s.in.Meal == people.MealNone {
			switch {
			case taste.SmallMeals == people.TasteEither:
			case (taste.SmallMeals == people.TasteSweet) == sweet:
				c.add(Tastes, SmallMealTastePoints, "%s likes small meals %s", person.Name, taste.SmallMeals)
			default:
				c.add(Tastes, -SmallMealTastePenalty, "%s likes small meals %s", person.Name, taste.SmallMeals)
			}
		}
		switch {
		case !spicy:
		case taste.Spice == people.SpiceMild:
			c.add(Tastes, -TooSpicyPenalty, "spicy, and %s likes food mild", person.Name)
		case taste.Spice == people.SpiceHot:
			c.add(Tastes, SpicyPoints, "spicy, as %s likes it", person.Name)
		}
	}
}

// matchTaste returns the first like or dislike the recipe's terms
// mention, ignoring case. It must start a word, so "rice" doesn't match
// "licorice", but may end within one, so "mushroom" matches "mushrooms".
func matchTaste(terms string, items []string) (string, bool) {
	for _, item := range items {
		want := strings.ToLower(strings.TrimSpace(item))
		if want == "" {
			continue
		}
		for rest, at := terms, 0; ; {
			i := strings.Index(rest, want)
			if i < 0 {
				break
			}
			at += i
			if at == 0 || !unicode.IsLetter(rune(terms[at-1])) {
				return item, true
			}
			at++
			rest = terms[at:]
		}
	}
	return "", false
}

// hasTag reports whether a recipe's tags include any of the words, as a
// tag or the value of a "category: value" tag, ignoring case.
func hasTag(tags []string, words ...string) bool {
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if _, value, ok := strings.Cut(tag, ":"); ok {
			tag = strings.TrimSpace(value)
		}
		for _, word := range words {
			if tag == word {
				return true
			}
		}
	}
	return false
}

// sugarsShare returns the share of a recipe's carbohydrate in sugars, 0 if
// either isn't known.
func sugarsShare(n *nutrition.Recipe) float64 {
	sugars, carbohydrate := n.PerServing[nutrition.SugarsGrams], n.PerServing[nutrition.CarbohydrateGrams]
	if sugars == nil || carbohydrate == nil || *carbohydrate <= 0 {
		return 0
	}
	return *sugars / *carbohydrate
}
