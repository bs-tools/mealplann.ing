package plan

import (
	"context"
	"math"
	"slices"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// frozenStock is what's in the freezer of each recipe, to supply the
// components of new cooks from rather than cooking them again.
type frozenStock map[pgtype.UUID][]*frozenServings

// frozenServings are a cook's frozen servings not planned yet.
type frozenServings struct {
	cook *plans.Cook
	left float64
	// until is the last date they keep, zero if unknown.
	until time.Time
}

// take takes servings of a recipe out of the freezer for a cook on a
// date, from the oldest cook with enough, frozen before the date and
// keeping until it, returning that cook, or nil if none has enough.
func (f frozenStock) take(recipeID pgtype.UUID, servings float64, date time.Time) *plans.Cook {
	for _, fs := range f[recipeID] {
		if fs.left >= servings-1e-9 && fs.cook.Date.Before(date) && (fs.until.IsZero() || !fs.until.Before(date)) {
			fs.left -= servings
			return fs.cook
		}
	}
	return nil
}

// freezerFor reads what's in the freezer, if a cook to create has a recipe
// with components it could supply; nil if none has, saving the read.
func (s *Service) freezerFor(ctx context.Context, householdID pgtype.UUID, list []*plans.CookWithPortions, byID map[pgtype.UUID]*recipes.Recipe) (frozenStock, error) {
	if !slices.ContainsFunc(list, func(cp *plans.CookWithPortions) bool {
		r := byID[cp.Cook.RecipeID]
		return !cp.Existing && r != nil && len(r.ComponentsFor(cp.Cook.Servings)) > 0
	}) {
		return nil, nil
	}
	frozen, err := s.plans.ListFrozenCooks(ctx, householdID)
	if err != nil {
		return nil, err
	}
	stock := frozenStock{}
	for _, f := range frozen {
		fs := &frozenServings{cook: f.Cook, left: inFreezer(f.Cook)}
		if f.FreezerDays != nil {
			fs.until = f.Date.AddDate(0, 0, int(*f.FreezerDays))
		}
		stock[f.RecipeID] = append(stock[f.RecipeID], fs)
	}
	return stock, nil
}

// planSuppliers returns the cooks to create with suppliers of their
// components (see withSuppliers), from the freezer where it has them.
func (s *Service) planSuppliers(ctx context.Context, householdID pgtype.UUID, list []*plans.CookWithPortions, byID map[pgtype.UUID]*recipes.Recipe) ([]*plans.CookWithPortions, error) {
	freezer, err := s.freezerFor(ctx, householdID, list, byID)
	if err != nil {
		return nil, err
	}
	return withSuppliers(list, byID, freezer), nil
}

// withSuppliers returns the cooks to create with a supplier of each of
// their recipes' components: frozen servings of an earlier cook of it, if
// the freezer has enough that keep until the cook (see frozenStock.take),
// or else a new cook of it, on the same date, for the servings they need,
// and so on for components of components. Every cook is given an ID, so
// the supplies can refer to cooks created together. recipes are the
// cooks' recipes by ID, with their components read; freezer is what's in
// the freezer, nil for nothing, which shrinks as it supplies cooks.
func withSuppliers(list []*plans.CookWithPortions, byID map[pgtype.UUID]*recipes.Recipe, freezer frozenStock) []*plans.CookWithPortions {
	var result []*plans.CookWithPortions
	var add func(cp *plans.CookWithPortions, r *recipes.Recipe, depth int)
	add = func(cp *plans.CookWithPortions, r *recipes.Recipe, depth int) {
		if !cp.Cook.ID.Valid {
			cp.Cook.ID = store.NewUUID()
		}
		result = append(result, cp)
		// An existing cook keeps its supplies.
		if r == nil || cp.Existing || depth >= recipes.MaxComponentDepth {
			return
		}
		for _, need := range r.ComponentsFor(cp.Cook.Servings) {
			if frozen := freezer.take(need.Recipe.ID, need.Servings, cp.Cook.Date); frozen != nil {
				cp.Supplies = append(cp.Supplies, &plans.Supply{
					ComponentRecipeID: need.Recipe.ID, SupplierCookID: frozen.ID, FromFreezer: true,
				})
				continue
			}
			supplier := &plans.CookWithPortions{Cook: &plans.Cook{
				ID:          store.NewUUID(),
				HouseholdID: cp.Cook.HouseholdID,
				RecipeID:    need.Recipe.ID,
				Date:        cp.Cook.Date,
				// The servings are rounded up to a hundredth, so the
				// supplier makes at least what's needed.
				Servings: max(math.Ceil(need.Servings*100-1e-9)/100, minCookServings),
			}}
			cp.Supplies = append(cp.Supplies, &plans.Supply{ComponentRecipeID: need.Recipe.ID, SupplierCookID: supplier.Cook.ID})
			add(supplier, need.Recipe, depth+1)
		}
	}
	for _, cp := range list {
		add(cp, byID[cp.Cook.RecipeID], 0)
	}
	return result
}

func (s *Service) SetCookSupplier(ctx context.Context, req *plansv1.SetCookSupplierRequest) (*plansv1.SetCookSupplierResponse, error) {
	user, ok := authn.GetUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, "")
	}
	cookID, err := parseID("cook_id", req.GetCookId(), "find cooks with GetWeek")
	if err != nil {
		return nil, err
	}
	componentID, err := parseID("component_recipe_id", req.GetComponentRecipeId(), "find the recipe's components with GetRecipe")
	if err != nil {
		return nil, err
	}
	supplierID, err := parseID("supplier_cook_id", req.GetSupplierCookId(), "find cooks with GetWeek")
	if err != nil {
		return nil, err
	}
	if supplierID == cookID {
		return nil, connect.NewError(connect.CodeInvalidArgument, "a cook can't supply itself: choose a cook of the component's recipe")
	}
	cook, err := s.plans.GetCook(ctx, user.HouseholdID(), cookID)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get cook", cookNotFound)
	}
	supplier, err := s.plans.GetCook(ctx, user.HouseholdID(), supplierID)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get cook", "the supplier cook doesn't exist in your household's plan; find cooks with GetWeek")
	}
	list, err := s.recipes.ListRecipesByID(ctx, user.HouseholdID(), []pgtype.UUID{cook.RecipeID})
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to read recipe", "")
	}
	if len(list) == 0 || !slices.ContainsFunc(list[0].ComponentsFor(cook.Servings), func(n recipes.ComponentNeed) bool { return n.Recipe.ID == componentID }) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"component_recipe_id isn't a component of the cook's recipe; find its components with GetRecipe")
	}
	if supplier.RecipeID != componentID {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"the supplier is a cook of %s, not of the component: choose a cook of the component's recipe", supplier.RecipeName)
	}
	var previous pgtype.UUID
	for _, supply := range cook.Supplies {
		if supply.ComponentRecipeID == componentID {
			previous = supply.SupplierCookID
		}
	}
	var need float64
	for _, n := range list[0].ComponentsFor(cook.Servings) {
		if n.Recipe.ID == componentID {
			need = n.Servings
		}
	}
	// An earlier cook with enough frozen supplies it from the freezer, like
	// beans cooked weeks ago; else the supplier makes enough for the cook
	// too: sharing a pot means a bigger pot.
	fromFreezer := supplier.Date.Before(cook.Date) && inFreezer(supplier) >= need-1e-9
	for _, supply := range cook.Supplies {
		if supply.ComponentRecipeID == componentID && supply.SupplierCookID == supplierID {
			// It's counted already.
			fromFreezer = supply.FromFreezer
		}
	}
	if previous != supplierID && !fromFreezer {
		planned := supplier.AssignedServings + supplier.SuppliedServings + need
		if supplier.Servings < planned {
			supplier.Servings = math.Min(math.Ceil(planned*100-1e-9)/100, maxCookServings)
			if _, err := s.plans.UpdateCook(ctx, supplier); err != nil {
				return nil, s.toConnectError(ctx, err, "failed to update the supplier's servings", cookNotFound)
			}
		}
	}
	if err := s.plans.SetCookSupply(ctx, user.HouseholdID(), &plans.Supply{
		ConsumerCookID:    cookID,
		ComponentRecipeID: componentID,
		SupplierCookID:    supplierID,
		FromFreezer:       fromFreezer,
	}, previous); err != nil {
		return nil, s.toConnectError(ctx, err, "failed to set the cook's supplier", cookNotFound)
	}
	updated, err := s.plans.GetCook(ctx, user.HouseholdID(), cookID)
	if err != nil {
		return nil, s.toConnectError(ctx, err, "failed to get cook", cookNotFound)
	}
	return plansv1.SetCookSupplierResponse_builder{Cook: cookToProto(updated)}.Build(), nil
}

// supplyToProto converts a supply.
func supplyToProto(s *plans.Supply) *plansv1.CookSupply {
	return plansv1.CookSupply_builder{
		ConsumerCookId:     store.UUIDToString(s.ConsumerCookID),
		ConsumerRecipeName: s.ConsumerRecipeName,
		ConsumerDate:       store.FormatDate(s.ConsumerDate),
		ComponentRecipeId:  store.UUIDToString(s.ComponentRecipeID),
		SupplierCookId:     store.UUIDToString(s.SupplierCookID),
		SupplierRecipeName: s.SupplierRecipeName,
		SupplierDate:       store.FormatDate(s.SupplierDate),
		Servings:           round(s.Servings),
		FromFreezer:        s.FromFreezer,
	}.Build()
}
