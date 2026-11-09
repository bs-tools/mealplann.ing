// Package cnf imports the foods of Health Canada's Canadian Nutrient File
// (CNF), from its CSV download on the Open Government Portal
// (https://open.canada.ca), which households create their foods from
// alongside USDA FoodData Central's (see internal/fdc). CNF has the foods as
// sold in Canada, with Canadian fortification, like vitamin D and B12 in
// plant beverages and vitamin D in margarine.
//
// The download is a zip file of CSV files, one per table. The importer reads
// both the 2026 edition and the 2015 one, whose files and columns are named
// differently (see the 2026 edition's "database structure and file content
// description", Appendix A). Of the 2015 edition, it reads:
//
//   - FOOD NAME.csv: FoodID, FoodGroupID, FoodDescription, FoodDescriptionF
//     and FoodDateOfPublication (or FoodDateOfEntry).
//   - FOOD GROUP.csv: FoodGroupID and FoodGroupName, to leave out the
//     animal groups.
//   - NUTRIENT NAME.csv: NutrientID, NutrientSymbol, NutrientUnit and
//     NutrientName, to check the nutrients read (see sources).
//   - NUTRIENT AMOUNT.csv: FoodID, NutrientID and NutrientValue, per 100 g
//     of the food's edible portion.
//   - CONVERSION FACTOR.csv: FoodID, MeasureID and ConversionFactorValue,
//     the weight of a measure of a food in hundreds of grams, and MEASURE
//     NAME.csv: MeasureID and MeasureDescription, like "250ml" or "1
//     medium", for the foods' portions. They're optional.
//
// The 2026 edition renames the files "Food Name", "CNF Food Group",
// "Nutrient Name", "Nutrient Amount", "Measure Weight Conversion" and
// "Measure Name", and their columns, like Food_Code for FoodID and
// Nutrient_Amount for NutrientValue. Its Measure Weight Conversion gives a
// measure's weight in grams rather than hundreds of grams, and has refuse
// and yield weights too, told apart by its Measure_Type_Code, which its
// "Measure Type" file describes. It spells units out, like "microgram".
//
// Health Canada's files have changed over the years, so the reader is
// tolerant: it finds the files ignoring case, spaces and underscores in
// their names, in any directory of the download; reads column names
// ignoring case; parses dates in several layouts; and reads files that
// aren't valid UTF-8 as Windows-1252, which older releases were in, as
// their French text has accents.
package cnf

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/bs-tools/mealplann.ing/internal/foodimport"
	"github.com/bs-tools/mealplann.ing/internal/foods"
	"golang.org/x/text/encoding/charmap"
)

// dataType is the data type of CNF foods in reference_foods.
const dataType = "cnf"

// The files read, by their names normalized with normalizeFileName.
const (
	fileFoodName         = "foodname"
	fileFoodGroup        = "foodgroup"
	fileNutrientName     = "nutrientname"
	fileNutrientAmount   = "nutrientamount"
	fileConversionFactor = "conversionfactor"
	fileMeasureName      = "measurename"
	fileMeasureType      = "measuretype"
)

// fileAliases are the 2026 edition's names of the files whose 2015 names
// differ, normalized, by the 2015 names they're read as.
var fileAliases = map[string]string{
	"cnffoodgroup":             fileFoodGroup,
	"measureweightconversion":  fileConversionFactor,
	"measureweightconversions": fileConversionFactor,
}

// requiredFiles are the files a download must have. Without FOOD GROUP.csv
// the animal foods couldn't be left out.
var requiredFiles = []string{fileFoodName, fileFoodGroup, fileNutrientName, fileNutrientAmount}

// displayNames are the files' names in the download, for errors.
var displayNames = map[string]string{
	fileFoodName:         "FOOD NAME.csv",
	fileFoodGroup:        "FOOD GROUP.csv (CNF Food Group.csv in 2026)",
	fileNutrientName:     "NUTRIENT NAME.csv",
	fileNutrientAmount:   "NUTRIENT AMOUNT.csv",
	fileConversionFactor: "CONVERSION FACTOR.csv (Measure Weight Conversion.csv in 2026)",
	fileMeasureName:      "MEASURE NAME.csv",
	fileMeasureType:      "Measure Type.csv",
}

// normalizeFileName normalizes the name of a CSV file of the download, so
// "FOOD NAME.csv", "food_name.CSV" and "FoodName.csv" are all "foodname".
// It returns "" for files that aren't CSV.
func normalizeFileName(name string) string {
	ext := path.Ext(name)
	if !strings.EqualFold(ext, ".csv") {
		return ""
	}
	base := strings.ToLower(strings.TrimSuffix(path.Base(name), ext))
	return strings.NewReplacer(" ", "", "_", "", "-", "").Replace(base)
}

// Stats count what was read from a download.
type Stats struct {
	// Foods is the number of foods read.
	Foods int
	// Excluded is the number of foods of excluded groups.
	Excluded int
}

// Read reads the foods of CNF's CSV download, extracted or not, in fsys.
// Foods of excluded groups (see foodimport.IsExcludedCategory) are left
// out. Foods are returned by FoodID.
func Read(fsys fs.FS) ([]*foods.ReferenceFood, Stats, error) {
	files, err := findFiles(fsys)
	if err != nil {
		return nil, Stats{}, err
	}
	r := &reader{fsys: fsys, files: files}
	return r.read()
}

// findFiles returns the paths of the files read in fsys, by their
// normalized names.
func findFiles(fsys fs.FS) (map[string]string, error) {
	files := map[string]string{}
	// The CSV files seen, to say what the download has instead.
	var seen []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// macOS's zip files have copies of the files' metadata in
			// __MACOSX, with the same names.
			if strings.HasPrefix(d.Name(), "__") {
				return fs.SkipDir
			}
			return nil
		}
		name := normalizeFileName(d.Name())
		if alias, ok := fileAliases[name]; ok {
			name = alias
		}
		if _, ok := displayNames[name]; ok {
			if _, dup := files[name]; !dup {
				files[name] = p
			}
		} else if name != "" && len(seen) < 10 {
			seen = append(seen, d.Name())
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to look for the CNF files: %w", err)
	}
	var missing []string
	for _, name := range requiredFiles {
		if files[name] == "" {
			missing = append(missing, displayNames[name])
		}
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("no %s found; give the Canadian Nutrient File's CSV download, as a zip file or extracted",
			strings.Join(missing, " or "))
		if len(seen) > 0 {
			msg += fmt.Sprintf(" (it has %s)", strings.Join(seen, ", "))
		}
		return nil, errors.New(msg)
	}
	return files, nil
}

// reader reads a download.
type reader struct {
	fsys fs.FS
	// files are the paths of the files read, by their normalized names.
	files map[string]string
	// foods are the foods read so far, by FoodID.
	foods map[int32]*foods.ReferenceFood
}

func (r *reader) read() ([]*foods.ReferenceFood, Stats, error) {
	var stats Stats
	groups := map[string]string{}
	if err := r.readCSV(fileFoodGroup, func(row foodimport.Record) error {
		groups[get(row, "CNF_Food_Group_Code", "FoodGroupID")] = get(row, "CNF_Food_Group_Description_EN", "FoodGroupName")
		return nil
	}); err != nil {
		return nil, stats, err
	}

	names := map[int]nutrientName{}
	if err := r.readCSV(fileNutrientName, func(row foodimport.Record) error {
		id, err := parseNutrientID(row)
		if err != nil {
			return err
		}
		names[id] = nutrientName{
			id:      id,
			symbol:  get(row, "Nutrient_Symbol", "NutrientSymbol"),
			unit:    get(row, "Nutrient_Unit", "NutrientUnit"),
			name:    get(row, "Nutrient_Name_EN", "NutrientName"),
			tagname: row.Get("Tagname"),
		}
		return nil
	}); err != nil {
		return nil, stats, err
	}
	read, err := checkNutrients(names)
	if err != nil {
		return nil, stats, err
	}

	r.foods = map[int32]*foods.ReferenceFood{}
	if err := r.readCSV(fileFoodName, func(row foodimport.Record) error {
		id, err := parseFoodID(row)
		if err != nil {
			return err
		}
		groupID := get(row, "CNF_Food_Group_Code", "FoodGroupID")
		group, ok := groups[groupID]
		if !ok {
			// Its group can't be told, so neither can whether it's an
			// animal food.
			return fmt.Errorf("food %d is in food group %q, which the food group file doesn't have", id, groupID)
		}
		if foodimport.IsExcludedCategory(group) {
			stats.Excluded++
			return nil
		}
		r.foods[id] = &foods.ReferenceFood{
			Source:            foods.SourceCNF,
			SourceID:          id,
			DataType:          dataType,
			Description:       get(row, "Food_Description_EN", "FoodDescription"),
			FrenchDescription: get(row, "Food_Description_FR", "FoodDescriptionF"),
			Category:          group,
			// Foods without a date are kept, without it.
			PublicationDate: parseDate(row.Get("Food_Last_Updated_Date"), row.Get("FoodDateOfPublication"), row.Get("FoodDateOfEntry")),
		}
		return nil
	}); err != nil {
		return nil, stats, err
	}

	if err := r.readPortions(); err != nil {
		return nil, stats, err
	}

	// NUTRIENT AMOUNT.csv is the largest file, with over half a million
	// rows, so only the amounts of the nutrients read of the foods kept are
	// held.
	amounts := map[int32]map[int]float64{}
	if err := r.readCSV(fileNutrientAmount, func(row foodimport.Record) error {
		id, err := parseFoodID(row)
		if err != nil {
			return err
		}
		if r.foods[id] == nil {
			return nil
		}
		nutrientID, err := parseNutrientID(row)
		if err != nil {
			return err
		}
		value := get(row, "Nutrient_Amount", "NutrientValue")
		if read[nutrientID] == nil || value == "" {
			return nil
		}
		amount, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid amount of nutrient %d of food %d: %w", nutrientID, id, err)
		}
		if amounts[id] == nil {
			amounts[id] = map[int]float64{}
		}
		amounts[id][nutrientID] = amount
		return nil
	}); err != nil {
		return nil, stats, err
	}

	result := make([]*foods.ReferenceFood, 0, len(r.foods))
	for _, id := range slices.Sorted(maps.Keys(r.foods)) {
		f := r.foods[id]
		setNutrients(&f.Per100Grams, amounts[id])
		result = append(result, f)
	}
	stats.Foods = len(result)
	return result, stats, nil
}

// readPortions reads the portions of the foods kept, in the order of
// CONVERSION FACTOR.csv (Measure Weight Conversion.csv in 2026). Downloads
// without it or MEASURE NAME.csv are read without portions.
func (r *reader) readPortions() error {
	if r.files[fileConversionFactor] == "" || r.files[fileMeasureName] == "" {
		return nil
	}
	measures := map[string]string{}
	if err := r.readCSV(fileMeasureName, func(row foodimport.Record) error {
		measures[get(row, "Measure_Code", "MeasureID")] = get(row, "Measure_Description_&_Unit_EN", "MeasureDescription")
		return nil
	}); err != nil {
		return err
	}
	// The 2026 edition's refuse and yield weights aren't portions, so only
	// the measures of other types are read: the household measures, like
	// "250 ml" or "1 medium". Without its Measure Type file, the types
	// can't be told apart, so no measure with a type is read.
	portionTypes := map[string]bool{}
	if r.files[fileMeasureType] != "" {
		if err := r.readCSV(fileMeasureType, func(row foodimport.Record) error {
			description := strings.ToLower(row.Get("Measure_Type_Description_EN"))
			if !strings.Contains(description, "refuse") && !strings.Contains(description, "yield") {
				portionTypes[row.Get("Measure_Type_Code")] = true
			}
			return nil
		}); err != nil {
			return err
		}
	}
	return r.readCSV(fileConversionFactor, func(row foodimport.Record) error {
		id, err := parseFoodID(row)
		if err != nil {
			return err
		}
		f := r.foods[id]
		if f == nil {
			return nil
		}
		if t := row.Get("Measure_Type_Code"); t != "" && !portionTypes[t] {
			return nil
		}
		description := measures[get(row, "Measure_Code", "MeasureID")]
		if description == "" {
			return nil
		}
		grams, err := measureGrams(row)
		if err != nil {
			return fmt.Errorf("invalid measure weight of food %d: %w", id, err)
		}
		if grams <= 0 {
			return nil
		}
		f.Portions = append(f.Portions, foods.Portion{
			Description: description,
			Amount:      portionAmount(description),
			Grams:       roundGrams(grams),
		})
		return nil
	})
}

// measureGrams returns the weight of a measure of a food in a row of
// CONVERSION FACTOR.csv or Measure Weight Conversion.csv.
func measureGrams(row foodimport.Record) (float64, error) {
	if w := row.Get("Measure_Weight_Conversion"); w != "" {
		// The 2026 edition gives the weight in grams.
		return strconv.ParseFloat(w, 64)
	}
	// The 2015 edition gives the weight over the 100 g the nutrients are
	// given for.
	factor, err := strconv.ParseFloat(row.Get("ConversionFactorValue"), 64)
	return factor * 100, err
}

// roundGrams rounds a portion's weight to a hundredth of a gram, as the
// conversion factors' floating-point multiplication leaves noise, like
// 257.99999999999997.
func roundGrams(g float64) float64 {
	return math.Round(g*100) / 100
}

// leadingAmount matches the amount at the start of a measure, like "250"
// of "250ml", "1/2" of "1/2 cup" or "1 1/2" of "1 1/2 cup".
var leadingAmount = regexp.MustCompile(`^(\d+(?:\.\d+)?)(?:\s+(\d+)/(\d+)|/(\d+))?`)

// portionAmount returns how many of its unit a measure is, like 250 for
// "250ml", 0.5 for "1/2 cup" or 1 for "1 medium" or "medium".
func portionAmount(description string) float64 {
	m := leadingAmount.FindStringSubmatch(strings.TrimSpace(description))
	if m == nil {
		return 1
	}
	amount, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 1
	}
	switch {
	case m[2] != "":
		// A mixed number, like "1 1/2".
		num, _ := strconv.ParseFloat(m[2], 64)
		den, _ := strconv.ParseFloat(m[3], 64)
		if den > 0 {
			amount += num / den
		}
	case m[4] != "":
		// A fraction, like "1/2".
		den, _ := strconv.ParseFloat(m[4], 64)
		if den > 0 {
			amount /= den
		}
	}
	if amount <= 0 {
		return 1
	}
	return amount
}

// dateLayouts are the layouts of CNF's dates, which releases have written
// in several.
var dateLayouts = []string{
	"2006-01-02",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006/01/02",
	"01/02/2006",
	"1/2/2006",
	"2006-01",
	"2006/01",
}

// parseDate returns the first of dates that parses, zero if none does.
func parseDate(dates ...string) time.Time {
	for _, d := range dates {
		for _, layout := range dateLayouts {
			if t, err := time.Parse(layout, d); err == nil {
				return t
			}
		}
	}
	return time.Time{}
}

// get returns a row's field in the first of columns it has, as the 2026
// edition's columns are named differently from the 2015 one's: each is
// given its 2026 name, like "Food_Code", then its 2015 name, like "FoodID".
func get(row foodimport.Record, columns ...string) string {
	for _, c := range columns {
		if v := row.Get(c); v != "" {
			return v
		}
	}
	return ""
}

// parseFoodID parses a row's FoodID (Food_Code in 2026).
func parseFoodID(row foodimport.Record) (int32, error) {
	id, err := strconv.ParseInt(get(row, "Food_Code", "FoodID"), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid FoodID: %w", err)
	}
	return int32(id), nil
}

// parseNutrientID parses a row's NutrientID (Nutrient_Code in 2026).
func parseNutrientID(row foodimport.Record) (int, error) {
	id, err := strconv.Atoi(get(row, "Nutrient_Code", "NutrientID"))
	if err != nil {
		return 0, fmt.Errorf("invalid NutrientID: %w", err)
	}
	return id, nil
}

// readCSV streams the rows of a file to fn, read as UTF-8 or else
// Windows-1252.
func (r *reader) readCSV(file string, fn func(foodimport.Record) error) error {
	p := r.files[file]
	f, err := openText(r.fsys, p)
	if err != nil {
		return err
	}
	defer f.Close()
	return foodimport.ReadCSV(f, path.Base(p), fn)
}

// openText opens a text file, decoding it from Windows-1252 if it isn't
// valid UTF-8. CNF's French text has accents, like "Bœuf haché", which
// older releases wrote in Windows-1252 (or Latin-1, which it extends). The
// file is read through once to tell, rather than held in memory, as
// NUTRIENT AMOUNT.csv is tens of megabytes.
func openText(fsys fs.FS, name string) (io.ReadCloser, error) {
	utf8OK, err := isUTF8(fsys, name)
	if err != nil {
		return nil, err
	}
	f, err := fsys.Open(name)
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", name, err)
	}
	if utf8OK {
		return f, nil
	}
	return struct {
		io.Reader
		io.Closer
	}{charmap.Windows1252.NewDecoder().Reader(f), f}, nil
}

// isUTF8 reports whether a file is valid UTF-8.
func isUTF8(fsys fs.FS, name string) (bool, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return false, fmt.Errorf("failed to open %s: %w", name, err)
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 64*1024)
	for {
		r, size, err := br.ReadRune()
		if errors.Is(err, io.EOF) {
			return true, nil
		}
		if err != nil {
			return false, fmt.Errorf("failed to read %s: %w", name, err)
		}
		// Invalid bytes read as the replacement character, one byte long;
		// the character itself is three.
		if r == utf8.RuneError && size == 1 {
			return false, nil
		}
	}
}

// Import imports CNF's downloads at paths, each a zip file or extracted into
// a directory, upserting their foods.
func Import(ctx context.Context, log *slog.Logger, db foodimport.Upserter, paths ...string) error {
	if len(paths) == 0 {
		return errors.New("give the path of the Canadian Nutrient File's CSV download")
	}
	for _, p := range paths {
		fsys, closeFn, err := foodimport.Open(p)
		if err != nil {
			return err
		}
		cnfFoods, stats, err := Read(fsys)
		closeErr := closeFn()
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", p, err)
		}
		if closeErr != nil {
			return fmt.Errorf("failed to close %s: %w", p, closeErr)
		}
		if err := foodimport.Upsert(ctx, db, cnfFoods); err != nil {
			return err
		}
		log.InfoContext(ctx, "Imported the Canadian Nutrient File", slog.String("path", p),
			slog.Int("foods", stats.Foods), slog.Int("excluded", stats.Excluded))
	}
	return nil
}
