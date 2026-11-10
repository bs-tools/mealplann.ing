import {
  AmountUnit,
  FoodSource,
  Measure,
  StoreSection,
} from "gen/foods/v1/foods_pb";
import { Storage } from "gen/pantry/v1/pantry_pb";
import { EatingTimeKind, HouseholdMeal } from "gen/people/v1/people_pb";
import { Weekday } from "gen/plans/v1/plans_pb";
import type { UnitSystem } from "gen/users/v1/users_pb";

import type { FoodMeasures } from "./measures";
import { formatMeasure } from "./measures";

// MEASURES are how a food's amounts can show in recipes.
export const MEASURES: { value: Measure; label: string }[] = [
  {
    label: "Automatically: spoons for small amounts",
    value: Measure.UNSPECIFIED,
  },
  { label: "By weight, like tofu or pasta", value: Measure.WEIGHT },
  { label: "By volume, like oil, spices or milk", value: Measure.VOLUME },
  {
    label: "In milliliters, like a 400 ml can of coconut milk",
    value: Measure.MILLILITERS,
  },
  { label: "In pieces, like onions", value: Measure.PIECES },
];

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

// STORAGES are where pantry lots are kept, with "Not said" first.
export const STORAGES: { value: Storage; label: string }[] = [
  { label: "Not said", value: Storage.UNSPECIFIED },
  { label: "Pantry", value: Storage.PANTRY },
  { label: "Fridge", value: Storage.FRIDGE },
  { label: "Freezer", value: Storage.FREEZER },
];

/**
 * AMOUNT_UNITS are the units of foods' amounts: grams, milliliters and
 * pieces, then the kitchen units converted to them, which recipes keep to
 * show their ingredients as entered.
 */
export const AMOUNT_UNITS: { value: AmountUnit; label: string }[] = [
  { label: "g", value: AmountUnit.GRAMS },
  { label: "ml", value: AmountUnit.MILLILITERS },
  { label: "pieces", value: AmountUnit.PIECES },
  { label: "packages", value: AmountUnit.PACKAGES },
  { label: "tsp", value: AmountUnit.TEASPOONS },
  { label: "tbsp", value: AmountUnit.TABLESPOONS },
  { label: "cups", value: AmountUnit.CUPS },
  { label: "L", value: AmountUnit.LITERS },
  { label: "kg", value: AmountUnit.KILOGRAMS },
  { label: "oz", value: AmountUnit.OUNCES },
  { label: "lb", value: AmountUnit.POUNDS },
];

/**
 * FormatIngredientAmount formats an amount of a food in a unit system,
 * converting it where it can (see formatMeasure), e.g. "2 tbsp".
 */
export const formatIngredientAmount = (
  amount: number,
  unit: AmountUnit,
  system: UnitSystem,
  food?: FoodMeasures
): string => formatMeasure(amount, unit, system, food);

/**
 * OptionValue returns the value of the option whose value's string is
 * text, or fallback. Select inputs give strings, and looking the option up
 * keeps the enum's type.
 */
export const optionValue = <Value>(
  options: { value: Value; label: string }[],
  text: string,
  fallback: Value
): Value =>
  options.find((option) => String(option.value) === text)?.value ?? fallback;

// EATING_TIME_KINDS are the kinds of eating times.
export const EATING_TIME_KINDS: { value: EatingTimeKind; label: string }[] = [
  { label: "Main meal", value: EatingTimeKind.MAIN },
  { label: "Small meal", value: EatingTimeKind.SMALL },
];

// HOUSEHOLD_MEALS are the meals a household cooks to share.
export const HOUSEHOLD_MEALS: { value: HouseholdMeal; label: string }[] = [
  { label: "Breakfast", value: HouseholdMeal.BREAKFAST },
  { label: "Lunch", value: HouseholdMeal.LUNCH },
  { label: "Dinner", value: HouseholdMeal.DINNER },
];

// HouseholdMealLabel names a shared meal, "" if none.
export const householdMealLabel = (meal: HouseholdMeal): string =>
  HOUSEHOLD_MEALS.find((option) => option.value === meal)?.label ?? "";

// WEEKDAYS are the days of the week, from Monday.
export const WEEKDAYS: { value: Weekday; label: string }[] = [
  { label: "Mon", value: Weekday.MONDAY },
  { label: "Tue", value: Weekday.TUESDAY },
  { label: "Wed", value: Weekday.WEDNESDAY },
  { label: "Thu", value: Weekday.THURSDAY },
  { label: "Fri", value: Weekday.FRIDAY },
  { label: "Sat", value: Weekday.SATURDAY },
  { label: "Sun", value: Weekday.SUNDAY },
];

/**
 * FormatWeekdays describes the days a staple is on, e.g. "Every day",
 * "Weekdays" or "Mon · Wed · Fri".
 */
export const formatWeekdays = (weekdays: Weekday[]): string => {
  if (weekdays.length === 0 || weekdays.length === WEEKDAYS.length) {
    return "Every day";
  }
  const days = WEEKDAYS.filter((option) => weekdays.includes(option.value));
  const names = days.map((option) => option.label).join(" · ");
  if (names === "Mon · Tue · Wed · Thu · Fri") {
    return "Weekdays";
  }
  if (names === "Sat · Sun") {
    return "Weekends";
  }
  return names;
};

// FormatServings formats servings, e.g. "1 serving" or "0.66 servings".
export const formatServings = (servings: number): string => {
  const value = servings.toLocaleString(undefined, {
    maximumFractionDigits: 2,
  });
  return `${value} ${servings === 1 ? "serving" : "servings"}`;
};

// FoodSourceInfo describes a source of reference foods.
export interface FoodSourceInfo {
  // The short label of its badge, e.g. "Canada".
  badge: string;
  // The Tailwind classes of its badge.
  badgeClass: string;
  // Its full name, e.g. "Health Canada's Canadian Nutrient File".
  name: string;
  // The link to the food on the source's website.
  url: string;
}

/**
 * FoodSourceInfo describes the source of a reference food with an ID, or
 * returns undefined for custom foods. Badges name the country rather than
 * the database, which reads better to home cooks than "CNF" or "FDC".
 */
export const foodSourceInfo = (
  source: FoodSource,
  sourceId: number
): FoodSourceInfo | undefined => {
  if (source === FoodSource.CNF) {
    return {
      badge: "Canada",
      badgeClass: "bg-red-50 text-red-800 ring-red-200",
      name: "Health Canada's Canadian Nutrient File",
      url: `https://food-nutrition.canada.ca/cnf-fce/foodDetails?lang=eng&id=${sourceId}`,
    };
  }
  if (source === FoodSource.FDC) {
    return {
      badge: "USDA",
      badgeClass: "bg-sky-50 text-sky-800 ring-sky-200",
      name: "USDA FoodData Central",
      url: `https://fdc.nal.usda.gov/food-details/${sourceId}/nutrients`,
    };
  }
  return undefined;
};

// FormatShare formats a person's servings, e.g. "⅓" or "1.5".
export const formatShare = (servings: number): string => {
  const thirds = servings * 3;
  if (
    Math.abs(thirds - Math.round(thirds)) < 0.02 &&
    Math.round(thirds) % 3 !== 0
  ) {
    const whole = Math.floor(Math.round(thirds) / 3);
    const fraction = Math.round(thirds) % 3 === 1 ? "⅓" : "⅔";
    return whole > 0 ? `${whole}${fraction}` : fraction;
  }
  return servings.toLocaleString(undefined, { maximumFractionDigits: 2 });
};
