package food

import (
	"strings"
	"testing"

	foodsv1 "github.com/bs-tools/foodplann.ing/internal/gen/foods/v1"
	"github.com/bs-tools/foodplann.ing/internal/nutrition"
)

// TestNutrientEnum checks that the API's Nutrient values name the
// nutrients in their order, as NutrientToProto converts them by number.
func TestNutrientEnum(t *testing.T) {
	t.Parallel()
	for n := range nutrition.Count {
		p := NutrientToProto(n)
		if want := "NUTRIENT_" + strings.ToUpper(n.String()); p.String() != want {
			t.Errorf("NutrientToProto(%v) = %v, want %v", n, p, want)
		}
		if got, ok := NutrientFromProto(p); !ok || got != n {
			t.Errorf("NutrientFromProto(%v) = %v, %v, want %v", p, got, ok, n)
		}
	}
	values := foodsv1.Nutrient_NUTRIENT_UNSPECIFIED.Descriptor().Values()
	if values.Len() != int(nutrition.Count)+1 {
		t.Errorf("Nutrient has %d values, want %d", values.Len(), nutrition.Count+1)
	}
	for _, p := range []foodsv1.Nutrient{foodsv1.Nutrient_NUTRIENT_UNSPECIFIED, foodsv1.Nutrient(99)} {
		if _, ok := NutrientFromProto(p); ok {
			t.Errorf("NutrientFromProto(%v) is a nutrient", p)
		}
	}
}
