package pantry

import (
	"cmp"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// emptyGrams is the most a lot can have left to count as used up. Amounts
// are scaled floats, so a lot used exactly can have a crumb of a gram left,
// which would otherwise linger in the pantry.
const emptyGrams = 0.5

// Deduct returns what to take out of each lot to use amounts of foods: for
// each food, from the lots that must be used first (see ByUseBy), as far
// as they're in stock. Lots are never taken below zero, so a use beyond the
// stock takes out what there is. Uses of the same food add up. Deductions
// are in the order of the uses' foods, then of the lots.
func Deduct(lots []*Item, uses []Use) []Deduction {
	needed := map[pgtype.UUID]float64{}
	var order []pgtype.UUID
	for _, u := range uses {
		if u.Grams <= 0 {
			continue
		}
		if _, ok := needed[u.FoodID]; !ok {
			order = append(order, u.FoodID)
		}
		needed[u.FoodID] += u.Grams
	}
	sorted := slices.Clone(lots)
	SortByUseBy(sorted)
	var deductions []Deduction
	for _, foodID := range order {
		left := needed[foodID]
		for _, lot := range sorted {
			if left <= 0 {
				break
			}
			if lot.FoodID != foodID || lot.Grams <= 0 {
				continue
			}
			grams := min(left, lot.Grams)
			// Taking out all but a crumb empties the lot.
			if lot.Grams-grams <= emptyGrams {
				grams = lot.Grams
			}
			deductions = append(deductions, Deduction{Item: lot, Grams: grams})
			left -= grams
		}
	}
	return deductions
}

// SortByUseBy sorts lots in the order to use them: those to use by the
// earliest date first, then those without a date, the oldest first.
func SortByUseBy(lots []*Item) {
	slices.SortStableFunc(lots, func(a, b *Item) int {
		ua, ub := a.UseBy(), b.UseBy()
		switch {
		case ua.IsZero() != ub.IsZero():
			if ua.IsZero() {
				return 1
			}
			return -1
		case !ua.Equal(ub):
			return ua.Compare(ub)
		}
		return cmp.Compare(a.CreateTime.UnixNano(), b.CreateTime.UnixNano())
	})
}

// DaysTo returns the whole days from one date to another, both at midnight
// UTC, negative if to is before from.
func DaysTo(from, to time.Time) int {
	return int(to.Sub(from).Hours() / 24)
}
