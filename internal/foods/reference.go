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

	"github.com/bs-tools/mealplann.ing/internal/foods/internal/gen"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// Source is a database of reference foods, which households create their
// foods from.
type Source string

// The sources, as stored. The zero value is no source, for custom foods.
const (
	SourceNone Source = ""
	// SourceFDC is USDA FoodData Central's Foundation Foods and SR Legacy.
	SourceFDC Source = "fdc"
	// SourceCNF is Health Canada's Canadian Nutrient File.
	SourceCNF Source = "cnf"
)

// ReferenceFood is a food of a reference database, FDC or CNF.
type ReferenceFood struct {
	Source Source
	// SourceID is the food's ID in its source: FDC's fdc_id or CNF's
	// FoodID.
	SourceID int32
	// DataType is the dataset: "foundation_food" or "sr_legacy_food" for
	// FDC, "cnf" for CNF.
	DataType    string
	Description string
	// FrenchDescription is the French description, empty if the source has
	// none, like FDC.
	FrenchDescription string
	// Category is the source's food category or group.
	Category string
	// PublicationDate is when the source published the food, zero if
	// unknown.
	PublicationDate time.Time
	// Per100Grams are the nutrients in 100 g of the food, nil where unknown.
	Per100Grams nutrition.Nutrients
	Portions    []Portion
}

// Portion is a common portion of a reference food, stored as JSON.
type Portion struct {
	// Description is the portion, e.g. "1 cup, chopped", "1 medium" or
	// "250ml".
	Description string `json:"description"`
	// Amount is how many of the portion's unit it is, e.g. 0.5 for "0.5
	// cup" or 250 for "250ml".
	Amount float64 `json:"amount"`
	// Grams is the portion's weight.
	Grams float64 `json:"grams"`
}

// ReferenceMatch is a reference food found by a search.
type ReferenceMatch struct {
	*ReferenceFood
	// FoodID is the household's food created from it, invalid if none.
	FoodID pgtype.UUID
}

// GetReferenceFood gets a reference food. It returns store.ErrNotFound if
// it isn't imported.
func (r *Repository) GetReferenceFood(ctx context.Context, source Source, id int32) (*ReferenceFood, error) {
	row, err := r.querier.GetReferenceFood(ctx, &gen.GetReferenceFoodParams{
		Source:   gen.ReferenceFoodSource(source),
		SourceID: id,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get reference food: %w", err)
	}
	return referenceFoodFromDB(row)
}

// SearchReferenceFoods returns the limit reference foods of all sources
// best matching query, with the household's foods created from them.
func (r *Repository) SearchReferenceFoods(ctx context.Context, householdID pgtype.UUID, query string, limit int32) ([]*ReferenceMatch, error) {
	rows, err := r.querier.SearchReferenceFoods(ctx, &gen.SearchReferenceFoodsParams{
		HouseholdID: householdID,
		Query:       query,
		RowLimit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to search reference foods: %w", err)
	}
	matches := make([]*ReferenceMatch, 0, len(rows))
	for _, row := range rows {
		f, err := referenceFoodFromDB(&row.ReferenceFood)
		if err != nil {
			return nil, err
		}
		matches = append(matches, &ReferenceMatch{ReferenceFood: f, FoodID: row.FoodID})
	}
	return matches, nil
}

// UpsertReferenceFoods creates or replaces reference foods, in a single
// round trip.
func (r *Repository) UpsertReferenceFoods(ctx context.Context, foods []*ReferenceFood) error {
	var b store.Batch
	q := gen.New(&b)
	for _, f := range foods {
		if f.Source != SourceFDC && f.Source != SourceCNF {
			return fmt.Errorf("reference food %d has an unknown source %q", f.SourceID, f.Source)
		}
		portions, err := json.Marshal(f.Portions)
		if err != nil {
			return fmt.Errorf("failed to encode the portions of %s food %d: %w", f.Source, f.SourceID, err)
		}
		if f.Portions == nil {
			portions = []byte("[]")
		}
		n := &f.Per100Grams
		_ = q.UpsertReferenceFood(ctx, &gen.UpsertReferenceFoodParams{
			Source:                          gen.ReferenceFoodSource(f.Source),
			SourceID:                        f.SourceID,
			DataType:                        f.DataType,
			Description:                     f.Description,
			FrenchDescription:               f.FrenchDescription,
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
		return fmt.Errorf("failed to upsert reference foods: %w", err)
	}
	return nil
}

func referenceFoodFromDB(row *gen.ReferenceFood) (*ReferenceFood, error) {
	var portions []Portion
	if err := json.Unmarshal(row.Portions, &portions); err != nil {
		return nil, fmt.Errorf("failed to decode the portions of %s food %d: %w", row.Source, row.SourceID, err)
	}
	return &ReferenceFood{
		Source:            Source(row.Source),
		SourceID:          row.SourceID,
		DataType:          row.DataType,
		Description:       row.Description,
		FrenchDescription: row.FrenchDescription,
		Category:          row.Category,
		PublicationDate:   store.DateFromPG(row.PublicationDate),
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
	"millilitre": true, "millilitres": true, "litre": true, "litres": true,
	"l": true, "liter": true, "liters": true, "quart": true, "pint": true, "gallon": true,
	"lb": true, "lbs": true, "pound": true, "g": true, "gram": true, "grams": true,
	"kg": true, "serving": true, "servings": true, "package": true, "container": true,
	"can": true, "slice": true, "slices": true, "bunch": true, "leaf": true,
	"leaves": true, "sprig": true, "sprigs": true, "nlea": true, "racc": true,
	"dash": true, "pinch": true, "cubic": true, "pat": true, "drop": true,
	"bottle": true, "jar": true, "bag": true, "box": true,
}

// milliliters are the volumes of the units of portions, by their words.
// FDC's are mostly US cups and spoons, CNF's metric, like "250ml".
var milliliters = map[string]float64{
	"cup": 236.588, "cups": 236.588,
	"tbsp": 14.787, "tablespoon": 14.787, "tablespoons": 14.787,
	"tsp": 4.929, "teaspoon": 4.929, "teaspoons": 4.929,
	"ml": 1, "milliliter": 1, "milliliters": 1, "millilitre": 1, "millilitres": 1,
	"l": 1000, "liter": 1000, "liters": 1000, "litre": 1000, "litres": 1000,
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
func (f *ReferenceFood) SuggestGramsPerPiece() float64 {
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
// portions measured by volume, preferring the largest, like "1 cup" over
// "1 tbsp" or "250ml" over "15ml", as the most precise. It returns 0 if
// there's none.
func (f *ReferenceFood) SuggestGramsPerMilliliter() float64 {
	var best, bestMilliliters float64
	for _, p := range f.Portions {
		if p.Amount <= 0 || p.Grams <= 0 {
			continue
		}
		for _, w := range portionWords(p) {
			ml, ok := milliliters[w]
			if !ok {
				continue
			}
			// The portion's unit is its first word that's a volume.
			if total := p.Amount * ml; total > bestMilliliters {
				best, bestMilliliters = round(p.Grams/total), total
			}
			break
		}
	}
	return best
}

// round rounds a suggested weight to 3 decimals, which is more precise than
// the portions it comes from.
func round(v float64) float64 {
	return math.Round(v*1000) / 1000
}
