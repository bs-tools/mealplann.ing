// Package nutrition computes nutrition from the nutrients of foods per
// 100 g: the nutrients the app tracks, weighing amounts in milliliters and
// pieces, and the nutrition of recipes. It has no database or API, so that
// recipes, and later plans and shopping lists, all compute the same way.
package nutrition

// Nutrient is a nutrient the app tracks.
type Nutrient int

// The nutrients, in the order of the API's Nutrients message.
const (
	EnergyKcal Nutrient = iota
	ProteinGrams
	CarbohydrateGrams
	SugarsGrams
	FatGrams
	FiberGrams
	VitaminB12Micrograms
	IronMilligrams
	CalciumMilligrams
	VitaminDMicrograms
	IodineMicrograms
	ZincMilligrams
	Omega3ALAGrams
	FolateMicrograms
	VitaminAMicrograms
	VitaminEMilligrams
	VitaminKMicrograms
	CopperMilligrams
	MagnesiumMilligrams
	SodiumMilligrams
	PotassiumMilligrams
	// Count is the number of nutrients.
	Count
)

// fields are the nutrients' field names in the API's Nutrients message,
// which also name their database columns.
var fields = [Count]string{
	EnergyKcal:           "energy_kcal",
	ProteinGrams:         "protein_grams",
	CarbohydrateGrams:    "carbohydrate_grams",
	SugarsGrams:          "sugars_grams",
	FatGrams:             "fat_grams",
	FiberGrams:           "fiber_grams",
	VitaminB12Micrograms: "vitamin_b12_micrograms",
	IronMilligrams:       "iron_milligrams",
	CalciumMilligrams:    "calcium_milligrams",
	VitaminDMicrograms:   "vitamin_d_micrograms",
	IodineMicrograms:     "iodine_micrograms",
	ZincMilligrams:       "zinc_milligrams",
	Omega3ALAGrams:       "omega3_ala_grams",
	FolateMicrograms:     "folate_micrograms",
	VitaminAMicrograms:   "vitamin_a_micrograms",
	VitaminEMilligrams:   "vitamin_e_milligrams",
	VitaminKMicrograms:   "vitamin_k_micrograms",
	CopperMilligrams:     "copper_milligrams",
	MagnesiumMilligrams:  "magnesium_milligrams",
	SodiumMilligrams:     "sodium_milligrams",
	PotassiumMilligrams:  "potassium_milligrams",
}

// String returns the nutrient's field name in the API's Nutrients message,
// e.g. "protein_grams".
func (n Nutrient) String() string {
	if n < 0 || n >= Count {
		return "unknown"
	}
	return fields[n]
}

// ParseNutrient returns the nutrient with the field name, e.g.
// "protein_grams", and false if there's none.
func ParseNutrient(name string) (Nutrient, bool) {
	for n, field := range fields {
		if field == name {
			return Nutrient(n), true
		}
	}
	return 0, false
}

// Nutrients are amounts of each nutrient in some amount of food, indexed by
// Nutrient. A nil amount is unknown, which is different from 0.
type Nutrients [Count]*float64

// Scale returns the nutrients multiplied by factor, keeping unknown ones
// unknown.
func (n *Nutrients) Scale(factor float64) Nutrients {
	var scaled Nutrients
	for i, v := range n {
		if v != nil {
			scaled[i] = new(*v * factor)
		}
	}
	return scaled
}

// Unit is the unit of an amount of a food.
type Unit int

const (
	// Grams need no conversion.
	Grams Unit = iota + 1
	// Milliliters are weighed with the food's density.
	Milliliters
	// Pieces are weighed with the food's weight per piece.
	Pieces
	// Packages, like a block of tofu or a can of beans, are weighed with the
	// food's package weight.
	Packages
)

// Food is what weighing and nutrition need of a food.
type Food struct {
	// Per100Grams are the nutrients in 100 g of the food.
	Per100Grams Nutrients
	// GramsPerPiece is the weight of one piece, 0 if unknown.
	GramsPerPiece float64
	// GramsPerMilliliter is the density, 0 if unknown.
	GramsPerMilliliter float64
	// PackageGrams is the weight of a package, 0 if unknown.
	PackageGrams float64
}

// Grams returns the weight of an amount of the food in a unit, and false
// if it can't be weighed, as the food lacks the weight per piece, the
// density or the package weight the unit needs.
func (f *Food) Grams(amount float64, unit Unit) (float64, bool) {
	switch unit {
	case Grams:
		return amount, true
	case Milliliters:
		if f.GramsPerMilliliter <= 0 {
			return 0, false
		}
		return amount * f.GramsPerMilliliter, true
	case Pieces:
		if f.GramsPerPiece <= 0 {
			return 0, false
		}
		return amount * f.GramsPerPiece, true
	case Packages:
		if f.PackageGrams <= 0 {
			return 0, false
		}
		return amount * f.PackageGrams, true
	default:
		return 0, false
	}
}

// KitchenUnit is a unit people measure in, like a tablespoon or a pound,
// that an amount can be entered in. Amounts are stored and computed in
// their base unit, grams or milliliters, and the kitchen unit is kept only
// to show them as entered, like "1 tbsp" rather than "15 ml".
type KitchenUnit string

// The kitchen units. Their names are stored in the database.
const (
	NoKitchenUnit KitchenUnit = ""
	Teaspoons     KitchenUnit = "teaspoons"
	Tablespoons   KitchenUnit = "tablespoons"
	Cups          KitchenUnit = "cups"
	Liters        KitchenUnit = "liters"
	Kilograms     KitchenUnit = "kilograms"
	Ounces        KitchenUnit = "ounces"
	Pounds        KitchenUnit = "pounds"
)

// Base returns the unit an amount in a kitchen unit is stored in, and how
// many of it one of the kitchen unit is. Spoons and cups are the API's
// documented sizes: a teaspoon is 5 ml, a tablespoon 15 ml and a cup a US
// cup's 240 ml. It returns 0 for no kitchen unit.
func (u KitchenUnit) Base() (Unit, float64) {
	switch u {
	case Teaspoons:
		return Milliliters, 5
	case Tablespoons:
		return Milliliters, 15
	case Cups:
		return Milliliters, 240
	case Liters:
		return Milliliters, 1000
	case Kilograms:
		return Grams, 1000
	case Ounces:
		return Grams, 28.349523125
	case Pounds:
		return Grams, 453.59237
	default:
		return 0, 0
	}
}
