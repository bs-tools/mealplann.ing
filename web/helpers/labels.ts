import { AmountUnit, StoreSection } from "gen/foods/v1/foods_pb";

// STORE_SECTIONS are the store sections, in the order of a typical shop.
export const STORE_SECTIONS: { value: StoreSection; label: string }[] = [
  { label: "Produce", value: StoreSection.PRODUCE },
  { label: "Bakery", value: StoreSection.BAKERY },
  { label: "Chilled", value: StoreSection.CHILLED },
  { label: "Frozen", value: StoreSection.FROZEN },
  { label: "Dry goods", value: StoreSection.DRY_GOODS },
  { label: "Canned", value: StoreSection.CANNED },
  { label: "Spices", value: StoreSection.SPICES },
  { label: "Drinks", value: StoreSection.DRINKS },
  { label: "Household", value: StoreSection.HOUSEHOLD },
  { label: "Other", value: StoreSection.OTHER },
];

// StoreSectionLabel names a store section, "" if it's unspecified.
export const storeSectionLabel = (section: StoreSection): string =>
  STORE_SECTIONS.find((option) => option.value === section)?.label ?? "";

// AMOUNT_UNITS are the units of ingredients' amounts.
export const AMOUNT_UNITS: { value: AmountUnit; label: string }[] = [
  { label: "g", value: AmountUnit.GRAMS },
  { label: "ml", value: AmountUnit.MILLILITERS },
  { label: "pieces", value: AmountUnit.PIECES },
];

/**
 * FormatIngredientAmount formats an amount in a unit, e.g. "400 g", "200 ml"
 * or "2", as pieces read best before the food's name ("2 Onions").
 */
export const formatIngredientAmount = (
  amount: number,
  unit: AmountUnit
): string => {
  const value = amount.toLocaleString(undefined, { maximumFractionDigits: 2 });
  if (unit === AmountUnit.GRAMS) {
    return `${value} g`;
  }
  if (unit === AmountUnit.MILLILITERS) {
    return `${value} ml`;
  }
  return value;
};
