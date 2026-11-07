package services_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect/v2"
	foodsv1 "github.com/bs-tools/foodplann.ing/internal/gen/foods/v1"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

// createFood creates a food, failing the test if it can't.
func createFood(t *testing.T, c interface {
	CreateFood(context.Context, *foodsv1.CreateFoodRequest) (*foodsv1.CreateFoodResponse, error)
}, req *foodsv1.CreateFoodRequest,
) *foodsv1.Food {
	t.Helper()
	resp, err := c.CreateFood(context.Background(), req)
	if err != nil {
		t.Fatalf("CreateFood: %v", err)
	}
	return resp.GetFood()
}

func foodNames(list []*foodsv1.Food) []string {
	var names []string
	for _, f := range list {
		names = append(names, f.GetName())
	}
	return names
}

func TestFoods(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.importFDC()
	ctx := context.Background()

	// Searching FoodData Central finds onions, with their nutrients and
	// portions.
	search, err := e.foods.SearchFoodData(ctx, foodsv1.SearchFoodDataRequest_builder{Query: "onions raw"}.Build())
	if err != nil {
		t.Fatalf("SearchFoodData: %v", err)
	}
	if len(search.GetFoods()) != 2 {
		t.Fatalf("got %d foods, want both onions", len(search.GetFoods()))
	}
	onionData := search.GetFoods()[0]
	if onionData.GetFdcId() != 170000 || onionData.GetDescription() != "Onions, raw" ||
		onionData.GetCategory() != "Vegetables and Vegetable Products" || onionData.GetDataType() != "sr_legacy_food" ||
		onionData.GetNutrientsPer_100Grams().GetEnergyKcal() != 40 || onionData.GetNutrientsPer_100Grams().HasIodineMicrograms() ||
		len(onionData.GetPortions()) != 4 || onionData.GetPortions()[1].GetGrams() != 150 || onionData.GetFoodId() != "" {
		t.Errorf("got %v, want the SR Legacy onions", onionData)
	}

	// Creating a food from FDC copies its nutrients, and suggests its weight
	// and store section.
	onion := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{
		FdcId: 170000,
		Food:  foodsv1.Food_builder{Name: " Onions "}.Build(),
	}.Build())
	if onion.GetName() != "Onions" || onion.GetSource() != foodsv1.FoodSource_FOOD_SOURCE_FDC || onion.GetFdcId() != 170000 ||
		onion.GetNutrientsPer_100Grams().GetEnergyKcal() != 40 || onion.GetNutrientsPer_100Grams().GetSugarsGrams() != 4.24 ||
		onion.GetNutrientsPer_100Grams().HasIodineMicrograms() || onion.GetGramsPerPiece() != 110 ||
		onion.GetGramsPerMilliliter() != 0.676 || onion.GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_PRODUCE ||
		onion.GetFastCarb() || onion.GetId() == "" || !onion.GetCreateTime().IsValid() {
		t.Errorf("got %v, want onions from FDC", onion)
	}
	// Without a name, the food is named after FDC, and fields given override
	// the suggestions.
	soyMilk := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{
		FdcId: 2257046,
		Food: foodsv1.Food_builder{
			StoreSection:        foodsv1.StoreSection_STORE_SECTION_DRINKS,
			GramsPerMilliliter:  1.03,
			PackageGrams:        1000,
			OpenedShelfLifeDays: 4,
		}.Build(),
	}.Build())
	if soyMilk.GetName() != "Soy milk, unsweetened, plain, shelf stable" || soyMilk.GetGramsPerMilliliter() != 1.03 ||
		soyMilk.GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_DRINKS || soyMilk.GetPackageGrams() != 1000 ||
		soyMilk.GetOpenedShelfLifeDays() != 4 || soyMilk.GetNutrientsPer_100Grams().GetEnergyKcal() != 32.3 {
		t.Errorf("got %v, want soy milk with the given fields", soyMilk)
	}
	// The search now points to the household's food.
	search, err = e.foods.SearchFoodData(ctx, foodsv1.SearchFoodDataRequest_builder{Query: "onion", PageSize: 1}.Build())
	if err != nil {
		t.Fatalf("SearchFoodData: %v", err)
	}
	if len(search.GetFoods()) != 1 || search.GetFoods()[0].GetFoodId() != onion.GetId() {
		t.Errorf("got %v, want the onions pointing to the household's food", search.GetFoods())
	}

	// Names are unique ignoring case.
	_, err = e.foods.CreateFood(ctx, foodsv1.CreateFoodRequest_builder{
		Food: foodsv1.Food_builder{Name: "ONIONS"}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeAlreadyExists)
	// FDC foods bring their own nutrients, and must exist.
	_, err = e.foods.CreateFood(ctx, foodsv1.CreateFoodRequest_builder{
		FdcId: 172420,
		Food:  foodsv1.Food_builder{NutrientsPer_100Grams: foodsv1.Nutrients_builder{ProteinGrams: new(25.0)}.Build()}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.foods.CreateFood(ctx, foodsv1.CreateFoodRequest_builder{FdcId: 999}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// A custom food, from a label.
	yogurt := createFood(t, e.foods, foodsv1.CreateFoodRequest_builder{
		Food: foodsv1.Food_builder{
			Name: "Soy yogurt, plain",
			NutrientsPer_100Grams: foodsv1.Nutrients_builder{
				EnergyKcal:           new(50.0),
				ProteinGrams:         new(4.0),
				CarbohydrateGrams:    new(2.1),
				SugarsGrams:          new(0.0),
				CalciumMilligrams:    new(120.0),
				VitaminB12Micrograms: new(0.38),
			}.Build(),
			StoreSection:        foodsv1.StoreSection_STORE_SECTION_CHILLED,
			PackageGrams:        500,
			ShelfLifeDays:       21,
			OpenedShelfLifeDays: 5,
			Notes:               "The one in the blue tub",
		}.Build(),
	}.Build())
	if yogurt.GetSource() != foodsv1.FoodSource_FOOD_SOURCE_CUSTOM || yogurt.GetFdcId() != 0 ||
		!yogurt.GetNutrientsPer_100Grams().HasSugarsGrams() || yogurt.GetNutrientsPer_100Grams().GetSugarsGrams() != 0 ||
		yogurt.GetNutrientsPer_100Grams().HasIronMilligrams() || yogurt.GetShelfLifeDays() != 21 || yogurt.GetNotes() != "The one in the blue tub" {
		t.Errorf("got %v, want the custom yogurt, with zero sugars and unknown iron", yogurt)
	}

	// Invalid foods are refused.
	for name, f := range map[string]*foodsv1.Food{
		"no name":           foodsv1.Food_builder{Name: "  "}.Build(),
		"long name":         foodsv1.Food_builder{Name: strings.Repeat("a", 201)}.Build(),
		"negative nutrient": foodsv1.Food_builder{Name: "x", NutrientsPer_100Grams: foodsv1.Nutrients_builder{ProteinGrams: new(-1.0)}.Build()}.Build(),
		"too much protein":  foodsv1.Food_builder{Name: "x", NutrientsPer_100Grams: foodsv1.Nutrients_builder{ProteinGrams: new(101.0)}.Build()}.Build(),
		"too much energy":   foodsv1.Food_builder{Name: "x", NutrientsPer_100Grams: foodsv1.Nutrients_builder{EnergyKcal: new(1000.0)}.Build()}.Build(),
		"too dense":         foodsv1.Food_builder{Name: "x", GramsPerMilliliter: 10}.Build(),
		"negative piece":    foodsv1.Food_builder{Name: "x", GramsPerPiece: -5}.Build(),
		"long shelf life":   foodsv1.Food_builder{Name: "x", ShelfLifeDays: 5000}.Build(),
		"unknown section":   foodsv1.Food_builder{Name: "x", StoreSection: 99}.Build(),
	} {
		_, err := e.foods.CreateFood(ctx, foodsv1.CreateFoodRequest_builder{Food: f}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("%s: got %v, want invalid argument", name, err)
		}
	}

	// Get a food.
	got, err := e.foods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Id: yogurt.GetId()}.Build())
	if err != nil {
		t.Fatalf("GetFood: %v", err)
	}
	if got.GetFood().GetName() != "Soy yogurt, plain" || got.GetFood().GetNutrientsPer_100Grams().GetVitaminB12Micrograms() != 0.38 {
		t.Errorf("got %v, want the yogurt", got.GetFood())
	}
	_, err = e.foods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Id: "nope"}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.foods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Id: store.UUIDToString(store.NewUUID())}.Build())
	wantCode(t, err, connect.CodeNotFound)

	// List foods by name, ignoring case, optionally by part of it, and in
	// pages.
	list, err := e.foods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFoods: %v", err)
	}
	if got, want := strings.Join(foodNames(list.GetFoods()), "|"), "Onions|Soy milk, unsweetened, plain, shelf stable|Soy yogurt, plain"; got != want || list.GetNextPageToken() != "" {
		t.Errorf("got foods %q (next page %q), want %q", got, list.GetNextPageToken(), want)
	}
	list, err = e.foods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{Query: "SOY"}.Build())
	if err != nil {
		t.Fatalf("ListFoods: %v", err)
	}
	if len(list.GetFoods()) != 2 {
		t.Errorf("got %v, want the soy foods", foodNames(list.GetFoods()))
	}
	var paged []string
	token := ""
	for range 5 {
		page, err := e.foods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{PageSize: 2, PageToken: token}.Build())
		if err != nil {
			t.Fatalf("ListFoods: %v", err)
		}
		paged = append(paged, foodNames(page.GetFoods())...)
		token = page.GetNextPageToken()
		if token == "" {
			break
		}
	}
	if len(paged) != 3 || paged[2] != "Soy yogurt, plain" {
		t.Errorf("got pages of %v, want all 3 foods", paged)
	}
	_, err = e.foods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{PageToken: "x"}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.foods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{PageSize: 201}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Update only the fields in the mask.
	time.Sleep(10 * time.Millisecond)
	updated, err := e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food: foodsv1.Food_builder{
			Id:            yogurt.GetId(),
			Name:          "Ignored",
			GramsPerPiece: 125,
			FastCarb:      true,
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"grams_per_piece", "fastCarb"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateFood: %v", err)
	}
	if f := updated.GetFood(); f.GetName() != "Soy yogurt, plain" || f.GetGramsPerPiece() != 125 || !f.GetFastCarb() ||
		f.GetNutrientsPer_100Grams().GetCalciumMilligrams() != 120 || !f.GetUpdateTime().AsTime().After(yogurt.GetUpdateTime().AsTime()) ||
		!f.GetCreateTime().AsTime().Equal(yogurt.GetCreateTime().AsTime()) {
		t.Errorf("got %v, want the weight and fast carb changed, and a later update time", f)
	}
	// One nutrient at a time.
	updated, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food: foodsv1.Food_builder{
			Id:                    yogurt.GetId(),
			NutrientsPer_100Grams: foodsv1.Nutrients_builder{IodineMicrograms: new(22.5)}.Build(),
		}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"nutrients_per_100_grams.iodine_micrograms", "nutrients_per_100_grams.calcium_milligrams"}},
	}.Build())
	if err != nil {
		t.Fatalf("UpdateFood: %v", err)
	}
	if n := updated.GetFood().GetNutrientsPer_100Grams(); n.GetIodineMicrograms() != 22.5 || n.HasCalciumMilligrams() || n.GetProteinGrams() != 4 {
		t.Errorf("got %v, want iodine set, calcium cleared and protein kept", n)
	}
	// Server fields and unknown paths can't be updated.
	for _, path := range []string{"id", "fdc_id", "source", "create_time", "nutrients_per_100_grams.vitamin_c_milligrams", "name.first"} {
		_, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
			Food:       foodsv1.Food_builder{Id: yogurt.GetId()}.Build(),
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{path}},
		}.Build())
		if connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("updating %s: got %v, want invalid argument", path, err)
		}
	}
	// Updated foods are validated, and their names unique.
	_, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: yogurt.GetId(), Name: "onions"}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"name"}},
	}.Build())
	wantCode(t, err, connect.CodeAlreadyExists)
	_, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food:       foodsv1.Food_builder{Id: yogurt.GetId(), PackageGrams: -1}.Build(),
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"package_grams"}},
	}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	// Without a mask, everything is replaced, keeping the FDC ID.
	updated, err = e.foods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food: foodsv1.Food_builder{Id: onion.GetId(), Name: "Yellow onions", GramsPerPiece: 150, FdcId: 1}.Build(),
	}.Build())
	if err != nil {
		t.Fatalf("UpdateFood: %v", err)
	}
	if f := updated.GetFood(); f.GetName() != "Yellow onions" || f.GetGramsPerPiece() != 150 || f.HasNutrientsPer_100Grams() && f.GetNutrientsPer_100Grams().HasEnergyKcal() ||
		f.GetStoreSection() != foodsv1.StoreSection_STORE_SECTION_UNSPECIFIED || f.GetFdcId() != 170000 {
		t.Errorf("got %v, want the onions replaced, from the same FDC food", f)
	}

	// Another household sees none of it, and can't change it.
	otherList, err := e.otherFoods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{}.Build())
	if err != nil {
		t.Fatalf("ListFoods of the other household: %v", err)
	}
	if len(otherList.GetFoods()) != 0 {
		t.Errorf("the other household sees %v", foodNames(otherList.GetFoods()))
	}
	_, err = e.otherFoods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Id: yogurt.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherFoods.UpdateFood(ctx, foodsv1.UpdateFoodRequest_builder{
		Food: foodsv1.Food_builder{Id: yogurt.GetId(), Name: "Mine"}.Build(),
	}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.otherFoods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: yogurt.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	otherSearch, err := e.otherFoods.SearchFoodData(ctx, foodsv1.SearchFoodDataRequest_builder{Query: "onion"}.Build())
	if err != nil {
		t.Fatalf("SearchFoodData of the other household: %v", err)
	}
	if otherSearch.GetFoods()[0].GetFoodId() != "" {
		t.Error("the other household's search points to this household's food")
	}
	// It can have a food of the same name.
	createFood(t, e.otherFoods, foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{Name: "Yellow onions"}.Build()}.Build())

	// Searches need a query, of a reasonable size.
	_, err = e.foods.SearchFoodData(ctx, foodsv1.SearchFoodDataRequest_builder{Query: " "}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)
	_, err = e.foods.SearchFoodData(ctx, foodsv1.SearchFoodDataRequest_builder{Query: "onion", PageSize: 51}.Build())
	wantCode(t, err, connect.CodeInvalidArgument)

	// Delete a food.
	if _, err := e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: yogurt.GetId()}.Build()); err != nil {
		t.Fatalf("DeleteFood: %v", err)
	}
	_, err = e.foods.GetFood(ctx, foodsv1.GetFoodRequest_builder{Id: yogurt.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)
	_, err = e.foods.DeleteFood(ctx, foodsv1.DeleteFoodRequest_builder{Id: yogurt.GetId()}.Build())
	wantCode(t, err, connect.CodeNotFound)

	// Signing in is required.
	_, err = e.anonFoods.ListFoods(ctx, foodsv1.ListFoodsRequest_builder{}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = e.anonFoods.SearchFoodData(ctx, foodsv1.SearchFoodDataRequest_builder{Query: "onion"}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
	_, err = e.anonFoods.CreateFood(ctx, foodsv1.CreateFoodRequest_builder{Food: foodsv1.Food_builder{Name: "x"}.Build()}.Build())
	wantCode(t, err, connect.CodeUnauthenticated)
}
