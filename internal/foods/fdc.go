package foods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/bs-tools/foodplann.ing/internal/foods/internal/gen"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// FDCFood is a food of USDA FoodData Central.
type FDCFood struct {
	FDCID int32
	// DataType is the dataset: "foundation_food" or "sr_legacy_food".
	DataType    string
	Description string
	Category    string
	// PublicationDate is when FDC published the food, zero if unknown.
	PublicationDate time.Time
	// Per100Grams are the nutrients in 100 g of the food, nil where unknown.
	Per100Grams nutrition.Nutrients
	Portions    []Portion
}

// Portion is a common portion of an FDC food, stored as JSON.
type Portion struct {
	// Description is the portion, e.g. "1 cup, chopped" or "1 medium".
	Description string `json:"description"`
	// Amount is how many of the portion's unit it is, e.g. 0.5 for "0.5
	// cup".
	Amount float64 `json:"amount"`
	// Grams is the portion's weight.
	Grams float64 `json:"grams"`
}

// FDCMatch is an FDC food found by a search.
type FDCMatch struct {
	*FDCFood
	// FoodID is the household's food created from it, invalid if none.
	FoodID pgtype.UUID
}

// GetFDCFood gets an FDC food. It returns store.ErrNotFound if it isn't
// imported.
func (r *Repository) GetFDCFood(ctx context.Context, fdcID int32) (*FDCFood, error) {
	row, err := r.querier.GetFDCFood(ctx, fdcID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get FDC food: %w", err)
	}
	return fdcFoodFromDB(row)
}

// SearchFDCFoods returns the limit FDC foods best matching query, with the
// household's foods created from them.
func (r *Repository) SearchFDCFoods(ctx context.Context, householdID pgtype.UUID, query string, limit int32) ([]*FDCMatch, error) {
	rows, err := r.querier.SearchFDCFoods(ctx, &gen.SearchFDCFoodsParams{
		HouseholdID: householdID,
		Query:       query,
		RowLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search FDC foods: %w", err)
	}
	matches := make([]*FDCMatch, 0, len(rows))
	for _, row := range rows {
		f, err := fdcFoodFromDB(&row.FDCFood)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &FDCMatch{FDCFood: f, FoodID: row.FoodID})
	}
	return matches, nil
}

// UpsertFDCFoods creates or replaces FDC foods, in a single round trip.
func (r *Repository) UpsertFDCFoods(ctx context.Context, foods []*FDCFood) error {
	var b store.Batch
	q := gen.New(&b)
	for _, f := range foods {
		portions, err := json.Marshal(f.Portions)
		if err != nil {
			return fmt.Errorf("failed to encode the portions of FDC food %d: %w", f.FDCID, err)
		}
		if f.Portions == nil {
			portions = []byte("[]")
		}
		n := &f.Per100Grams
		_ = q.UpsertFDCFood(ctx, &gen.UpsertFDCFoodParams{
			FDCID:                           f.FDCID,
			DataType:                        f.DataType,
			Description:                     f.Description,
			Category:                        f.Category,
			Portions:                        portions,
			PublicationDate:                 store.DateToPG(f.PublicationDate),
			EnergyKcalPer100Grams:           n[nutrition.EnergyKcal],
			ProteinGramsPer100Grams:         n[nutrition.ProteinGrams],
			CarbohydrateGramsPer100Grams:    n[nutrition.CarbohydrateGrams],
			SugarsGramsPer100Grams:          n[nutrition.SugarsGrams],
			FatGramsPer100Grams:             n[nutrition.FatGrams],
			FiberGramsPer100Grams:           n[nutrition.FiberGrams],
			VitaminB12MicrogramsPer100Grams: n[nutrition.VitaminB12Micrograms],
			IronMilligramsPer100Grams:       n[nutrition.IronMilligrams],
			CalciumMilligramsPer100Grams:    n[nutrition.CalciumMilligrams],
			VitaminDMicrogramsPer100Grams:   n[nutrition.VitaminDMicrograms],
			IodineMicrogramsPer100Grams:     n[nutrition.IodineMicrograms],
			ZincMilligramsPer100Grams:       n[nutrition.ZincMilligrams],
			Omega3ALAGramsPer100Grams:       n[nutrition.Omega3ALAGrams],
			FolateMicrogramsPer100Grams:     n[nutrition.FolateMicrograms],
			VitaminAMicrogramsPer100Grams:   n[nutrition.VitaminAMicrograms],
			VitaminEMilligramsPer100Grams:   n[nutrition.VitaminEMilligrams],
			VitaminKMicrogramsPer100Grams:   n[nutrition.VitaminKMicrograms],
			CopperMilligramsPer100Grams:     n[nutrition.CopperMilligrams],
			MagnesiumMilligramsPer100Grams:  n[nutrition.MagnesiumMilligrams],
			SodiumMilligramsPer100Grams:     n[nutrition.SodiumMilligrams],
			PotassiumMilligramsPer100Grams:  n[nutrition.PotassiumMilligrams],
		})
	}
	if err := b.Send(ctx, r.db); err != nil {
		return fmt.Errorf("failed to upsert FDC foods: %w", err)
	}
	return nil
}

func fdcFoodFromDB(row *gen.FDCFood) (*FDCFood, error) {
	var portions []Portion
	if err := json.Unmarshal(row.Portions, &portions); err != nil {
		return nil, fmt.Errorf("failed to decode the portions of FDC food %d: %w", row.FDCID, err)
	}
	return &FDCFood{
		FDCID:           row.FDCID,
		DataType:        row.DataType,
		Description:     row.Description,
		Category:        row.Category,
		PublicationDate: store.DateFromPG(row.PublicationDate),
		Per100Grams: nutrition.Nutrients{
			nutrition.EnergyKcal:           row.EnergyKcalPer100Grams,
			nutrition.ProteinGrams:         row.ProteinGramsPer100Grams,
			nutrition.CarbohydrateGrams:    row.CarbohydrateGramsPer100Grams,
			nutrition.SugarsGrams:          row.SugarsGramsPer100Grams,
			nutrition.FatGrams:             row.FatGramsPer100Grams,
			nutrition.FiberGrams:           row.FiberGramsPer100Grams,
			nutrition.VitaminB12Micrograms: row.VitaminB12MicrogramsPer100Grams,
			nutrition.IronMilligrams:       row.IronMilligramsPer100Grams,
			nutrition.CalciumMilligrams:    row.CalciumMilligramsPer100Grams,
			nutrition.VitaminDMicrograms:   row.VitaminDMicrogramsPer100Grams,
			nutrition.IodineMicrograms:     row.IodineMicrogramsPer100Grams,
			nutrition.ZincMilligrams:       row.ZincMilligramsPer100Grams,
			nutrition.Omega3ALAGrams:       row.Omega3ALAGramsPer100Grams,
			nutrition.FolateMicrograms:     row.FolateMicrogramsPer100Grams,
			nutrition.VitaminAMicrograms:   row.VitaminAMicrogramsPer100Grams,
			nutrition.VitaminEMilligrams:   row.VitaminEMilligramsPer100Grams,
			nutrition.VitaminKMicrograms:   row.VitaminKMicrogramsPer100Grams,
			nutrition.CopperMilligrams:     row.CopperMilligramsPer100Grams,
			nutrition.MagnesiumMilligrams:  row.MagnesiumMilligramsPer100Grams,
			nutrition.SodiumMilligrams:     row.SodiumMilligramsPer100Grams,
			nutrition.PotassiumMilligrams:  row.PotassiumMilligramsPer100Grams,
		},
		Portions: portions,
	}, nil
}

// notPieceWords are the words of portions that aren't a whole piece of the
// food, like volumes, weights, servings and packs, or parts too small to
// count in, like leaves.
var notPieceWords = map[string]bool{
	"cup": true, "cups": true, "tbsp": true, "tablespoon": true, "tablespoons": true,
	"tsp": true, "teaspoon": true, "teaspoons": true, "fl": true, "oz": true,
	"ounce": true, "ounces": true, "ml": true, "milliliter": true, "milliliters": true,
	"l": true, "liter": true, "liters": true, "quart": true, "pint": true, "gallon": true,
	"lb": true, "lbs": true, "pound": true, "g": true, "gram": true, "grams": true,
	"kg": true, "serving": true, "servings": true, "package": true, "container": true,
	"can": true, "slice": true, "slices": true, "bunch": true, "leaf": true,
	"leaves": true, "sprig": true, "sprigs": true, "nlea": true, "racc": true,
	"dash": true, "pinch": true, "cubic": true, "pat": true, "drop": true,
	"bottle": true, "jar": true, "bag": true, "box": true,
}

// milliliters are the volumes of the units of portions, by their words.
var milliliters = map[string]float64{
	"cup": 236.588, "cups": 236.588,
	"tbsp": 14.787, "tablespoon": 14.787, "tablespoons": 14.787,
	"tsp": 4.929, "teaspoon": 4.929, "teaspoons": 4.929,
	"ml": 1, "milliliter": 1, "milliliters": 1,
}

// portionWords returns the lowercase words of a portion's description.
func portionWords(p Portion) []string {
	return strings.FieldsFunc(strings.ToLower(p.Description), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
}

// SuggestGramsPerPiece suggests the weight of one piece of the food from
// its portions: preferably a medium one, else the first portion that's a
// whole piece rather than a volume or weight, like "1 large" or "1 clove".
// It returns 0 if there's none.
func (f *FDCFood) SuggestGramsPerPiece() float64 {
	var first float64
	for _, p := range f.Portions {
		if p.Amount <= 0 || p.Grams <= 0 {
			continue
		}
		words := portionWords(p)
		piece := true
		for _, w := range words {
			if notPieceWords[w] {
				piece = false
				break
			}
		}
		if !piece {
			continue
		}
		grams := round(p.Grams / p.Amount)
		for _, w := range words {
			if w == "medium" {
				return grams
			}
		}
		if first == 0 {
			first = grams
		}
	}
	return first
}

// SuggestGramsPerMilliliter suggests the density of the food from its
// portions measured by volume, preferring cups, the largest and so the
// most precise. It returns 0 if there's none.
func (f *FDCFood) SuggestGramsPerMilliliter() float64 {
	var best, bestMilliliters float64
	for _, p := range f.Portions {
		if p.Amount <= 0 || p.Grams <= 0 {
			continue
		}
		for _, w := range portionWords(p) {
			if ml, ok := milliliters[w]; ok && ml > bestMilliliters {
				best, bestMilliliters = round(p.Grams/(p.Amount*ml)), ml
				break
			}
		}
	}
	return best
}

// round rounds a suggested weight to 3 decimals, which is more precise than
// the portions it comes from.
func round(v float64) float64 {
	return math.Round(v*1000) / 1000
}
