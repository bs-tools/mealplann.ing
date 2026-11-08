package plan

import (
	"context"
	"math"
	"slices"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// withSuppliers returns the cooks to create with a cook of each of their
// recipes' components, on the same date, for the servings they need, each
// supplying its cook, and so on for components of components. Every cook
// is given an ID, so the supplies can refer to cooks created together.
// recipes are the cooks' recipes by ID, with their components read.
func withSuppliers(list []*plans.CookWithPortions, byID map[pgtype.UUID]*recipes.Recipe) []*plans.CookWithPortions {
	var result []*plans.CookWithPortions
	var add func(cp *plans.CookWithPortions, r *recipes.Recipe, depth int)
	add = func(cp *plans.CookWithPortions, r *recipes.Recipe, depth int) {
		if !cp.Cook.ID.Valid {
			cp.Cook.ID = store.NewUUID()
		}
		result = append(result, cp)
		if r == nil || depth >= recipes.MaxComponentDepth {
			return
		}
		for _, need := range r.ComponentsFor(cp.Cook.Servings) {
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
	// The supplier makes enough for the cook too: sharing a pot means a
	// bigger pot.
	if previous != supplierID {
		var need float64
		for _, n := range list[0].ComponentsFor(cook.Servings) {
			if n.Recipe.ID == componentID {
				need = n.Servings
			}
		}
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
	}.Build()
}
