package food

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/mealplann.ing/internal/foodimport"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Limits of a food's fields.
const (
	maxNameLength = 200
	// maxPriceCentsPerKilogram is $10,000/kg, more than saffron.
	maxPriceCentsPerKilogram = 1_000_000
	maxNotesLength           = 2000
	// maxGramsPerPiece is the most a piece can weigh, like a large pumpkin
	// or a whole block of something.
	maxGramsPerPiece = 20_000
	// maxGramsPerMilliliter is more than any food's density, which is about
	// 1.5 for syrups and 2.2 for salt.
	maxGramsPerMilliliter = 5
	// maxPackageGrams is the largest pack, like a 25 kg sack of rice.
	maxPackageGrams = 50_000
	// maxShelfLifeDays is 10 years, longer than any food keeps.
	maxShelfLifeDays = 3650
)

// nutrientFields are the fields of the Nutrients message, by nutrient,
// which are named after them.
var nutrientFields = func() [nutrition.Count]protoreflect.FieldDescriptor {
	var fds [nutrition.Count]protoreflect.FieldDescriptor
	fields := (&foodsv1.Nutrients{}).ProtoReflect().Descriptor().Fields()
	for n := range nutrition.Count {
		fd := fields.ByName(protoreflect.Name(n.String()))
		if fd == nil {
			panic(fmt.Sprintf("foods.v1.Nutrients has no field %s", n))
		}
		fds[n] = fd
	}
	return fds
}()

// NutrientToProto converts a nutrient to the API's Nutrient, whose values
// are in the same order, from 1.
func NutrientToProto(n nutrition.Nutrient) foodsv1.Nutrient {
	return foodsv1.Nutrient(n + 1)
}

// NutrientFromProto converts the API's Nutrient, returning false if it's
// unspecified or unknown.
func NutrientFromProto(n foodsv1.Nutrient) (nutrition.Nutrient, bool) {
	if n <= foodsv1.Nutrient_NUTRIENT_UNSPECIFIED || int(n) > int(nutrition.Count) {
		return 0, false
	}
	return nutrition.Nutrient(n - 1), true
}

// maxPer100Grams is the most of a nutrient in 100 g of food: all of it for
// a nutrient in grams, and the energy of pure fat.
func maxPer100Grams(n nutrition.Nutrient) float64 {
	name := n.String()
	switch {
	case n == nutrition.EnergyKcal:
		return 900
	case strings.HasSuffix(name, "_milligrams"):
		return 100_000
	case strings.HasSuffix(name, "_micrograms"):
		return 100_000_000
	default:
		return 100
	}
}

// NutrientsToProto converts nutrients, rounded to digits decimals if
// digits isn't negative. Rounding computed values keeps float noise, like
// 12.000000000000002, from agents and users.
func NutrientsToProto(n *nutrition.Nutrients, digits int) *foodsv1.Nutrients {
	msg := &foodsv1.Nutrients{}
	m := msg.ProtoReflect()
	scale := math.Pow10(digits)
	for i, v := range n {
		if v == nil {
			continue
		}
		value := *v
		if digits >= 0 {
			value = math.Round(value*scale) / scale
		}
		m.Set(nutrientFields[i], protoreflect.ValueOfFloat64(value))
	}
	return msg
}

// nutrientsFromProto converts and validates nutrients per 100 g, naming
// them after field in errors.
func nutrientsFromProto(msg *foodsv1.Nutrients, field string) (nutrition.Nutrients, error) {
	var n nutrition.Nutrients
	if msg == nil {
		return n, nil
	}
	m := msg.ProtoReflect()
	for i, fd := range nutrientFields {
		if !m.Has(fd) {
			continue
		}
		v := m.Get(fd).Float()
		nutrient := nutrition.Nutrient(i)
		if most := maxPer100Grams(nutrient); math.IsNaN(v) || v < 0 || v > most {
			return n, connect.Errorf(connect.CodeInvalidArgument,
				"%s.%s must be from 0 to %v, the amount in 100 g of the food; leave it unset if unknown", field, nutrient, most)
		}
		n[i] = new(v)
	}
	return n, nil
}

var storeSectionsToProto = map[foods.StoreSection]foodsv1.StoreSection{
	foods.StoreSectionUnspecified: foodsv1.StoreSection_STORE_SECTION_UNSPECIFIED,
	foods.StoreSectionProduce:     foodsv1.StoreSection_STORE_SECTION_PRODUCE,
	foods.StoreSectionBakery:      foodsv1.StoreSection_STORE_SECTION_BAKERY,
	foods.StoreSectionChilled:     foodsv1.StoreSection_STORE_SECTION_CHILLED,
	foods.StoreSectionFrozen:      foodsv1.StoreSection_STORE_SECTION_FROZEN,
	foods.StoreSectionDryGoods:    foodsv1.StoreSection_STORE_SECTION_DRY_GOODS,
	foods.StoreSectionCanned:      foodsv1.StoreSection_STORE_SECTION_CANNED,
	foods.StoreSectionSpices:      foodsv1.StoreSection_STORE_SECTION_SPICES,
	foods.StoreSectionDrinks:      foodsv1.StoreSection_STORE_SECTION_DRINKS,
	foods.StoreSectionHousehold:   foodsv1.StoreSection_STORE_SECTION_HOUSEHOLD,
	foods.StoreSectionOther:       foodsv1.StoreSection_STORE_SECTION_OTHER,
}

// StoreSectionToProto converts a store section.
func StoreSectionToProto(s foods.StoreSection) foodsv1.StoreSection {
	return storeSectionsToProto[s]
}

var storeSectionsFromProto = func() map[foodsv1.StoreSection]foods.StoreSection {
	m := map[foodsv1.StoreSection]foods.StoreSection{}
	for k, v := range storeSectionsToProto {
		m[v] = k
	}
	return m
}()

// sourcesToProto are the API's sources of the reference foods' sources.
var sourcesToProto = map[foods.Source]foodsv1.FoodSource{
	foods.SourceFDC: foodsv1.FoodSource_FOOD_SOURCE_FDC,
	foods.SourceCNF: foodsv1.FoodSource_FOOD_SOURCE_CNF,
}

var sourcesFromProto = func() map[foodsv1.FoodSource]foods.Source {
	m := map[foodsv1.FoodSource]foods.Source{}
	for k, v := range sourcesToProto {
		m[v] = k
	}
	return m
}()

// sourceNames name the sources in errors.
var sourceNames = map[foods.Source]string{
	foods.SourceFDC: "FoodData Central",
	foods.SourceCNF: "Canadian Nutrient File",
}

// fdcID returns a reference food's FDC ID, 0 unless it's from FDC.
func fdcID(source foods.Source, id int32) int32 {
	if source != foods.SourceFDC {
		return 0
	}
	return id
}

// referenceFromProto returns the reference food a CreateFoodRequest creates
// its food from, SourceNone for a custom food. fdc_id is the same as source
// FDC with its source_id, which it predates.
func referenceFromProto(req *foodsv1.CreateFoodRequest) (foods.Source, int32, error) {
	protoSource, id, legacyID := req.GetSource(), req.GetSourceId(), req.GetFdcId()
	if legacyID < 0 || id < 0 {
		return foods.SourceNone, 0, connect.NewError(connect.CodeInvalidArgument,
			"source_id and fdc_id can't be negative; pass the source_id of a food found with SearchFoodData")
	}
	if legacyID != 0 {
		switch {
		case protoSource == foodsv1.FoodSource_FOOD_SOURCE_UNSPECIFIED && id == 0,
			protoSource == foodsv1.FoodSource_FOOD_SOURCE_FDC && (id == 0 || id == legacyID):
			return foods.SourceFDC, legacyID, nil
		}
		return foods.SourceNone, 0, connect.NewError(connect.CodeInvalidArgument,
			"fdc_id is another way to pass source FOOD_SOURCE_FDC and its source_id; pass either fdc_id or source and source_id, not both")
	}
	switch protoSource {
	case foodsv1.FoodSource_FOOD_SOURCE_UNSPECIFIED, foodsv1.FoodSource_FOOD_SOURCE_CUSTOM:
		if id != 0 {
			return foods.SourceNone, 0, connect.NewError(connect.CodeInvalidArgument,
				"source_id needs its source; pass the source and source_id of a food found with SearchFoodData")
		}
		return foods.SourceNone, 0, nil
	}
	source, ok := sourcesFromProto[protoSource]
	if !ok {
		return foods.SourceNone, 0, connect.Errorf(connect.CodeInvalidArgument,
			"invalid source %v; pass FOOD_SOURCE_CNF or FOOD_SOURCE_FDC with a source_id, or leave it unset for a custom food", protoSource)
	}
	if id == 0 {
		return foods.SourceNone, 0, connect.NewError(connect.CodeInvalidArgument,
			"source_id is required with source; pass the source_id of a food found with SearchFoodData")
	}
	return source, id, nil
}

func foodToProto(f *foods.Food) *foodsv1.Food {
	source, ok := sourcesToProto[f.Source]
	if !ok {
		source = foodsv1.FoodSource_FOOD_SOURCE_CUSTOM
	}
	return foodsv1.Food_builder{
		Id:                    store.UUIDToString(f.ID),
		Name:                  f.Name,
		Source:                source,
		SourceId:              f.SourceID,
		FdcId:                 fdcID(f.Source, f.SourceID),
		Nutrients:             NutrientsToProto(&f.Per100Grams, -1),
		GramsPerPiece:         f.GramsPerPiece,
		GramsPerMilliliter:    f.GramsPerMilliliter,
		Measure:               MeasuresToProto[f.Measure],
		FastCarb:              f.FastCarb,
		StoreSection:          storeSectionsToProto[f.StoreSection],
		PackageGrams:          f.PackageGrams,
		ShelfLifeDays:         f.ShelfLifeDays,
		OpenedShelfLifeDays:   f.OpenedShelfLifeDays,
		FreezerShelfLifeDays:  f.FreezerShelfLifeDays,
		PriceCentsPerKilogram: f.PriceCentsPerKilogram,
		PriceDate:             store.FormatDate(f.PriceDate),
		Notes:                 f.Notes,
		PreferredStoreId:      optionalID(f.PreferredStoreID),
		AlwaysInStock:         f.AlwaysInStock,
		CreateTime:            timestamppb.New(f.CreateTime),
		UpdateTime:            timestamppb.New(f.UpdateTime),
	}.Build()
}

// foodFromProto converts and validates a food. Its ID, reference food,
// household and times are left for the caller to set.
func foodFromProto(p *foodsv1.Food) (*foods.Food, error) {
	name := strings.TrimSpace(p.GetName())
	switch {
	case name == "":
		return nil, connect.NewError(connect.CodeInvalidArgument, `name is required: give the food a name, e.g. "Chickpeas, canned"`)
	case utf8.RuneCountInString(name) > maxNameLength:
		return nil, connect.Errorf(connect.CodeInvalidArgument, "name must be at most %d characters", maxNameLength)
	}
	notes := strings.TrimSpace(p.GetNotes())
	if utf8.RuneCountInString(notes) > maxNotesLength {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "notes must be at most %d characters", maxNotesLength)
	}
	var priceDate time.Time
	if d := strings.TrimSpace(p.GetPriceDate()); d != "" {
		parsed, err := store.ParseDate(d)
		if err != nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "price_date must be a date (YYYY-MM-DD), or empty if unknown: %v", err)
		}
		priceDate = parsed
	}
	per100, err := nutrientsFromProto(p.GetNutrients(), "nutrients")
	if err != nil {
		return nil, err
	}
	section, ok := storeSectionsFromProto[p.GetStoreSection()]
	if !ok {
		return nil, connect.Errorf(connect.CodeInvalidArgument, "invalid store_section %v", p.GetStoreSection())
	}
	for _, check := range []struct {
		field string
		value float64
		most  float64
	}{
		{"grams_per_piece", p.GetGramsPerPiece(), maxGramsPerPiece},
		{"grams_per_milliliter", p.GetGramsPerMilliliter(), maxGramsPerMilliliter},
		{"package_grams", p.GetPackageGrams(), maxPackageGrams},
		{"shelf_life_days", float64(p.GetShelfLifeDays()), maxShelfLifeDays},
		{"opened_shelf_life_days", float64(p.GetOpenedShelfLifeDays()), maxShelfLifeDays},
		{"freezer_shelf_life_days", float64(p.GetFreezerShelfLifeDays()), maxShelfLifeDays},
		{"price_cents_per_kilogram", float64(p.GetPriceCentsPerKilogram()), maxPriceCentsPerKilogram},
	} {
		if math.IsNaN(check.value) || check.value < 0 || check.value > check.most {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s must be from 0 to %v, with 0 for unknown", check.field, check.most)
		}
	}
	measure, ok := measuresFromProto[p.GetMeasure()]
	if !ok {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"invalid measure %v: use MEASURE_WEIGHT, MEASURE_VOLUME, MEASURE_PIECES, or MEASURE_UNSPECIFIED to choose by the amount", p.GetMeasure())
	}
	var storeID pgtype.UUID
	if s := strings.TrimSpace(p.GetPreferredStoreId()); s != "" {
		var err error
		storeID, err = store.StringToUUID(s)
		if err != nil {
			return nil, connect.Errorf(connect.CodeInvalidArgument,
				"invalid preferred_store_id: %v; find stores with ListStores, or leave it empty for the main store", err).WithCause(err)
		}
	}
	return &foods.Food{
		Name:                  name,
		Per100Grams:           per100,
		GramsPerPiece:         p.GetGramsPerPiece(),
		GramsPerMilliliter:    p.GetGramsPerMilliliter(),
		Measure:               measure,
		FastCarb:              p.GetFastCarb(),
		StoreSection:          section,
		PackageGrams:          p.GetPackageGrams(),
		ShelfLifeDays:         p.GetShelfLifeDays(),
		OpenedShelfLifeDays:   p.GetOpenedShelfLifeDays(),
		FreezerShelfLifeDays:  p.GetFreezerShelfLifeDays(),
		PriceCentsPerKilogram: p.GetPriceCentsPerKilogram(),
		PriceDate:             priceDate,
		Notes:                 notes,
		PreferredStoreID:      storeID,
		AlwaysInStock:         p.GetAlwaysInStock(),
	}, nil
}

// MeasuresToProto converts measures to the API.
var MeasuresToProto = map[foods.Measure]foodsv1.Measure{
	foods.MeasureAuto:   foodsv1.Measure_MEASURE_UNSPECIFIED,
	foods.MeasureWeight: foodsv1.Measure_MEASURE_WEIGHT,
	foods.MeasureVolume: foodsv1.Measure_MEASURE_VOLUME,
	foods.MeasurePieces: foodsv1.Measure_MEASURE_PIECES,
}

// measuresFromProto converts measures from the API.
var measuresFromProto = map[foodsv1.Measure]foods.Measure{
	foodsv1.Measure_MEASURE_UNSPECIFIED: foods.MeasureAuto,
	foodsv1.Measure_MEASURE_WEIGHT:      foods.MeasureWeight,
	foodsv1.Measure_MEASURE_VOLUME:      foods.MeasureVolume,
	foodsv1.Measure_MEASURE_PIECES:      foods.MeasurePieces,
}

// foodFromReference returns a food created from a reference food, with the
// fields set in the request's food overriding what's taken from it.
func foodFromReference(ref *foods.ReferenceFood, in *foodsv1.Food) (*foods.Food, error) {
	if in.HasNutrients() {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"the nutrients are copied from %s food %d, so leave nutrients unset; change them with UpdateFood afterwards if needed",
			sourceNames[ref.Source], ref.SourceID)
	}
	base := foodsv1.Food_builder{
		Name:               truncate(ref.Description, maxNameLength),
		GramsPerPiece:      ref.SuggestGramsPerPiece(),
		GramsPerMilliliter: ref.SuggestGramsPerMilliliter(),
		StoreSection:       storeSectionsToProto[suggestStoreSection(ref)],
	}.Build()
	// Proto3 scalars have no presence, so fields left at their zero value
	// keep what the reference food suggests.
	if name := strings.TrimSpace(in.GetName()); name != "" {
		base.SetName(name)
	}
	if v := in.GetGramsPerPiece(); v != 0 {
		base.SetGramsPerPiece(v)
	}
	if v := in.GetGramsPerMilliliter(); v != 0 {
		base.SetGramsPerMilliliter(v)
	}
	if v := in.GetStoreSection(); v != foodsv1.StoreSection_STORE_SECTION_UNSPECIFIED {
		base.SetStoreSection(v)
	}
	base.SetMeasure(in.GetMeasure())
	base.SetFastCarb(in.GetFastCarb())
	base.SetPackageGrams(in.GetPackageGrams())
	base.SetShelfLifeDays(in.GetShelfLifeDays())
	base.SetOpenedShelfLifeDays(in.GetOpenedShelfLifeDays())
	base.SetFreezerShelfLifeDays(in.GetFreezerShelfLifeDays())
	base.SetPriceCentsPerKilogram(in.GetPriceCentsPerKilogram())
	base.SetPriceDate(in.GetPriceDate())
	base.SetNotes(in.GetNotes())
	base.SetPreferredStoreId(in.GetPreferredStoreId())
	base.SetAlwaysInStock(in.GetAlwaysInStock())
	f, err := foodFromProto(base)
	if err != nil {
		return nil, err
	}
	f.Source = ref.Source
	f.SourceID = ref.SourceID
	f.Per100Grams = ref.Per100Grams
	return f, nil
}

// categorySections are the store sections of the reference food
// categories whose foods are mostly found in one, by their names
// normalized with foodimport.NormalizeCategory, which FDC's and CNF's
// variants share.
var categorySections = map[string]foods.StoreSection{
	// FDC's and CNF's "Vegetables and Vegetable Products".
	"vegetables vegetable": foods.StoreSectionProduce,
	// FDC's "Fruits and Fruit Juices" and CNF's "Fruits and fruit juices".
	"fruits fruit juices": foods.StoreSectionProduce,
	// FDC's and CNF's "Spices and Herbs".
	"spices herbs": foods.StoreSectionSpices,
	// FDC's and CNF's "Baked Products".
	"baked": foods.StoreSectionBakery,
	// FDC's and CNF's "Beverages".
	"beverages": foods.StoreSectionDrinks,
	// FDC's "Cereal Grains and Pasta" and CNF's "Cereals, Grains and Pasta".
	"cereal grains pasta":  foods.StoreSectionDryGoods,
	"cereals grains pasta": foods.StoreSectionDryGoods,
	// FDC's and CNF's "Breakfast Cereals".
	"breakfast cereals": foods.StoreSectionDryGoods,
	// FDC's "Nut and Seed Products" and CNF's "Nuts and Seeds".
	"nut seed":   foods.StoreSectionDryGoods,
	"nuts seeds": foods.StoreSectionDryGoods,
}

// suggestStoreSection suggests where a reference food is found in the
// store, from how it's sold, like canned or frozen, or its category. It's
// unspecified when the category is too mixed to tell, like legumes, which
// are dried, canned or chilled.
func suggestStoreSection(f *foods.ReferenceFood) foods.StoreSection {
	description := strings.ToLower(f.Description)
	switch {
	case strings.Contains(description, "canned"):
		return foods.StoreSectionCanned
	case strings.Contains(description, "frozen"):
		return foods.StoreSectionFrozen
	}
	return categorySections[foodimport.NormalizeCategory(f.Category)]
}

// optionalID returns the string of an ID, empty if it's invalid.
func optionalID(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return store.UUIDToString(id)
}

// truncate shortens s to at most n characters.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func foodDataToProto(m *foods.ReferenceMatch) *foodsv1.FoodData {
	portions := make([]*foodsv1.FoodDataPortion, 0, len(m.Portions))
	for _, p := range m.Portions {
		portions = append(portions, foodsv1.FoodDataPortion_builder{
			Description: p.Description,
			Grams:       p.Grams,
		}.Build())
	}
	return foodsv1.FoodData_builder{
		FdcId:             fdcID(m.Source, m.SourceID),
		Source:            sourcesToProto[m.Source],
		SourceId:          m.SourceID,
		Description:       m.Description,
		FrenchDescription: m.FrenchDescription,
		Category:          m.Category,
		DataType:          m.DataType,
		Nutrients:         NutrientsToProto(&m.Per100Grams, -1),
		Portions:          portions,
		FoodId:            store.UUIDToString(m.FoodID),
	}.Build()
}
