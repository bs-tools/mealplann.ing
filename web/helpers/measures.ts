import { AmountUnit, Measure } from "gen/foods/v1/foods_pb";
import { UnitSystem } from "gen/users/v1/users_pb";

/*
 * MILLILITERS_PER_TEASPOON and the others are the volumes recipes use, as
 * the API documents them.
 */
const MILLILITERS_PER_TEASPOON = 5;
const MILLILITERS_PER_TABLESPOON = 15;
const MILLILITERS_PER_CUP = 240;
const GRAMS_PER_OUNCE = 28.3495;
const OUNCES_PER_POUND = 16;

/*
 * AUTO_VOLUME_MILLILITERS is the most an automatic measure shows by
 * volume: up to 5 tablespoons is spooned, like spices or oil, and more is
 * shown as it's entered.
 */
const AUTO_VOLUME_MILLILITERS = 75;

// CUP_FRACTIONS are the fractions of cups that measuring sets have.
const CUP_FRACTIONS: [number, string][] = [
  [0, ""],
  [1 / 4, "¼"],
  [1 / 3, "⅓"],
  [1 / 2, "½"],
  [2 / 3, "⅔"],
  [3 / 4, "¾"],
  [1, ""],
];

// SPOON_FRACTIONS are the fractions of spoons that measuring sets have.
const SPOON_FRACTIONS: [number, string][] = [
  [0, ""],
  [1 / 8, "⅛"],
  [1 / 4, "¼"],
  [1 / 2, "½"],
  [3 / 4, "¾"],
  [1, ""],
];

/**
 * FormatFraction formats a number to the nearest of fractions, e.g.
 * "1 ½" or "⅓", "" if it rounds to nothing.
 */
const formatFraction = (
  value: number,
  fractions: [number, string][] = CUP_FRACTIONS
): string => {
  let whole = Math.floor(value);
  let best = fractions[0] ?? [0, ""];
  for (const fraction of fractions) {
    if (
      Math.abs(value - whole - fraction[0]) < Math.abs(value - whole - best[0])
    ) {
      best = fraction;
    }
  }
  if (best[0] === 1) {
    whole++;
  }
  if (whole === 0) {
    return best[1];
  }
  return best[1] === "" ? String(whole) : `${whole} ${best[1]}`;
};

// Round rounds a number to a precision that reads well for its size.
const round = (value: number): string => {
  let digits = 0;
  if (value < 10) {
    digits = 1;
  }
  return value.toLocaleString(undefined, { maximumFractionDigits: digits });
};

// FormatSpoons formats a small volume in teaspoons or tablespoons.
const formatSpoons = (milliliters: number): string => {
  // A tablespoon or more is measured in tablespoons, to the half.
  if (milliliters >= MILLILITERS_PER_TABLESPOON - 1) {
    const tablespoons =
      Math.round((milliliters / MILLILITERS_PER_TABLESPOON) * 2) / 2;
    return `${formatFraction(tablespoons, SPOON_FRACTIONS)} tbsp`;
  }
  const text = formatFraction(
    milliliters / MILLILITERS_PER_TEASPOON,
    SPOON_FRACTIONS
  );
  return text === "" ? "a pinch" : `${text} tsp`;
};

/**
 * FormatVolume formats a volume in milliliters in a unit system: spoons
 * for small amounts, then milliliters or liters for metric, and cups for
 * imperial.
 */
export const formatVolume = (
  milliliters: number,
  system: UnitSystem
): string => {
  if (milliliters < 4 * MILLILITERS_PER_TABLESPOON) {
    return formatSpoons(milliliters);
  }
  if (system === UnitSystem.IMPERIAL) {
    const cups = milliliters / MILLILITERS_PER_CUP;
    return `${formatFraction(cups)} ${cups < 1.125 ? "cup" : "cups"}`;
  }
  if (milliliters >= 1000) {
    return `${round(milliliters / 1000)} L`;
  }
  // Milliliters are measured to the nearest 5.
  return `${Math.round(milliliters / 5) * 5} ml`;
};

/**
 * FormatWeight formats a weight in grams in a unit system: grams or
 * kilograms, or ounces and pounds.
 */
export const formatWeight = (grams: number, system: UnitSystem): string => {
  if (system === UnitSystem.IMPERIAL) {
    const ounces = grams / GRAMS_PER_OUNCE;
    // Under a quarter of an ounce, too little for kitchen scales, is grams.
    if (ounces < 0.25) {
      return `${round(grams)} g`;
    }
    if (ounces < OUNCES_PER_POUND) {
      // Small amounts are weighed to a quarter of an ounce.
      const text =
        ounces < 4
          ? formatFraction(ounces, SPOON_FRACTIONS)
          : String(Math.round(ounces));
      return `${text} oz`;
    }
    const pounds = Math.floor(ounces / OUNCES_PER_POUND);
    const rest = Math.round(ounces - pounds * OUNCES_PER_POUND);
    if (rest === 0 || rest === OUNCES_PER_POUND) {
      return `${pounds + (rest === 0 ? 0 : 1)} lb`;
    }
    return `${pounds} lb ${rest} oz`;
  }
  if (grams >= 1000) {
    return `${round(grams / 1000)} kg`;
  }
  return `${round(grams)} g`;
};

// FoodMeasures is what converting a food's amounts needs of it.
export interface FoodMeasures {
  measure: Measure;
  // GramsPerMilliliter and gramsPerPiece are 0 if unknown.
  gramsPerMilliliter: number;
  gramsPerPiece: number;
}

// UNKNOWN_FOOD converts nothing: amounts show in their own unit.
const UNKNOWN_FOOD: FoodMeasures = {
  gramsPerMilliliter: 0,
  gramsPerPiece: 0,
  measure: Measure.UNSPECIFIED,
};

// FormatPieces formats a number of pieces, to the nearest quarter.
const formatPieces = (pieces: number): string => {
  const text = formatFraction(pieces);
  return text === "" ? "¼" : text;
};

// KitchenUnit is how an amount in a kitchen unit is shown.
interface KitchenUnit {
  // Singular and plural are its name, like "tbsp" or "cup" and "cups".
  singular: string;
  plural: string;
  // Fractions are those it's measured to, none to show decimals.
  fractions?: [number, string][];
  // Milliliters and grams are how much one of it is, of one of them.
  milliliters?: number;
  grams?: number;
}

// KITCHEN_UNITS are the kitchen units amounts can be entered in.
const KITCHEN_UNITS = new Map<AmountUnit, KitchenUnit>([
  [
    AmountUnit.TEASPOONS,
    {
      fractions: SPOON_FRACTIONS,
      milliliters: MILLILITERS_PER_TEASPOON,
      plural: "tsp",
      singular: "tsp",
    },
  ],
  [
    AmountUnit.TABLESPOONS,
    {
      fractions: SPOON_FRACTIONS,
      milliliters: MILLILITERS_PER_TABLESPOON,
      plural: "tbsp",
      singular: "tbsp",
    },
  ],
  [
    AmountUnit.CUPS,
    {
      fractions: CUP_FRACTIONS,
      milliliters: MILLILITERS_PER_CUP,
      plural: "cups",
      singular: "cup",
    },
  ],
  [AmountUnit.LITERS, { milliliters: 1000, plural: "L", singular: "L" }],
  [AmountUnit.KILOGRAMS, { grams: 1000, plural: "kg", singular: "kg" }],
  [
    AmountUnit.OUNCES,
    {
      fractions: SPOON_FRACTIONS,
      grams: GRAMS_PER_OUNCE,
      plural: "oz",
      singular: "oz",
    },
  ],
  [
    AmountUnit.POUNDS,
    {
      fractions: SPOON_FRACTIONS,
      grams: GRAMS_PER_OUNCE * OUNCES_PER_POUND,
      plural: "lb",
      singular: "lb",
    },
  ],
]);

// IsVolumeUnit reports whether a unit is a volume, weighed with a density.
export const isVolumeUnit = (unit: AmountUnit): boolean =>
  unit === AmountUnit.MILLILITERS ||
  KITCHEN_UNITS.get(unit)?.milliliters !== undefined;

/**
 * FormatKitchenAmount formats an amount in a kitchen unit as entered, e.g.
 * "1 ½ tbsp" or "2 cups", or undefined if the unit isn't one.
 */
const formatKitchenAmount = (
  amount: number,
  unit: AmountUnit
): string | undefined => {
  const kitchen = KITCHEN_UNITS.get(unit);
  if (kitchen === undefined) {
    return undefined;
  }
  let text =
    kitchen.fractions === undefined
      ? round(amount)
      : formatFraction(amount, kitchen.fractions);
  if (text === "") {
    // Less than the smallest fraction, like a pinch of a teaspoon.
    text = kitchen.fractions?.[1]?.[1] ?? round(amount);
    return `< ${text} ${kitchen.singular}`;
  }
  return `${text} ${amount > 1.125 ? kitchen.plural : kitchen.singular}`;
};

/**
 * FormatMeasure formats an amount of a food as people read it: in the
 * food's measure (by weight, volume or pieces), converted with its density
 * or piece weight, in the user's unit system. An automatic measure shows
 * small amounts by volume, like a teaspoon of cumin or a tablespoon of oil,
 * and the rest as entered. An amount that can't be converted shows as
 * entered. An amount in a kitchen unit, like tablespoons, shows as entered,
 * as the recipe wrote it.
 */
export const formatMeasure = (
  amount: number,
  unit: AmountUnit,
  system: UnitSystem,
  food: FoodMeasures = UNKNOWN_FOOD
): string => {
  const kitchen = formatKitchenAmount(amount, unit);
  if (kitchen !== undefined) {
    return kitchen;
  }
  let grams: number | undefined;
  let milliliters: number | undefined;
  let pieces: number | undefined;
  const density = food.gramsPerMilliliter;
  switch (unit) {
    case AmountUnit.MILLILITERS: {
      milliliters = amount;
      if (density > 0) {
        grams = amount * density;
      }
      break;
    }
    case AmountUnit.PIECES: {
      pieces = amount;
      if (food.gramsPerPiece > 0) {
        grams = amount * food.gramsPerPiece;
      }
      break;
    }
    default: {
      grams = amount;
    }
  }
  if (milliliters === undefined && grams !== undefined && density > 0) {
    milliliters = grams / density;
  }
  if (pieces === undefined && grams !== undefined && food.gramsPerPiece > 0) {
    pieces = grams / food.gramsPerPiece;
  }

  let { measure } = food;
  if (measure === Measure.UNSPECIFIED) {
    if (
      unit !== AmountUnit.PIECES &&
      milliliters !== undefined &&
      milliliters <= AUTO_VOLUME_MILLILITERS
    ) {
      measure = Measure.VOLUME;
    } else if (unit === AmountUnit.MILLILITERS) {
      measure = Measure.VOLUME;
    } else if (unit === AmountUnit.PIECES) {
      measure = Measure.PIECES;
    } else {
      measure = Measure.WEIGHT;
    }
  }
  if (measure === Measure.VOLUME && milliliters !== undefined) {
    return formatVolume(milliliters, system);
  }
  // Milliliters are metric whatever the user's units, like a 400 ml can.
  if (measure === Measure.MILLILITERS && milliliters !== undefined) {
    return formatVolume(milliliters, UnitSystem.METRIC);
  }
  if (measure === Measure.PIECES && pieces !== undefined) {
    return formatPieces(pieces);
  }
  if (measure === Measure.WEIGHT && grams !== undefined) {
    return formatWeight(grams, system);
  }
  // The amount shows in its own unit, as it can't be converted.
  if (unit === AmountUnit.PIECES) {
    return formatPieces(amount);
  }
  if (unit === AmountUnit.MILLILITERS) {
    return formatVolume(amount, system);
  }
  return formatWeight(amount, system);
};
