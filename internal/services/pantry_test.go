package services_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/mealplann.ing/internal/gen/recipes/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// shopFood creates a custom food with no nutrients known, for the pantry
// and the shopping list.
func shopFood(t *testing.T, e *env, name string, edit func(*foodsv1.Food)) *foodsv1.Food {
	t.Helper()
	f := foodsv1.Food_builder{Name: name}.Build()
	if edit != nil {
		edit(f)
	}
	return createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{Food: f}.Build())
}

func addToPantry(t *testing.T, e *env, req *pantryv1.AddToPantryRequest) *pantryv1.PantryItem {
	t.Helper()
	resp, err := e.pantry.AddToPantry(context.Background(), req)
	if err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	return resp.GetItem()
}

func listPantry(t *testing.T, e *env) *pantryv1.ListPantryResponse {
	t.Helper()
	resp, err := e.pantry.ListPantry(context.Background(), pantryv1.ListPantryRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListPantry: %v", err)
	}
	return resp
}

// stock describes the pantry's lots, e.g. "Lentils 500 g until 2026-11-10".
func stock(resp *pantryv1.ListPantryResponse) []string {
	var lots []string
	for _, it := range resp.GetItems() {
		lot := it.GetFoodName() + " " + formatGrams(it.GetGrams()) + " g"
		if it.GetUseByDate() != "" {
			lot += " until " + it.GetUseByDate()
		}
		lots = append(lots, lot)
	}
	return lots
}

// formatGrams formats grams without trailing zeros.
func formatGrams(g float64) string {
	return strconv.FormatFloat(g, 'f', -1, 64)
}

func TestPantry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	today := time.Now().UTC().Format(time.DateOnly)

	onions := shopFood(t, e, "Onions", func(f *foodsv1.Food) {
		f.SetGramsPerPiece(150)
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_PRODUCE)
	})
	soyMilk := shopFood(t, e, "Soy milk", func(f *foodsv1.Food) {
		f.SetGramsPerMilliliter(1.03)
		f.SetShelfLifeDays(60)
		f.SetOpenedShelfLifeDays(4)
	})
	garlic := shopFood(t, e, "Garlic", nil)

	// Pieces and milliliters are weighed with the food's weights, and fail
	// with what to set on the food when it has none.
	addedOnions := addToPantry(t, e, pantryv1.AddToPantryRequest_builder{
		FoodId: onions.GetId(), Amount: 3, Unit: pieces, ExpiryDate: "2026-11-20", Notes: "red",
	}.Build())
	if addedOnions.GetGrams() != 450 || addedOnions.GetFoodName() != "Onions" || addedOnions.GetGramsPerPiece() != 150 ||
		addedOnions.GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_PRODUCE || addedOnions.GetUseByDate() != "2026-11-20" ||
		!addedOnions.HasDaysToExpiry() || addedOnions.GetNotes() != "red" {
		t.Errorf("got %v, want 450 g of onions until 20 November", addedOnions)
	}
	// Without an expiry date, it's today plus the food's shelf life. The
	// response says how the amount was weighed.
	added, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: soyMilk.GetId(), Amount: 1000, Unit: milliliters}.Build())
	if err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	milk := added.GetItem()
	if milk.GetGrams() != 1030 || milk.GetDaysToExpiry() != 60 || milk.GetExpiryDate() == "" {
		t.Errorf("got %v, want 1030 g expiring in 60 days", milk)
	}
	if got, want := added.GetConversion(), "1000 ml at 1.03 g/ml is 1030 g"; got != want {
		t.Errorf("got conversion %q, want %q", got, want)
	}
	_, err = e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 3, Unit: pieces}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	if !strings.Contains(err.Error(), "set grams_per_piece on the food") {
		t.Errorf("got %v, want it to say to set grams_per_piece", err)
	}
	_, err = e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 3, Unit: milliliters}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	if !strings.Contains(err.Error(), "set grams_per_milliliter on the food") {
		t.Errorf("got %v, want it to say to set grams_per_milliliter", err)
	}
	for name, req := range map[string]*pantryv1.AddToPantryRequest{
		"no unit":      pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 3}.Build(),
		"no amount":    pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Unit: grams}.Build(),
		"bad date":     pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 3, Unit: grams, ExpiryDate: "20/11/2026"}.Build(),
		"bad food ID":  pantryv1.AddToPantryRequest_builder{FoodId: "garlic", Amount: 3, Unit: grams}.Build(),
		"too much":     pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 200_000, Unit: grams}.Build(),
		"long notes":   pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 3, Unit: grams, Notes: strings.Repeat("a", 501)}.Build(),
		"opened typos": pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 3, Unit: grams, OpenedDate: "0226-11-01"}.Build(),
	} {
		_, err := e.pantry.AddToPantry(ctx, req)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want InvalidArgument", name, err)
		}
	}
	// Another household's food isn't found.
	_, err = e.otherPantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: garlic.GetId(), Amount: 3, Unit: grams}.Build())
	wantCode(t, err, connect.CodeNotFound)

	// A second lot of onions, without an expiry date, lists after the first.
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: onions.GetId(), Amount: 100, Unit: grams}.Build())
	if got, want := stock(listPantry(t, e)), []string{"Onions 450 g until 2026-11-20", "Onions 100 g", "Soy milk 1030 g until " + milk.GetExpiryDate()}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Opening the milk shortens its use-by date to the opened shelf life.
	updated, err := e.pantry.UpdatePantryItem(ctx, pantryv1.UpdatePantryItemRequest_builder{
		Item:       pantryv1.PantryItem_builder{Id: milk.GetId(), Grams: 800, OpenedDate: today, FoodName: "ignored"}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"grams", "opened_date"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdatePantryItem: %v", err)
	}
	if it := updated.GetItem(); it.GetGrams() != 800 || it.GetOpenedDate() != today || it.GetDaysToExpiry() != 4 ||
		it.GetFoodName() != "Soy milk" || it.GetExpiryDate() != milk.GetExpiryDate() {
		t.Errorf("got %v, want 800 g opened today, keeping 4 days", it)
	}
	for _, grams := range []float64{0, -1, 200_000} {
		_, err = e.pantry.UpdatePantryItem(ctx, pantryv1.UpdatePantryItemRequest_builder{
			Item:       pantryv1.PantryItem_builder{Id: milk.GetId(), Grams: grams}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"grams"}},
		}.Build())
		wantCode(t, err, connect.CodeInvalidArgument)
	}

	// Another household can't see, change or remove the lots.
	if items := listPantry(t, &env{pantry: e.otherPantry}).GetItems(); len(items) != 0 {
		t.Errorf("other household sees %v", items)
	}
	_, err = e.otherPantry.UpdatePantryItem(ctx, pantryv1.UpdatePantryItemRequest_builder{
		Item:       pantryv1.PantryItem_builder{Id: milk.GetId(), Grams: 1}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"grams"}},
	}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherPantry.RemoveFromPantry(ctx, pantryv1.RemoveFromPantryRequest_builder{Id: milk.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.anonPantry.ListPantry(ctx, pantryv1.ListPantryRequest_builder{}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Removing a lot.
	if _, err := e.pantry.RemoveFromPantry(ctx, pantryv1.RemoveFromPantryRequest_builder{Id: addedOnions.GetId()}.Build()); err != nil {
		t.Fatalf("RemoveFromPantry: %v", err)
	}
	_, err = e.pantry.RemoveFromPantry(ctx, pantryv1.RemoveFromPantryRequest_builder{Id: addedOnions.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)

	// Salt is always in stock: it lists as such, and isn't tracked.
	salt := shopFood(t, e, "Salt", func(f *foodsv1.Food) { f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_SPICES) })
	if _, err := e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: salt.GetId(), AlwaysInStock: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"always_in_stock"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateFood: %v", err)
	}
	resp := listPantry(t, e)
	if got := resp.GetAlwaysInStockFoods(); len(got) != 1 || got[0].GetFoodName() != "Salt" || got[0].GetFoodId() != salt.GetId() ||
		got[0].GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_SPICES {
		t.Errorf("got %v, want salt always in stock", got)
	}
	_, err = e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: salt.GetId(), Amount: 500, Unit: grams}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	// Unless asked to track it, which clears always in stock.
	tracked, err := e.pantry.AddToPantry(ctx, pantryv1.AddToPantryRequest_builder{FoodId: salt.GetId(), Amount: 500, Unit: grams, TrackStock: true}.Build())
	if err != nil {
		t.Fatalf("AddToPantry: %v", err)
	}
	if !tracked.GetStoppedAlwaysInStock() || tracked.GetItem().GetGrams() != 500 || tracked.GetConversion() != "" {
		t.Errorf("got %v, want 500 g of salt, no longer always in stock", tracked)
	}
	if got := listPantry(t, e).GetAlwaysInStockFoods(); len(got) != 0 {
		t.Errorf("got %v always in stock, want none", got)
	}
	if _, err := e.pantry.RemoveFromPantry(ctx, pantryv1.RemoveFromPantryRequest_builder{Id: tracked.GetItem().GetId()}.Build()); err != nil {
		t.Fatalf("RemoveFromPantry: %v", err)
	}

	// Deleting a food deletes its stock.
	if _, err := e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: onions.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteFood: %v", err)
	}
	if got, want := stock(listPantry(t, e)), []string{"Soy milk 800 g until " + updated.GetItem().GetUseByDate()}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCookDonePantry checks that marking a cook done takes its ingredients
// out of the pantry, and un-marking it puts back exactly what it took.
func TestCookDonePantry(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	lentils := shopFood(t, e, "Lentils", nil)
	onions := shopFood(t, e, "Onions", func(f *foodsv1.Food) { f.SetGramsPerPiece(150) })
	garlic := shopFood(t, e, "Garlic", nil)
	coriander := shopFood(t, e, "Coriander", nil)
	created, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Name:     "Dal",
			Servings: 2,
			Ingredients: []*recipesv1.Ingredient{
				ingredient(lentils.GetId(), 300, grams),
				ingredient(onions.GetId(), 1, pieces),
				// Garlic in pieces can't be weighed, so it's left in.
				ingredient(garlic.GetId(), 2, pieces),
				recipesv1.Ingredient_builder{FoodId: coriander.GetId(), Amount: 10, Unit: grams, Optional: true}.Build(),
			},
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	dal := created.GetRecipe()

	// The pantry: lentils expiring soon, more without expiry, an onion, and
	// coriander, which is optional in the dal.
	soon := addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: lentils.GetId(), Amount: 100, Unit: grams, ExpiryDate: "2026-11-10"}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: lentils.GetId(), Amount: 1000, Unit: grams, Notes: "big bag"}.Build())
	onion := addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: onions.GetId(), Amount: 1, Unit: pieces, OpenedDate: "2026-11-01"}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: coriander.GetId(), Amount: 30, Unit: grams}.Build())
	before := listPantry(t, e).GetItems()

	// A cook of 4 servings, twice the recipe.
	cook, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: dal.GetId(), Date: monday, Servings: 4}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	if len(cook.GetPantryChanges()) != 0 {
		t.Errorf("got %v, want no pantry changes for a cook not done", cook.GetPantryChanges())
	}
	setDone := func(done bool) *plansv1.UpdateCookResponse {
		t.Helper()
		resp, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
			Cook:       plansv1.Cook_builder{Id: cook.GetCook().GetId(), Done: done}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
		}.Build())
		if err != nil {
			t.Fatalf("UpdateCook: %v", err)
		}
		return resp
	}
	changes := func(resp interface {
		GetPantryChanges() []*plansv1.PantryChange
	},
	) []string {
		var got []string
		for _, c := range resp.GetPantryChanges() {
			got = append(got, c.GetFoodName()+" "+formatGrams(c.GetGrams()))
		}
		return got
	}

	// Marking it done takes 600 g of lentils out, the soonest to expire
	// first, and the onion, all there is of the 300 g needed. The garlic and
	// the optional coriander stay.
	done := setDone(true)
	if got, want := changes(done), []string{"Lentils 600", "Onions 150"}; !slices.Equal(got, want) || done.GetPantryRestored() {
		t.Errorf("got changes %v, want %v", got, want)
	}
	if got, want := stock(listPantry(t, e)), []string{"Coriander 30 g", "Lentils 500 g"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	// Marking it done again, or changing it while done, changes nothing.
	if got := changes(setDone(true)); len(got) != 0 {
		t.Errorf("got changes %v marking done twice, want none", got)
	}
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetCook().GetId(), Servings: 2}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"servings"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	if got, want := stock(listPantry(t, e)), []string{"Coriander 30 g", "Lentils 500 g"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Un-marking it puts back exactly what was taken out, into the same
	// lots, even those it emptied.
	undone := setDone(false)
	if got, want := changes(undone), []string{"Lentils 600", "Onions 150"}; !slices.Equal(got, want) || !undone.GetPantryRestored() {
		t.Errorf("got changes %v, want %v put back", got, want)
	}
	after := listPantry(t, e).GetItems()
	if len(after) != len(before) {
		t.Fatalf("got %v, want %v", after, before)
	}
	for i := range before {
		b, a := before[i], after[i]
		if a.GetId() != b.GetId() || a.GetGrams() != b.GetGrams() || a.GetExpiryDate() != b.GetExpiryDate() ||
			a.GetOpenedDate() != b.GetOpenedDate() || a.GetNotes() != b.GetNotes() {
			t.Errorf("got lot %v after undoing, want %v", a, b)
		}
	}
	if after[1].GetId() != soon.GetId() || after[3].GetId() != onion.GetId() {
		t.Errorf("got %v, want the lots in order", after)
	}
	if got := changes(setDone(false)); len(got) != 0 {
		t.Errorf("got changes %v un-marking twice, want none", got)
	}

	// A cook created done takes its ingredients out too, now of 2
	// servings.
	createdDone, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: dal.GetId(), Date: tuesday, Servings: 2, Done: true}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	if got, want := changes(createdDone), []string{"Lentils 300", "Onions 150"}; !slices.Equal(got, want) {
		t.Errorf("got changes %v, want %v", got, want)
	}
	// Deleting it leaves the pantry as it is.
	if _, err := e.plans.DeleteCook(ctx, plansv1.DeleteCookRequest_builder{Id: createdDone.GetCook().GetId()}.Build()); err != nil {
		t.Fatalf("DeleteCook: %v", err)
	}
	if got, want := stock(listPantry(t, e)), []string{"Coriander 30 g", "Lentils 800 g"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Two phones marking the cook done at the same time take its
	// ingredients out once.
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			_, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
				Cook:       plansv1.Cook_builder{Id: cook.GetCook().GetId(), Done: true}.Build(),
				UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
			}.Build())
			if err != nil {
				t.Errorf("UpdateCook: %v", err)
			}
		})
	}
	wg.Wait()
	if got, want := stock(listPantry(t, e)), []string{"Coriander 30 g", "Lentils 500 g"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Another household can't mark the cook done, nor see the pantry.
	_, err = e.otherPlans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: cook.GetCook().GetId(), Done: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"done"}},
	}.Build())
	wantCode(t, err, connect.CodeNotFound)
	if got := stock(listPantry(t, &env{pantry: e.otherPantry})); len(got) != 0 {
		t.Errorf("other household sees %v", got)
	}
}
