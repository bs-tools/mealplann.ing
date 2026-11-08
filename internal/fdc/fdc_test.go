package fdc

import (
	"archive/zip"
	"context"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
	"github.com/bs-tools/mealplann.ing/internal/store/storetest"
	"github.com/jackc/pgx/v5/pgtype"
)

// The fixtures are small extracts of the real CSV downloads, with a few
// foods each, in the same format.
const (
	srLegacyDir   = "testdata/sr_legacy"
	foundationDir = "testdata/foundation"
	// The same Foundation foods as foundationDir, as a JSON download.
	foundationJSONDir = "testdata/foundation_json"
)

// zipDir zips a directory into a file, like FDC's downloads, and returns
// its path.
func zipDir(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), filepath.Base(dir)+".zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	if err := w.AddFS(os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func byID(fdcFoods []*foods.ReferenceFood) map[int32]*foods.ReferenceFood {
	m := map[int32]*foods.ReferenceFood{}
	for _, f := range fdcFoods {
		m[f.SourceID] = f
	}
	return m
}

// wantNutrient checks a food's amount of a nutrient, nil for unknown.
func wantNutrient(t *testing.T, f *foods.ReferenceFood, n nutrition.Nutrient, want *float64) {
	t.Helper()
	got := f.Per100Grams[n]
	switch {
	case want == nil && got != nil:
		t.Errorf("%s: %v = %v, want unknown", f.Description, n, *got)
	case want != nil && got == nil:
		t.Errorf("%s: %v is unknown, want %v", f.Description, n, *want)
	case want != nil && math.Abs(*got-*want) > 1e-9:
		t.Errorf("%s: %v = %v, want %v", f.Description, n, *got, *want)
	}
}

func TestReadSRLegacy(t *testing.T) {
	t.Parallel()
	fdcFoods, stats, err := Read(os.DirFS(srLegacyDir))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The beef and the butter are left out.
	if stats.Foods != 5 || stats.Excluded != 2 || len(fdcFoods) != 5 {
		t.Errorf("got %d foods and stats %+v, want 5 foods and 2 excluded", len(fdcFoods), stats)
	}
	if !slices.IsSortedFunc(fdcFoods, func(a, b *foods.ReferenceFood) int { return int(a.SourceID - b.SourceID) }) {
		t.Error("foods aren't ordered by FDC ID")
	}
	m := byID(fdcFoods)
	if m[174036] != nil || m[173430] != nil {
		t.Error("the beef or the butter was read")
	}

	chickpeas := m[173756]
	if chickpeas == nil {
		t.Fatal("no chickpeas")
	}
	if chickpeas.Description != "Chickpeas (garbanzo beans, bengal gram), mature seeds, raw" ||
		chickpeas.Category != "Legumes and Legume Products" || chickpeas.DataType != "sr_legacy_food" ||
		chickpeas.PublicationDate.Format("2006-01-02") != "2019-04-01" {
		t.Errorf("got chickpeas %+v", chickpeas)
	}
	wantNutrient(t, chickpeas, nutrition.EnergyKcal, new(378.0))
	wantNutrient(t, chickpeas, nutrition.ProteinGrams, new(20.47))
	// Sugars are SR Legacy's "including NLEA", and ALA its undifferentiated
	// 18:3.
	wantNutrient(t, chickpeas, nutrition.SugarsGrams, new(10.7))
	wantNutrient(t, chickpeas, nutrition.Omega3ALAGrams, new(0.101))
	wantNutrient(t, chickpeas, nutrition.FolateMicrograms, new(557.0))
	// Zero is known, and different from the missing iodine.
	wantNutrient(t, chickpeas, nutrition.VitaminB12Micrograms, new(0.0))
	wantNutrient(t, chickpeas, nutrition.IodineMicrograms, nil)
	if got := chickpeas.SuggestGramsPerMilliliter(); math.Abs(got-0.845) > 1e-9 {
		t.Errorf("chickpeas' density = %v, want 200 g per cup, 0.845", got)
	}
	if got := chickpeas.SuggestGramsPerPiece(); got != 0 {
		t.Errorf("chickpeas weigh %v g a piece, want none", got)
	}

	// Spinach has differentiated ALA, preferred over 18:3.
	spinach := m[168462]
	wantNutrient(t, spinach, nutrition.Omega3ALAGrams, new(0.138))
	wantNutrient(t, spinach, nutrition.VitaminKMicrograms, new(482.9))
	// Its bunches and leaves aren't pieces to count in.
	if got := spinach.SuggestGramsPerPiece(); got != 0 {
		t.Errorf("spinach weighs %v g a piece, want none", got)
	}
	wantPortions := []foods.Portion{
		{Description: "1 cup", Amount: 1, Grams: 30},
		{Description: "1 bunch", Amount: 1, Grams: 340},
		{Description: "1 leaf", Amount: 1, Grams: 10},
	}
	if !slices.Equal(spinach.Portions, wantPortions) {
		t.Errorf("got spinach portions %+v, want %+v", spinach.Portions, wantPortions)
	}

	// A medium onion is preferred to a large one.
	onions := m[170000]
	if got := onions.SuggestGramsPerPiece(); got != 110 {
		t.Errorf("an onion weighs %v g, want a medium one's 110 g", got)
	}
	if got := onions.Portions[2].Description; got != `1 medium (2-1/2" dia)` {
		t.Errorf("got portion %q", got)
	}
	// Portions are ordered by their sequence numbers: the quarter block of
	// tofu comes first, and is a piece of a whole block.
	tofu := m[172475]
	if got := tofu.Portions[0].Description; got != "0.25 block" {
		t.Errorf("got tofu's first portion %q, want 0.25 block", got)
	}
	if got := tofu.SuggestGramsPerPiece(); got != 324 {
		t.Errorf("a block of tofu weighs %v g, want 324 g", got)
	}
	wantNutrient(t, tofu, nutrition.SugarsGrams, nil)
}

func TestReadFoundationZip(t *testing.T) {
	t.Parallel()
	z, err := zip.OpenReader(zipDir(t, foundationDir))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	fdcFoods, stats, err := Read(z)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The samples and acquisitions aren't foods, and the eggs are excluded.
	if stats.Foods != 2 || stats.Excluded != 1 {
		t.Errorf("got stats %+v, want 2 foods and 1 excluded", stats)
	}
	m := byID(fdcFoods)
	soyMilk := m[2257046]
	if soyMilk == nil || m[2257047] != nil || m[2257048] != nil {
		t.Fatalf("got foods %v, want the soy milk without its samples", m)
	}
	// Energy is from the specific Atwater factors, sugars "Sugars, Total".
	wantNutrient(t, soyMilk, nutrition.EnergyKcal, new(32.3))
	wantNutrient(t, soyMilk, nutrition.SugarsGrams, new(0.0))
	wantNutrient(t, soyMilk, nutrition.Omega3ALAGrams, new(0.12))
	wantNutrient(t, soyMilk, nutrition.VitaminDMicrograms, new(1.2))
	wantPortions := []foods.Portion{
		{Description: "1 cup", Amount: 1, Grams: 243},
		{Description: "1 tablespoon", Amount: 1, Grams: 15.2},
	}
	if !slices.Equal(soyMilk.Portions, wantPortions) {
		t.Errorf("got soy milk portions %+v, want %+v", soyMilk.Portions, wantPortions)
	}
	if got := soyMilk.SuggestGramsPerMilliliter(); got != 1.027 {
		t.Errorf("soy milk's density = %v, want 1.027", got)
	}
	if got := m[2346393].SuggestGramsPerPiece(); got != 143 {
		t.Errorf("a yellow onion weighs %v g, want 143 g", got)
	}
}

func TestReadFoundationJSONZip(t *testing.T) {
	t.Parallel()
	z, err := zip.OpenReader(zipDir(t, foundationJSONDir))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	fdcFoods, stats, err := Read(z)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The samples are nested in the soy milk, not foods, and the eggs are
	// excluded.
	if stats.Foods != 2 || stats.Excluded != 1 {
		t.Errorf("got stats %+v, want 2 foods and 1 excluded", stats)
	}
	m := byID(fdcFoods)
	soyMilk := m[2257046]
	if soyMilk == nil || m[2257047] != nil {
		t.Fatalf("got foods %v, want the soy milk without its samples", m)
	}
	if soyMilk.DataType != "foundation_food" || soyMilk.Category != "Legumes and Legume Products" ||
		soyMilk.PublicationDate != time.Date(2022, 4, 28, 0, 0, 0, 0, time.UTC) {
		t.Errorf("got soy milk %+v, want a Foundation legume product published on 2022-04-28", soyMilk)
	}
	// The same nutrients as from the CSV download, including those in
	// micrograms, which the JSON writes with the micro sign.
	wantNutrient(t, soyMilk, nutrition.EnergyKcal, new(32.3))
	wantNutrient(t, soyMilk, nutrition.SugarsGrams, new(0.0))
	wantNutrient(t, soyMilk, nutrition.Omega3ALAGrams, new(0.12))
	wantNutrient(t, soyMilk, nutrition.VitaminDMicrograms, new(1.2))
	wantNutrient(t, soyMilk, nutrition.VitaminB12Micrograms, new(1.11))
	wantNutrient(t, soyMilk, nutrition.IodineMicrograms, nil)
	wantPortions := []foods.Portion{
		{Description: "1 cup", Amount: 1, Grams: 243},
		{Description: "1 tablespoon", Amount: 1, Grams: 15.2},
	}
	if !slices.Equal(soyMilk.Portions, wantPortions) {
		t.Errorf("got soy milk portions %+v, want %+v", soyMilk.Portions, wantPortions)
	}
	if got := soyMilk.SuggestGramsPerMilliliter(); got != 1.027 {
		t.Errorf("soy milk's density = %v, want 1.027", got)
	}
	if got := m[2346393].SuggestGramsPerPiece(); got != 143 {
		t.Errorf("a yellow onion weighs %v g, want 143 g", got)
	}
}

func TestReadSRLegacyJSON(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{"FoodData_Central_sr_legacy_food_json_2018-04.json": {Data: []byte(`{
		"SRLegacyFoods": [
			{
				"foodClass": "FinalFood",
				"description": "Kale, raw",
				"foodNutrients": [
					{"nutrient": {"id": 1003, "name": "Protein", "unitName": "g"}, "amount": 2.92},
					{"nutrient": {"id": 1008, "name": "Energy", "unitName": "kcal"}, "amount": 35},
					{"nutrient": {"id": 2000, "name": "Sugars, total including NLEA", "unitName": "g"}, "amount": 0.99},
					{"nutrient": {"id": 1087, "name": "Calcium, Ca", "unitName": "mg"}, "amount": 254},
					{"nutrient": {"id": 1062, "name": "Energy", "unitName": "kJ"}, "amount": 146},
					{"nutrient": {"id": 1100, "name": "Iodine, I", "unitName": "µg"}}
				],
				"foodPortions": [
					{"amount": 1, "measureUnit": {"name": "undetermined"}, "modifier": "cup, chopped", "gramWeight": 21, "sequenceNumber": 1}
				],
				"publicationDate": "4/1/2019",
				"foodCategory": {"description": "Vegetables and Vegetable Products"},
				"fdcId": 168421,
				"dataType": "SR Legacy"
			},
			{
				"foodClass": "FinalFood",
				"description": "Beef, ground, raw",
				"foodCategory": {"description": "Beef Products"},
				"fdcId": 1,
				"dataType": "SR Legacy"
			},
			{
				"description": "A branded food",
				"fdcId": 2,
				"dataType": "Branded"
			}
		],
		"metadata": {"version": "2018-04"}
	}`)}}
	fdcFoods, stats, err := Read(fsys)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if stats.Foods != 1 || stats.Excluded != 1 || len(fdcFoods) != 1 {
		t.Fatalf("got %d foods and stats %+v, want the kale, 1 food and 1 excluded", len(fdcFoods), stats)
	}
	kale := fdcFoods[0]
	if kale.SourceID != 168421 || kale.DataType != "sr_legacy_food" || kale.PublicationDate != time.Date(2019, 4, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("got kale %+v, want SR Legacy food 168421 published on 2019-04-01", kale)
	}
	wantNutrient(t, kale, nutrition.ProteinGrams, new(2.92))
	wantNutrient(t, kale, nutrition.EnergyKcal, new(35.0))
	wantNutrient(t, kale, nutrition.SugarsGrams, new(0.99))
	wantNutrient(t, kale, nutrition.CalciumMilligrams, new(254.0))
	// A nutrient without an amount is unknown.
	wantNutrient(t, kale, nutrition.IodineMicrograms, nil)
	want := []foods.Portion{{Description: "1 cup, chopped", Amount: 1, Grams: 21}}
	if !slices.Equal(kale.Portions, want) {
		t.Errorf("got kale portions %+v, want %+v", kale.Portions, want)
	}
}

func TestReadErrors(t *testing.T) {
	t.Parallel()
	dataset := func(nutrients string) fstest.MapFS {
		return fstest.MapFS{
			"dl/food.csv":          {Data: []byte("fdc_id,data_type,description,food_category_id,publication_date\n1,sr_legacy_food,Kale,11,\n")},
			"dl/food_category.csv": {Data: []byte("id,code,description\n11,1100,Vegetables and Vegetable Products\n")},
			"dl/nutrient.csv":      {Data: []byte(nutrients)},
			"dl/food_nutrient.csv": {Data: []byte("id,fdc_id,nutrient_id,amount\n1,1,1003,2.9\n")},
		}
	}
	tests := map[string]struct {
		fsys    fstest.MapFS
		wantErr string
	}{
		"no food.csv": {
			fsys:    fstest.MapFS{"dl/foods.csv": {}},
			wantErr: "no food.csv or JSON file found; give the Foundation Foods or SR Legacy download, CSV or JSON, as a zip file or extracted (it has foods.csv)",
		},
		// The Foundation Foods CSV download of October 2026 has the samples
		// and lab methods, but not the foods or their nutrients.
		"csv without foods": {
			fsys: fstest.MapFS{
				"FoodData_Central_foundation_food_csv_2026-10-29/foundation_food.csv":     {},
				"FoodData_Central_foundation_food_csv_2026-10-29/sub_sample_result.csv":   {},
				"FoodData_Central_foundation_food_csv_2026-10-29/nutrient.csv":            {},
				"FoodData_Central_foundation_food_csv_2026-10-29/acquisition_samples.csv": {},
			},
			wantErr: "no food.csv or JSON file found",
		},
		"json not an object": {
			fsys:    fstest.MapFS{"dl.json": {Data: []byte(`[]`)}},
			wantErr: "dl.json isn't an FDC JSON download",
		},
		"json without foods": {
			fsys:    fstest.MapFS{"dl.json": {Data: []byte(`{"other": [1, 2]}`)}},
			wantErr: "dl.json has no foods; give the Foundation Foods or SR Legacy JSON download",
		},
		"json wrong unit": {
			fsys: fstest.MapFS{"dl.json": {Data: []byte(`{"SRLegacyFoods": [{"fdcId": 1, "dataType": "SR Legacy",
				"foodNutrients": [{"nutrient": {"id": 1003, "name": "Protein", "unitName": "mg"}, "amount": 2.9}]}]}`)}},
			wantErr: "dl.json, food 1: nutrient 1003 (Protein) is in mg, want G",
		},
		"wrong unit": {
			fsys:    dataset("id,name,unit_name\n1003,Protein,MG\n"),
			wantErr: "nutrient 1003 (Protein) is in MG, want G",
		},
		"missing file": {
			fsys: fstest.MapFS{
				"food.csv": {Data: []byte("fdc_id\n")},
			},
			wantErr: "failed to open food_category.csv",
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := Read(tt.fsys)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got error %v, want %q", err, tt.wantErr)
			}
		})
	}

	// Without portions or measure units, foods are read without portions.
	fdcFoods, _, err := Read(dataset("id,name,unit_name\n1003,Protein,G\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(fdcFoods) != 1 || *fdcFoods[0].Per100Grams[nutrition.ProteinGrams] != 2.9 || fdcFoods[0].Portions != nil {
		t.Errorf("got %+v, want kale with its protein", fdcFoods)
	}
}

func TestImport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := storetest.New(t)
	repo := foods.NewRepository(slog.New(slog.NewTextHandler(io.Discard, nil)), db)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Both datasets are imported, one extracted and the other zipped.
	if err := Import(ctx, log, repo, srLegacyDir, zipDir(t, foundationDir)); err != nil {
		t.Fatalf("Import: %v", err)
	}
	// Importing again replaces the foods, and the JSON download of the same
	// Foundation foods replaces them without adding any.
	if err := Import(ctx, log, repo, srLegacyDir, zipDir(t, foundationJSONDir)); err != nil {
		t.Fatalf("Import again: %v", err)
	}
	var count int
	if err := db.QueryRow(ctx, "select count(*) from reference_foods where source = 'fdc'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 7 {
		t.Errorf("got %d FDC foods, want 7", count)
	}

	chickpeas, err := repo.GetReferenceFood(ctx, foods.SourceFDC, 173756)
	if err != nil {
		t.Fatalf("GetReferenceFood: %v", err)
	}
	if got := chickpeas.Per100Grams[nutrition.ProteinGrams]; got == nil || *got != 20.47 || chickpeas.Per100Grams[nutrition.IodineMicrograms] != nil {
		t.Errorf("got chickpeas %+v, want their protein and no iodine", chickpeas)
	}
	if len(chickpeas.Portions) != 1 || chickpeas.PublicationDate.IsZero() {
		t.Errorf("got chickpeas %+v, want their portion and publication date", chickpeas)
	}

	tests := map[string]struct {
		query string
		want  []int32
	}{
		// Words are stemmed.
		"stemmed":    {query: "chickpea", want: []int32{173756}},
		"misspelled": {query: "spinnach", want: []int32{168462}},
		"partial":    {query: "chick", want: []int32{173756}},
		// The onions named onions come before others.
		"several":  {query: "onions raw", want: []int32{170000, 2346393}},
		"excluded": {query: "beef", want: nil},
		"none":     {query: "durian", want: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			matches, err := repo.SearchReferenceFoods(ctx, pgtype.UUID{}, tt.query, 10)
			if err != nil {
				t.Fatalf("SearchReferenceFoods: %v", err)
			}
			var got []int32
			for _, m := range matches {
				got = append(got, m.SourceID)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("SearchReferenceFoods(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}
