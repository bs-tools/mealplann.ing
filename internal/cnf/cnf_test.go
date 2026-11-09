package cnf

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

// downloadDir is a small download in CNF 2015's layout, written for the
// tests as canada.ca's files can't be fetched here: a fortified soy
// beverage and margarine, canned chickpeas, an apple, and lean ground beef,
// which is left out. Its FOOD NAME.csv is in Windows-1252, its NUTRIENT
// NAME.csv in UTF-8 with a byte order mark, and its lines end in CRLF.
const downloadDir = "testdata/cnf_2015"

// download2026Dir is the same download in CNF 2026's layout, with its files
// and columns renamed, its units spelled out, its measures' weights in
// grams, and refuse and yield weights, which aren't portions. Its files
// are in UTF-8 with a byte order mark.
const download2026Dir = "testdata/cnf_2026"

// IDs of the fixture's foods.
const (
	margarineID = 102
	appleID     = 1520
	beefID      = 1931
	chickpeasID = 3183
	soyID       = 6288
)

// zipDir zips a directory into a file, under a directory like CNF's zip
// files, and returns its path.
func zipDir(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cnf-fcen-csv.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		fw, err := w.Create("CNF 2015/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func byID(list []*foods.ReferenceFood) map[int32]*foods.ReferenceFood {
	m := map[int32]*foods.ReferenceFood{}
	for _, f := range list {
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

// checkDownload checks the foods read from the fixture.
func checkDownload(t *testing.T, list []*foods.ReferenceFood, stats Stats) {
	t.Helper()
	if stats.Foods != 4 || stats.Excluded != 1 || len(list) != 4 {
		t.Fatalf("got %d foods and stats %+v, want 4 foods and the beef excluded", len(list), stats)
	}
	if !slices.IsSortedFunc(list, func(a, b *foods.ReferenceFood) int { return int(a.SourceID - b.SourceID) }) {
		t.Error("foods aren't ordered by FoodID")
	}
	m := byID(list)
	if m[beefID] != nil {
		t.Error("the beef was read")
	}

	soy := m[soyID]
	if soy == nil {
		t.Fatal("no soy beverage")
	}
	// The French description is decoded from Windows-1252, and a food
	// without a publication date has its date of entry.
	if soy.Source != foods.SourceCNF || soy.DataType != "cnf" || soy.Description != "Soy beverage, original, fortified" ||
		soy.FrenchDescription != "Boisson de soya, originale, enrichie" || soy.Category != "Legumes and Legume Products" ||
		soy.PublicationDate != time.Date(2009, 3, 1, 0, 0, 0, 0, time.UTC) {
		t.Errorf("got soy beverage %+v", soy)
	}
	// Canadian fortification: vitamin D in micrograms, not IU, and B12.
	wantNutrient(t, soy, nutrition.VitaminDMicrograms, new(1.03))
	wantNutrient(t, soy, nutrition.VitaminB12Micrograms, new(0.39))
	wantNutrient(t, soy, nutrition.CalciumMilligrams, new(123.0))
	wantNutrient(t, soy, nutrition.EnergyKcal, new(41.0))
	wantNutrient(t, soy, nutrition.FolateMicrograms, new(7.0))
	wantNutrient(t, soy, nutrition.VitaminAMicrograms, new(41.0))
	// Without 18:3 n-3, ALA is the undifferentiated 18:3.
	wantNutrient(t, soy, nutrition.Omega3ALAGrams, new(0.11))
	wantNutrient(t, soy, nutrition.IodineMicrograms, nil)
	wantPortions := []foods.Portion{
		{Description: "250ml", Amount: 250, Grams: 258},
		{Description: "15ml", Amount: 15, Grams: 15.5},
		{Description: "100g", Amount: 100, Grams: 100},
	}
	if !slices.Equal(soy.Portions, wantPortions) {
		t.Errorf("got soy beverage portions %+v, want %+v", soy.Portions, wantPortions)
	}
	// The largest volume gives the density, and weights aren't pieces.
	if got := soy.SuggestGramsPerMilliliter(); got != 1.032 {
		t.Errorf("soy beverage's density = %v, want 1.032", got)
	}
	if got := soy.SuggestGramsPerPiece(); got != 0 {
		t.Errorf("soy beverage weighs %v g a piece, want none", got)
	}

	chickpeas := m[chickpeasID]
	if chickpeas.FrenchDescription != "Pois chiches, grains entiers mûrs, en conserve" ||
		chickpeas.PublicationDate != time.Date(2015, 5, 6, 0, 0, 0, 0, time.UTC) {
		t.Errorf("got chickpeas %+v", chickpeas)
	}
	// 18:3 n-3 is preferred over 18:3, and a zero is known.
	wantNutrient(t, chickpeas, nutrition.Omega3ALAGrams, new(0.05))
	wantNutrient(t, chickpeas, nutrition.VitaminB12Micrograms, new(0.0))
	wantNutrient(t, chickpeas, nutrition.FiberGrams, new(6.4))
	wantNutrient(t, chickpeas, nutrition.VitaminDMicrograms, nil)
	if got := chickpeas.SuggestGramsPerMilliliter(); got != 1.012 {
		t.Errorf("chickpeas' density = %v, want 253 g per 250 ml, 1.012", got)
	}

	apple := m[appleID]
	// Dates are read in other layouts too, and a nutrient without a value
	// is unknown.
	if apple.PublicationDate != time.Date(2015, 5, 6, 0, 0, 0, 0, time.UTC) || apple.Category != "Fruits and fruit juices" {
		t.Errorf("got apple %+v", apple)
	}
	wantNutrient(t, apple, nutrition.VitaminDMicrograms, nil)
	wantNutrient(t, apple, nutrition.VitaminKMicrograms, new(2.2))
	if got := apple.SuggestGramsPerPiece(); got != 182 {
		t.Errorf("an apple weighs %v g, want a medium one's 182 g", got)
	}

	margarine := m[margarineID]
	wantNutrient(t, margarine, nutrition.VitaminDMicrograms, new(13.3))
	wantNutrient(t, margarine, nutrition.VitaminEMilligrams, new(10.2))
	if got := margarine.SuggestGramsPerMilliliter(); got != 0.933 {
		t.Errorf("margarine's density = %v, want 14 g per 15 ml, 0.933", got)
	}
}

func TestRead(t *testing.T) {
	t.Parallel()
	list, stats, err := Read(os.DirFS(downloadDir))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	checkDownload(t, list, stats)
}

func TestReadZip(t *testing.T) {
	t.Parallel()
	z, err := zip.OpenReader(zipDir(t, downloadDir))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	list, stats, err := Read(z)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	checkDownload(t, list, stats)
}

func TestRead2026(t *testing.T) {
	t.Parallel()
	list, stats, err := Read(os.DirFS(download2026Dir))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	checkDownload(t, list, stats)

	z, err := zip.OpenReader(zipDir(t, download2026Dir))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	list, stats, err = Read(z)
	if err != nil {
		t.Fatalf("Read zip: %v", err)
	}
	checkDownload(t, list, stats)
}

// TestRead2026WithoutMeasureTypes reads CNF 2026 without its Measure Type
// file, which leaves no way to tell portions from refuse and yield weights,
// so its foods have no portions.
func TestRead2026WithoutMeasureTypes(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{}
	entries, err := os.ReadDir(download2026Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "Measure Type.csv" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(download2026Dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		fsys[e.Name()] = &fstest.MapFile{Data: data}
	}
	list, _, err := Read(fsys)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for _, f := range list {
		if f.Portions != nil || f.Per100Grams[nutrition.EnergyKcal] == nil {
			t.Errorf("got %s with portions %+v, want its nutrients and no portions", f.Description, f.Portions)
		}
	}
}

// TestReadOddNames reads the fixture with its files named in other ways,
// in a directory, and their columns in other cases.
func TestReadOddNames(t *testing.T) {
	t.Parallel()
	names := map[string]string{
		"FOOD NAME.csv":         "Food_Name.CSV",
		"FOOD GROUP.csv":        "foodgroup.csv",
		"NUTRIENT NAME.csv":     "Nutrient Name.csv",
		"NUTRIENT AMOUNT.csv":   "NUTRIENT_AMOUNT.csv",
		"CONVERSION FACTOR.csv": "conversion-factor.csv",
		"MEASURE NAME.csv":      "MEASURE  NAME.csv",
	}
	fsys := fstest.MapFS{
		// Files of other tables, and macOS's metadata, are skipped.
		"__MACOSX/cnf/._FOOD NAME.csv": {Data: []byte("\x00\x05")},
		"cnf/README.txt":               {Data: []byte("Canadian Nutrient File")},
	}
	for name, odd := range names {
		data, err := os.ReadFile(filepath.Join(downloadDir, name))
		if err != nil {
			t.Fatal(err)
		}
		header, rest, _ := strings.Cut(string(data), "\r\n")
		if name == "FOOD NAME.csv" {
			header = strings.ToLower(header)
		} else {
			header = strings.ToUpper(header)
		}
		fsys["cnf/"+odd] = &fstest.MapFile{Data: []byte(header + "\n" + rest)}
	}
	list, stats, err := Read(fsys)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	checkDownload(t, list, stats)
}

func TestReadWithoutPortions(t *testing.T) {
	t.Parallel()
	fsys := fstest.MapFS{}
	for _, name := range []string{"FOOD NAME.csv", "FOOD GROUP.csv", "NUTRIENT NAME.csv", "NUTRIENT AMOUNT.csv"} {
		data, err := os.ReadFile(filepath.Join(downloadDir, name))
		if err != nil {
			t.Fatal(err)
		}
		fsys[name] = &fstest.MapFile{Data: data}
	}
	list, _, err := Read(fsys)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(list) != 4 || list[0].Portions != nil || list[0].Per100Grams[nutrition.EnergyKcal] == nil {
		t.Errorf("got %+v, want 4 foods with nutrients and without portions", list)
	}
}

func TestReadErrors(t *testing.T) {
	t.Parallel()
	const nutrientHeader = "NutrientID,NutrientCode,NutrientSymbol,NutrientUnit,NutrientName,NutrientNameF,Tagname,NutrientDecimals\n"
	// macros are the required nutrients, as CNF names them.
	const macros = "203,203,PROT,g,PROTEIN,,PROCNT,2\n" +
		"204,204,FAT,g,FAT (TOTAL LIPIDS),,FAT,2\n" +
		"205,205,CARB,g,\"CARBOHYDRATE, TOTAL (BY DIFFERENCE)\",,CHOCDF,2\n" +
		"208,208,KCAL,kCal,ENERGY (KILOCALORIES),,ENERC_KCAL,0\n"
	download := func(nutrients, foodGroups string) fstest.MapFS {
		return fstest.MapFS{
			"FOOD NAME.csv":       {Data: []byte("FoodID,FoodGroupID,FoodDescription\n1,11,\"Kale, raw\"\n")},
			"FOOD GROUP.csv":      {Data: []byte("FoodGroupID,FoodGroupName\n" + foodGroups)},
			"NUTRIENT NAME.csv":   {Data: []byte(nutrientHeader + nutrients)},
			"NUTRIENT AMOUNT.csv": {Data: []byte("FoodID,NutrientID,NutrientValue\n1,203,2.9\n")},
		}
	}
	const vegetables = "11,Vegetables and Vegetable Products\n"
	tests := map[string]struct {
		fsys    fstest.MapFS
		wantErr string
	}{
		"not a download": {
			fsys:    fstest.MapFS{"dl/food.csv": {}, "dl/nutrient.csv": {}},
			wantErr: "no FOOD NAME.csv or FOOD GROUP.csv (CNF Food Group.csv in 2026) or NUTRIENT NAME.csv or NUTRIENT AMOUNT.csv found; give the Canadian Nutrient File's CSV download, as a zip file or extracted (it has food.csv, nutrient.csv)",
		},
		"no food groups": {
			fsys: func() fstest.MapFS {
				fsys := download(macros, vegetables)
				delete(fsys, "FOOD GROUP.csv")
				return fsys
			}(),
			wantErr: "no FOOD GROUP.csv (CNF Food Group.csv in 2026) found",
		},
		"unknown food group": {
			fsys:    download(macros, "12,Nuts and Seeds\n"),
			wantErr: `FOOD NAME.csv, line 2: food 1 is in food group "11", which the food group file doesn't have`,
		},
		// A release that numbers its nutrients differently.
		"renumbered": {
			fsys: download("1,1,PROT,g,PROTEIN,,PROCNT,2\n"+
				"2,2,FAT,g,FAT (TOTAL LIPIDS),,FAT,2\n", vegetables),
			wantErr: "NUTRIENT NAME.csv has no nutrient 208, energy: this release of the Canadian Nutrient File may number its nutrients differently",
		},
		"wrong nutrient": {
			fsys:    download(strings.Replace(macros, "203,203,PROT,g,PROTEIN", "203,203,ASH,g,ASH", 1), vegetables),
			wantErr: `CNF nutrient 203 is "ASH" (symbol "ASH") in "g", but the importer reads it as protein in g`,
		},
		"wrong unit": {
			fsys:    download(macros+"328,328,D-IU,IU,VITAMIN D (INTERNATIONAL UNITS),,VITD,0\n", vegetables),
			wantErr: `CNF nutrient 328 is "VITAMIN D (INTERNATIONAL UNITS)" (symbol "D-IU") in "IU", but the importer reads it as vitamin D (D2 + D3) in ug`,
		},
		"folate not DFE": {
			fsys:    download(macros+"435,435,FOL,µg,\"FOLATE, TOTAL\",,FOL,0\n", vegetables),
			wantErr: `CNF nutrient 435 is "FOLATE, TOTAL" (symbol "FOL") in "µg", but the importer reads it as folate as dietary folate equivalents (DFE)`,
		},
		"retinol not RAE": {
			fsys:    download(macros+"320,320,RETOL,µg,RETINOL,,RETOL,0\n", vegetables),
			wantErr: `CNF nutrient 320 is "RETINOL"`,
		},
		"invalid amount": {
			fsys: func() fstest.MapFS {
				fsys := download(macros, vegetables)
				fsys["NUTRIENT AMOUNT.csv"] = &fstest.MapFile{Data: []byte("FoodID,NutrientID,NutrientValue\n1,203,lots\n")}
				return fsys
			}(),
			wantErr: "NUTRIENT AMOUNT.csv, line 2: invalid amount of nutrient 203 of food 1",
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

	// Units and names are matched ignoring case and the micro sign's
	// spelling, and mcg is micrograms.
	list, _, err := Read(download(macros+
		"418,418,B12,mcg,Vitamin B12,,VITB12,2\n"+
		"328,328,VITD,µg,Vitamin D (D2 + D3),,VITDD,1\n"+
		"208,208,KCAL,KCAL,Energy (kcal),,ENERC_KCAL,0\n", vegetables))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(list) != 1 || *list[0].Per100Grams[nutrition.ProteinGrams] != 2.9 {
		t.Errorf("got %+v, want kale with its protein", list)
	}
}

func TestPortionAmount(t *testing.T) {
	t.Parallel()
	tests := map[string]float64{
		"250ml":     250,
		"15 ml":     15,
		"1 medium":  1,
		"medium":    1,
		"100g":      100,
		"1/2 cup":   0.5,
		"1 1/2 cup": 1.5,
		"0.5 cup":   0.5,
		"0 slices":  1,
	}
	for description, want := range tests {
		if got := portionAmount(description); got != want {
			t.Errorf("portionAmount(%q) = %v, want %v", description, got, want)
		}
	}
}

func TestImport(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	db := storetest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	repo := foods.NewRepository(log, db)

	// Importing the zip file and then the directory upserts the same
	// foods.
	if err := Import(ctx, log, repo, zipDir(t, downloadDir)); err != nil {
		t.Fatalf("Import: %v", err)
	}
	if err := Import(ctx, log, repo, downloadDir); err != nil {
		t.Fatalf("Import again: %v", err)
	}
	var count int
	if err := db.QueryRow(ctx, "select count(*) from reference_foods where source = 'cnf'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Errorf("got %d CNF foods, want 4", count)
	}

	soy, err := repo.GetReferenceFood(ctx, foods.SourceCNF, soyID)
	if err != nil {
		t.Fatalf("GetReferenceFood: %v", err)
	}
	if got := soy.Per100Grams[nutrition.VitaminDMicrograms]; got == nil || *got != 1.03 ||
		soy.FrenchDescription != "Boisson de soya, originale, enrichie" || len(soy.Portions) != 3 {
		t.Errorf("got soy beverage %+v, want its vitamin D, French description and portions", soy)
	}
	if _, err := repo.GetReferenceFood(ctx, foods.SourceFDC, soyID); err == nil {
		t.Error("got the soy beverage as an FDC food")
	}

	tests := map[string]struct {
		query string
		want  []int32
	}{
		"stemmed":   {query: "chickpea", want: []int32{chickpeasID}},
		"synonym":   {query: "garbanzos", want: []int32{chickpeasID}},
		"fortified": {query: "soy beverage", want: []int32{soyID}},
		"excluded":  {query: "beef", want: nil},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			matches, err := repo.SearchReferenceFoods(ctx, pgtype.UUID{}, tt.query, 10)
			if err != nil {
				t.Fatalf("SearchReferenceFoods: %v", err)
			}
			var got []int32
			for _, m := range matches {
				if m.Source != foods.SourceCNF {
					t.Errorf("got %v food %d", m.Source, m.SourceID)
				}
				got = append(got, m.SourceID)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("SearchReferenceFoods(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}
