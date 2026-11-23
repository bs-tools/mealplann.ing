package bowls

import (
	"testing"

	"github.com/bs-tools/mealplann.ing/internal/foods"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
)

// TestFoodRole checks the roles of the household's foods in a bowl, by
// their names, store sections and energy.
func TestFoodRole(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		section foods.StoreSection
		kcal    float64
		want    Role
	}{
		{"Basmati rice", foods.StoreSectionDryGoods, 360, RoleCarb},
		{"Gluten-free pasta, dry (rice & corn)", foods.StoreSectionDryGoods, 360, RoleCarb},
		{"Couscous", foods.StoreSectionDryGoods, 376, RoleCarb},
		{"Brown rice flour", foods.StoreSectionDryGoods, 363, RoleUnspecified},
		{"Brown rice tortillas", foods.StoreSectionFrozen, 220, RoleUnspecified},
		{"Apple cider vinegar", foods.StoreSectionDryGoods, 21, RoleUnspecified},
		{"Instant noodle cup, vegetable", foods.StoreSectionDryGoods, 450, RoleUnspecified},
		{"Black beans, canned", foods.StoreSectionCanned, 91, RoleProtein},
		{"Black beans, dried", foods.StoreSectionDryGoods, 341, RoleUnspecified},
		{"Brown lentils", foods.StoreSectionDryGoods, 352, RoleProtein},
		{"Butter beans, canned", foods.StoreSectionCanned, 80, RoleProtein},
		{"Tofu, extra firm", foods.StoreSectionChilled, 145, RoleProtein},
		{"Edamame, shelled", foods.StoreSectionFrozen, 121, RoleProtein},
		{"Chickpea flour", foods.StoreSectionDryGoods, 387, RoleUnspecified},
		{"Green beans", foods.StoreSectionProduce, 31, RoleVegetable},
		{"Broccoli sprouts", foods.StoreSectionProduce, 35, RoleVegetable},
		{"Baby spinach", foods.StoreSectionProduce, 23, RoleVegetable},
		{"Avocado", foods.StoreSectionProduce, 160, RoleVegetable},
		{"Corn kernels, frozen", foods.StoreSectionFrozen, 88, RoleVegetable},
		{"Green bell pepper", foods.StoreSectionProduce, 20, RoleVegetable},
		{"Cherry tomatoes", foods.StoreSectionProduce, 18, RoleVegetable},
		{"Garlic", foods.StoreSectionProduce, 149, RoleUnspecified},
		{"Cilantro", foods.StoreSectionProduce, 23, RoleUnspecified},
		{"Jalapeño", foods.StoreSectionProduce, 29, RoleUnspecified},
		{"Black pepper", foods.StoreSectionSpices, 251, RoleUnspecified},
		{"Corn tortillas", foods.StoreSectionBakery, 218, RoleUnspecified},
		{"Crushed tomatoes, canned", foods.StoreSectionCanned, 32, RoleUnspecified},
		{"Carrot coconut ginger soup (Vancouver Soup Co.)", foods.StoreSectionChilled, 62, RoleUnspecified},
		{"Dried mushrooms", foods.StoreSectionDryGoods, 296, RoleUnspecified},
		{"Hummus", foods.StoreSectionChilled, 166, RoleSauce},
		{"Hoisin sauce", foods.StoreSectionCanned, 220, RoleSauce},
		{"Italian dressing", foods.StoreSectionCanned, 240, RoleSauce},
		{"Soy sauce (tamari)", foods.StoreSectionCanned, 60, RoleUnspecified},
		{"Chili garlic sauce", foods.StoreSectionCanned, 90, RoleUnspecified},
		{"Peanut butter", foods.StoreSectionDryGoods, 590, RoleUnspecified},
	} {
		f := &foods.Food{Name: tt.name, StoreSection: tt.section, Per100Grams: nutrition.Nutrients{nutrition.EnergyKcal: new(tt.kcal)}}
		if got := foodRole(f); got != tt.want {
			t.Errorf("foodRole(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
