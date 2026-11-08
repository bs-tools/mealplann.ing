package services_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestPantryLotDetails checks where lots are kept, how long frozen ones
// keep, lots opened on an unknown day, guessed amounts, labels and
// packages.
func TestPantryLotDetails(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	today := time.Now().UTC()
	date := func(days int) string { return today.AddDate(0, 0, days).Format(time.DateOnly) }

	// Cooked beans keep 4 days in the fridge and 180 frozen.
	beans := shopFood(t, e, "Black beans, cooked from dry", func(f *foodsv1.Food) {
		f.SetShelfLifeDays(4)
		f.SetFreezerShelfLifeDays(180)
		f.SetPackageGrams(500)
	})
	sauce := shopFood(t, e, "Hot sauce", func(f *foodsv1.Food) {
		f.SetShelfLifeDays(365)
		f.SetOpenedShelfLifeDays(90)
		f.SetPackageGrams(150)
	})
	add := func(req *pantryv1.AddToPantryRequest) *pantryv1.AddToPantryResponse {
		t.Helper()
		resp, err := e.pantry.AddToPantry(ctx, req)
		if err != nil {
			t.Fatalf("AddToPantry: %v", err)
		}
		return resp
	}

	// Frozen today, they keep 180 days, and get no fridge expiry date.
	frozen := add(pantryv1.AddToPantryRequest_builder{
		FoodId: beans.GetId(), Amount: 1000, Unit: grams, Storage: pantryv1.Storage_STORAGE_FREEZER, Location: "chest freezer",
	}.Build()).GetItem()
	if frozen.GetFrozenDate() != date(0) || frozen.GetUseByDate() != date(180) || frozen.GetExpiryDate() != "" ||
		frozen.GetStorage() != pantryv1.Storage_STORAGE_FREEZER || frozen.GetLocation() != "chest freezer" {
		t.Errorf("got %v, want frozen today, to use in 180 days", frozen)
	}
	// Frozen a month ago, they have 150 days left.
	older := add(pantryv1.AddToPantryRequest_builder{
		FoodId: beans.GetId(), Packages: 1.5, Storage: pantryv1.Storage_STORAGE_FREEZER, FrozenDate: date(-30),
	}.Build())
	if older.GetItem().GetGrams() != 750 || older.GetItem().GetDaysToExpiry() != 150 || older.GetConversion() != "1.5 packages of 500 g is 750 g" {
		t.Errorf("got %v, want 750 g with 150 days left", older)
	}
	// Thawed into the fridge, the beans keep as they would there.
	thawed, err := e.pantry.UpdatePantryItem(ctx, pantryv1.UpdatePantryItemRequest_builder{
		Item:       pantryv1.PantryItem_builder{Id: frozen.GetId(), Storage: pantryv1.Storage_STORAGE_FRIDGE, ExpiryDate: date(3)}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"storage", "expiry_date"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePantryItem: %v", err)
	}
	if got := thawed.GetItem(); got.GetFrozenDate() != "" || got.GetUseByDate() != date(3) || got.GetLocation() != "chest freezer" {
		t.Errorf("got %v, want it in the fridge until %s", got, date(3))
	}
	// Moved back into the freezer, it was frozen today.
	refrozen, err := e.pantry.UpdatePantryItem(ctx, pantryv1.UpdatePantryItemRequest_builder{
		Item:       pantryv1.PantryItem_builder{Id: frozen.GetId(), Storage: pantryv1.Storage_STORAGE_FREEZER}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"storage"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePantryItem: %v", err)
	}
	if got := refrozen.GetItem(); got.GetFrozenDate() != date(0) || got.GetUseByDate() != date(180) {
		t.Errorf("got %v, want it frozen today", got)
	}

	// Opened on a day nobody knows, a sauce keeps until its expiry date,
	// not 90 days from today. It's one of two labels, and its amount is a
	// guess.
	opened := add(pantryv1.AddToPantryRequest_builder{
		FoodId: sauce.GetId(), Packages: 0.33, ExpiryDate: date(200), Opened: true, Estimated: true, Label: "Sriracha",
	}.Build()).GetItem()
	if !opened.GetOpened() || opened.GetOpenedDate() != "" || opened.GetUseByDate() != date(200) || !opened.GetEstimated() ||
		opened.GetLabel() != "Sriracha" || !near(opened.GetGrams(), 49.5) {
		t.Errorf("got %v, want an opened, estimated 49.5 g of Sriracha until its expiry date", opened)
	}
	// Opened today, it keeps 90 days.
	other := add(pantryv1.AddToPantryRequest_builder{
		FoodId: sauce.GetId(), Amount: 150, Unit: grams, ExpiryDate: date(300), OpenedDate: date(0), Label: "Cholula",
	}.Build()).GetItem()
	if !other.GetOpened() || other.GetUseByDate() != date(90) {
		t.Errorf("got %v, want it opened, to use in 90 days", other)
	}

	// Filters by storage and guesses.
	list := func(req *pantryv1.ListPantryRequest) []string {
		t.Helper()
		resp, err := e.pantry.ListPantry(ctx, req)
		if err != nil {
			t.Fatalf("ListPantry: %v", err)
		}
		var got []string
		for _, it := range resp.GetItems() {
			got = append(got, it.GetFoodName()+" "+it.GetLabel())
		}
		return got
	}
	if got, want := list(pantryv1.ListPantryRequest_builder{Storage: pantryv1.Storage_STORAGE_FREEZER}.Build()), []string{
		"Black beans, cooked from dry ", "Black beans, cooked from dry ",
	}; !slices.Equal(got, want) {
		t.Errorf("got %v in the freezer, want %v", got, want)
	}
	if got, want := list(pantryv1.ListPantryRequest_builder{EstimatedOnly: true}.Build()), []string{"Hot sauce Sriracha"}; !slices.Equal(got, want) {
		t.Errorf("got %v estimated, want %v", got, want)
	}

	// Mistakes say what to do.
	for name, req := range map[string]*pantryv1.AddToPantryRequest{
		"frozen date outside the freezer": pantryv1.AddToPantryRequest_builder{FoodId: beans.GetId(), Amount: 100, Unit: grams, FrozenDate: date(0)}.Build(),
		"packages and amount":             pantryv1.AddToPantryRequest_builder{FoodId: beans.GetId(), Amount: 100, Unit: grams, Packages: 1}.Build(),
		"long label":                      pantryv1.AddToPantryRequest_builder{FoodId: beans.GetId(), Amount: 100, Unit: grams, Label: string(make([]byte, 101))}.Build(),
	} {
		_, err := e.pantry.AddToPantry(ctx, req)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid argument", name, err)
		}
	}
	unpacked := shopFood(t, e, "Loose rice", nil)
	_, err = e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: unpacked.GetId(), Packages: 1}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
}

// TestCookFromStock checks that cooked food already in stock is entered as
// a cook that took nothing from the pantry.
func TestCookFromStock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	lentils := customFood(t, e, "Lentils", 350, 25, 60, 2, 1, nil)
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: lentils.GetId(), Amount: 1000, Unit: grams}.Build())
	ragu, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Lentil ragù", Servings: 6, FreezerDays: new(int32(90)),
		Ingredients: []*recipesv1.Ingredient{ingredient(lentils.GetId(), 300, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	cookedOn := time.Now().UTC().AddDate(0, 0, -10).Format(time.DateOnly)
	created, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{
			RecipeId: ragu.GetRecipe().GetId(), Date: cookedOn, Servings: 4, FrozenServings: 4,
		}.Build(),
		FromStock: true,
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	if !created.GetCook().GetDone() || len(created.GetPantryChanges()) != 0 {
		t.Errorf("got %v, want a done cook taking nothing out of the pantry", created)
	}
	if got := listPantry(t, e).GetItems(); len(got) != 1 || got[0].GetGrams() != 1000 {
		t.Errorf("got pantry %v, want the lentils untouched", got)
	}
	frozen, err := e.plans.ListFrozenLeftovers(ctx, plansv1.ListFrozenLeftoversRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFrozenLeftovers: %v", err)
	}
	if got := frozen.GetLeftovers(); len(got) != 1 || got[0].GetRemainingServings() != 4 {
		t.Errorf("got %v, want the 4 frozen servings of ragù", got)
	}
}

// TestPantryLotDetailsRestored checks that un-marking a cook that emptied
// a lot puts it back with its details.
func TestPantryLotDetailsRestored(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	beans := customFood(t, e, "Black beans", 130, 9, 24, 0, 0.5, nil)
	lot := addToPantry(t, e, pantryv1.AddToPantryRequest_builder{
		FoodId: beans.GetId(), Amount: 200, Unit: grams, Storage: pantryv1.Storage_STORAGE_FREEZER,
		Location: "chest freezer", Label: "Home cooked", Estimated: true,
	}.Build())
	recipe, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{Recipe: recipesv1.Recipe_builder{
		Name: "Bean tacos", Servings: 2, Ingredients: []*recipesv1.Ingredient{ingredient(beans.GetId(), 300, grams)},
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	cook, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{Cook: plansv1.Cook_builder{
		RecipeId: recipe.GetRecipe().GetId(), Date: friday, Servings: 2, Done: true,
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	if got := listPantry(t, e).GetItems(); len(got) != 0 {
		t.Fatalf("got %v, want the lot used up", got)
	}
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetCook().GetId(), Done: false}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	got := listPantry(t, e).GetItems()
	if len(got) != 1 || got[0].GetId() != lot.GetId() || got[0].GetStorage() != pantryv1.Storage_STORAGE_FREEZER ||
		got[0].GetFrozenDate() != lot.GetFrozenDate() || got[0].GetLocation() != "chest freezer" ||
		got[0].GetLabel() != "Home cooked" || !got[0].GetEstimated() {
		t.Errorf("got %v, want the lot back as it was, %v", got, lot)
	}
}
