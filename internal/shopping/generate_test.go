package shopping

import (
	"slices"
	"testing"
	"time"

	"github.com/bs-tools/foodplann.ing/internal/foods"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/pantry"
	"github.com/bs-tools/foodplann.ing/internal/plans"
	"github.com/bs-tools/foodplann.ing/internal/recipes"
	"github.com/jackc/pgx/v5/pgtype"
)

func date(s string) time.Time {
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		panic(err)
	}
	return t
}

func id(b byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{b}, Valid: true}
}

// The foods of the tests.
var (
	chickpeas = &foods.Food{ID: id(1), Name: "Chickpeas, canned", PackageGrams: 400, ShelfLifeDays: 730, StoreSection: foods.StoreSectionCanned}
	onions    = &foods.Food{ID: id(2), Name: "Onions", GramsPerPiece: 150, ShelfLifeDays: 30, StoreSection: foods.StoreSectionProduce}
	spinach   = &foods.Food{ID: id(3), Name: "Spinach", ShelfLifeDays: 4, StoreSection: foods.StoreSectionProduce, PreferredStoreID: id(90)}
	soyMilk   = &foods.Food{ID: id(4), Name: "Soy milk", GramsPerMilliliter: 1.03, ShelfLifeDays: 60, StoreSection: foods.StoreSectionDrinks}
	salt      = &foods.Food{ID: id(5), Name: "Salt", AlwaysInStock: true}
	garlic    = &foods.Food{ID: id(6), Name: "Garlic", StoreSection: foods.StoreSectionProduce}
	cartons   = &foods.Food{ID: id(7), Name: "Oat milk", GramsPerMilliliter: 1, PackageGrams: 1000}
	allFoods  = map[pgtype.UUID]*foods.Food{
		chickpeas.ID: chickpeas, onions.ID: onions, spinach.ID: spinach, soyMilk.ID: soyMilk, salt.ID: salt, garlic.ID: garlic, cartons.ID: cartons,
	}
)

func TestNeedsOf(t *testing.T) {
	t.Parallel()
	// A curry of 4 servings, cooked for 2 on Monday and 6 on Wednesday, the
	// latter already done; a portion of spinach; and a staple of a recipe
	// of 2 servings, every day, skipped on Tuesday.
	curry := &recipes.Recipe{ID: id(50), Servings: 4, Ingredients: []*recipes.Ingredient{
		{FoodID: chickpeas.ID, Amount: 800, Unit: nutrition.Grams},
		{FoodID: onions.ID, Amount: 2, Unit: nutrition.Pieces},
		{FoodID: spinach.ID, Amount: 50, Unit: nutrition.Grams, Optional: true},
	}}
	oats := &recipes.Recipe{ID: id(51), Servings: 2, Ingredients: []*recipes.Ingredient{
		{FoodID: soyMilk.ID, Amount: 400, Unit: nutrition.Milliliters},
	}}
	plan := &plans.Plan{
		From: date("2026-11-02"),
		To:   date("2026-11-03"),
		Cooks: []*plans.Cook{
			{RecipeID: curry.ID, Date: date("2026-11-02"), Servings: 2},
			{RecipeID: curry.ID, Date: date("2026-11-03"), Servings: 6, Done: true},
		},
		Portions: []*plans.Portion{
			{FoodID: spinach.ID, Date: date("2026-11-03"), Amount: 80, Unit: nutrition.Grams},
			// A share of a cook is bought for with the cook.
			{CookID: id(60), CookRecipeID: curry.ID, Date: date("2026-11-03"), Servings: 1},
		},
		Staples: []*plans.Staple{
			{RecipeID: oats.ID, Servings: 1, SkippedDates: []time.Time{date("2026-11-03")}},
		},
	}
	got := NeedsOf(plan, map[pgtype.UUID]*recipes.Recipe{curry.ID: curry, oats.ID: oats})
	want := []Need{
		{FoodID: chickpeas.ID, Date: date("2026-11-02"), Amount: 400, Unit: nutrition.Grams},
		{FoodID: onions.ID, Date: date("2026-11-02"), Amount: 1, Unit: nutrition.Pieces},
		{FoodID: soyMilk.ID, Date: date("2026-11-02"), Amount: 200, Unit: nutrition.Milliliters},
		{FoodID: spinach.ID, Date: date("2026-11-03"), Amount: 80, Unit: nutrition.Grams},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestGenerate(t *testing.T) {
	t.Parallel()
	shop := date("2026-11-07")
	type item struct {
		food                      pgtype.UUID
		amount                    string
		grams, needed, fromPantry float64
		packages                  int32
		neededBy                  string
		topUp                     bool
		warns                     bool
	}
	tests := map[string]struct {
		needs []Need
		lots  []*pantry.Item
		want  []item
	}{
		"rounded up to whole packs": {
			needs: []Need{
				{FoodID: chickpeas.ID, Date: date("2026-11-07"), Amount: 300, Unit: nutrition.Grams},
				{FoodID: chickpeas.ID, Date: date("2026-11-09"), Amount: 250, Unit: nutrition.Grams},
			},
			want: []item{{food: chickpeas.ID, amount: "2 × 400 g", grams: 800, needed: 550, packages: 2, neededBy: "2026-11-07"}},
		},
		"pieces kept for display": {
			needs: []Need{
				{FoodID: onions.ID, Date: date("2026-11-07"), Amount: 1.5, Unit: nutrition.Pieces},
				{FoodID: onions.ID, Date: date("2026-11-08"), Amount: 100, Unit: nutrition.Grams},
			},
			want: []item{{food: onions.ID, amount: "3 pieces (~450 g)", grams: 450, needed: 325, neededBy: "2026-11-07"}},
		},
		"milliliters kept for display": {
			needs: []Need{{FoodID: soyMilk.ID, Date: date("2026-11-07"), Amount: 1250, Unit: nutrition.Milliliters}},
			want:  []item{{food: soyMilk.ID, amount: "1.25 l", grams: 1288, needed: 1287.5, neededBy: "2026-11-07"}},
		},
		"packs in milliliters": {
			needs: []Need{{FoodID: cartons.ID, Date: date("2026-11-07"), Amount: 1500, Unit: nutrition.Milliliters}},
			want:  []item{{food: cartons.ID, amount: "2 × 1 l", grams: 2000, needed: 1500, packages: 2, neededBy: "2026-11-07"}},
		},
		"pantry subtracted, usable lots only, soonest first": {
			needs: []Need{
				{FoodID: chickpeas.ID, Date: date("2026-11-08"), Amount: 400, Unit: nutrition.Grams},
				{FoodID: chickpeas.ID, Date: date("2026-11-12"), Amount: 400, Unit: nutrition.Grams},
			},
			lots: []*pantry.Item{
				// Expires before it's needed on the 12th, so only covers the
				// 8th.
				{FoodID: chickpeas.ID, Grams: 300, ExpiryDate: date("2026-11-10")},
				{FoodID: chickpeas.ID, Grams: 200},
			},
			// 300 g of the first lot and 100 g of the second go on the 8th,
			// and the 100 g left of the second on the 12th.
			want: []item{{food: chickpeas.ID, amount: "1 × 400 g", grams: 400, needed: 300, fromPantry: 500, packages: 1, neededBy: "2026-11-12"}},
		},
		"covered by the pantry": {
			needs: []Need{{FoodID: onions.ID, Date: date("2026-11-08"), Amount: 2, Unit: nutrition.Pieces}},
			lots:  []*pantry.Item{{FoodID: onions.ID, Grams: 299.8}},
		},
		"always in stock left out": {
			needs: []Need{{FoodID: salt.ID, Date: date("2026-11-08"), Amount: 5, Unit: nutrition.Grams}},
		},
		"what won't keep is topped up later": {
			needs: []Need{
				{FoodID: spinach.ID, Date: date("2026-11-08"), Amount: 100, Unit: nutrition.Grams},
				{FoodID: spinach.ID, Date: date("2026-11-11"), Amount: 50, Unit: nutrition.Grams},
				{FoodID: spinach.ID, Date: date("2026-11-12"), Amount: 120.2, Unit: nutrition.Grams},
				{FoodID: spinach.ID, Date: date("2026-11-13"), Amount: 80, Unit: nutrition.Grams},
			},
			want: []item{
				{food: spinach.ID, amount: "150 g", grams: 150, needed: 150, neededBy: "2026-11-08"},
				{food: spinach.ID, amount: "201 g", grams: 201, needed: 200.2, neededBy: "2026-11-12", topUp: true, warns: true},
			},
		},
		"unweighed amounts": {
			needs: []Need{
				{FoodID: garlic.ID, Date: date("2026-11-08"), Amount: 3, Unit: nutrition.Pieces},
				{FoodID: garlic.ID, Date: date("2026-11-09"), Amount: 20, Unit: nutrition.Grams},
			},
			want: []item{{food: garlic.ID, amount: "20 g + 3 pieces", grams: 20, needed: 20, neededBy: "2026-11-08", warns: true}},
		},
		"foods unknown left out": {
			needs: []Need{{FoodID: id(99), Date: date("2026-11-08"), Amount: 5, Unit: nutrition.Grams}},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var got []item
			for _, it := range Generate(&Input{ShopDate: shop, Needs: tt.needs, Foods: allFoods, Lots: tt.lots}) {
				got = append(got, item{
					food:       it.FoodID,
					amount:     it.AmountText,
					grams:      it.Grams,
					needed:     it.NeededGrams,
					fromPantry: it.PantryGrams,
					packages:   it.Packages,
					neededBy:   it.NeededBy.Format(time.DateOnly),
					topUp:      it.TopUp,
					warns:      it.Warning != "",
				})
				f := allFoods[it.FoodID]
				if it.Name != f.Name || it.StoreSection != f.StoreSection || it.StoreID != f.PreferredStoreID {
					t.Errorf("got %v, want the name, section and store of %v", it, f)
				}
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %+v,\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestGenerateTopUpWarning(t *testing.T) {
	t.Parallel()
	items := Generate(&Input{
		ShopDate: date("2026-11-07"),
		Needs:    []Need{{FoodID: spinach.ID, Date: date("2026-11-12"), Amount: 100, Unit: nutrition.Grams}},
		Foods:    allFoods,
	})
	want := "Won't keep until Thu, Nov 12, as it keeps 4 days: buy it fresh closer to then, or move the meal earlier."
	if len(items) != 1 || items[0].Warning != want {
		t.Errorf("got %v, want one item warning %q", items, want)
	}
}

func TestSortItems(t *testing.T) {
	t.Parallel()
	main, bulk := &Store{ID: id(80), Name: "Supermarket"}, &Store{ID: id(81), Name: "Bulk store"}
	stores := []*Store{main, bulk}
	items := []*Item{
		{Name: "Dish soap", StoreSection: foods.StoreSectionOther},
		{Name: "Lentils", StoreSection: foods.StoreSectionDryGoods, StoreID: bulk.ID},
		{Name: "spinach", StoreSection: foods.StoreSectionProduce, TopUp: true},
		{Name: "Mystery", StoreSection: foods.StoreSectionUnspecified},
		{Name: "Spinach", StoreSection: foods.StoreSectionProduce},
		{Name: "Apples", StoreSection: foods.StoreSectionProduce, StoreID: main.ID},
		{Name: "Tofu", StoreSection: foods.StoreSectionChilled, StoreID: id(99)},
	}
	SortItems(items, stores)
	var got []string
	for _, it := range items {
		name := it.Name
		if it.TopUp {
			name += " (top-up)"
		}
		got = append(got, name)
	}
	want := []string{"Apples", "Spinach", "spinach (top-up)", "Tofu", "Dish soap", "Mystery", "Lentils"}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestKeepChecks(t *testing.T) {
	t.Parallel()
	checkTime := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	old := []*Item{
		{ID: id(10), FoodID: chickpeas.ID, Checked: true, CheckedBy: id(70), CheckTime: checkTime, Note: "the organic ones"},
		{ID: id(11), FoodID: spinach.ID},
		{ID: id(12), FoodID: spinach.ID, TopUp: true, Checked: true, CheckedBy: id(70), CheckTime: checkTime},
		{ID: id(13), FoodID: onions.ID, Checked: true, CheckedBy: id(70), CheckTime: checkTime},
		{ID: id(14), FoodID: garlic.ID, Manual: true, Checked: true},
	}
	generated := []*Item{
		{FoodID: chickpeas.ID},
		{FoodID: spinach.ID, TopUp: true},
		{FoodID: spinach.ID},
		// Onions are topped up now; they keep the check of the onions bought
		// before.
		{FoodID: onions.ID, TopUp: true},
		// Garlic was only added by hand before.
		{FoodID: garlic.ID},
		{FoodID: soyMilk.ID},
	}
	KeepChecks(old, generated)
	type kept struct {
		id      pgtype.UUID
		checked bool
		note    string
	}
	var got []kept
	for _, it := range generated {
		got = append(got, kept{it.ID, it.Checked, it.Note})
	}
	want := []kept{
		{id(10), true, "the organic ones"},
		{id(12), true, ""},
		{id(11), false, ""},
		{id(13), true, ""},
		{},
		{},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if generated[0].CheckedBy != id(70) || !generated[0].CheckTime.Equal(checkTime) {
		t.Errorf("got %v, want who checked it and when", generated[0])
	}
}

func TestPantryGrams(t *testing.T) {
	t.Parallel()
	// The pantry covers all of the onions, which have no item, part of the
	// chickpeas, and none of the spinach, gone off by Wednesday. Salt is
	// always in stock.
	got := PantryGrams(&Input{
		ShopDate: date("2026-11-02"),
		Needs: []Need{
			{FoodID: onions.ID, Date: date("2026-11-02"), Amount: 1, Unit: nutrition.Pieces},
			{FoodID: chickpeas.ID, Date: date("2026-11-02"), Amount: 600, Unit: nutrition.Grams},
			{FoodID: spinach.ID, Date: date("2026-11-04"), Amount: 100, Unit: nutrition.Grams},
			{FoodID: salt.ID, Date: date("2026-11-02"), Amount: 5, Unit: nutrition.Grams},
		},
		Foods: allFoods,
		Lots: []*pantry.Item{
			{FoodID: onions.ID, Grams: 300},
			{FoodID: chickpeas.ID, Grams: 400},
			{FoodID: spinach.ID, Grams: 200, ExpiryDate: date("2026-11-03")},
			{FoodID: salt.ID, Grams: 500},
		},
	})
	want := map[pgtype.UUID]float64{onions.ID: 150, chickpeas.ID: 400, spinach.ID: 0}
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for id, grams := range want {
		if got[id] != grams {
			t.Errorf("got %v, want %v", got, want)
		}
	}
}
