package shopping

import (
	"context"
	"fmt"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/shopping"
	"github.com/jackc/pgx/v5/pgtype"
)

// openListsRead is how many of the latest lists are looked at to find the
// open one: an incomplete list whose dates aren't over.
const openListsRead = 5

// Restock adds the foods kept in stock that the pantry has less of than
// their amount to the household's open shopping list, by regenerating it,
// if they aren't on it already: once something takes them below it, they
// go on the list without waiting for the next one. Of foodIDs, only foods
// kept in stock count, so for others it costs reading the foods' version,
// a few bytes. Without an open list, the next list made buys them.
func (s *Service) Restock(ctx context.Context, householdID pgtype.UUID, foodIDs []pgtype.UUID) error {
	if len(foodIDs) == 0 {
		return nil
	}
	kept, err := s.foods.ListKeptInStockFoods(ctx, householdID)
	if err != nil {
		return err
	}
	wanted := map[pgtype.UUID]bool{}
	for _, id := range foodIDs {
		wanted[id] = true
	}
	minimum := map[pgtype.UUID]float64{}
	for _, f := range kept {
		if grams, ok := f.KeepInStockGrams(); ok && wanted[f.ID] {
			minimum[f.ID] = grams
		}
	}
	if len(minimum) == 0 {
		return nil
	}
	lots, err := s.pantry.ListItems(ctx, householdID)
	if err != nil {
		return err
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	have := map[pgtype.UUID]float64{}
	for _, lot := range lots {
		if useBy := lot.UseBy(); useBy.IsZero() || !useBy.Before(today) {
			have[lot.FoodID] += lot.Grams
		}
	}
	low := map[pgtype.UUID]bool{}
	for id, grams := range minimum {
		if have[id] < grams {
			low[id] = true
		}
	}
	if len(low) == 0 {
		return nil
	}
	lists, err := s.shopping.ListLists(ctx, householdID, openListsRead, 0)
	if err != nil {
		return err
	}
	var open *shopping.List
	for _, l := range lists {
		if l.CompleteTime.IsZero() && !l.EndDate.Before(today) {
			open = l
			break
		}
	}
	if open == nil {
		return nil
	}
	existing, _, err := s.shopping.GetList(ctx, householdID, open.ID)
	if err != nil {
		return err
	}
	// A food on the list already, for the plan or added by hand, is bought
	// anyway.
	for _, it := range existing.Items {
		delete(low, it.FoodID)
	}
	if len(low) == 0 {
		return nil
	}
	items, err := s.generate(ctx, householdID, existing.StartDate, existing.EndDate)
	if err != nil {
		return err
	}
	shopping.KeepChecks(existing.Items, items)
	if _, _, err := s.shopping.ReplaceGeneratedItems(ctx, householdID, open.ID, items); err != nil {
		return fmt.Errorf("failed to add foods running low to the shopping list: %w", err)
	}
	return nil
}

// RestockingPantry is the pantry, which, whenever food is taken out of it,
// puts the foods kept in stock that run low on the open shopping list: see
// Service.Restock. Failing to is reported, but doesn't fail taking the
// food out, as the next list buys it anyway.
type RestockingPantry struct {
	*pantry.Repository
	shopping *Service
}

// NewRestockingPantry returns the pantry, restocking with the service.
func NewRestockingPantry(repo *pantry.Repository, s *Service) *RestockingPantry {
	return &RestockingPantry{Repository: repo, shopping: s}
}

// restock restocks the foods taken out.
func (p *RestockingPantry) restock(ctx context.Context, householdID pgtype.UUID, foodIDs []pgtype.UUID) {
	if err := p.shopping.Restock(ctx, householdID, foodIDs); err != nil {
		p.shopping.report.Report(ctx, "Failed to restock foods running low", err)
	}
}

// deducted restocks the foods of deductions.
func (p *RestockingPantry) deducted(ctx context.Context, householdID pgtype.UUID, deductions []pantry.Deduction) {
	ids := make([]pgtype.UUID, 0, len(deductions))
	for _, d := range deductions {
		ids = append(ids, d.Item.FoodID)
	}
	p.restock(ctx, householdID, ids)
}

func (p *RestockingPantry) UseForCook(ctx context.Context, householdID, cookID pgtype.UUID, uses []pantry.Use) ([]pantry.Deduction, error) {
	deductions, err := p.Repository.UseForCook(ctx, householdID, cookID, uses)
	if err == nil {
		p.deducted(ctx, householdID, deductions)
	}
	return deductions, err
}

func (p *RestockingPantry) UseForStaple(ctx context.Context, householdID, stapleID pgtype.UUID, date time.Time, uses []pantry.Use) ([]pantry.Deduction, error) {
	deductions, err := p.Repository.UseForStaple(ctx, householdID, stapleID, date, uses)
	if err == nil {
		p.deducted(ctx, householdID, deductions)
	}
	return deductions, err
}

func (p *RestockingPantry) UseForPortion(ctx context.Context, householdID, portionID pgtype.UUID, uses []pantry.Use) ([]pantry.Deduction, error) {
	deductions, err := p.Repository.UseForPortion(ctx, householdID, portionID, uses)
	if err == nil {
		p.deducted(ctx, householdID, deductions)
	}
	return deductions, err
}

func (p *RestockingPantry) UpdateItem(ctx context.Context, it *pantry.Item) (*pantry.Item, error) {
	updated, err := p.Repository.UpdateItem(ctx, it)
	if err == nil {
		p.restock(ctx, updated.HouseholdID, []pgtype.UUID{updated.FoodID})
	}
	return updated, err
}

func (p *RestockingPantry) DeleteItem(ctx context.Context, householdID, id pgtype.UUID) error {
	// The lot is read first for its food, as it's gone after.
	lot, err := p.Repository.GetItem(ctx, householdID, id)
	if err != nil {
		return err
	}
	if err := p.Repository.DeleteItem(ctx, householdID, id); err != nil {
		return err
	}
	p.restock(ctx, householdID, []pgtype.UUID{lot.FoodID})
	return nil
}
