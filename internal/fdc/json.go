package fdc

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/bs-tools/foodplann.ing/internal/foodimport"
	"github.com/bs-tools/foodplann.ing/internal/foods"
	"github.com/bs-tools/foodplann.ing/internal/store"
)

// jsonDataTypes are the data types of the foods imported, as the JSON
// downloads name them, and as the CSV downloads and the database do.
var jsonDataTypes = map[string]string{
	"Foundation": "foundation_food",
	"SR Legacy":  "sr_legacy_food",
}

// jsonFood is a food of a JSON download, with the fields the importer
// reads.
type jsonFood struct {
	FDCID int32 `json:"fdcId"`
	// DataType is "Foundation" or "SR Legacy" for the foods imported.
	DataType string `json:"dataType"`
	// FoodClass is "FinalFood" for foods to cook with. The samples a
	// Foundation food was analyzed from are nested in its inputFoods
	// rather than listed, but are skipped if they ever are.
	FoodClass   string `json:"foodClass"`
	Description string `json:"description"`
	// PublicationDate is M/D/YYYY, like "4/28/2022".
	PublicationDate string `json:"publicationDate"`
	FoodCategory    struct {
		Description string `json:"description"`
	} `json:"foodCategory"`
	FoodNutrients []struct {
		Nutrient struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			UnitName string `json:"unitName"`
		} `json:"nutrient"`
		// Amount is per 100 g, and nil when FDC has none.
		Amount *float64 `json:"amount"`
	} `json:"foodNutrients"`
	FoodPortions []jsonPortion `json:"foodPortions"`
}

// jsonPortion is a portion of a food of a JSON download.
type jsonPortion struct {
	// Amount is the number of the unit, e.g. 1 for "1 cup". Some portions
	// only have it as their value.
	Amount      *float64 `json:"amount"`
	Value       *float64 `json:"value"`
	MeasureUnit struct {
		Name string `json:"name"`
	} `json:"measureUnit"`
	PortionDescription string `json:"portionDescription"`
	Modifier           string `json:"modifier"`
	// GramWeight is the portion's weight in grams.
	GramWeight float64 `json:"gramWeight"`
	// SequenceNumber orders the portions.
	SequenceNumber int `json:"sequenceNumber"`
}

// jsonDateLayouts are the layouts of publication dates in the JSON
// downloads, which have been M/D/YYYY, and of the CSV downloads, in case a
// release switches.
var jsonDateLayouts = []string{"1/2/2006", store.DateLayout}

// readJSON reads the foods of a JSON download, the file at name in fsys:
// an object with an array of foods, FoundationFoods or SRLegacyFoods. It's
// read one food at a time, as SR Legacy's is a couple of hundred megabytes.
func readJSON(fsys fs.FS, name string) ([]*foods.ReferenceFood, Stats, error) {
	var stats Stats
	f, err := fsys.Open(name)
	if err != nil {
		return nil, stats, fmt.Errorf("failed to open %s: %w", name, err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, stats, fmt.Errorf("%s isn't an FDC JSON download: it doesn't start with an object", name)
	}
	// The foods read, by FDC ID: a food listed twice keeps its last entry, as
	// upserting both would.
	byID := map[int32]*foods.ReferenceFood{}
	foundFoods := false
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, stats, fmt.Errorf("failed to read %s: %w", name, err)
		}
		key, _ := tok.(string)
		// The foods are the array named after the dataset. Anything else is
		// skipped.
		if !strings.HasSuffix(key, "Foods") {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return nil, stats, fmt.Errorf("failed to read %s: %w", name, err)
			}
			continue
		}
		if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
			return nil, stats, fmt.Errorf("failed to read %s: %s isn't an array of foods", name, key)
		}
		foundFoods = true
		for dec.More() {
			var jf jsonFood
			if err := dec.Decode(&jf); err != nil {
				return nil, stats, fmt.Errorf("failed to read a food of %s: %w", name, err)
			}
			food, excluded, err := jf.food()
			if err != nil {
				return nil, stats, fmt.Errorf("%s, food %d: %w", name, jf.FDCID, err)
			}
			if excluded {
				stats.Excluded++
			}
			if food != nil {
				byID[food.SourceID] = food
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, stats, fmt.Errorf("failed to read %s: %w", name, err)
		}
	}
	if !foundFoods {
		return nil, stats, fmt.Errorf("%s has no foods; give the Foundation Foods or SR Legacy JSON download", name)
	}
	result := make([]*foods.ReferenceFood, 0, len(byID))
	for _, id := range slices.Sorted(maps.Keys(byID)) {
		result = append(result, byID[id])
	}
	stats.Foods = len(result)
	return result, stats, nil
}

// food converts a food of a JSON download, returning nil for foods that
// aren't imported, and whether it's of an excluded category.
func (jf *jsonFood) food() (*foods.ReferenceFood, bool, error) {
	dataType, ok := jsonDataTypes[jf.DataType]
	if !ok || (jf.FoodClass != "" && jf.FoodClass != "FinalFood") {
		return nil, false, nil
	}
	category := jf.FoodCategory.Description
	if foodimport.IsExcludedCategory(category) {
		return nil, true, nil
	}
	f := &foods.ReferenceFood{
		Source:      foods.SourceFDC,
		SourceID:    jf.FDCID,
		DataType:    dataType,
		Description: jf.Description,
		Category:    category,
	}
	// Foods without a publication date are kept, without it.
	for _, layout := range jsonDateLayouts {
		if t, err := time.Parse(layout, jf.PublicationDate); err == nil {
			f.PublicationDate = t
			break
		}
	}
	amounts := map[int]float64{}
	for _, n := range jf.FoodNutrients {
		if _, ok := trackedUnits[n.Nutrient.ID]; !ok {
			continue
		}
		if err := checkUnit(n.Nutrient.ID, n.Nutrient.Name, n.Nutrient.UnitName); err != nil {
			return nil, false, err
		}
		if n.Amount != nil {
			amounts[n.Nutrient.ID] = *n.Amount
		}
	}
	setNutrients(f, amounts)
	portions := slices.Clone(jf.FoodPortions)
	slices.SortStableFunc(portions, func(a, b jsonPortion) int {
		return cmp.Compare(a.SequenceNumber, b.SequenceNumber)
	})
	for _, p := range portions {
		if p.GramWeight <= 0 {
			continue
		}
		amount := 1.0
		if p.Amount != nil && *p.Amount > 0 {
			amount = *p.Amount
		} else if p.Value != nil && *p.Value > 0 {
			amount = *p.Value
		}
		f.Portions = append(f.Portions, foods.Portion{
			Description: describePortion(amount, p.MeasureUnit.Name, p.PortionDescription, p.Modifier),
			Amount:      amount,
			Grams:       p.GramWeight,
		})
	}
	return f, false, nil
}
