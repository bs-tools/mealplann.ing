// Package amounts converts the API's amounts of foods, in any of its units,
// to the base units the app stores and computes them in: grams,
// milliliters, pieces or packages.
package amounts

import (
	foodsv1 "github.com/bs-tools/mealplann.ing/internal/gen/foods/v1"
	"github.com/bs-tools/mealplann.ing/internal/nutrition"
)

// baseUnits are the API's base units.
var baseUnits = map[foodsv1.AmountUnit]nutrition.Unit{
	foodsv1.AmountUnit_AMOUNT_UNIT_GRAMS:       nutrition.Grams,
	foodsv1.AmountUnit_AMOUNT_UNIT_MILLILITERS: nutrition.Milliliters,
	foodsv1.AmountUnit_AMOUNT_UNIT_PIECES:      nutrition.Pieces,
	foodsv1.AmountUnit_AMOUNT_UNIT_PACKAGES:    nutrition.Packages,
}

// kitchenUnits are the API's kitchen units.
var kitchenUnits = map[foodsv1.AmountUnit]nutrition.KitchenUnit{
	foodsv1.AmountUnit_AMOUNT_UNIT_TEASPOONS:   nutrition.Teaspoons,
	foodsv1.AmountUnit_AMOUNT_UNIT_TABLESPOONS: nutrition.Tablespoons,
	foodsv1.AmountUnit_AMOUNT_UNIT_CUPS:        nutrition.Cups,
	foodsv1.AmountUnit_AMOUNT_UNIT_LITERS:      nutrition.Liters,
	foodsv1.AmountUnit_AMOUNT_UNIT_KILOGRAMS:   nutrition.Kilograms,
	foodsv1.AmountUnit_AMOUNT_UNIT_OUNCES:      nutrition.Ounces,
	foodsv1.AmountUnit_AMOUNT_UNIT_POUNDS:      nutrition.Pounds,
}

// UnitsHelp lists the units, for errors.
const UnitsHelp = "AMOUNT_UNIT_GRAMS, AMOUNT_UNIT_MILLILITERS, AMOUNT_UNIT_PIECES or AMOUNT_UNIT_PACKAGES, " +
	"or a kitchen unit like AMOUNT_UNIT_TABLESPOONS"

// FromProto converts an amount in a unit of the API to its base unit,
// returning the kitchen unit it was in, if any, and false if the unit is
// unspecified or unknown.
func FromProto(amount float64, unit foodsv1.AmountUnit) (float64, nutrition.Unit, nutrition.KitchenUnit, bool) {
	if base, ok := baseUnits[unit]; ok {
		return amount, base, nutrition.NoKitchenUnit, true
	}
	kitchen, ok := kitchenUnits[unit]
	if !ok {
		return 0, 0, nutrition.NoKitchenUnit, false
	}
	base, size := kitchen.Base()
	return amount * size, base, kitchen, true
}

// ToProto converts an amount in a base unit to the API, in the kitchen unit
// it was entered in, if any.
func ToProto(amount float64, unit nutrition.Unit, kitchen nutrition.KitchenUnit) (float64, foodsv1.AmountUnit) {
	if base, size := kitchen.Base(); size > 0 && base == unit {
		for u, k := range kitchenUnits {
			if k == kitchen {
				return amount / size, u
			}
		}
	}
	for u, b := range baseUnits {
		if b == unit {
			return amount, u
		}
	}
	return amount, foodsv1.AmountUnit_AMOUNT_UNIT_UNSPECIFIED
}
