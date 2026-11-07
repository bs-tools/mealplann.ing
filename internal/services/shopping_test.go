package services_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/foodplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/foodplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/foodplann.ing/internal/gen/plans/v1"
	recipesv1 "github.com/bs-tools/foodplann.ing/internal/gen/recipes/v1"
	shoppingv1 "github.com/bs-tools/foodplann.ing/internal/gen/shopping/v1"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func createStore(t *testing.T, e *env, name string) *shoppingv1.Store {
	t.Helper()
	resp, err := e.shopping.CreateStore(context.Background(), shoppingv1.CreateStoreRequest_builder{
		Store: shoppingv1.Store_builder{Name: name}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}
	return resp.GetStore()
}

// shoppingLines describes a list's items in order, e.g. "Supermarket ·
// Onions · 3 pieces (~450 g)", with "checked" and "top-up" when they are.
func shoppingLines(list *shoppingv1.ShoppingList) []string {
	var lines []string
	for _, it := range list.GetItems() {
		line := it.GetStoreName() + " · " + it.GetName() + " · " + it.GetAmountText()
		if it.GetTopUp() {
			line += " · top-up"
		}
		if it.GetChecked() {
			line += " · checked by " + it.GetCheckedBy()
		}
		lines = append(lines, line)
	}
	return lines
}

func itemNamed(t *testing.T, list *shoppingv1.ShoppingList, name string, topUp bool) *shoppingv1.ShoppingItem {
	t.Helper()
	for _, it := range list.GetItems() {
		if it.GetName() == name && it.GetTopUp() == topUp {
			return it
		}
	}
	t.Fatalf("no item %s in %v", name, shoppingLines(list))
	return nil
}

func TestShopping(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()

	// Ben is the signed in user, and checks items off.
	b := ben()
	b.SetUserId(store.UUIDToString(e.userID))
	ben := createPerson(t, e, b)

	// The stores: the supermarket, the main store, and a bulk store for
	// lentils.
	supermarket := createStore(t, e, "Supermarket")
	bulk := createStore(t, e, "Bulk store")
	if supermarket.GetPosition() != 0 || bulk.GetPosition() != 1 {
		t.Errorf("got positions %d and %d, want 0 and 1", supermarket.GetPosition(), bulk.GetPosition())
	}
	_, err := e.shopping.CreateStore(ctx, shoppingv1.CreateStoreRequest_builder{Store: shoppingv1.Store_builder{Name: "supermarket"}.Build()}.Build())
	wantCode(t, err, connect.CodeAlreadyExists)
	_, err = e.shopping.CreateStore(ctx, shoppingv1.CreateStoreRequest_builder{Store: shoppingv1.Store_builder{Name: " "}.Build()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// The foods.
	chickpeas := shopFood(t, e, "Chickpeas, canned", func(f *foodsv1.Food) {
		f.SetPackageGrams(400)
		f.SetShelfLifeDays(730)
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_CANNED)
	})
	onions := shopFood(t, e, "Onions", func(f *foodsv1.Food) {
		f.SetGramsPerPiece(150)
		f.SetShelfLifeDays(30)
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_PRODUCE)
	})
	spinach := shopFood(t, e, "Spinach", func(f *foodsv1.Food) {
		f.SetShelfLifeDays(3)
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_PRODUCE)
	})
	soyMilk := shopFood(t, e, "Soy milk", func(f *foodsv1.Food) {
		f.SetGramsPerMilliliter(1.03)
		f.SetPackageGrams(1030)
		f.SetShelfLifeDays(60)
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_DRINKS)
	})
	salt := shopFood(t, e, "Salt", func(f *foodsv1.Food) { f.SetAlwaysInStock(true) })
	coriander := shopFood(t, e, "Coriander", func(f *foodsv1.Food) { f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_PRODUCE) })
	lentils := shopFood(t, e, "Lentils", func(f *foodsv1.Food) {
		f.SetStoreSection(foodsv1.StoreSection_STORE_SECTION_DRY_GOODS)
		f.SetPreferredStoreId(bulk.GetId())
	})
	if lentils.GetPreferredStoreId() != bulk.GetId() || !salt.GetAlwaysInStock() {
		t.Errorf("got %v and %v, want lentils from the bulk store and salt always in stock", lentils, salt)
	}
	// Another household's store can't be preferred.
	otherStore, err := e.otherShopping.CreateStore(ctx, shoppingv1.CreateStoreRequest_builder{Store: shoppingv1.Store_builder{Name: "Corner shop"}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}
	_, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: onions.GetId(), PreferredStoreId: otherStore.GetStore().GetId()}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"preferred_store_id"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// The recipes: a curry of 4 servings, with salt and optional
	// coriander, and a dal of 2.
	newRecipe := func(name string, servings int32, ingredients ...*recipesv1.Ingredient) *recipesv1.Recipe {
		t.Helper()
		resp, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
			Recipe: recipesv1.Recipe_builder{Name: name, Servings: servings, Ingredients: ingredients}.Build(),
		}.Build())
		if err != nil {
			t.Fatalf("CreateRecipe: %v", err)
		}
		return resp.GetRecipe()
	}
	curry := newRecipe("Chickpea curry", 4,
		ingredient(chickpeas.GetId(), 400, grams),
		ingredient(onions.GetId(), 2, pieces),
		ingredient(spinach.GetId(), 200, grams),
		ingredient(salt.GetId(), 5, grams),
		recipesv1.Ingredient_builder{FoodId: coriander.GetId(), Amount: 10, Unit: grams, Optional: true}.Build(),
	)
	dal := newRecipe("Dal", 2, ingredient(lentils.GetId(), 200, grams), ingredient(onions.GetId(), 1, pieces))

	// The week: the curry for 8 on Monday, twice the recipe, the dal for 2
	// on Friday, spinach for Ben on Friday, and his soy milk every day.
	createCook := func(recipeID, date string, servings float64) *plansv1.Cook {
		t.Helper()
		resp, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
			Cook: plansv1.Cook_builder{RecipeId: recipeID, Date: date, Servings: servings}.Build(),
		}.Build())
		if err != nil {
			t.Fatalf("CreateCook: %v", err)
		}
		return resp.GetCook()
	}
	curryCook := createCook(curry.GetId(), monday, 8)
	createCook(dal.GetId(), friday, 2)
	if _, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{
		Portion: plansv1.Portion_builder{
			PersonId:     ben.GetId(),
			EatingTimeId: eatingTimeID(t, ben, "Lunch"),
			Date:         friday,
			Food:         plansv1.FoodAmount_builder{FoodId: spinach.GetId(), Amount: 100, Unit: grams}.Build(),
		}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	if _, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{
		Staple: plansv1.Staple_builder{
			PersonId:     ben.GetId(),
			EatingTimeId: eatingTimeID(t, ben, "Breakfast"),
			Food:         plansv1.FoodAmount_builder{FoodId: soyMilk.GetId(), Amount: 250, Unit: milliliters}.Build(),
		}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}

	// The pantry: 2 onions, a can of chickpeas, half a liter of soy milk,
	// and spinach that will have gone off by Monday.
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: onions.GetId(), Amount: 2, Unit: pieces, ExpiryDate: "2026-11-30"}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: chickpeas.GetId(), Amount: 400, Unit: grams, ExpiryDate: "2027-01-01"}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: soyMilk.GetId(), Amount: 500, Unit: milliliters, ExpiryDate: "2026-12-01"}.Build())
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: spinach.GetId(), Amount: 100, Unit: grams, ExpiryDate: "2026-11-01"}.Build())

	// Make the list for the week, shopping on Monday.
	created, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: monday}.Build())
	if err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}
	list := created.GetList()
	if list.GetStartDate() != monday || list.GetEndDate() != "2026-11-08" || list.GetComplete() || list.GetItemCount() != 6 || list.GetTopUpCount() != 1 {
		t.Errorf("got list %v, want a week from Monday with 6 items, 1 to top up", list)
	}
	// Onions: 4 for the curry and 1 for the dal, less the 2 in stock.
	// Spinach: 400 g for the curry, as the spinach in stock expires first,
	// and 100 g on Friday, which won't keep 4 days. Chickpeas: 800 g less a
	// can in stock. Soy milk: 7 × 250 ml less 500 ml, in liter cartons.
	// Lentils from the bulk store. Salt is always in stock, and coriander
	// optional.
	want := []string{
		"Supermarket · Onions · 3 pieces (~450 g)",
		"Supermarket · Spinach · 400 g",
		"Supermarket · Spinach · 100 g · top-up",
		"Supermarket · Chickpeas, canned · 1 × 400 g",
		"Supermarket · Soy milk · 2 × 1 l",
		"Bulk store · Lentils · 200 g",
	}
	if got := shoppingLines(list); !slices.Equal(got, want) {
		t.Errorf("got items\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	onionItem := itemNamed(t, list, "Onions", false)
	if onionItem.GetGrams() != 450 || onionItem.GetNeededGrams() != 450 || onionItem.GetPantryGrams() != 300 ||
		onionItem.GetNeededByDate() != monday || onionItem.GetStoreId() != supermarket.GetId() ||
		onionItem.GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_PRODUCE || onionItem.GetFoodId() != onions.GetId() {
		t.Errorf("got %v, want 450 g of onions needed on Monday", onionItem)
	}
	topUp := itemNamed(t, list, "Spinach", true)
	if topUp.GetNeededByDate() != friday || !strings.HasPrefix(topUp.GetWarning(), "Won't keep until Fri, Nov 6, as it keeps 3 days") {
		t.Errorf("got %v, want spinach to top up for Friday", topUp)
	}
	milk := itemNamed(t, list, "Soy milk", false)
	if milk.GetPackages() != 2 || milk.GetGrams() != 2060 || milk.GetNeededGrams() != 1287.5 {
		t.Errorf("got %v, want 2 cartons for 1287.5 g", milk)
	}

	// Lists of other dates, and bad ones.
	for name, req := range map[string]*shoppingv1.CreateShoppingListRequest{
		"no start":       shoppingv1.CreateShoppingListRequest_builder{}.Build(),
		"end before":     shoppingv1.CreateShoppingListRequest_builder{StartDate: tuesday, EndDate: monday}.Build(),
		"over two weeks": shoppingv1.CreateShoppingListRequest_builder{StartDate: monday, EndDate: "2026-11-30"}.Build(),
	} {
		_, err := e.shopping.CreateShoppingList(ctx, req)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want InvalidArgument", name, err)
		}
	}

	// Another household can't see or change the list.
	_, err = e.otherShopping.GetShoppingList(ctx, shoppingv1.GetShoppingListRequest_builder{Id: list.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherShopping.CheckItem(ctx, shoppingv1.CheckItemRequest_builder{Id: onionItem.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherShopping.DeleteItem(ctx, shoppingv1.DeleteItemRequest_builder{Id: onionItem.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherShopping.CompleteShopping(ctx, shoppingv1.CompleteShoppingRequest_builder{Id: list.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherShopping.AddManualItem(ctx, shoppingv1.AddManualItemRequest_builder{ListId: list.GetId(), Text: new("Soap")}.Build())
	wantCode(t, err, connect.CodeNotFound)
	if lists, err := e.otherShopping.ListShoppingLists(ctx, shoppingv1.ListShoppingListsRequest_builder{}.Build()); err != nil || len(lists.GetLists()) != 0 {
		t.Errorf("got %v, %v, want the other household to have no lists", lists, err)
	}
	_, err = e.anonShopping.ListStores(ctx, shoppingv1.ListStoresRequest_builder{}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)

	// Check off the chickpeas, twice, and the onions, then uncheck the
	// onions.
	for range 2 {
		checked, err := e.shopping.CheckItem(ctx, shoppingv1.CheckItemRequest_builder{Id: itemNamed(t, list, "Chickpeas, canned", false).GetId()}.Build())
		if err != nil {
			t.Fatalf("CheckItem: %v", err)
		}
		if it := checked.GetItem(); !it.GetChecked() || it.GetCheckedBy() != "Ben" || it.GetStoreName() != "Supermarket" || it.GetAmountText() != "1 × 400 g" {
			t.Errorf("got %v, want the chickpeas checked by Ben", it)
		}
	}
	if _, err := e.shopping.CheckItem(ctx, shoppingv1.CheckItemRequest_builder{Id: onionItem.GetId()}.Build()); err != nil {
		t.Fatalf("CheckItem: %v", err)
	}
	unchecked, err := e.shopping.UncheckItem(ctx, shoppingv1.UncheckItemRequest_builder{Id: onionItem.GetId()}.Build())
	if err != nil {
		t.Fatalf("UncheckItem: %v", err)
	}
	if it := unchecked.GetItem(); it.GetChecked() || it.GetCheckedBy() != "" {
		t.Errorf("got %v, want the onions unchecked", it)
	}

	// Add dish soap and salt by hand, salt at the bulk store.
	soap, err := e.shopping.AddManualItem(ctx, shoppingv1.AddManualItemRequest_builder{ListId: list.GetId(), Text: new("Dish soap"), AmountText: "1 bottle"}.Build())
	if err != nil {
		t.Fatalf("AddManualItem: %v", err)
	}
	if it := soap.GetItem(); !it.GetManual() || it.GetName() != "Dish soap" || it.GetStoreName() != "Supermarket" ||
		it.GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_OTHER || it.GetAmountText() != "1 bottle" {
		t.Errorf("got %v, want dish soap at the supermarket", it)
	}
	if _, err := e.shopping.AddManualItem(ctx, shoppingv1.AddManualItemRequest_builder{
		ListId: list.GetId(), FoodId: new(salt.GetId()), Grams: 1000, StoreId: bulk.GetId(), Note: "coarse",
	}.Build()); err != nil {
		t.Fatalf("AddManualItem: %v", err)
	}
	for name, req := range map[string]*shoppingv1.AddManualItemRequest{
		"nothing":     shoppingv1.AddManualItemRequest_builder{ListId: list.GetId()}.Build(),
		"empty text":  shoppingv1.AddManualItemRequest_builder{ListId: list.GetId(), Text: new(" ")}.Build(),
		"other store": shoppingv1.AddManualItemRequest_builder{ListId: list.GetId(), Text: new("Soap"), StoreId: otherStore.GetStore().GetId()}.Build(),
		"bad grams":   shoppingv1.AddManualItemRequest_builder{ListId: list.GetId(), Text: new("Soap"), Grams: -1}.Build(),
	} {
		_, err := e.shopping.AddManualItem(ctx, req)
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want InvalidArgument", name, err)
		}
	}

	// The curry is cooked for 12 instead: regenerating needs another can of
	// chickpeas, and keeps the check and the items added by hand.
	if _, err := e.plans.UpdateCook(ctx, plansv1.UpdateCookRequest_builder{
		Cook:       plansv1.Cook_builder{Id: curryCook.GetId(), Servings: 12}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"servings"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateCook: %v", err)
	}
	regenerated, err := e.shopping.RegenerateShoppingList(ctx, shoppingv1.RegenerateShoppingListRequest_builder{Id: list.GetId()}.Build())
	if err != nil {
		t.Fatalf("RegenerateShoppingList: %v", err)
	}
	list = regenerated.GetList()
	want = []string{
		"Supermarket · Onions · 5 pieces (~750 g)",
		"Supermarket · Spinach · 600 g",
		"Supermarket · Spinach · 100 g · top-up",
		"Supermarket · Chickpeas, canned · 2 × 400 g · checked by Ben",
		"Supermarket · Soy milk · 2 × 1 l",
		"Supermarket · Dish soap · 1 bottle",
		"Bulk store · Lentils · 200 g",
		"Bulk store · Salt · 1 kg",
	}
	if got := shoppingLines(list); !slices.Equal(got, want) {
		t.Errorf("got items\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if list.GetCheckedCount() != 1 || itemNamed(t, list, "Onions", false).GetId() != onionItem.GetId() ||
		itemNamed(t, list, "Salt", false).GetNote() != "coarse" {
		t.Errorf("got %v, want the items to keep their IDs and notes", list)
	}

	// Deleting an item; a generated one comes back when regenerating.
	if _, err := e.shopping.DeleteItem(ctx, shoppingv1.DeleteItemRequest_builder{Id: soap.GetItem().GetId()}.Build()); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	_, err = e.shopping.DeleteItem(ctx, shoppingv1.DeleteItemRequest_builder{Id: soap.GetItem().GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)

	// Check off the soy milk and the salt too, and complete the shopping:
	// the chickpeas and soy milk go into the pantry, expiring after their
	// shelf life from Monday; the salt is always in stock.
	for _, name := range []string{"Soy milk", "Salt"} {
		if _, err := e.shopping.CheckItem(ctx, shoppingv1.CheckItemRequest_builder{Id: itemNamed(t, list, name, false).GetId()}.Build()); err != nil {
			t.Fatalf("CheckItem: %v", err)
		}
	}
	completed, err := e.shopping.CompleteShopping(ctx, shoppingv1.CompleteShoppingRequest_builder{Id: list.GetId(), Date: monday}.Build())
	if err != nil {
		t.Fatalf("CompleteShopping: %v", err)
	}
	if l := completed.GetList(); !l.GetComplete() || !l.HasCompleteTime() || l.GetCheckedCount() != 3 {
		t.Errorf("got %v, want the list complete", l)
	}
	var added []string
	for _, it := range completed.GetPantryItems() {
		added = append(added, it.GetFoodName()+" "+formatGrams(it.GetGrams())+" g until "+it.GetExpiryDate())
	}
	if want := []string{"Chickpeas, canned 800 g until 2028-11-01", "Soy milk 2060 g until 2027-01-01"}; !slices.Equal(added, want) {
		t.Errorf("got %v added to the pantry, want %v", added, want)
	}
	if got, want := stock(listPantry(t, e)), []string{
		"Chickpeas, canned 400 g until 2027-01-01",
		"Chickpeas, canned 800 g until 2028-11-01",
		"Onions 300 g until 2026-11-30",
		"Soy milk 515 g until 2026-12-01",
		"Soy milk 2060 g until 2027-01-01",
		"Spinach 100 g until 2026-11-01",
	}; !slices.Equal(got, want) {
		t.Errorf("got pantry %v, want %v", got, want)
	}
	// A complete list can't be completed again, nor regenerated.
	_, err = e.shopping.CompleteShopping(ctx, shoppingv1.CompleteShoppingRequest_builder{Id: list.GetId()}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)
	_, err = e.shopping.RegenerateShoppingList(ctx, shoppingv1.RegenerateShoppingListRequest_builder{Id: list.GetId()}.Build())
	wantCode(t, err, connect.CodeFailedPrecondition)

	// The next list, shopping on Saturday for a week, uses what was bought
	// and the curry's leftovers aren't bought again.
	next, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: saturday, EndDate: "2026-11-09"}.Build())
	if err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}
	if got := shoppingLines(next.GetList()); len(got) != 0 {
		t.Errorf("got %v, want nothing to buy, as the soy milk is in stock", got)
	}
	lists, err := e.shopping.ListShoppingLists(ctx, shoppingv1.ListShoppingListsRequest_builder{PageSize: 1}.Build())
	if err != nil {
		t.Fatalf("ListShoppingLists: %v", err)
	}
	if got := lists.GetLists(); len(got) != 1 || got[0].GetId() != next.GetList().GetId() || len(got[0].GetItems()) != 0 || lists.GetNextPageToken() == "" {
		t.Errorf("got %v, want the latest list, and a next page", lists)
	}
	lists, err = e.shopping.ListShoppingLists(ctx, shoppingv1.ListShoppingListsRequest_builder{PageSize: 1, PageToken: lists.GetNextPageToken()}.Build())
	if err != nil {
		t.Fatalf("ListShoppingLists: %v", err)
	}
	if got := lists.GetLists(); len(got) != 1 || got[0].GetId() != list.GetId() || got[0].GetItemCount() != 7 ||
		got[0].GetCheckedCount() != 3 || got[0].GetTopUpCount() != 1 || !got[0].GetComplete() || lists.GetNextPageToken() != "" {
		t.Errorf("got %v, want the first list, last", lists)
	}

	// Moving the bulk store first makes it the main store; deleting it moves
	// the lentils, and the items bought there, to the supermarket.
	moved, err := e.shopping.UpdateStore(ctx, shoppingv1.UpdateStoreRequest_builder{
		Store:      shoppingv1.Store_builder{Id: supermarket.GetId(), Position: 2}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"position"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateStore: %v", err)
	}
	if moved.GetStore().GetName() != "Supermarket" || moved.GetStore().GetPosition() != 2 {
		t.Errorf("got %v, want the supermarket at 2", moved)
	}
	stores, err := e.shopping.ListStores(ctx, shoppingv1.ListStoresRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListStores: %v", err)
	}
	if got := stores.GetStores(); len(got) != 2 || got[0].GetId() != bulk.GetId() {
		t.Errorf("got %v, want the bulk store first", got)
	}
	if _, err := e.shopping.DeleteStore(ctx, shoppingv1.DeleteStoreRequest_builder{Id: bulk.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteStore: %v", err)
	}
	food, err := e.foods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Id: lentils.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetFood: %v", err)
	}
	if food.GetFood().GetPreferredStoreId() != "" {
		t.Errorf("got %v, want the lentils from the main store", food.GetFood())
	}
	got, err := e.shopping.GetShoppingList(ctx, shoppingv1.GetShoppingListRequest_builder{Id: list.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetShoppingList: %v", err)
	}
	for _, it := range got.GetList().GetItems() {
		if it.GetStoreName() != "Supermarket" {
			t.Errorf("got %v, want it at the supermarket", it)
		}
	}
	_, err = e.otherShopping.DeleteStore(ctx, shoppingv1.DeleteStoreRequest_builder{Id: supermarket.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)

	// Deleting the list.
	if _, err := e.shopping.DeleteShoppingList(ctx, shoppingv1.DeleteShoppingListRequest_builder{Id: list.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteShoppingList: %v", err)
	}
	_, err = e.shopping.GetShoppingList(ctx, shoppingv1.GetShoppingListRequest_builder{Id: list.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
}

// TestShoppingSkipsDoneCooksAndUsesStaples checks that a list leaves out
// cooks already done, and adds up the scaled ingredients of recipe staples.
func TestShoppingSkipsDoneCooksAndUsesStaples(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	ben := createPerson(t, e, ben())
	oats := shopFood(t, e, "Oats", func(f *foodsv1.Food) { f.SetPackageGrams(500) })
	berries := shopFood(t, e, "Berries", nil)
	porridge, err := e.recipes.CreateRecipe(ctx, recipesv1.CreateRecipeRequest_builder{
		Recipe: recipesv1.Recipe_builder{
			Name:        "Porridge",
			Servings:    2,
			Ingredients: []*recipesv1.Ingredient{ingredient(oats.GetId(), 120, grams), ingredient(berries.GetId(), 100, grams)},
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	// Half the recipe on weekdays as a staple, skipped on Wednesday, and a
	// cook of 4 servings on Saturday, already done.
	staple, err := e.plans.CreateStaple(ctx, plansv1.CreateStapleRequest_builder{
		Staple: plansv1.Staple_builder{
			PersonId:     ben.GetId(),
			EatingTimeId: eatingTimeID(t, ben, "Breakfast"),
			Recipe:       plansv1.RecipeServings_builder{RecipeId: porridge.GetRecipe().GetId(), Servings: 1}.Build(),
			Weekdays: []plansv1.Weekday{
				plansv1.Weekday_WEEKDAY_MONDAY, plansv1.Weekday_WEEKDAY_TUESDAY, plansv1.Weekday_WEEKDAY_WEDNESDAY,
				plansv1.Weekday_WEEKDAY_THURSDAY, plansv1.Weekday_WEEKDAY_FRIDAY,
			},
		}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("CreateStaple: %v", err)
	}
	if _, err := e.plans.SkipStaple(ctx, plansv1.SkipStapleRequest_builder{Id: staple.GetStaple().GetId(), Date: wednesday}.Build()); err != nil {
		t.Fatalf("SkipStaple: %v", err)
	}
	if _, err := e.plans.CreateCook(ctx, plansv1.CreateCookRequest_builder{
		Cook: plansv1.Cook_builder{RecipeId: porridge.GetRecipe().GetId(), Date: saturday, Servings: 4, Done: true}.Build(),
	}.Build()); err != nil {
		t.Fatalf("CreateCook: %v", err)
	}
	created, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: monday}.Build())
	if err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}
	// 4 days of 60 g of oats and 50 g of berries, without a store.
	want := []string{" · Berries · 200 g", " · Oats · 1 × 500 g"}
	if got := shoppingLines(created.GetList()); !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
