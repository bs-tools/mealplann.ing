// Package foodimport has what the importers of reference foods share: USDA
// FoodData Central's (internal/fdc) and the Canadian Nutrient File's
// (internal/cnf). It opens the downloads, reads their CSV files, leaves out
// the animal food categories, checks nutrient units and upserts the foods in
// batches.
package foodimport

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"slices"
	"strings"
	"unicode"

	"github.com/bs-tools/foodplann.ing/internal/foods"
)

// excludedCategories are the food categories of meat, fish, dairy and
// eggs, which a vegan household never cooks with, so they aren't imported,
// normalized with NormalizeCategory. FDC and CNF name them alike, as CNF
// started from FDC's predecessor, but not quite: FDC's "Lamb, Veal, and Game
// Products" is CNF's "Lamb, Veal and Game", and CNF writes "Sausages and
// Luncheon meats". Other categories can still hold some animal foods, like
// lard among fats and oils, or mixed dishes with meat.
var excludedCategories = map[string]bool{
	// FDC's and CNF's "Beef Products".
	"beef": true,
	// FDC's and CNF's "Pork Products".
	"pork": true,
	// FDC's and CNF's "Poultry Products".
	"poultry": true,
	// FDC's and CNF's "Finfish and Shellfish Products".
	"finfish shellfish": true,
	// FDC's "Lamb, Veal, and Game Products" and CNF's "Lamb, Veal and
	// Game".
	"lamb veal game": true,
	// FDC's "Sausages and Luncheon Meats" and CNF's "Sausages and Luncheon
	// meats".
	"sausages luncheon meats": true,
	// FDC's and CNF's "Dairy and Egg Products".
	"dairy egg": true,
}

// categoryFillerWords are left out of normalized categories, as the
// sources differ in them.
var categoryFillerWords = map[string]bool{"and": true, "products": true, "product": true}

// NormalizeCategory normalizes the name of a food category, so the
// sources' variants of a name compare equal: lowercase words of letters,
// without "and" or "products", separated by spaces. "Lamb, Veal, and Game
// Products" is "lamb veal game".
func NormalizeCategory(name string) string {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	words = slices.DeleteFunc(words, func(w string) bool { return categoryFillerWords[w] })
	return strings.Join(words, " ")
}

// IsExcludedCategory reports whether foods of a category are left out, as
// a vegan household doesn't eat them.
func IsExcludedCategory(name string) bool {
	return excludedCategories[NormalizeCategory(name)]
}

// NormalizeUnit normalizes the unit of a nutrient, so the sources'
// spellings compare equal: lowercase, without spaces or underscores, with
// micrograms as "ug" whether written "µg", "μg", "ug" or "mcg". FDC writes
// "UG" in its CSV downloads and "µg" in its JSON, CNF "µg" and "kCal".
func NormalizeUnit(unit string) string {
	u := strings.ToLower(strings.TrimSpace(unit))
	u = strings.NewReplacer("µ", "u", "μ", "u", " ", "", "_", "").Replace(u)
	if strings.HasPrefix(u, "mcg") {
		u = "ug" + strings.TrimPrefix(u, "mcg")
	}
	return u
}

// Record is a row of a CSV file, with the columns of its header.
type Record struct {
	columns map[string]int
	fields  []string
}

// Get returns the row's field in a column, named ignoring case and
// surrounding spaces, trimmed of spaces, or "" if there's no such column.
func (r Record) Get(column string) string {
	i, ok := r.columns[strings.ToLower(column)]
	if !ok || i >= len(r.fields) {
		return ""
	}
	return strings.TrimSpace(r.fields[i])
}

// byteOrderMark starts some downloads' files.
const byteOrderMark = "\xef\xbb\xbf"

// ReadCSV streams the rows of a CSV file with a header, named name in
// errors, to fn.
func ReadCSV(r io.Reader, name string, fn func(Record) error) error {
	cr := csv.NewReader(r)
	cr.ReuseRecord = true
	cr.FieldsPerRecord = -1
	// Some downloads quote sloppily, like a description with an inch sign.
	cr.LazyQuotes = true
	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("failed to read the header of %s: %w", name, err)
	}
	columns := map[string]int{}
	for i, column := range header {
		// Some downloads start with a byte order mark.
		column = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(column, byteOrderMark)))
		if _, ok := columns[column]; !ok {
			columns[column] = i
		}
	}
	for line := 2; ; line++ {
		fields, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", name, err)
		}
		// Blank lines, which some files end with, have no fields.
		if len(fields) == 1 && strings.TrimSpace(fields[0]) == "" {
			continue
		}
		if err := fn(Record{columns: columns, fields: fields}); err != nil {
			return fmt.Errorf("%s, line %d: %w", name, line, err)
		}
	}
}

// Open opens a download, a zip file or a directory, returning a function
// closing it.
func Open(p string) (fs.FS, func() error, error) {
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

// Upserter stores reference foods.
type Upserter interface {
	UpsertReferenceFoods(ctx context.Context, foods []*foods.ReferenceFood) error
}

// batchSize is how many foods are upserted in a round trip. SR Legacy has
// about 7,800 foods and CNF about 5,700, and each upsert sends about 30
// parameters.
const batchSize = 500

// Upsert upserts reference foods in batches.
func Upsert(ctx context.Context, db Upserter, list []*foods.ReferenceFood) error {
	for batch := range slices.Chunk(list, batchSize) {
		if err := db.UpsertReferenceFoods(ctx, batch); err != nil {
			return err
		}
	}
	return nil
}
