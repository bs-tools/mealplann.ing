package shopping

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/pantry"
	"github.com/bs-tools/mealplann.ing/internal/plans"
	"github.com/bs-tools/mealplann.ing/internal/recipes"
	"github.com/jackc/pgx/v5/pgtype"
)

// crumbGrams is the least shortfall worth buying. Amounts are scaled
// floats, so a pantry covering a need exactly can leave a crumb of a gram
// short, which would otherwise put a whole pack on the list.
const crumbGrams = 0.5

// Need is an amount of a food the plan eats on a date.
type Need struct {
	FoodID pgtype.UUID
	// Date is at midnight UTC.
	Date   time.Time
	Amount float64
	Unit   nutrition.Unit
}

// NeedsOf returns what a plan needs bought, in the order of its dates: the
// ingredients of the cooks not yet done, scaled to their servings, with
// their components made of their own ingredients unless another cook
// supplies them, which buys its own, the
// portions of foods, and the staples on each date not marked eaten, with
// recipe staples contributing their scaled ingredients. Optional ingredients are left
// out. Shares of cooks aren't counted, as the cook is bought for once.
// recipes are the plan's recipes by ID; a recipe missing from it is left
// out.
func NeedsOf(plan *plans.Plan, byID map[pgtype.UUID]*recipes.Recipe) []Need {
	var needs []Need
	addRecipe := func(recipeID pgtype.UUID, servings float64, date time.Time, supplied map[pgtype.UUID]bool) {
		r, ok := byID[recipeID]
		if !ok {
			return
		}
		for _, in := range r.IngredientsWithout(servings, supplied) {
			needs = append(needs, Need{FoodID: in.FoodID, Date: date, Amount: in.Amount, Unit: in.Unit})
		}
	}
	for _, c := range plan.Cooks {
		if !c.Done {
			addRecipe(c.RecipeID, c.Servings, c.Date, SuppliedComponents(c))
		}
	}
	for _, p := range plan.Portions {
		if p.FoodID.Valid {
			needs = append(needs, Need{FoodID: p.FoodID, Date: p.Date, Amount: p.Amount, Unit: p.Unit})
		}
	}
	for _, o := range plan.StapleOccurrences() {
		s := o.Staple
		// A staple marked eaten is out of the pantry already.
		if s.EatenOn(o.Date) {
			continue
		}
		if s.RecipeID.Valid {
			addRecipe(s.RecipeID, s.Servings, o.Date, nil)
		} else {
			needs = append(needs, Need{FoodID: s.FoodID, Date: o.Date, Amount: s.Amount, Unit: s.Unit})
		}
	}
	slices.SortStableFunc(needs, func(a, b Need) int { return a.Date.Compare(b.Date) })
	return needs
}

// SuppliedComponents returns the components of a cook that other cooks
// supply, by recipe ID, nil if none.
func SuppliedComponents(c *plans.Cook) map[pgtype.UUID]bool {
	if len(c.Supplies) == 0 {
		return nil
	}
	supplied := map[pgtype.UUID]bool{}
	for _, s := range c.Supplies {
		supplied[s.ComponentRecipeID] = true
	}
	return supplied
}

// Input is what generating a shopping list needs.
type Input struct {
	// ShopDate is the date of the shop, at midnight UTC, which shelf lives
	// count from.
	ShopDate time.Time
	Needs    []Need
	// Foods are the foods of the needs, by ID. Needs of foods missing from
	// it are left out.
	Foods map[pgtype.UUID]*foods.Food
	// Lots are the household's pantry.
	Lots []*pantry.Item
}

// dayNeed is what's needed of a food on a date.
type dayNeed struct {
	date time.Time
	// grams is the weight of what can be weighed.
	grams float64
	// inPieces and inMilliliters are whether some of it was planned in
	// pieces or milliliters, to show the amount to buy in them.
	inPieces      bool
	inMilliliters bool
	// unweighedPieces, unweighedMilliliters and unweighedPackages are the
	// amounts that can't be weighed, as the food lacks grams_per_piece,
	// grams_per_milliliter or package_grams.
	unweighedPieces      float64
	unweighedMilliliters float64
	unweighedPackages    float64
	// shortGrams is what the pantry lacks of grams, and pantryGrams what it
	// has.
	shortGrams  float64
	pantryGrams float64
}

// Generate makes the items of a shopping list: for each food, what the
// needs lack from the pantry, as one item of what keeps from the shop
// until it's needed, and one of what doesn't, to top up on a later trip.
// The pantry covers needs in date order, from the lots used first, as long
// as they're usable on the date. Amounts are rounded up to whole packs, or
// whole pieces for foods planned in pieces. Foods always in stock are left
// out. Items are in the order of their foods' first needs.
func Generate(in *Input) []*Item {
	order, byFood := cover(in)
	var items []*Item
	for _, foodID := range order {
		f := in.Foods[foodID]
		var keeps, late []*dayNeed
		for _, d := range byFood[foodID] {
			if f.ShelfLifeDays > 0 && pantry.DaysTo(in.ShopDate, d.date) > int(f.ShelfLifeDays) {
				late = append(late, d)
			} else {
				keeps = append(keeps, d)
			}
		}
		if it := itemOf(f, keeps, false); it != nil {
			items = append(items, it)
		}
		if it := itemOf(f, late, true); it != nil {
			items = append(items, it)
		}
	}
	return items
}

// PantryGrams returns the grams of each food the pantry covers of the
// needs, like Generate covers them, including the foods it covers fully,
// which have no item. Foods always in stock are left out.
func PantryGrams(in *Input) map[pgtype.UUID]float64 {
	order, byFood := cover(in)
	grams := make(map[pgtype.UUID]float64, len(order))
	for _, foodID := range order {
		for _, d := range byFood[foodID] {
			grams[foodID] += d.pantryGrams
		}
	}
	return grams
}

// cover adds up the needs per food and date, and covers them from the
// pantry. It returns the foods in the order of their first needs, and
// their needs by date.
func cover(in *Input) ([]pgtype.UUID, map[pgtype.UUID][]*dayNeed) {
	var order []pgtype.UUID
	byFood := map[pgtype.UUID][]*dayNeed{}
	for _, n := range in.Needs {
		f, ok := in.Foods[n.FoodID]
		if !ok || f.AlwaysInStock || n.Amount <= 0 {
			continue
		}
		days, seen := byFood[n.FoodID]
		if !seen {
			order = append(order, n.FoodID)
		}
		var d *dayNeed
		if len(days) > 0 && days[len(days)-1].date.Equal(n.Date) {
			d = days[len(days)-1]
		} else {
			d = &dayNeed{date: n.Date}
			byFood[n.FoodID] = append(days, d)
		}
		grams, ok := f.Nutrition().Grams(n.Amount, n.Unit)
		switch {
		case ok:
			d.grams += grams
			d.inPieces = d.inPieces || n.Unit == nutrition.Pieces
			d.inMilliliters = d.inMilliliters || n.Unit == nutrition.Milliliters
		case n.Unit == nutrition.Pieces:
			d.unweighedPieces += n.Amount
		case n.Unit == nutrition.Packages:
			d.unweighedPackages += n.Amount
		default:
			d.unweighedMilliliters += n.Amount
		}
	}

	lotsByFood := map[pgtype.UUID][]*pantry.Item{}
	for _, lot := range in.Lots {
		lotsByFood[lot.FoodID] = append(lotsByFood[lot.FoodID], lot)
	}
	for _, foodID := range order {
		days := byFood[foodID]
		slices.SortStableFunc(days, func(a, b *dayNeed) int { return a.date.Compare(b.date) })
		coverFromPantry(days, lotsByFood[foodID])
	}
	return order, byFood
}

// coverFromPantry covers the days' needs from the lots, in date order, from
// the lots used first among those usable on each date, setting each day's
// shortGrams and pantryGrams. It doesn't change the lots.
func coverFromPantry(days []*dayNeed, lots []*pantry.Item) {
	sorted := slices.Clone(lots)
	pantry.SortByUseBy(sorted)
	left := make([]float64, len(sorted))
	for i, lot := range sorted {
		left[i] = lot.Grams
	}
	for _, d := range days {
		need := d.grams
		for i, lot := range sorted {
			if need <= 0 {
				break
			}
			if useBy := lot.UseBy(); !useBy.IsZero() && useBy.Before(d.date) {
				continue
			}
			used := min(need, left[i])
			left[i] -= used
			need -= used
			d.pantryGrams += used
		}
		d.shortGrams = max(need, 0)
	}
}

// itemOf returns the item to buy for some days' needs of a food, or nil if
// the pantry covers them. topUp is whether they won't keep from the shop.
func itemOf(f *foods.Food, days []*dayNeed, topUp bool) *Item {
	var short, fromPantry, unweighedPieces, unweighedMilliliters, unweighedPackages float64
	var inPieces, inMilliliters bool
	var neededBy time.Time
	for _, d := range days {
		fromPantry += d.pantryGrams
		unweighedPieces += d.unweighedPieces
		unweighedMilliliters += d.unweighedMilliliters
		unweighedPackages += d.unweighedPackages
		if d.shortGrams <= crumbGrams && d.unweighedPieces == 0 && d.unweighedMilliliters == 0 && d.unweighedPackages == 0 {
			continue
		}
		short += d.shortGrams
		inPieces = inPieces || d.inPieces
		inMilliliters = inMilliliters || d.inMilliliters
		if neededBy.IsZero() {
			neededBy = d.date
		}
	}
	if neededBy.IsZero() {
		return nil
	}
	it := &Item{
		FoodID:       f.ID,
		Name:         f.Name,
		NeededGrams:  round(short),
		PantryGrams:  round(fromPantry),
		StoreID:      f.PreferredStoreID,
		StoreSection: f.StoreSection,
		NeededBy:     neededBy,
		TopUp:        topUp,
	}
	var parts []string
	if short > crumbGrams {
		var text string
		it.Grams, it.Packages, text = buy(f, short, inPieces, inMilliliters)
		parts = append(parts, text)
	}
	var warnings []string
	if unweighedPieces > 0 {
		parts = append(parts, formatCount(unweighedPieces, "piece", "pieces"))
		warnings = append(warnings, "Set the food's weight per piece to weigh the pieces and count the pantry.")
	}
	if unweighedMilliliters > 0 {
		parts = append(parts, formatMilliliters(unweighedMilliliters))
		warnings = append(warnings, "Set the food's density to weigh the milliliters and count the pantry.")
	}
	if unweighedPackages > 0 {
		parts = append(parts, formatCount(unweighedPackages, "package", "packages"))
		warnings = append(warnings, "Set the food's package weight to weigh the packages and count the pantry.")
	}
	it.AmountText = strings.Join(parts, " + ")
	if topUp {
		warnings = append([]string{fmt.Sprintf(
			"Won't keep until %s, as it keeps %d days: buy it fresh closer to then, or move the meal earlier.",
			neededBy.Format("Mon, Jan 2"), f.ShelfLifeDays)}, warnings...)
	}
	it.Warning = strings.Join(warnings, " ")
	return it
}

// buy returns how much of a food to buy for a need in grams: whole packs if
// the food has a pack size, whole pieces if it was planned in pieces and
// can be counted, or the grams rounded up. It returns the grams bought, the
// packs, and the amount for people, in the unit it was planned in.
func buy(f *foods.Food, grams float64, inPieces, inMilliliters bool) (float64, int32, string) {
	milliliters := inMilliliters && !inPieces && f.GramsPerMilliliter > 0
	size := func(g float64) string {
		if milliliters {
			return formatMilliliters(g / f.GramsPerMilliliter)
		}
		return FormatGrams(g)
	}
	switch {
	case f.PackageGrams > 0:
		packs := math.Ceil(grams/f.PackageGrams - 1e-9)
		return round(packs * f.PackageGrams), int32(packs), fmt.Sprintf("%d × %s", int(packs), size(f.PackageGrams))
	case inPieces && f.GramsPerPiece > 0:
		pieces := math.Ceil(grams/f.GramsPerPiece - 1e-9)
		bought := pieces * f.GramsPerPiece
		return round(bought), 0, fmt.Sprintf("%s (~%s)", formatCount(pieces, "piece", "pieces"), FormatGrams(bought))
	default:
		bought := math.Ceil(grams - 1e-9)
		return bought, 0, size(bought)
	}
}

// FormatGrams formats a weight, e.g. "450 g" or "1.25 kg".
func FormatGrams(g float64) string {
	if g >= 1000 {
		return formatNumber(g/1000) + " kg"
	}
	return formatNumber(math.Round(g)) + " g"
}

// formatMilliliters formats a volume, e.g. "250 ml" or "1.5 l".
func formatMilliliters(ml float64) string {
	if ml >= 1000 {
		return formatNumber(ml/1000) + " l"
	}
	return formatNumber(math.Round(ml)) + " ml"
}

// formatCount formats a count, e.g. "1 piece" or "2.5 pieces".
func formatCount(n float64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return formatNumber(n) + " " + many
}

// formatNumber formats a number with at most 2 decimals, without trailing
// zeros.
func formatNumber(v float64) string {
	return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
}

// round rounds grams to 3 decimals, keeping float noise, like
// 12.000000000000002, from agents and users.
func round(v float64) float64 {
	return math.Round(v*1000) / 1000
}
