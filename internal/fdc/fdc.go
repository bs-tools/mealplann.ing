// Package fdc imports the foods of USDA FoodData Central
// (https://fdc.nal.usda.gov/download-datasets) from the CSV downloads of
// its Foundation Foods and SR Legacy datasets, which households create
// their foods from. The Branded dataset is too big for the free tier, so
// branded products are entered as custom foods instead.
package fdc

import (
	"archive/zip"
	"cmp"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bs-tools/foodplann.ing/internal/foods"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
	"github.com/bs-tools/foodplann.ing/internal/store"
)

// excludedCategories are the FDC food categories of meat, fish, dairy and
// eggs, which a vegan household never cooks with, so they aren't imported.
// They're matched by name, ignoring case, as the datasets share them. Other
// categories can still hold some animal foods, like lard among fats and
// oils.
var excludedCategories = map[string]bool{
	"beef products":                  true,
	"pork products":                  true,
	"poultry products":               true,
	"finfish and shellfish products": true,
	"lamb, veal, and game products":  true,
	"sausages and luncheon meats":    true,
	"dairy and egg products":         true,
}

// dataTypes are the data types of the foods imported. Foundation Foods'
// downloads also have the samples and acquisitions its foods were analyzed
// from, which aren't foods to cook with.
var dataTypes = map[string]bool{
	"foundation_food": true,
	"sr_legacy_food":  true,
}

// source is where a nutrient's amount comes from in FDC: the first of the
// nutrient IDs a food has, in the unit.
type source struct {
	// ids are FDC nutrient IDs (nutrient.csv's id, not its nutrient_nbr), in
	// order of preference.
	ids []int
	// unit is the unit_name of the nutrients in nutrient.csv, uppercase.
	unit string
}

// sources are the FDC nutrients of each nutrient the app tracks.
var sources = [nutrition.Count]source{
	// Foundation Foods often only have energy computed with Atwater factors.
	// The specific factors of each food are more accurate than the general
	// 4-9-4.
	nutrition.EnergyKcal:        {ids: []int{1008, 2048, 2047}, unit: "KCAL"},
	nutrition.ProteinGrams:      {ids: []int{1003}, unit: "G"},
	nutrition.CarbohydrateGrams: {ids: []int{1005}, unit: "G"},
	// SR Legacy has "Sugars, total including NLEA", Foundation Foods mostly
	// "Sugars, Total".
	nutrition.SugarsGrams:          {ids: []int{2000, 1063}, unit: "G"},
	nutrition.FatGrams:             {ids: []int{1004}, unit: "G"},
	nutrition.FiberGrams:           {ids: []int{1079}, unit: "G"},
	nutrition.VitaminB12Micrograms: {ids: []int{1178}, unit: "UG"},
	nutrition.IronMilligrams:       {ids: []int{1089}, unit: "MG"},
	nutrition.CalciumMilligrams:    {ids: []int{1087}, unit: "MG"},
	// Vitamin D (D2 + D3), in micrograms rather than IU.
	nutrition.VitaminDMicrograms: {ids: []int{1114}, unit: "UG"},
	nutrition.IodineMicrograms:   {ids: []int{1100}, unit: "UG"},
	nutrition.ZincMilligrams:     {ids: []int{1095}, unit: "MG"},
	// "PUFA 18:3 n-3 c,c,c (ALA)", else "PUFA 18:3", which in plants is
	// almost all ALA.
	nutrition.Omega3ALAGrams: {ids: []int{1404, 1270}, unit: "G"},
	// "Folate, DFE".
	nutrition.FolateMicrograms: {ids: []int{1190}, unit: "UG"},
	// "Vitamin A, RAE".
	nutrition.VitaminAMicrograms: {ids: []int{1106}, unit: "UG"},
	// "Vitamin E (alpha-tocopherol)".
	nutrition.VitaminEMilligrams: {ids: []int{1109}, unit: "MG"},
	// "Vitamin K (phylloquinone)".
	nutrition.VitaminKMicrograms:  {ids: []int{1185}, unit: "UG"},
	nutrition.CopperMilligrams:    {ids: []int{1098}, unit: "MG"},
	nutrition.MagnesiumMilligrams: {ids: []int{1090}, unit: "MG"},
	nutrition.SodiumMilligrams:    {ids: []int{1093}, unit: "MG"},
	nutrition.PotassiumMilligrams: {ids: []int{1092}, unit: "MG"},
}

// trackedUnits are the units of the FDC nutrients in sources, by ID.
var trackedUnits = func() map[int]string {
	units := map[int]string{}
	for _, s := range sources {
		for _, id := range s.ids {
			units[id] = s.unit
		}
	}
	return units
}()

// Stats count what was read from a dataset.
type Stats struct {
	// Foods is the number of foods read.
	Foods int
	// Excluded is the number of foods of excluded categories.
	Excluded int
}

// Read reads the foods of an FDC dataset's CSV download, extracted or not,
// in fsys. The CSV files may be in a directory, as they are in the zip
// files. Foods of excluded categories are left out. Foods are returned by
// FDC ID.
func Read(fsys fs.FS) ([]*foods.FDCFood, Stats, error) {
	dir, err := findDataset(fsys)
	if err != nil {
		return nil, Stats{}, err
	}
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		return nil, Stats{}, err
	}
	r := &reader{fsys: sub}
	return r.read()
}

// findDataset returns the directory of fsys with food.csv.
func findDataset(fsys fs.FS) (string, error) {
	var found string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "food.csv" {
			found = path.Dir(p)
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("failed to look for food.csv: %w", err)
	}
	if found == "" {
		return "", errors.New("no food.csv found; give the Foundation Foods or SR Legacy CSV download, as a zip file or extracted")
	}
	return found, nil
}

// reader reads a dataset.
type reader struct {
	fsys fs.FS
	// foods are the foods read so far, by FDC ID.
	foods map[int32]*foods.FDCFood
	// amounts are the amounts of each food's tracked nutrients, by FDC ID
	// and nutrient ID.
	amounts map[int32]map[int]float64
}

func (r *reader) read() ([]*foods.FDCFood, Stats, error) {
	var stats Stats
	categories := map[string]string{}
	if err := readCSV(r.fsys, "food_category.csv", true, func(row record) error {
		categories[row.get("id")] = row.get("description")
		return nil
	}); err != nil {
		return nil, stats, err
	}
	if err := readCSV(r.fsys, "nutrient.csv", true, func(row record) error {
		id, err := strconv.Atoi(row.get("id"))
		if err != nil {
			return fmt.Errorf("invalid nutrient ID: %w", err)
		}
		if want, ok := trackedUnits[id]; ok && !strings.EqualFold(row.get("unit_name"), want) {
			return fmt.Errorf("nutrient %d (%s) is in %s, want %s: the importer needs updating for this release of FDC",
				id, row.get("name"), row.get("unit_name"), want)
		}
		return nil
	}); err != nil {
		return nil, stats, err
	}

	r.foods = map[int32]*foods.FDCFood{}
	r.amounts = map[int32]map[int]float64{}
	if err := readCSV(r.fsys, "food.csv", true, func(row record) error {
		if !dataTypes[row.get("data_type")] {
			return nil
		}
		id, err := parseID(row.get("fdc_id"))
		if err != nil {
			return err
		}
		category := cmp.Or(categories[row.get("food_category_id")], row.get("food_category_id"))
		if excludedCategories[strings.ToLower(category)] {
			stats.Excluded++
			return nil
		}
		// Foods without a publication date are kept, without it.
		published, _ := time.Parse(store.DateLayout, row.get("publication_date"))
		r.foods[id] = &foods.FDCFood{
			FDCID:           id,
			DataType:        row.get("data_type"),
			Description:     row.get("description"),
			Category:        category,
			PublicationDate: published,
		}
		return nil
	}); err != nil {
		return nil, stats, err
	}

	if err := r.readPortions(); err != nil {
		return nil, stats, err
	}

	// food_nutrient.csv is the largest file, with hundreds of thousands of
	// rows, so only the amounts of the tracked nutrients of the foods kept
	// are held.
	if err := readCSV(r.fsys, "food_nutrient.csv", true, func(row record) error {
		id, err := parseID(row.get("fdc_id"))
		if err != nil {
			return err
		}
		if r.foods[id] == nil {
			return nil
		}
		nutrientID, err := strconv.Atoi(row.get("nutrient_id"))
		if err != nil {
			return fmt.Errorf("invalid nutrient ID: %w", err)
		}
		if _, ok := trackedUnits[nutrientID]; !ok || row.get("amount") == "" {
			return nil
		}
		amount, err := strconv.ParseFloat(row.get("amount"), 64)
		if err != nil {
			return fmt.Errorf("invalid amount of nutrient %d of food %d: %w", nutrientID, id, err)
		}
		if r.amounts[id] == nil {
			r.amounts[id] = map[int]float64{}
		}
		r.amounts[id][nutrientID] = amount
		return nil
	}); err != nil {
		return nil, stats, err
	}

	result := make([]*foods.FDCFood, 0, len(r.foods))
	for _, id := range slices.Sorted(maps.Keys(r.foods)) {
		f := r.foods[id]
		amounts := r.amounts[id]
		for n, s := range sources {
			for _, nutrientID := range s.ids {
				if v, ok := amounts[nutrientID]; ok {
					f.Per100Grams[n] = new(v)
					break
				}
			}
		}
		result = append(result, f)
	}
	stats.Foods = len(result)
	return result, stats, nil
}

// readPortions reads the portions of the foods kept, in their order.
// Datasets without portions are read without them.
func (r *reader) readPortions() error {
	units := map[string]string{}
	if err := readCSV(r.fsys, "measure_unit.csv", false, func(row record) error {
		units[row.get("id")] = row.get("name")
		return nil
	}); err != nil {
		return err
	}
	type portion struct {
		foods.Portion
		seq int
	}
	portions := map[int32][]portion{}
	if err := readCSV(r.fsys, "food_portion.csv", false, func(row record) error {
		id, err := parseID(row.get("fdc_id"))
		if err != nil {
			return err
		}
		if r.foods[id] == nil {
			return nil
		}
		amount, err := strconv.ParseFloat(cmp.Or(row.get("amount"), "1"), 64)
		if err != nil {
			return fmt.Errorf("invalid amount of a portion of food %d: %w", id, err)
		}
		grams, err := strconv.ParseFloat(row.get("gram_weight"), 64)
		if err != nil {
			return fmt.Errorf("invalid weight of a portion of food %d: %w", id, err)
		}
		// Portions are ordered by their sequence number, if any.
		seq, _ := strconv.Atoi(row.get("seq_num"))
		portions[id] = append(portions[id], portion{
			Portion: foods.Portion{
				Description: describePortion(amount, units[row.get("measure_unit_id")], row.get("portion_description"), row.get("modifier")),
				Amount:      amount,
				Grams:       grams,
			},
			seq: seq,
		})
		return nil
	}); err != nil {
		return err
	}
	for id, ps := range portions {
		slices.SortStableFunc(ps, func(a, b portion) int { return cmp.Compare(a.seq, b.seq) })
		for _, p := range ps {
			r.foods[id].Portions = append(r.foods[id].Portions, p.Portion)
		}
	}
	return nil
}

// describePortion describes a portion, e.g. "1 cup, chopped". SR Legacy
// describes its portions with an amount, an undetermined unit and a
// modifier, like 1, "undetermined" and "cup, chopped". Foundation Foods
// with an amount and a unit, like 1 and "cup", or a description, like
// "1 medium".
func describePortion(amount float64, unit, description, modifier string) string {
	if description != "" {
		if modifier != "" {
			return description + ", " + modifier
		}
		return description
	}
	s := strconv.FormatFloat(amount, 'f', -1, 64)
	if unit != "" && unit != "undetermined" {
		s += " " + unit
		if modifier != "" {
			s += ","
		}
	}
	if modifier != "" {
		s += " " + modifier
	}
	return s
}

func parseID(s string) (int32, error) {
	id, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid FDC ID: %w", err)
	}
	return int32(id), nil
}

// record is a row of a CSV file, with the columns of its header.
type record struct {
	columns map[string]int
	fields  []string
}

// get returns the row's field in a column, or "" if there's no such column.
func (r record) get(column string) string {
	i, ok := r.columns[column]
	if !ok || i >= len(r.fields) {
		return ""
	}
	return r.fields[i]
}

// readCSV streams the rows of a CSV file with a header to fn. A missing
// file is an error if it's required, and has no rows otherwise.
func readCSV(fsys fs.FS, name string, required bool, fn func(record) error) error {
	f, err := fsys.Open(name)
	if err != nil {
		if !required && errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to open %s: %w", name, err)
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("failed to read the header of %s: %w", name, err)
	}
	columns := map[string]int{}
	for i, column := range header {
		// Some downloads start with a byte order mark.
		columns[strings.TrimPrefix(column, "\ufeff")] = i
	}
	for line := 2; ; line++ {
		fields, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", name, err)
		}
		if err := fn(record{columns: columns, fields: fields}); err != nil {
			return fmt.Errorf("%s, line %d: %w", name, line, err)
		}
	}
}

// Upserter stores FDC foods.
type Upserter interface {
	UpsertFDCFoods(ctx context.Context, foods []*foods.FDCFood) error
}

// batchSize is how many foods are upserted in a round trip. SR Legacy has
// about 7,800 foods, and each upsert sends about 30 parameters.
const batchSize = 500

// Import imports the FDC datasets at paths, each a CSV download as a zip
// file or extracted into a directory, upserting their foods.
func Import(ctx context.Context, log *slog.Logger, db Upserter, paths ...string) error {
	if len(paths) == 0 {
		return errors.New("give the path of at least one dataset")
	}
	for _, p := range paths {
		fsys, closeFn, err := open(p)
		if err != nil {
			return err
		}
		fdcFoods, stats, err := Read(fsys)
		closeErr := closeFn()
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", p, err)
		}
		if closeErr != nil {
			return fmt.Errorf("failed to close %s: %w", p, closeErr)
		}
		for batch := range slices.Chunk(fdcFoods, batchSize) {
			if err := db.UpsertFDCFoods(ctx, batch); err != nil {
				return err
			}
		}
		log.InfoContext(ctx, "Imported FDC dataset", slog.String("path", p),
			slog.Int("foods", stats.Foods), slog.Int("excluded", stats.Excluded))
	}
	return nil
}

// open opens a dataset, a zip file or a directory, returning a function
// closing it.
func open(p string) (fs.FS, func() error, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open dataset: %w", err)
	}
	if info.IsDir() {
		return os.DirFS(p), func() error { return nil }, nil
	}
	z, err := zip.OpenReader(p)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open %s as a zip file: %w", p, err)
	}
	return z, z.Close, nil
}
