package cnf

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/bs-tools/foodplann.ing/internal/foodimport"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
)

// source is a CNF nutrient a nutrient the app tracks is read from. CNF's
// NutrientIDs are believed to be USDA's SR nutrient numbers, which CNF
// started from, but its documentation doesn't promise it, so each is checked
// against its name or symbol and its unit in NUTRIENT NAME.csv before its
// amounts are read: a release that differs fails to import instead of
// importing wrong amounts.
type source struct {
	// id is the NutrientID in NUTRIENT NAME.csv and NUTRIENT AMOUNT.csv.
	id int
	// nutrient is the nutrient the app tracks.
	nutrient nutrition.Nutrient
	// units are the units it may be in, normalized with
	// foodimport.NormalizeUnit.
	units []string
	// want describes the nutrient expected, for errors.
	want string
	// names are the words of the nutrient's name or symbol, any of whose
	// lists must all be in them, ignoring case. Words of letters only, up
	// to 4 of them, must be whole words, so "rae" doesn't match "trae";
	// others, like "18:3" or "vitamin d", may be part of a word.
	names [][]string
	// required is whether NUTRIENT NAME.csv must have the nutrient. Energy
	// and the macronutrients are, so a release that numbers its nutrients
	// differently fails to import, rather than importing foods without
	// them.
	required bool
}

// Units, normalized with foodimport.NormalizeUnit.
var (
	grams       = []string{"g"}
	milligrams  = []string{"mg"}
	micrograms  = []string{"ug"}
	kilocalorie = []string{"kcal"}
)

// sources are the CNF nutrients of the nutrients the app tracks, in order of
// preference for nutrients with several.
//
//	NutrientID  Nutrient                       Unit       Name (CNF 2015)
//	208         energy_kcal                    kCal       ENERGY (KILOCALORIES)
//	203         protein_grams                  g          PROTEIN
//	205         carbohydrate_grams             g          CARBOHYDRATE, TOTAL (BY DIFFERENCE)
//	269         sugars_grams                   g          SUGARS, TOTAL
//	204         fat_grams                      g          FAT (TOTAL LIPIDS)
//	291         fiber_grams                    g          FIBRE, TOTAL DIETARY
//	418         vitamin_b12_micrograms         µg         VITAMIN B-12
//	303         iron_milligrams                mg         IRON
//	301         calcium_milligrams             mg         CALCIUM
//	328         vitamin_d_micrograms           µg         VITAMIN D (D2 + D3)
//	314         iodine_micrograms              µg         IODIDE (absent from CNF 2015, as far as known)
//	309         zinc_milligrams                mg         ZINC
//	851         omega3_ala_grams               g          18:3 c,c,c n-3 LINOLENIC
//	619         omega3_ala_grams (fallback)    g          18:3 UNDIFFERENTIATED, LINOLENIC
//	435         folate_micrograms              DFE or µg  DIETARY FOLATE EQUIVALENTS
//	320         vitamin_a_micrograms           RAE or µg  RETINOL ACTIVITY EQUIVALENTS
//	323         vitamin_e_milligrams           mg         ALPHA-TOCOPHEROL
//	430         vitamin_k_micrograms           µg         VITAMIN K
//	312         copper_milligrams              mg         COPPER
//	304         magnesium_milligrams           mg         MAGNESIUM
//	307         sodium_milligrams              mg         SODIUM
//	306         potassium_milligrams           mg         POTASSIUM
//
// CNF also has vitamin D in IU (324), which is left out for 328 in
// micrograms, and 18:3 undifferentiated (619), which in plants is almost all
// ALA, for foods without 851.
var sources = []source{
	{id: 208, nutrient: nutrition.EnergyKcal, units: kilocalorie, want: "energy", names: [][]string{{"energy"}}, required: true},
	{id: 203, nutrient: nutrition.ProteinGrams, units: grams, want: "protein", names: [][]string{{"protein"}}, required: true},
	{id: 205, nutrient: nutrition.CarbohydrateGrams, units: grams, want: "total carbohydrate", names: [][]string{{"carbohydrate"}}, required: true},
	{id: 269, nutrient: nutrition.SugarsGrams, units: grams, want: "total sugars", names: [][]string{{"sugars"}, {"sugar"}}},
	{id: 204, nutrient: nutrition.FatGrams, units: grams, want: "total fat", names: [][]string{{"fat"}, {"lipids"}}, required: true},
	{id: 291, nutrient: nutrition.FiberGrams, units: grams, want: "dietary fiber", names: [][]string{{"fiber"}, {"fibre"}}},
	{id: 418, nutrient: nutrition.VitaminB12Micrograms, units: micrograms, want: "vitamin B12", names: [][]string{{"b-12"}, {"b12"}, {"cobalamin"}}},
	{id: 303, nutrient: nutrition.IronMilligrams, units: milligrams, want: "iron", names: [][]string{{"iron"}}},
	{id: 301, nutrient: nutrition.CalciumMilligrams, units: milligrams, want: "calcium", names: [][]string{{"calcium"}}},
	{id: 328, nutrient: nutrition.VitaminDMicrograms, units: micrograms, want: "vitamin D (D2 + D3)", names: [][]string{{"vitamin d"}, {"calciferol"}}},
	{id: 314, nutrient: nutrition.IodineMicrograms, units: micrograms, want: "iodine", names: [][]string{{"iodine"}, {"iodide"}}},
	{id: 309, nutrient: nutrition.ZincMilligrams, units: milligrams, want: "zinc", names: [][]string{{"zinc"}}},
	{id: 851, nutrient: nutrition.Omega3ALAGrams, units: grams, want: "18:3 n-3 (ALA)", names: [][]string{{"18:3", "n-3"}, {"18:3", "n3"}, {"18:3", "linolenic"}}},
	{id: 619, nutrient: nutrition.Omega3ALAGrams, units: grams, want: "18:3 (linolenic acid)", names: [][]string{{"18:3"}}},
	{id: 435, nutrient: nutrition.FolateMicrograms, units: []string{"ug", "dfe", "ugdfe"}, want: "folate as dietary folate equivalents (DFE)", names: [][]string{{"folate", "dfe"}, {"dietary folate"}}},
	{id: 320, nutrient: nutrition.VitaminAMicrograms, units: []string{"ug", "rae", "ugrae"}, want: "vitamin A as retinol activity equivalents (RAE)", names: [][]string{{"retinol activity"}, {"rae"}}},
	{id: 323, nutrient: nutrition.VitaminEMilligrams, units: milligrams, want: "alpha-tocopherol (vitamin E)", names: [][]string{{"alpha-tocopherol"}, {"alpha tocopherol"}, {"vitamin e"}}},
	{id: 430, nutrient: nutrition.VitaminKMicrograms, units: micrograms, want: "vitamin K", names: [][]string{{"vitamin k"}, {"phylloquinone"}}},
	{id: 312, nutrient: nutrition.CopperMilligrams, units: milligrams, want: "copper", names: [][]string{{"copper"}}},
	{id: 304, nutrient: nutrition.MagnesiumMilligrams, units: milligrams, want: "magnesium", names: [][]string{{"magnesium"}}},
	{id: 307, nutrient: nutrition.SodiumMilligrams, units: milligrams, want: "sodium", names: [][]string{{"sodium"}}},
	{id: 306, nutrient: nutrition.PotassiumMilligrams, units: milligrams, want: "potassium", names: [][]string{{"potassium"}}},
}

// nutrientName is a row of NUTRIENT NAME.csv.
type nutrientName struct {
	id     int
	symbol string
	unit   string
	name   string
	// tagname is INFOODS' tag, like "PROCNT" for protein, which some
	// releases have.
	tagname string
}

// check checks that a CNF nutrient is the one the importer reads it as, in
// the unit it expects.
func (s *source) check(n nutrientName) error {
	text := strings.ToLower(n.name + " " + n.symbol + " " + n.tagname)
	matches := slices.ContainsFunc(s.names, func(words []string) bool {
		for _, w := range words {
			if !containsWord(text, w) {
				return false
			}
		}
		return true
	})
	unitOK := slices.Contains(s.units, foodimport.NormalizeUnit(n.unit))
	if matches && unitOK {
		return nil
	}
	return fmt.Errorf("CNF nutrient %d is %q (symbol %q) in %q, but the importer reads it as %s in %s: "+
		"this release of the Canadian Nutrient File differs from what the importer knows, so update the nutrient mapping in internal/cnf",
		n.id, n.name, n.symbol, n.unit, s.want, strings.Join(s.units, " or "))
}

// containsWord reports whether text has word: as a whole word for short
// words of letters, which could be part of another, and anywhere for
// others.
func containsWord(text, word string) bool {
	short := len(word) <= 4 && !strings.ContainsFunc(word, func(r rune) bool { return !unicode.IsLetter(r) })
	if !short {
		return strings.Contains(text, word)
	}
	return slices.Contains(strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), word)
}

// checkNutrients checks the CNF nutrients the importer reads, returning the
// IDs of those to read amounts of.
func checkNutrients(names map[int]nutrientName) (map[int]*source, error) {
	read := map[int]*source{}
	for i := range sources {
		s := &sources[i]
		n, ok := names[s.id]
		if !ok {
			if s.required {
				return nil, fmt.Errorf("NUTRIENT NAME.csv has no nutrient %d, %s: "+
					"this release of the Canadian Nutrient File may number its nutrients differently, so update the nutrient mapping in internal/cnf", s.id, s.want)
			}
			continue
		}
		if err := s.check(n); err != nil {
			return nil, err
		}
		read[s.id] = s
	}
	return read, nil
}

// setNutrients sets a food's nutrients from the amounts of its CNF
// nutrients, by NutrientID, each from the first of its sources the food
// has.
func setNutrients(per100Grams *nutrition.Nutrients, amounts map[int]float64) {
	for _, s := range sources {
		if per100Grams[s.nutrient] != nil {
			continue
		}
		if v, ok := amounts[s.id]; ok {
			per100Grams[s.nutrient] = new(v)
		}
	}
}
