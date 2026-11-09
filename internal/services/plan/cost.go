package plan

import (
	"context"
	"math"
	"slices"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// maxCostDays is the most days GetPlanCost estimates: a month.
const maxCostDays = 31

// cents rounds a cost to whole cents.
func cents(v float64) int32 {
	return int32(math.Round(v))
}

// unpricedNames collects the names of foods without a price, once each.
type unpricedNames []string

func (u *unpricedNames) add(names ...string) {
	for _, n := range names {
		if n != "" && !slices.Contains(*u, n) {
			*u = append(*u, n)
		}
	}
}

// needGrams returns what a need weighs and its food's price per kilogram,
// and false if it can't be weighed or has no price.
func needGrams(n shopping.Need, f *foods.Food) (float64, float64, bool) {
	if f == nil || f.PriceCentsPerKilogram <= 0 {
		return 0, 0, false
	}
	grams, ok := f.Nutrition().Grams(n.Amount, n.Unit)
	return grams, float64(f.PriceCentsPerKilogram), ok
}

func (s *Service) GetPlanCost(ctx context.Context, req *plansv1.GetPlanCostRequest) (*plansv1.GetPlanCostResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	start, err := parseDate("start_date", req.GetStartDate())
	if err != nil {
		return nil, err
	}
	end, err := parseDate("end_date", req.GetEndDate())
	if err != nil {
		return nil, err
	}
	if end.Before(start) || end.After(start.AddDate(0, 0, maxCostDays-1)) {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"end_date must be from start_date to %d days after it", maxCostDays-1)
	}
	d, _, lots, err := s.loadSuggestionData(ctx, user.HouseholdID(), start, end)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	// The plan read covers whole weeks around the dates; only the dates
	// count.
	view := &plans.Plan{From: start, To: end, Staples: d.plan.Staples}
	for _, c := range d.plan.Cooks {
		if !c.Date.Before(start) && !c.Date.After(end) {
			view.Cooks = append(view.Cooks, c)
		}
	}
	for _, p := range d.plan.Portions {
		if !p.Date.Before(start) && !p.Date.After(end) {
			view.Portions = append(view.Portions, p)
		}
	}

	var unpriced unpricedNames
	var total float64
	// Each cook costs its ingredients, without the components another
	// cook makes, which counts them itself.
	cooks := make([]*plansv1.CookCost, 0, len(view.Cooks))
	for _, c := range view.Cooks {
		r, ok := d.recipes[c.RecipeID]
		if !ok {
			continue
		}
		cost := recipes.CostOf(r.IngredientsWithout(c.Servings, shopping.SuppliedComponents(c)))
		unpriced.add(cost.Unpriced...)
		total += cost.Cents
		perServing := 0.0
		if c.Servings > 0 {
			perServing = cost.Cents / c.Servings
		}
		cooks = append(cooks, plansv1.CookCost_builder{
			CookId:          store.UUIDToString(c.ID),
			RecipeName:      c.RecipeName,
			Date:            store.FormatDate(c.Date),
			Servings:        c.Servings,
			TotalCents:      cents(cost.Cents),
			CentsPerServing: cents(perServing),
		}.Build())
	}

	// Foods eaten without a recipe: portions of foods and staples, whose
	// recipes count their ingredients.
	var foodsCost float64
	for _, n := range shopping.NeedsOf(&plans.Plan{From: start, To: end, Portions: view.Portions, Staples: view.Staples}, d.recipes) {
		f := d.foods[n.FoodID]
		grams, price, ok := needGrams(n, f)
		if !ok {
			if f != nil {
				unpriced.add(f.Name)
			}
			continue
		}
		foodsCost += grams / 1000 * price
	}
	total += foodsCost

	// What's left to buy is what the cooks not done yet, the portions and
	// the staples need, less what the pantry covers, by date, as the
	// shopping list takes it, and less the foods always in stock.
	needs := shopping.NeedsOf(view, d.recipes)
	fromPantry := shopping.PantryGrams(&shopping.Input{ShopDate: start, Needs: needs, Foods: d.foods, Lots: lots})
	needed := map[pgtype.UUID]float64{}
	price := map[pgtype.UUID]float64{}
	for _, n := range needs {
		f := d.foods[n.FoodID]
		grams, p, ok := needGrams(n, f)
		if !ok || f.AlwaysInStock {
			continue
		}
		needed[n.FoodID] += grams
		price[n.FoodID] = p
	}
	var toBuy float64
	for id, grams := range needed {
		toBuy += max(grams-fromPantry[id], 0) / 1000 * price[id]
	}

	slices.Sort(unpriced)
	return plansv1.GetPlanCostResponse_builder{
		TotalCents:        cents(total),
		ToBuyCents:        cents(toBuy),
		Cooks:             cooks,
		FoodsCents:        cents(foodsCost),
		UnpricedFoodNames: unpriced,
	}.Build(), nil
}
