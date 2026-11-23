package plan

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	"github.com/bs-tools/mealplann.ing/internal/bowls"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	suggestionsv1 "github.com/bs-tools/mealplann.ing/internal/gen/suggestions/v1"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/services/amounts"
	"github.com/bs-tools/mealplann.ing/internal/services/food"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// Bounds of a bowl request: how many servings a bowl's recipe makes, and
// how many bowls are suggested.
const (
	defaultBowlServings = 2
	maxBowlServings     = 12
	defaultBowls        = 3
	maxBowls            = 10
	// bowlTag tags the recipes of bowls, to find a bowl made before.
	bowlTag = "bowl-builder"
	// leftoverSoonDays is how many days before leftovers stop keeping
	// they're to use soon.
	leftoverSoonDays = 2
)

func (s *Service) ListBowlParts(ctx context.Context, req *suggestionsv1.ListBowlPartsRequest) (*suggestionsv1.ListBowlPartsResponse, error) {
	in, _, err := s.bowlInput(ctx, req.GetDate())
	if err != nil {
		return nil, err
	}
	parts := bowls.Parts(in)
	out := make([]*suggestionsv1.BowlPart, 0, len(parts))
	for _, p := range parts {
		out = append(out, bowlPartToProto(p))
	}
	return suggestionsv1.ListBowlPartsResponse_builder{Parts: out}.Build(), nil
}

func (s *Service) SuggestBowls(ctx context.Context, req *suggestionsv1.SuggestBowlsRequest) (*suggestionsv1.SuggestBowlsResponse, error) {
	servings, err := bowlServings(req.GetServings())
	if err != nil {
		return nil, err
	}
	n := int(req.GetMaxBowls())
	if n < 0 || n > maxBowls {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "max_bowls must be from 1 to %d, or 0 for %d", maxBowls, defaultBowls)
	}
	if n == 0 {
		n = defaultBowls
	}
	seed := req.GetSeed()
	if seed == 0 {
		seed = rand.Uint64()
	}
	in, all, err := s.bowlInput(ctx, req.GetDate())
	if err != nil {
		return nil, err
	}
	suggested := bowls.Suggest(bowls.Parts(in), n, seed)
	out := make([]*suggestionsv1.Bowl, 0, len(suggested))
	for _, b := range suggested {
		out = append(out, bowlToProto(b, servings, all))
	}
	return suggestionsv1.SuggestBowlsResponse_builder{Bowls: out, Seed: seed}.Build(), nil
}

func (s *Service) ComposeBowl(ctx context.Context, req *suggestionsv1.ComposeBowlRequest) (*suggestionsv1.ComposeBowlResponse, error) {
	servings, err := bowlServings(req.GetServings())
	if err != nil {
		return nil, err
	}
	if len(req.GetParts()) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, "parts must have a carbohydrate, a protein, a vegetable and a sauce, by their keys from ListBowlParts")
	}
	in, all, err := s.bowlInput(ctx, req.GetDate())
	if err != nil {
		return nil, err
	}
	byKey := map[string]*bowls.Part{}
	for _, p := range bowls.Parts(in) {
		byKey[p.Key] = p
	}
	chosen := make([]*bowls.Part, 0, len(req.GetParts()))
	for i, c := range req.GetParts() {
		p, ok := byKey[c.GetKey()]
		if !ok {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "parts[%d].key %q isn't a part of a bowl: use a key from ListBowlParts", i, c.GetKey())
		}
		if c.GetAmount() < 0 || c.GetAmount() > 5000 {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "parts[%d].amount must be from 0 to 5000, or 0 for the part's default", i)
		}
		part := *p
		if c.GetAmount() > 0 {
			part.Amount = c.GetAmount()
		}
		chosen = append(chosen, &part)
	}
	b, err := bowls.Compose(chosen)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err.Error()+": add the missing parts from ListBowlParts")
	}
	return suggestionsv1.ComposeBowlResponse_builder{Bowl: bowlToProto(b, servings, all)}.Build(), nil
}

// bowlServings checks a bowl's servings, defaulting them.
func bowlServings(servings int32) (int32, error) {
	if servings < 0 || servings > maxBowlServings {
		return 0, connect.Errorf(connect.CodeInvalidArgument, "servings must be from 1 to %d, or 0 for %d", maxBowlServings, defaultBowlServings)
	}
	if servings == 0 {
		return defaultBowlServings, nil
	}
	return servings, nil
}

// bowlInput reads what bowls are built of on a date: the household's
// recipes, its pantry, its leftovers already cooked, and the foods of
// them. It returns the recipes too, to find a bowl made before.
func (s *Service) bowlInput(ctx context.Context, dateText string) (*bowls.Input, []*recipes.Recipe, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	date := time.Now().UTC().Truncate(24 * time.Hour)
	if dateText != "" {
		var err error
		if date, err = parseDate("date", dateText); err != nil {
			return nil, nil, err
		}
	}
	householdID := user.HouseholdID()
	var all []*recipes.Recipe
	var lots []*pantry.Item
	var stock leftoverStock
	var recipesErr, lotsErr, stockErr error
	var wg sync.WaitGroup
	wg.Go(func() { all, _, recipesErr = s.recipes.ListRecipes(ctx, householdID, recipes.ListFilter{}) })
	wg.Go(func() { lots, lotsErr = s.pantry.ListItems(ctx, householdID) })
	wg.Go(func() { stock, stockErr = s.readLeftovers(ctx, householdID, date, date) })
	wg.Wait()
	if err := cmp.Or(recipesErr, lotsErr, stockErr); err != nil {
		return nil, nil, s.toConnectError(ctx, err, "failed to read the recipes, pantry and leftovers", "")
	}
	// The foods are those of the recipes, which have their sauces, and of
	// the pantry.
	var ids []pgtype.UUID
	for _, r := range all {
		for _, in := range r.Ingredients {
			if in.FoodID.Valid {
				ids = append(ids, in.FoodID)
			}
		}
	}
	for _, lot := range lots {
		ids = append(ids, lot.FoodID)
	}
	list, err := s.foods.ListFoodsByID(ctx, householdID, unique(ids))
	if err != nil {
		return nil, nil, s.toConnectError(ctx, err, "failed to read the foods", "")
	}
	in := &bowls.Input{Date: date, Foods: make(map[pgtype.UUID]*foods.Food, len(list)), Recipes: all, Lots: lots}
	for _, f := range list {
		in.Foods[f.ID] = f
	}
	for recipeID, cooks := range stock {
		for _, ls := range cooks {
			if !ls.cook.Date.Before(date) || (!ls.until.IsZero() && ls.until.Before(date)) {
				continue
			}
			where := "in the fridge"
			if ls.fromFreezer {
				where = "in the freezer"
			}
			in.Leftovers = append(in.Leftovers, &bowls.Leftover{
				RecipeID: recipeID, Servings: ls.left,
				Note:    fmt.Sprintf("%s, cooked %s", where, ls.cook.Date.Format("Mon 2 Jan")),
				UseSoon: !ls.fromFreezer && !ls.until.IsZero() && pantry.DaysTo(date, ls.until) <= leftoverSoonDays,
			})
		}
	}
	return in, all, nil
}

var bowlRolesToProto = map[bowls.Role]suggestionsv1.BowlRole{
	bowls.RoleCarb:      suggestionsv1.BowlRole_BOWL_ROLE_CARB,
	bowls.RoleProtein:   suggestionsv1.BowlRole_BOWL_ROLE_PROTEIN,
	bowls.RoleVegetable: suggestionsv1.BowlRole_BOWL_ROLE_VEGETABLE,
	bowls.RoleSauce:     suggestionsv1.BowlRole_BOWL_ROLE_SAUCE,
}

// bowlPartToProto converts a part of a bowl, in its amount for a serving.
func bowlPartToProto(p *bowls.Part) *suggestionsv1.BowlPart {
	b := suggestionsv1.BowlPart_builder{
		Role: bowlRolesToProto[p.Role], Key: p.Key, Name: p.Name, Detail: p.Detail,
		Amount: p.Amount, AmountText: bowlAmountText(p, 1),
		OnHand: p.OnHand, OnHandNote: p.OnHandNote, UseSoon: p.UseSoon, Leftovers: p.Leftovers,
	}
	if p.Food != nil {
		b.FoodId = store.UUIDToString(p.Food.ID)
	}
	if p.Recipe != nil {
		b.RecipeId = store.UUIDToString(p.Recipe.ID)
	}
	for _, style := range p.Styles {
		b.Styles = append(b.Styles, string(style))
	}
	return b.Build()
}

// bowlAmountText says how much of a part servings have, like "150 g", "1.5
// servings" or "2 servings' worth" of a recipe's sauce.
func bowlAmountText(p *bowls.Part, servings int32) string {
	v := bowls.Scaled(p, servings)
	switch {
	case p.Food != nil:
		return fmt.Sprintf("%g g", v)
	case p.Sauce != nil:
		if v == 1 {
			return "1 serving's worth"
		}
		return fmt.Sprintf("%g servings' worth", v)
	case v == 1:
		return "1 serving"
	}
	return fmt.Sprintf("%g servings", v)
}

// bowlToProto converts a bowl, with its recipe for servings, and the
// household's recipe of it made before, if any.
func bowlToProto(b *bowls.Bowl, servings int32, all []*recipes.Recipe) *suggestionsv1.Bowl {
	parts := make([]*suggestionsv1.BowlPart, 0, len(b.Parts))
	for _, p := range b.Parts {
		parts = append(parts, bowlPartToProto(p))
	}
	out := suggestionsv1.Bowl_builder{
		Name: b.Name, Style: string(b.Style), Parts: parts,
		Nutrients: food.NutrientsToProto(&b.PerServing, 1), Servings: servings,
		Reasons: b.Reasons, Recipe: bowlRecipeToProto(b.Recipe(servings)), OnHand: b.OnHand(),
	}
	if i := slices.IndexFunc(all, func(r *recipes.Recipe) bool {
		return strings.EqualFold(r.Name, b.Name) && slices.Contains(r.Tags, bowlTag)
	}); i >= 0 {
		out.ExistingRecipeId = store.UUIDToString(all[i].ID)
	}
	return out.Build()
}

// bowlRecipeToProto converts a bowl's recipe to create.
func bowlRecipeToProto(r *recipes.Recipe) *recipesv1.Recipe {
	ingredients := make([]*recipesv1.Ingredient, 0, len(r.Ingredients))
	for _, in := range r.Ingredients {
		b := recipesv1.Ingredient_builder{Note: in.Note, Optional: in.Optional}
		if in.ComponentRecipeID.Valid {
			b.RecipeId = store.UUIDToString(in.ComponentRecipeID)
			b.Amount = in.Amount
		} else {
			b.FoodId = store.UUIDToString(in.FoodID)
			b.Amount, b.Unit = amounts.ToProto(in.Amount, in.Unit, in.KitchenUnit)
		}
		ingredients = append(ingredients, b.Build())
	}
	steps := make([]*recipesv1.Step, 0, len(r.Instructions))
	for _, st := range r.Instructions {
		b := recipesv1.Step_builder{
			Text: st.Text, Section: st.Section, DurationSeconds: st.DurationSeconds,
			Waiting: st.Waiting, FromStart: st.FromStart,
		}
		for _, i := range st.Ingredients {
			b.IngredientIndexes = append(b.IngredientIndexes, int32(i))
		}
		for _, i := range st.After {
			b.AfterStepIndexes = append(b.AfterStepIndexes, int32(i))
		}
		steps = append(steps, b.Build())
	}
	return recipesv1.Recipe_builder{
		Name: r.Name, Description: r.Description, Servings: r.Servings,
		PrepSeconds: r.PrepSeconds, CookSeconds: r.CookSeconds,
		Ingredients: ingredients, Instructions: steps, Tags: r.Tags,
		FridgeDays: r.FridgeDays, Notes: r.Notes,
	}.Build()
}
