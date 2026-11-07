import type { Nutrients } from "gen/foods/v1/foods_pb";

// NutrientKey is the name of a nutrient's field in Nutrients.
export type NutrientKey = Exclude<keyof Nutrients, "$typeName" | "$unknown">;

export type NutrientGroup =
  | "Energy and macronutrients"
  | "Vitamins"
  | "Minerals";

export interface NutrientInfo {
  // The field in Nutrients.
  key: NutrientKey;
  // The field's name in the API, which issues and incomplete nutrients use.
  field: string;
  label: string;
  unit: "kcal" | "g" | "mg" | "µg";
  group: NutrientGroup;
  // A note on what's measured, for hints.
  hint?: string;
}

// NUTRIENTS are the nutrients the app tracks, in the API's order.
export const NUTRIENTS: NutrientInfo[] = [
  {
    field: "energy_kcal",
    group: "Energy and macronutrients",
    key: "energyKcal",
    label: "Energy",
    unit: "kcal",
  },
  {
    field: "protein_grams",
    group: "Energy and macronutrients",
    key: "proteinGrams",
    label: "Protein",
    unit: "g",
  },
  {
    field: "carbohydrate_grams",
    group: "Energy and macronutrients",
    hint: "Including fiber. European labels leave fiber out: add it.",
    key: "carbohydrateGrams",
    label: "Carbohydrate",
    unit: "g",
  },
  {
    field: "sugars_grams",
    group: "Energy and macronutrients",
    key: "sugarsGrams",
    label: "Sugars",
    unit: "g",
  },
  {
    field: "fat_grams",
    group: "Energy and macronutrients",
    key: "fatGrams",
    label: "Fat",
    unit: "g",
  },
  {
    field: "fiber_grams",
    group: "Energy and macronutrients",
    key: "fiberGrams",
    label: "Fiber",
    unit: "g",
  },
  {
    field: "vitamin_b12_micrograms",
    group: "Vitamins",
    key: "vitaminB12Micrograms",
    label: "Vitamin B12",
    unit: "µg",
  },
  {
    field: "iron_milligrams",
    group: "Minerals",
    key: "ironMilligrams",
    label: "Iron",
    unit: "mg",
  },
  {
    field: "calcium_milligrams",
    group: "Minerals",
    key: "calciumMilligrams",
    label: "Calcium",
    unit: "mg",
  },
  {
    field: "vitamin_d_micrograms",
    group: "Vitamins",
    hint: "1 µg is 40 IU.",
    key: "vitaminDMicrograms",
    label: "Vitamin D",
    unit: "µg",
  },
  {
    field: "iodine_micrograms",
    group: "Minerals",
    key: "iodineMicrograms",
    label: "Iodine",
    unit: "µg",
  },
  {
    field: "zinc_milligrams",
    group: "Minerals",
    key: "zincMilligrams",
    label: "Zinc",
    unit: "mg",
  },
  {
    field: "omega3_ala_grams",
    group: "Energy and macronutrients",
    key: "omega3AlaGrams",
    label: "Omega-3 (ALA)",
    unit: "g",
  },
  {
    field: "folate_micrograms",
    group: "Vitamins",
    hint: "Dietary folate equivalents (DFE).",
    key: "folateMicrograms",
    label: "Folate",
    unit: "µg",
  },
  {
    field: "vitamin_a_micrograms",
    group: "Vitamins",
    hint: "Retinol activity equivalents (RAE).",
    key: "vitaminAMicrograms",
    label: "Vitamin A",
    unit: "µg",
  },
  {
    field: "vitamin_e_milligrams",
    group: "Vitamins",
    key: "vitaminEMilligrams",
    label: "Vitamin E",
    unit: "mg",
  },
  {
    field: "vitamin_k_micrograms",
    group: "Vitamins",
    key: "vitaminKMicrograms",
    label: "Vitamin K",
    unit: "µg",
  },
  {
    field: "copper_milligrams",
    group: "Minerals",
    key: "copperMilligrams",
    label: "Copper",
    unit: "mg",
  },
  {
    field: "magnesium_milligrams",
    group: "Minerals",
    key: "magnesiumMilligrams",
    label: "Magnesium",
    unit: "mg",
  },
  {
    field: "sodium_milligrams",
    group: "Minerals",
    hint: "Salt divided by 2.5.",
    key: "sodiumMilligrams",
    label: "Sodium",
    unit: "mg",
  },
  {
    field: "potassium_milligrams",
    group: "Minerals",
    key: "potassiumMilligrams",
    label: "Potassium",
    unit: "mg",
  },
];

// NUTRIENT_GROUPS are the groups nutrients are shown in, in order.
export const NUTRIENT_GROUPS: NutrientGroup[] = [
  "Energy and macronutrients",
  "Vitamins",
  "Minerals",
];

// NutrientLabel returns a nutrient's label from its API field name.
export const nutrientLabel = (field: string): string =>
  NUTRIENTS.find((nutrient) => nutrient.field === field)?.label ?? field;

/**
 * FormatNutrient formats an amount of a nutrient with its unit, with fewer
 * decimals for larger amounts, e.g. "412 kcal", "12.4 g" or "0.38 µg".
 */
export const formatNutrient = (value: number, unit: string): string =>
  `${formatAmount(value)} ${unit}`;

// FormatAmount formats a number with fewer decimals the larger it is.
export const formatAmount = (value: number): string => {
  const abs = Math.abs(value);
  let digits = 2;
  if (abs >= 100) {
    digits = 0;
  } else if (abs >= 10) {
    digits = 1;
  }
  return value.toLocaleString(undefined, {
    maximumFractionDigits: digits,
  });
};
