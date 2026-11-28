package plan

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// Bounds of picks: how many recipes are picked for a shop, and how many
// suggestions are returned.
const (
	maxPicks               = 5
	defaultPickSuggestions = 10
	maxPickSuggestions     = 30
	// pickPlanDays is how many days of the plan, from the shop, count as
	// bought already, for the picks to share ingredients with.
	pickPlanDays = 7
)

// Points of a pick: for each food to use soon it uses, each food the
// pantry has, and each it shares with the picks and the plan, up to their
// caps; for being a favorite; and against each food to buy.
const (
	pickUseSoonPoints = 4.0
	pickPantryPoints  = 1.0
	maxPickPantry     = 5.0
	pickSharedPoints  = 1.5
	maxPickShared     = 6.0
	pickFavoritePoint = 2.0
	pickToBuyPenalty  = 0.75
)

func (s *Service) ListAnytimeCooks(ctx context.Context, _ *plansv1.ListAnytimeCooksRequest) (*plansv1.ListAnytimeCooksResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	cooks, err := s.plans.ListAnytimeCooks(ctx, user.HouseholdID())
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to list the cooks to cook any time", "")
	}
	out := make([]*plansv1.Cook, 0, len(cooks))
	for _, c := range cooks {
		out = append(out, cookToProto(c))
	}
	return plansv1.ListAnytimeCooksResponse_builder{Cooks: out}.Build(), nil
}

func (s *Service) SuggestPicks(ctx context.Context, req *suggestionsv1.SuggestPicksRequest) (*suggestionsv1.SuggestPicksResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	date := time.Now().UTC().Truncate(24 * time.Hour)
	if req.GetDate() != "" {
		var err error
		if date, err = parseDate("date", req.GetDate()); err != nil {
			return nil, err
		}
	}
	if len(req.GetPickedRecipeIds()) > maxPicks {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "picked_recipe_ids has at most %d recipes", maxPicks)
	}
	picked := make([]pgtype.UUID, 0, len(req.GetPickedRecipeIds()))
	for i, id := range req.GetPickedRecipeIds() {
		parsed, err := parseID(fmt.Sprintf("picked_recipe_ids[%d]", i), id, "find recipes with ListRecipes")
		if err != nil {
			return nil, err
		}
		picked = append(picked, parsed)
	}
	n := int(req.GetMaxSuggestions())
	if n < 0 || n > maxPickSuggestions {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "max_suggestions must be from 1 to %d, or 0 for %d", maxPickSuggestions, defaultPickSuggestions)
	}
	if n == 0 {
		n = defaultPickSuggestions
	}
	householdID := user.HouseholdID()
	var all []*recipes.Recipe
	var lots []*pantry.Item
	var plan *plans.Plan
	var recipesErr, lotsErr, planErr error
	var wg sync.WaitGroup
	wg.Go(func() { all, _, recipesErr = s.recipes.ListRecipes(ctx, householdID, recipes.ListFilter{}) })
	wg.Go(func() { lots, lotsErr = s.pantry.ListItems(ctx, householdID) })
	wg.Go(func() { plan, planErr = s.plans.GetPlan(ctx, householdID, date, date.AddDate(0, 0, pickPlanDays-1)) })
	wg.Wait()
	if err := cmp.Or(recipesErr, lotsErr, planErr); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read the recipes, pantry and plan", "")
	}
	byID := make(map[pgtype.UUID]*recipes.Recipe, len(all))
	var foodIDs []pgtype.UUID
	for _, r := range all {
		byID[r.ID] = r
		for _, in := range r.IngredientsWithout(float64(r.Servings), nil) {
			foodIDs = append(foodIDs, in.FoodID)
		}
	}
	for _, id := range picked {
		if byID[id] == nil {
			return nil, connect.NewError(connect.CodeNotFound, "picked_recipe_ids has a recipe that isn't the household's: find recipes with ListRecipes")
		}
	}
	foodList, err := s.foods.ListFoodsByID(ctx, householdID, unique(foodIDs))
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read the foods", "")
	}
	p := newPicker(date, lots, foodList)
	// What the plan's next days and the picks need comes out of the pantry
	// first, and is bought in the shop.
	// Recipes cooked in the plan's next days, or picked already, aren't
	// suggested again.
	planned := map[pgtype.UUID]bool{}
	for _, c := range plan.Cooks {
		if r := byID[c.RecipeID]; r != nil && !c.Done {
			p.add(r, c.Servings, r.Name)
			planned[r.ID] = true
		}
	}
	var pickedToBuy []string
	for _, id := range picked {
		r := byID[id]
		for _, name := range p.add(r, float64(r.Servings), r.Name) {
			if !slices.Contains(pickedToBuy, name) {
				pickedToBuy = append(pickedToBuy, name)
			}
		}
	}
	var suggestions []*suggestionsv1.PickSuggestion
	for _, r := range all {
		if slices.Contains(picked, r.ID) || planned[r.ID] || !r.Suggestable(date) || !mainDish(r) {
			continue
		}
		if sg := p.score(r); sg != nil {
			suggestions = append(suggestions, sg)
		}
	}
	slices.SortStableFunc(suggestions, func(a, b *suggestionsv1.PickSuggestion) int {
		return cmp.Or(cmp.Compare(b.GetScore(), a.GetScore()), strings.Compare(strings.ToLower(a.GetRecipeName()), strings.ToLower(b.GetRecipeName())))
	})
	return suggestionsv1.SuggestPicksResponse_builder{
		Suggestions: suggestions[:min(n, len(suggestions))], PickedToBuy: pickedToBuy,
	}.Build(), nil
}

// mainDish reports whether a recipe is a lunch or dinner to pick: tagged
// for one, or for no meal and not a side or a basic, like a pot of rice.
func mainDish(r *recipes.Recipe) bool {
	var meals []string
	for _, tag := range r.Tags {
		switch t := strings.ToLower(tag); t {
		case "breakfast", "lunch", "dinner", "snack":
			meals = append(meals, t)
		case "side", "basics", "sauce", "dressing", "drink":
			return false
		}
	}
	return len(meals) == 0 || slices.Contains(meals, "lunch") || slices.Contains(meals, "dinner")
}

// picker scores recipes to pick against the pantry, and what the plan
// and the picks need of it.
type picker struct {
	date  time.Time
	foods map[pgtype.UUID]*foods.Food
	// left is what the pantry has of each food, less what's needed of it
	// already, and soon the earliest use-by of a lot to use soon.
	left map[pgtype.UUID]float64
	soon map[pgtype.UUID]time.Time
	// users are the names of the recipes needing each food.
	users map[pgtype.UUID][]string
}

func newPicker(date time.Time, lots []*pantry.Item, foodList []*foods.Food) *picker {
	p := &picker{
		date: date, foods: map[pgtype.UUID]*foods.Food{}, left: map[pgtype.UUID]float64{},
		soon: map[pgtype.UUID]time.Time{}, users: map[pgtype.UUID][]string{},
	}
	for _, f := range foodList {
		p.foods[f.ID] = f
	}
	for _, lot := range lots {
		useBy := lot.UseBy()
		if !useBy.IsZero() && useBy.Before(date) {
			continue
		}
		p.left[lot.FoodID] += lot.Grams
		if !useBy.IsZero() && pantry.DaysTo(date, useBy) <= useSoonDays {
			if have, ok := p.soon[lot.FoodID]; !ok || useBy.Before(have) {
				p.soon[lot.FoodID] = useBy
			}
		}
	}
	return p
}

// needs returns the grams of each food a recipe needs for servings, in
// the order of its ingredients, foods always in stock left out.
func (p *picker) needs(r *recipes.Recipe, servings float64) ([]pgtype.UUID, map[pgtype.UUID]float64) {
	var order []pgtype.UUID
	grams := map[pgtype.UUID]float64{}
	for _, in := range r.IngredientsWithout(servings, nil) {
		f := p.foods[in.FoodID]
		if f == nil || f.AlwaysInStock || in.Optional {
			continue
		}
		g, ok := f.Nutrition().Grams(in.Amount, in.Unit)
		if !ok {
			continue
		}
		if _, seen := grams[in.FoodID]; !seen {
			order = append(order, in.FoodID)
		}
		grams[in.FoodID] += g
	}
	return order, grams
}

// add takes what a recipe needs out of what's left, noting the recipe as
// using its foods, and returns the names of those to buy.
func (p *picker) add(r *recipes.Recipe, servings float64, name string) []string {
	order, grams := p.needs(r, servings)
	var toBuy []string
	for _, id := range order {
		if p.left[id] < grams[id]-1e-9 {
			toBuy = append(toBuy, p.foods[id].Name)
		}
		p.left[id] = math.Max(p.left[id]-grams[id], 0)
		if !slices.Contains(p.users[id], name) {
			p.users[id] = append(p.users[id], name)
		}
	}
	return toBuy
}

// score scores a recipe to pick, nil if it needs nothing that can be
// weighed.
func (p *picker) score(r *recipes.Recipe) *suggestionsv1.PickSuggestion {
	order, grams := p.needs(r, float64(r.Servings))
	if len(order) == 0 {
		return nil
	}
	var score, pantryPoints, sharedPoints float64
	var useSoon, shared, toBuy []string
	sharedWith := map[string]bool{}
	for _, id := range order {
		f := p.foods[id]
		short := strings.ToLower(shortName(f.Name))
		left := p.left[id]
		switch {
		case left > 0 && !p.soon[id].IsZero():
			score += pickUseSoonPoints * math.Min(1, grams[id]/left)
			useSoon = append(useSoon, fmt.Sprintf("the %s to use by %s", short, p.dayName(p.soon[id])))
		case left >= grams[id]:
			pantryPoints += pickPantryPoints
		}
		if left < grams[id]-1e-9 {
			toBuy = append(toBuy, f.Name)
		}
		if users := p.users[id]; len(users) > 0 {
			sharedPoints += pickSharedPoints
			shared = append(shared, short)
			for _, u := range users {
				if u != r.Name {
					sharedWith[u] = true
				}
			}
		}
	}
	score += math.Min(pantryPoints, maxPickPantry) + math.Min(sharedPoints, maxPickShared) - pickToBuyPenalty*float64(len(toBuy))
	if r.Favorite {
		score += pickFavoritePoint
	}
	var reasons []string
	if len(useSoon) > 0 {
		reasons = append(reasons, "uses "+list(useSoon))
	}
	if len(shared) > 0 {
		names := make([]string, 0, len(sharedWith))
		for name := range sharedWith {
			names = append(names, name)
		}
		slices.Sort(names)
		reasons = append(reasons, fmt.Sprintf("shares %s with %s", list(shared[:min(3, len(shared))]), list(names[:min(2, len(names))])))
	}
	switch len(toBuy) {
	case 0:
		reasons = append(reasons, "everything's in the pantry")
	case 1:
		reasons = append(reasons, "1 thing to buy")
	default:
		reasons = append(reasons, fmt.Sprintf("%d things to buy", len(toBuy)))
	}
	return suggestionsv1.PickSuggestion_builder{
		RecipeId: store.UUIDToString(r.ID), RecipeName: r.Name, Servings: r.Servings,
		Score: math.Round(score*10) / 10, Reasons: reasons, ToBuy: toBuy,
	}.Build()
}

// dayName names a day near the date: "today", "tomorrow" or the weekday.
func (p *picker) dayName(day time.Time) string {
	switch pantry.DaysTo(p.date, day) {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	}
	return day.Format("Mon")
}

// shortName returns a food's name before its first comma, like "Tofu" for
// "Tofu, extra firm".
func shortName(name string) string {
	short, _, _ := strings.Cut(name, ",")
	short, _, _ = strings.Cut(short, " (")
	return strings.TrimSpace(short)
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
