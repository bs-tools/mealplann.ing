package plan

import (
	"cmp"
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/people"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/rules"
	pantrysvc "github.com/bs-tools/mealplann.ing/internal/services/pantry"
	"github.com/bs-tools/mealplann.ing/internal/services/person"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/suggest"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	// defaultSuggestions and maxSuggestions are the default and most
	// suggestions SuggestRecipes returns.
	defaultSuggestions = 8
	maxSuggestions     = 50
	// shoppingListsRead is how many of the latest shopping lists are read
	// to find the shop of a slot: a few weeks' worth.
	shoppingListsRead = 8
)

func (s *Service) SuggestRecipes(ctx context.Context, req *suggestionsv1.SuggestRecipesRequest) (*suggestionsv1.SuggestRecipesResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	date, err := parseDate("date", req.GetDate())
	if err != nil {
		return nil, err
	}
	count := int(req.GetMaxSuggestions())
	switch {
	case count == 0:
		count = defaultSuggestions
	case count < 0 || count > maxSuggestions:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "max_suggestions must be from 1 to %d, or 0 for %d", maxSuggestions, defaultSuggestions)
	}
	in := &suggest.Input{Date: date, Max: count}
	var personIDs []pgtype.UUID
	var slotPersonID pgtype.UUID
	switch {
	case req.HasMeal():
		in.Meal, err = mealFromProto(req.GetMeal())
		if err != nil {
			return nil, err
		}
		for _, raw := range req.GetPersonIds() {
			id, err := parseID("person_ids", raw, "find people with ListPeople")
			if err != nil {
				return nil, err
			}
			personIDs = append(personIDs, id)
		}
	case req.HasEatingTime():
		slotPersonID, err = parseID("eating_time.person_id", req.GetEatingTime().GetPersonId(), "find people with ListPeople")
		if err != nil {
			return nil, err
		}
		in.EatingTimeID, err = parseID("eating_time.eating_time_id", req.GetEatingTime().GetEatingTimeId(), "find the person's eating times with GetPerson")
		if err != nil {
			return nil, err
		}
	default:
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"set meal, for a shared meal, or eating_time, for one person's eating time")
	}

	d, lists, lots, err := s.loadSuggestionData(ctx, user.HouseholdID(), date, date)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read plan", "")
	}
	if in.Meal != people.MealNone {
		in.People, err = mealEaters(d.people, in.Meal, personIDs)
		if err != nil {
			return nil, err
		}
		// People with a staple at the meal eat it instead, unless they're
		// asked for.
		if len(personIDs) == 0 {
			if in.People, _, _ = withoutStapled(in.People, d.plan, in.Meal, date); len(in.People) == 0 {
				return nil, allStapled(in.Meal, date)
			}
		}
	} else {
		i := slices.IndexFunc(d.people, func(p *people.Person) bool { return p.ID == slotPersonID })
		if i < 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				"eating_time.person_id isn't one of your household's people; find them with ListPeople")
		}
		p := d.people[i]
		if p.EatingTime(in.EatingTimeID) == nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"eating_time.eating_time_id isn't one of %s's eating times; find them with GetPerson", p.Name)
		}
		in.People = []*people.Person{p}
	}

	monday := date.AddDate(0, 0, -int(date.Weekday()+6)%7)
	sunday := monday.AddDate(0, 0, 6)
	in.ShopDate = s.shopDate(lists, date)
	in.Week = &plans.Plan{From: monday, To: sunday, Staples: d.plan.Staples}
	for _, c := range d.plan.Cooks {
		if !c.Date.Before(monday) && !c.Date.After(sunday) {
			in.Week.Cooks = append(in.Week.Cooks, c)
		}
	}
	for _, p := range d.plan.Portions {
		if !p.Date.Before(monday) && !p.Date.After(sunday) {
			in.Week.Portions = append(in.Week.Portions, p)
		}
	}
	in.Cooks = d.plan.Cooks
	in.Planned = map[plans.Slot]*rules.EatingTime{}
	for _, p := range in.People {
		for _, et := range p.EatingTimes {
			for _, day := range []time.Time{date, date.AddDate(0, 0, 1)} {
				slot := plans.Slot{PersonID: p.ID, EatingTimeID: et.ID, Date: day}
				if planned := d.ruleEatingTime(slot, et.Name); planned != nil {
					in.Planned[slot] = planned
				}
			}
		}
	}
	in.Recipes = make([]*recipes.Recipe, 0, len(d.recipes))
	for _, r := range d.recipes {
		in.Recipes = append(in.Recipes, r)
	}
	// Recipes of the same score are ordered by name, and the IDs make the
	// order of those of the same name stable.
	slices.SortFunc(in.Recipes, func(a, b *recipes.Recipe) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(store.UUIDToString(a.ID), store.UUIDToString(b.ID)))
	})
	in.Foods = d.foods
	in.Lots = lots
	in.History = d.history
	// One person's small meal can be the rest of an earlier meal.
	in.Leftovers = in.Meal == people.MealNone

	suggestions := suggest.Suggest(in)
	result := make([]*suggestionsv1.Suggestion, 0, len(suggestions))
	for _, sg := range suggestions {
		result = append(result, suggestionToProto(sg))
	}
	return suggestionsv1.SuggestRecipesResponse_builder{
		Suggestions: result,
		ShopDate:    store.FormatDate(in.ShopDate),
		RecipeCount: int32(len(in.Recipes)),
	}.Build(), nil
}

// loadSuggestionData reads what suggesting recipes for the dates from
// first to last needs: the plan from suggest.VarietyDays before the first
// date, or the start of its week, to the end of the last date's week, and
// the next day, with the people, all the household's recipes, and the
// foods they use; and the shopping lists and the pantry. It reads the
// plan, people, recipes, shopping lists and pantry together, and then the
// foods: two round trips.
func (s *Service) loadSuggestionData(ctx context.Context, householdID pgtype.UUID, first, last time.Time) (*planData, []*shopping.List, []*pantry.Item, error) {
	from := first.AddDate(0, 0, -int(first.Weekday()+6)%7)
	lastMonday := last.AddDate(0, 0, -int(last.Weekday()+6)%7)
	to := lastMonday.AddDate(0, 0, 6)
	if earliest := first.AddDate(0, 0, -suggest.VarietyDays); earliest.Before(from) {
		from = earliest
	}
	if next := last.AddDate(0, 0, 1); next.After(to) {
		to = next
	}
	d := &planData{}
	var recipeList []*recipes.Recipe
	var lists []*shopping.List
	var lots []*pantry.Item
	var planErr, peopleErr, recipesErr, listsErr, lotsErr, historyErr error
	var wg sync.WaitGroup
	wg.Go(func() {
		d.history, historyErr = s.plans.CountDoneCooks(ctx, householdID, first.AddDate(0, 0, -suggest.HistoryDays), first.AddDate(0, 0, -1))
	})
	wg.Go(func() { d.plan, planErr = s.plans.GetPlan(ctx, householdID, from, to) })
	wg.Go(func() { d.people, peopleErr = s.people.ListPeople(ctx, householdID) })
	wg.Go(func() { recipeList, _, recipesErr = s.recipes.ListRecipes(ctx, householdID, recipes.ListFilter{}) })
	wg.Go(func() { lists, listsErr = s.shopping.ListLists(ctx, householdID, shoppingListsRead, 0) })
	wg.Go(func() { lots, lotsErr = s.pantry.ListItems(ctx, householdID) })
	wg.Wait()
	for _, err := range []error{planErr, peopleErr, recipesErr, listsErr, lotsErr, historyErr} {
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Recipes come with their ingredients' nutrition, but not their foods'
	// pack sizes and shelf lives, which the shopping needs.
	var foodIDs []pgtype.UUID
	for _, r := range recipeList {
		for _, in := range r.IngredientsFor(float64(r.Servings)) {
			foodIDs = append(foodIDs, in.FoodID)
		}
	}
	for _, p := range d.plan.Portions {
		foodIDs = append(foodIDs, p.FoodID)
	}
	for _, st := range d.plan.Staples {
		foodIDs = append(foodIDs, st.FoodID)
	}
	foodList, err := s.foods.ListFoodsByID(ctx, householdID, unique(foodIDs))
	if err != nil {
		return nil, nil, nil, err
	}
	d.index(recipeList, foodList)
	return d, lists, lots, nil
}

// shopDate returns the date of the shop the fresh foods of a date are
// bought on: the start date of the latest shopping list not yet complete
// whose dates include it, or else today, or the date if that's earlier.
// lists are the latest lists, latest start date first.
func (s *Service) shopDate(lists []*shopping.List, date time.Time) time.Time {
	for _, l := range lists {
		if l.CompleteTime.IsZero() && !l.StartDate.After(date) && !l.EndDate.Before(date) {
			return l.StartDate
		}
	}
	if today := pantrysvc.Today(s.nowFn()); today.Before(date) {
		return today
	}
	return date
}

var scoreKindsToProto = map[suggest.Kind]suggestionsv1.ScoreKind{
	suggest.SharedIngredients: suggestionsv1.ScoreKind_SCORE_KIND_SHARED_INGREDIENTS,
	suggest.PackLeftovers:     suggestionsv1.ScoreKind_SCORE_KIND_PACK_LEFTOVERS,
	suggest.Pantry:            suggestionsv1.ScoreKind_SCORE_KIND_PANTRY,
	suggest.Nutrition:         suggestionsv1.ScoreKind_SCORE_KIND_NUTRITION,
	suggest.Rules:             suggestionsv1.ScoreKind_SCORE_KIND_RULES,
	suggest.Keeping:           suggestionsv1.ScoreKind_SCORE_KIND_KEEPING,
	suggest.Variety:           suggestionsv1.ScoreKind_SCORE_KIND_VARIETY,
	suggest.MealTag:           suggestionsv1.ScoreKind_SCORE_KIND_MEAL_TAG,
	suggest.Favorite:          suggestionsv1.ScoreKind_SCORE_KIND_FAVORITE,
	suggest.Leftover:          suggestionsv1.ScoreKind_SCORE_KIND_LEFTOVER,
	suggest.Cooked:            suggestionsv1.ScoreKind_SCORE_KIND_COOKED,
	suggest.Tastes:            suggestionsv1.ScoreKind_SCORE_KIND_TASTES,
}

// suggestionToProto converts a suggestion to the API.
func suggestionToProto(sg *suggest.Suggestion) *suggestionsv1.Suggestion {
	components := make([]*suggestionsv1.ScoreComponent, 0, len(sg.Components))
	for _, c := range sg.Components {
		components = append(components, suggestionsv1.ScoreComponent_builder{
			Kind:   scoreKindsToProto[c.Kind],
			Points: c.Points,
			Reason: c.Reason,
		}.Build())
	}
	warnings := make([]*plansv1.Warning, 0, len(sg.Warnings))
	for _, w := range sg.Warnings {
		warnings = append(warnings, warningToProto(&w.Warning, warningContext{
			personID:     w.PersonID,
			date:         w.Date,
			eatingTimeID: w.EatingTimeID,
		}))
	}
	portions := make([]*suggestionsv1.SuggestedPortion, 0, len(sg.Portions))
	for _, p := range sg.Portions {
		b := suggestionsv1.SuggestedPortion_builder{
			PersonId:     store.UUIDToString(p.PersonID),
			PersonName:   p.Person.Name,
			EatingTimeId: store.UUIDToString(p.EatingTimeID),
			Date:         store.FormatDate(p.Date),
			Servings:     round(p.Servings),
			Grams:        round(p.Grams),
		}
		if p.EatingTime != nil {
			b.EatingTimeName = p.EatingTime.Name
		}
		portions = append(portions, b.Build())
	}
	items := make([]*suggestionsv1.ItemToBuy, 0, len(sg.ItemsToBuy))
	for _, it := range sg.ItemsToBuy {
		items = append(items, suggestionsv1.ItemToBuy_builder{
			FoodId:        store.UUIDToString(it.Food.ID),
			FoodName:      it.Food.Name,
			AmountText:    it.AmountText,
			AlreadyListed: it.AlreadyListed,
		}.Build())
	}
	return suggestionsv1.Suggestion_builder{
		RecipeId:     store.UUIDToString(sg.Recipe.ID),
		RecipeName:   sg.Recipe.Name,
		Score:        sg.Score,
		Components:   components,
		FitsRules:    sg.FitsRules,
		Warnings:     warnings,
		Portions:     portions,
		CookServings: round(sg.CookServings),
		NewItemCount: int32(sg.NewItemCount),
		ItemsToBuy:   items,
		LeftoverOf:   leftoverToProto(sg.LeftoverRef()),
	}.Build()
}

// leftoverToProto converts the earlier meal of a leftover to the API, nil
// for none.
func leftoverToProto(ref *suggest.LeftoverRef) *plansv1.LeftoverOf {
	if ref == nil {
		return nil
	}
	return plansv1.LeftoverOf_builder{Date: store.FormatDate(ref.Date), Meal: person.MealsToProto[ref.Meal]}.Build()
}
