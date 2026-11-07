package food

import (
	"fmt"
	"math"
	"strings"
	"unicode/utf8"

	"connectrpc.com/connect/v2"
	"github.com/bs-tools/foodplann.ing/internal/foods"
	foodsv1 "github.com/bs-tools/foodplann.ing/internal/gen/foods/v1"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Limits of a food's fields.
const (
	maxNameLength  = 200
	maxNotesLength = 2000
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

var storeSectionsFromProto = func() map[foodsv1.StoreSection]foods.StoreSection {
	m := map[foodsv1.StoreSection]foods.StoreSection{}
	for k, v := range storeSectionsToProto {
		m[v] = k
	}
	return m
}()

func foodToProto(f *foods.Food) *foodsv1.Food {
	source := foodsv1.FoodSource_FOOD_SOURCE_CUSTOM
	if f.FDCID != 0 {
		source = foodsv1.FoodSource_FOOD_SOURCE_FDC
	}
	return foodsv1.Food_builder{
		Id:                  store.UUIDToString(f.ID),
		Name:                f.Name,
		Source:              source,
		FdcId:               f.FDCID,
		Nutrients:           NutrientsToProto(&f.Per100Grams, -1),
		GramsPerPiece:       f.GramsPerPiece,
		GramsPerMilliliter:  f.GramsPerMilliliter,
		FastCarb:            f.FastCarb,
		StoreSection:        storeSectionsToProto[f.StoreSection],
		PackageGrams:        f.PackageGrams,
		ShelfLifeDays:       f.ShelfLifeDays,
		OpenedShelfLifeDays: f.OpenedShelfLifeDays,
		Notes:               f.Notes,
		CreateTime:          timestamppb.New(f.CreateTime),
		UpdateTime:          timestamppb.New(f.UpdateTime),
	}.Build()
}

// foodFromProto converts and validates a food. Its ID, FDC ID, household
// and times are left for the caller to set.
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
	} {
		if math.IsNaN(check.value) || check.value < 0 || check.value > check.most {
			return nil, connect.Errorf(connect.CodeInvalidArgument, "%s must be from 0 to %v, with 0 for unknown", check.field, check.most)
		}
	}
	return &foods.Food{
		Name:                name,
		Per100Grams:         per100,
		GramsPerPiece:       p.GetGramsPerPiece(),
		GramsPerMilliliter:  p.GetGramsPerMilliliter(),
		FastCarb:            p.GetFastCarb(),
		StoreSection:        section,
		PackageGrams:        p.GetPackageGrams(),
		ShelfLifeDays:       p.GetShelfLifeDays(),
		OpenedShelfLifeDays: p.GetOpenedShelfLifeDays(),
		Notes:               notes,
	}, nil
}

// foodFromFDC returns a food created from an FDC food, with the fields set
// in the request's food overriding what's taken from FDC.
func foodFromFDC(fdcFood *foods.FDCFood, in *foodsv1.Food) (*foods.Food, error) {
	if in.HasNutrients() {
		return nil, connect.Errorf(connect.CodeInvalidArgument,
			"the nutrients are copied from FoodData Central food %d, so leave nutrients unset; change them with UpdateFood afterwards if needed",
			fdcFood.FDCID)
	}
	base := foodsv1.Food_builder{
		Name:               truncate(fdcFood.Description, maxNameLength),
		GramsPerPiece:      fdcFood.SuggestGramsPerPiece(),
		GramsPerMilliliter: fdcFood.SuggestGramsPerMilliliter(),
		StoreSection:       storeSectionsToProto[suggestStoreSection(fdcFood)],
	}.Build()
	// Proto3 scalars have no presence, so fields left at their zero value
	// keep what FDC suggests.
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
	base.SetFastCarb(in.GetFastCarb())
	base.SetPackageGrams(in.GetPackageGrams())
	base.SetShelfLifeDays(in.GetShelfLifeDays())
	base.SetOpenedShelfLifeDays(in.GetOpenedShelfLifeDays())
	base.SetNotes(in.GetNotes())
	f, err := foodFromProto(base)
	if err != nil {
		return nil, err
	}
	f.FDCID = fdcFood.FDCID
	f.Per100Grams = fdcFood.Per100Grams
	return f, nil
}

// categorySections are the store sections of the FDC food categories whose
// foods are mostly found in one.
var categorySections = map[string]foods.StoreSection{
	"vegetables and vegetable products": foods.StoreSectionProduce,
	"fruits and fruit juices":           foods.StoreSectionProduce,
	"spices and herbs":                  foods.StoreSectionSpices,
	"baked products":                    foods.StoreSectionBakery,
	"beverages":                         foods.StoreSectionDrinks,
	"cereal grains and pasta":           foods.StoreSectionDryGoods,
	"breakfast cereals":                 foods.StoreSectionDryGoods,
	"nut and seed products":             foods.StoreSectionDryGoods,
}

// suggestStoreSection suggests where an FDC food is found in the store,
// from how it's sold, like canned or frozen, or its category. It's
// unspecified when the category is too mixed to tell, like legumes, which
// are dried, canned or chilled.
func suggestStoreSection(f *foods.FDCFood) foods.StoreSection {
	description := strings.ToLower(f.Description)
	switch {
	case strings.Contains(description, "canned"):
		return foods.StoreSectionCanned
	case strings.Contains(description, "frozen"):
		return foods.StoreSectionFrozen
	}
	return categorySections[strings.ToLower(f.Category)]
}

// truncate shortens s to at most n characters.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func foodDataToProto(m *foods.FDCMatch) *foodsv1.FoodData {
	portions := make([]*foodsv1.FoodDataPortion, 0, len(m.Portions))
	for _, p := range m.Portions {
		portions = append(portions, foodsv1.FoodDataPortion_builder{
			Description: p.Description,
			Grams:       p.Grams,
		}.Build())
	}
	return foodsv1.FoodData_builder{
		FdcId:       m.FDCID,
		Description: m.Description,
		Category:    m.Category,
		DataType:    m.DataType,
		Nutrients:   NutrientsToProto(&m.Per100Grams, -1),
		Portions:    portions,
		FoodId:      store.UUIDToString(m.FoodID),
	}.Build()
}
