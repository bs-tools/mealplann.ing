package foodimport

import (
	"strings"
	"testing"
)

func TestIsExcludedCategory(t *testing.T) {
	t.Parallel()
	tests := map[string]bool{
		// FDC's names.
		"Beef Products":                  true,
		"Lamb, Veal, and Game Products":  true,
		"Sausages and Luncheon Meats":    true,
		"Finfish and Shellfish Products": true,
		"Dairy and Egg Products":         true,
		// CNF's names.
		"Lamb, Veal and Game":         true,
		"Sausages and Luncheon meats": true,
		"POULTRY PRODUCTS":            true,
		"Pork Products":               true,
		// Plant categories are kept.
		"Legumes and Legume Products":       false,
		"Vegetables and Vegetable Products": false,
		"Fats and Oils":                     false,
		"Nuts and Seeds":                    false,
		"":                                  false,
	}
	for category, want := range tests {
		if got := IsExcludedCategory(category); got != want {
			t.Errorf("IsExcludedCategory(%q) = %v, want %v", category, got, want)
		}
	}
}

func TestNormalizeUnit(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"µg":          "ug",
		"μg":          "ug",
		"UG":          "ug",
		"mcg":         "ug",
		"kCal":        "kcal",
		"KCAL":        "kcal",
		" g ":         "g",
		"µg DFE":      "ugdfe",
		"µg_RAE":      "ugrae",
		"gram":        "g",
		"Milligrams":  "mg",
		"microgram":   "ug",
		"kilocalorie": "kcal",
		"kilojoule":   "kj",
	}
	for unit, want := range tests {
		if got := NormalizeUnit(unit); got != want {
			t.Errorf("NormalizeUnit(%q) = %q, want %q", unit, got, want)
		}
	}
}

func TestReadCSV(t *testing.T) {
	t.Parallel()
	// Column names are matched ignoring case, a byte order mark and
	// spaces, and blank lines are skipped.
	input := "\xef\xbb\xbfFoodID, FoodDescription\r\n1, Kale \r\n\r\n2,Tofu\r\n"
	var got []string
	if err := ReadCSV(strings.NewReader(input), "food.csv", func(r Record) error {
		got = append(got, r.Get("foodid")+"="+r.Get("FOODDESCRIPTION")+r.Get("missing"))
		return nil
	}); err != nil {
		t.Fatalf("ReadCSV: %v", err)
	}
	if strings.Join(got, ";") != "1=Kale;2=Tofu" {
		t.Errorf("got %v, want 1=Kale;2=Tofu", got)
	}
}
