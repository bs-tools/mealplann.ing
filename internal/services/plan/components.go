package plan

import (
	"bytes"
	"cmp"
	"context"
	"math"
	"slices"
	"sync"
	"time"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/authn"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/bs-tools/mealplann.ing/internal/suggest"
	"github.com/jackc/pgx/v5/pgtype"
)

// leftoverStock are the leftovers of earlier cooks to plan before cooking
// again, by recipe ID: those in the fridge, and those in the freezer.
type leftoverStock map[pgtype.UUID][]*leftoverServings

// leftoverServings are a cook's servings not planned yet: in the fridge,
// or frozen.
type leftoverServings struct {
	cook *plans.Cook
	left float64
	// until is the last date they keep, zero if unknown.
	until       time.Time
	fromFreezer bool
}

// minSideLeftovers is the least of leftovers worth a side: less is a crumb,
// so the side is cooked.
const minSideLeftovers = 0.25

// find returns the leftovers of a recipe with enough servings for a cook
// or portion on a date: those in the fridge first, as they keep less, and
// the oldest first; nil if none have enough. Fridge leftovers are of a
// cook before the date, and frozen ones of a cook frozen before it, both
// keeping until it.
func (l leftoverStock) find(recipeID pgtype.UUID, servings float64, date time.Time) *leftoverServings {
	var best *leftoverServings
	for _, ls := range l[recipeID] {
		if ls.left < servings-1e-9 || !ls.cook.Date.Before(date) || (!ls.until.IsZero() && ls.until.Before(date)) {
			continue
		}
		if best == nil || (best.fromFreezer && !ls.fromFreezer) ||
			(best.fromFreezer == ls.fromFreezer && ls.cook.Date.Before(best.cook.Date)) {
			best = ls
		}
	}
	return best
}

// take takes what a component needs from leftovers for a cook on a date:
// of its recipe, or an alternative recipe, in that recipe's servings (see
// find), returning them, or nil if none have enough.
func (l leftoverStock) take(need recipes.ComponentNeed, date time.Time) *leftoverServings {
	var best *leftoverServings
	var bestServings float64
	for _, o := range need.Options() {
		ls := l.find(o.Recipe.ID, o.Servings, date)
		if ls != nil && (best == nil || (best.fromFreezer && !ls.fromFreezer) ||
			(best.fromFreezer == ls.fromFreezer && ls.cook.Date.Before(best.cook.Date))) {
			best, bestServings = ls, o.Servings
		}
	}
	if best != nil {
		best.left -= bestServings
	}
	return best
}

// onHand returns the leftovers in stock for the planner, oldest first, the
// freezer's after the fridge's.
func (stock leftoverStock) onHand() []*suggest.OnHand {
	var list []*suggest.OnHand
	for _, all := range stock {
		for _, ls := range all {
			list = append(list, &suggest.OnHand{Cook: ls.cook, Servings: ls.left, FromFreezer: ls.fromFreezer, Until: ls.until})
		}
	}
	slices.SortStableFunc(list, func(a, b *suggest.OnHand) int {
		if a.FromFreezer != b.FromFreezer {
			if a.FromFreezer {
				return 1
			}
			return -1
		}
		return cmp.Or(a.Cook.Date.Compare(b.Cook.Date), bytes.Compare(a.Cook.ID.Bytes[:], b.Cook.ID.Bytes[:]))
	})
	return list
}

// readLeftovers reads the leftovers to plan before cooking from one date
// to another: the frozen servings of cooks, and the fridge leftovers of
// cooks from 14 days before the first date, which still keep on it.
func (s *Service) readLeftovers(ctx context.Context, householdID pgtype.UUID, from, to time.Time) (leftoverStock, error) {
	var frozen []*plans.FrozenCook
	var fridge []*plans.FridgeCook
	var frozenErr, fridgeErr error
	var wg sync.WaitGroup
	wg.Go(func() { frozen, frozenErr = s.plans.ListFrozenCooks(ctx, householdID) })
	wg.Go(func() { fridge, fridgeErr = s.plans.ListFridgeCooks(ctx, householdID, from, to.AddDate(0, 0, 1)) })
	wg.Wait()
	if frozenErr != nil {
		return nil, frozenErr
	}
	if fridgeErr != nil {
		return nil, fridgeErr
	}
	stock := leftoverStock{}
	for _, f := range frozen {
		ls := &leftoverServings{cook: f.Cook, left: inFreezer(f.Cook), fromFreezer: true}
		if f.FreezerDays != nil {
			ls.until = f.Date.AddDate(0, 0, int(*f.FreezerDays))
		}
		stock[f.RecipeID] = append(stock[f.RecipeID], ls)
	}
	for _, f := range fridge {
		if left := leftServings(f.Cook); left > servingsSlack {
			stock[f.RecipeID] = append(stock[f.RecipeID], &leftoverServings{
				cook: f.Cook, left: left, until: f.Date.AddDate(0, 0, int(f.FridgeDays)),
			})
		}
	}
	return stock, nil
}

// leftoversFor reads the leftovers to plan before cooking, if a cook to
// create has a recipe with components or is a side that could come from
// leftovers; nil if none could, saving the reads.
func (s *Service) leftoversFor(ctx context.Context, householdID pgtype.UUID, list []*plans.CookWithPortions, byID map[pgtype.UUID]*recipes.Recipe) (leftoverStock, error) {
	var from, to time.Time
	found := false
	for _, cp := range list {
		r := byID[cp.Cook.RecipeID]
		if cp.Existing || (!cp.FromLeftovers && (r == nil || len(r.ComponentsFor(cp.Cook.Servings)) == 0)) {
			continue
		}
		if !found || cp.Cook.Date.Before(from) {
			from = cp.Cook.Date
		}
		if !found || cp.Cook.Date.After(to) {
			to = cp.Cook.Date
		}
		found = true
	}
	if !found {
		return nil, nil
	}
	return s.readLeftovers(ctx, householdID, from, to)
}

// planSuppliers returns the cooks to create with suppliers of their
// components (see withSuppliers), from the freezer where it has them.
func (s *Service) planSuppliers(ctx context.Context, householdID pgtype.UUID, list []*plans.CookWithPortions, byID map[pgtype.UUID]*recipes.Recipe) ([]*plans.CookWithPortions, error) {
	freezer, err := s.leftoversFor(ctx, householdID, list, byID)
	if err != nil {
		return nil, err
	}
	// The pantry is read only if a component can be replaced with a food.
	var stock map[pgtype.UUID]float64
	if slices.ContainsFunc(list, func(cp *plans.CookWithPortions) bool {
		r := byID[cp.Cook.RecipeID]
		return !cp.Existing && r != nil && slices.ContainsFunc(r.ComponentsFor(cp.Cook.Servings), func(n recipes.ComponentNeed) bool { return n.Instead != nil })
	}) {
		items, err := s.pantry.ListItems(ctx, householdID)
		if err != nil {
			return nil, err
		}
		stock = map[pgtype.UUID]float64{}
		for _, it := range items {
			stock[it.FoodID] += it.Grams
		}
	}
	return withSuppliers(list, byID, freezer, stock), nil
}

// batchKey identifies a batch of a component: its recipe, on a date.
type batchKey struct {
	recipeID pgtype.UUID
	date     time.Time
}

// batch is a cook of a component created for the cooks of a day, with the
// servings of it they don't use yet.
type batch struct {
	cookID pgtype.UUID
	spare  float64
}

// batchServings returns how many servings of a component to cook for a
// need of it: a whole batch, the servings its recipe makes, as nobody
// pressure cooks 0.44 servings of beans, and what the cook doesn't use is
// leftovers to plan or freeze. A need for more than a batch is cooked as
// it is, rounded up to a hundredth so there's at least enough.
func batchServings(need recipes.ComponentNeed) float64 {
	exact := max(math.Ceil(need.Servings*100-1e-9)/100, minCookServings)
	return max(exact, float64(need.Recipe.Servings))
}

// withSuppliers returns the cooks to create with a supplier of each of
// their recipes' components: frozen servings of an earlier cook of it, if
// leftovers in the fridge or freezer have enough that keep until the cook,
// or of an alternative recipe (see leftoverStock.take), or none if the pantry has
// enough of a food it can be replaced with, from stock, by food, which
// shrinks as it's used (nil for none),
// or else a new cook of it, on the same date, of a whole batch of it (see
// batchServings), which cooks of the same day share, and so on for
// components of components. Every cook is given an ID, so
// the supplies can refer to cooks created together. recipes are the
// cooks' recipes by ID, with their components read; freezer is what's in
// the freezer, nil for nothing, which shrinks as it supplies cooks.
func withSuppliers(list []*plans.CookWithPortions, byID map[pgtype.UUID]*recipes.Recipe, freezer leftoverStock, stock map[pgtype.UUID]float64) []*plans.CookWithPortions {
	var result []*plans.CookWithPortions
	batches := map[batchKey]*batch{}
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
			if ls := freezer.take(need, cp.Cook.Date); ls != nil {
				cp.Supplies = append(cp.Supplies, &plans.Supply{
					ComponentRecipeID: need.Recipe.ID, SupplierCookID: ls.cook.ID, FromFreezer: ls.fromFreezer,
				})
				continue
			}
			// A food the component can be replaced with that the pantry has
			// enough of, like canned beans, is used instead of cooking it:
			// the cook is then made with it (see recipes.ComponentFoods).
			if inst := need.Instead; inst != nil && slices.ContainsFunc(inst.Foods, func(f pgtype.UUID) bool {
				if grams := inst.GramsOf(f); grams > 0 && stock[f] >= grams {
					stock[f] -= grams
					return true
				}
				return false
			}) {
				continue
			}
			// A batch made for an earlier cook of the day supplies this
			// one too, from what it has spare.
			key := batchKey{recipeID: need.Recipe.ID, date: cp.Cook.Date}
			if batch := batches[key]; batch != nil && batch.spare >= need.Servings-1e-9 {
				batch.spare -= need.Servings
				cp.Supplies = append(cp.Supplies, &plans.Supply{ComponentRecipeID: need.Recipe.ID, SupplierCookID: batch.cookID})
				continue
			}
			supplier := &plans.CookWithPortions{Cook: &plans.Cook{
				ID:          store.NewUUID(),
				HouseholdID: cp.Cook.HouseholdID,
				RecipeID:    need.Recipe.ID,
				Date:        cp.Cook.Date,
				Servings:    batchServings(need),
			}}
			batches[key] = &batch{cookID: supplier.Cook.ID, spare: supplier.Cook.Servings - need.Servings}
			cp.Supplies = append(cp.Supplies, &plans.Supply{ComponentRecipeID: need.Recipe.ID, SupplierCookID: supplier.Cook.ID})
			add(supplier, need.Recipe, depth+1)
		}
	}
	for _, cp := range list {
		// A side eats leftovers of it, if there are enough for everyone,
		// rather than cooking it again.
		if cp.FromLeftovers && !cp.Existing {
			total := 0.0
			for _, p := range cp.Portions {
				total += p.Servings
			}
			// Leftovers too few for everyone are shared out, each eating a
			// smaller side, rather than cooking it again.
			share := 1.0
			ls := freezer.find(cp.Cook.RecipeID, total, cp.Cook.Date)
			if ls == nil {
				if ls = freezer.find(cp.Cook.RecipeID, minSideLeftovers, cp.Cook.Date); ls != nil {
					share = ls.left / total
				}
			}
			if ls != nil {
				ls.left -= total * share
				portions := make([]*plans.Portion, 0, len(cp.Portions))
				for _, p := range cp.Portions {
					eaten := *p
					eaten.Servings = math.Floor(p.Servings*share*100) / 100
					eaten.FromFreezer = ls.fromFreezer
					portions = append(portions, &eaten)
				}
				// The cook exists, so it grows by none.
				existing := *ls.cook
				existing.Servings = 0
				result = append(result, &plans.CookWithPortions{Cook: &existing, Existing: true, Portions: portions})
				continue
			}
		}
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
	var component *recipes.ComponentNeed
	if len(list) > 0 {
		for _, n := range list[0].ComponentsFor(cook.Servings) {
			if n.Recipe.ID == componentID {
				component = &n
			}
		}
	}
	if component == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			"component_recipe_id isn't a component of the cook's recipe; find its components with GetRecipe")
	}
	// The supplier is a cook of the component's recipe, or of one the
	// ingredient lists as an alternative, which needs its own servings.
	need, ok := component.ServingsOf(supplier.RecipeID)
	if !ok {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"the supplier is a cook of %s, which isn't the component nor one of its alternatives: choose a cook of the component's recipe, or of an alternative in the ingredient's alternatives", supplier.RecipeName)
	}
	var previous pgtype.UUID
	for _, supply := range cook.Supplies {
		if supply.ComponentRecipeID == componentID {
			previous = supply.SupplierCookID
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

// alternativeIDs converts recipe IDs to strings, nil for none.
func alternativeIDs(ids []pgtype.UUID) []string {
	var out []string
	for _, id := range ids {
		out = append(out, store.UUIDToString(id))
	}
	return out
}

// supplyToProto converts a supply.
func supplyToProto(s *plans.Supply) *plansv1.CookSupply {
	return plansv1.CookSupply_builder{
		ConsumerCookId:       store.UUIDToString(s.ConsumerCookID),
		ConsumerRecipeName:   s.ConsumerRecipeName,
		ConsumerDate:         store.FormatDate(s.ConsumerDate),
		ComponentRecipeId:    store.UUIDToString(s.ComponentRecipeID),
		SupplierCookId:       store.UUIDToString(s.SupplierCookID),
		SupplierRecipeName:   s.SupplierRecipeName,
		SupplierDate:         store.FormatDate(s.SupplierDate),
		Servings:             round(s.Servings),
		FromFreezer:          s.FromFreezer,
		AlternativeRecipeIds: alternativeIDs(s.AlternativeRecipeIDs),
	}.Build()
}
