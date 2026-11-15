package services_test

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	pantryv1 "github.com/bs-tools/mealplann.ing/internal/gen/pantry/v1"
	plansv1 "github.com/bs-tools/mealplann.ing/internal/gen/plans/v1"
	shoppingv1 "github.com/bs-tools/mealplann.ing/internal/gen/shopping/v1"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// TestKeepInStock checks foods kept in stock: that the amount must be
// weighable and isn't for foods always in stock; that a shopping list buys
// them once the pantry has less, whether or not the plan uses them; and
// that once something takes one below its amount, or it's set to be kept
// in stock with less in the pantry, the open list gets it.
func TestKeepInStock(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ctx := context.Background()
	b := createPerson(t, e, ben())

	// Kept in packages, it needs the package weight.
	_, err := e.foods.CreateFood(ctx, foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{
		Name: "Tahini", KeepInStockAmount: 1, KeepInStockUnit: foodsv1.AmountUnit_AMOUNT_UNIT_PACKAGES,
	}.Build()}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	// A jar of peanut butter, of 1 kg, kept in stock.
	peanutButter := customFood(t, e, "Peanut butter", 590, 25, 20, 9, 50, func(f *foodsv1.Food) {
		f.SetPackageGrams(1000)
		f.SetKeepInStockAmount(1)
		f.SetKeepInStockUnit(foodsv1.AmountUnit_AMOUNT_UNIT_PACKAGES)
	})
	if peanutButter.GetKeepInStockAmount() != 1 || peanutButter.GetKeepInStockUnit() != foodsv1.AmountUnit_AMOUNT_UNIT_PACKAGES {
		t.Errorf("got %v in stock, want a package", peanutButter)
	}
	// It can't also be always in stock, which isn't tracked.
	_, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: peanutButter.GetId(), AlwaysInStock: true}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"always_in_stock"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// The open list is this week's, as only a list whose dates aren't over
	// gets the foods running low.
	today := time.Now().UTC().Format(time.DateOnly)
	end := time.Now().UTC().AddDate(0, 0, 6).Format(time.DateOnly)
	restockItems := func(list *shoppingv1.ShoppingList) []*shoppingv1.ShoppingItem {
		t.Helper()
		var found []*shoppingv1.ShoppingItem
		for _, it := range list.GetItems() {
			if it.GetFoodId() == peanutButter.GetId() {
				found = append(found, it)
			}
		}
		return found
	}

	// With 1.2 kg, more than a jar, nothing's bought.
	lot := addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: peanutButter.GetId(), Amount: 1200, Unit: grams}.Build())
	created, err := e.shopping.CreateShoppingList(ctx, shoppingv1.CreateShoppingListRequest_builder{StartDate: today, EndDate: end}.Build())
	if err != nil {
		t.Fatalf("CreateShoppingList: %v", err)
	}
	listID := created.GetList().GetId()
	if got := restockItems(created.GetList()); len(got) != 0 {
		t.Fatalf("got %v with 1.2 kg in the pantry, want no peanut butter", got)
	}

	// Eating 300 g leaves 900 g, under a jar: the open list gets one.
	portion, err := e.plans.CreatePortion(ctx, plansv1.CreatePortionRequest_builder{Portion: plansv1.Portion_builder{
		PersonId: b.GetId(), EatingTimeId: eatingTimeID(t, b, "Breakfast"), Date: today,
		Food: plansv1.FoodAmount_builder{FoodId: peanutButter.GetId(), Amount: 300, Unit: grams}.Build(),
	}.Build()}.Build())
	if err != nil {
		t.Fatalf("CreatePortion: %v", err)
	}
	if _, err := e.plans.SetPortionEaten(ctx, plansv1.SetPortionEatenRequest_builder{Id: portion.GetPortion().GetId(), Eaten: true}.Build()); err != nil {
		t.Fatalf("SetPortionEaten: %v", err)
	}
	list, err := e.shopping.GetShoppingList(ctx, shoppingv1.GetShoppingListRequest_builder{Id: listID}.Build())
	if err != nil {
		t.Fatalf("GetShoppingList: %v", err)
	}
	got := restockItems(list.GetList())
	if len(got) != 1 || !got[0].GetRestock() || got[0].GetPackages() != 1 || got[0].GetNeededByDate() != today {
		t.Fatalf("got %v after eating 300 g, want a jar to restock, needed today", got)
	}

	// Checked, it stays checked when the pantry drops further, as it's on
	// the list already.
	if _, err := e.shopping.CheckItem(ctx, shoppingv1.CheckItemRequest_builder{Id: got[0].GetId()}.Build()); err != nil {
		t.Fatalf("CheckItem: %v", err)
	}
	if _, err := e.pantry.UpdatePantryItem(ctx, pantryv1.UpdatePantryItemRequest_builder{
		Item:       pantryv1.PantryItem_builder{Id: lot.GetId(), Grams: 100}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"grams"}},
	}.Build()); err != nil {
		t.Fatalf("UpdatePantryItem: %v", err)
	}
	list, err = e.shopping.GetShoppingList(ctx, shoppingv1.GetShoppingListRequest_builder{Id: listID}.Build())
	if err != nil {
		t.Fatalf("GetShoppingList: %v", err)
	}
	if got := restockItems(list.GetList()); len(got) != 1 || !got[0].GetChecked() {
		t.Errorf("got %v after taking more out, want the jar still checked", got)
	}

	// Removing the lot when the list doesn't have it adds it again.
	if _, err := e.shopping.DeleteItem(ctx, shoppingv1.DeleteItemRequest_builder{Id: got[0].GetId()}.Build()); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if _, err := e.pantry.RemoveFromPantry(ctx, pantryv1.RemoveFromPantryRequest_builder{Id: lot.GetId()}.Build()); err != nil {
		t.Fatalf("RemoveFromPantry: %v", err)
	}
	list, err = e.shopping.GetShoppingList(ctx, shoppingv1.GetShoppingListRequest_builder{Id: listID}.Build())
	if err != nil {
		t.Fatalf("GetShoppingList: %v", err)
	}
	if got := restockItems(list.GetList()); len(got) != 1 || !got[0].GetRestock() || got[0].GetPackages() != 1 {
		t.Errorf("got %v after removing the peanut butter, want a jar to restock", got)
	}

	// Setting a food to be kept in stock when the pantry has less puts it
	// on the open list right away.
	tahini := customFood(t, e, "Tahini", 600, 17, 20, 0.5, 54, func(f *foodsv1.Food) { f.SetPackageGrams(300) })
	addToPantry(t, e, pantryv1.AddToPantryRequest_builder{FoodId: tahini.GetId(), Amount: 100, Unit: grams}.Build())
	if _, err := e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food: foodsv1.Food_builder{
			Id: tahini.GetId(), KeepInStockAmount: 1, KeepInStockUnit: foodsv1.AmountUnit_AMOUNT_UNIT_PACKAGES,
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"keep_in_stock_amount", "keep_in_stock_unit"}},
	}.Build()); err != nil {
		t.Fatalf("UpdateFood: %v", err)
	}
	list, err = e.shopping.GetShoppingList(ctx, shoppingv1.GetShoppingListRequest_builder{Id: listID}.Build())
	if err != nil {
		t.Fatalf("GetShoppingList: %v", err)
	}
	found := false
	for _, it := range list.GetList().GetItems() {
		found = found || (it.GetFoodId() == tahini.GetId() && it.GetRestock())
	}
	if !found {
		t.Errorf("got items %v after keeping tahini in stock with 100 g left, want it to restock", list.GetList().GetItems())
	}
}
