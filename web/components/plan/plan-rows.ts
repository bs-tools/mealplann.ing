import type { Effort } from "@components/today/today-bits";
import { effortOf } from "@components/today/today-bits";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import type {
  PersonDay,
  PlanDay,
  PlannedEatingTime,
  PlannedPortion,
} from "gen/plans/v1/plans_pb";
import { UnitSystem } from "gen/users/v1/users_pb";
import { formatGrams } from "helpers/format";
import {
  formatIngredientAmount,
  formatServings,
  householdMealLabel,
} from "helpers/labels";

// RowSlot is one person's eating time in a row of the plan.
export interface RowSlot {
  person: PersonDay;
  planned: PlannedEatingTime;
}

/**
 * MealRow is a row of a day of the plan: a shared meal, everyone's eating
 * time of it together, or eating times of the same name, like everyone's
 * snack, as the household thinks of its day by meal, not by person.
 */
export interface MealRow {
  key: string;
  // What to call it, e.g. "Dinner".
  label: string;
  // The earliest of its eating times, "HH:MM", or "" if none has a time.
  time: string;
  // The shared meal, or unspecified for eating times of one person each.
  meal: HouseholdMeal;
  slots: RowSlot[];
}

// EFFORT_RANK ranks efforts, so a row takes its slots' most.
const EFFORT_RANK: Effort[] = ["none", "out", "ready", "heat", "cook"];

// RowEffort is how much work a row is on a date: its slots' most.
export const rowEffort = (row: MealRow, date: string): Effort =>
  row.slots.reduce<Effort>((most, slot) => {
    const own = effortOf(slot.planned, date);
    return EFFORT_RANK.indexOf(own) > EFFORT_RANK.indexOf(most) ? own : most;
  }, "none");

// RowPlanned reports whether anything is planned at a row, or eaten out.
export const rowPlanned = (row: MealRow): boolean =>
  row.slots.some(
    (slot) => slot.planned.eatingOut || slot.planned.portions.length > 0
  );

// RowShare is one person's share of an item of a row, e.g. "Ben 1 serving".
export interface RowShare {
  personName: string;
  amount: string;
}

/**
 * RowItem is something planned at a row, like a recipe everyone eats from,
 * with each person's share of it, whether it's the rest of an earlier
 * meal, and whether it's a staple: what someone has there every day.
 */
export interface RowItem {
  name: string;
  leftover: boolean;
  staple: boolean;
  shares: RowShare[];
}

/**
 * PortionAmount says how much a portion is: servings of a recipe, or the
 * weight of a food, e.g. "1.5 servings" or "120 g".
 */
export const portionAmount = (portion: PlannedPortion): string => {
  if (portion.cookId !== "" || portion.recipeId !== "") {
    return formatServings(portion.servings);
  }
  if (portion.grams > 0) {
    return formatGrams(portion.grams);
  }
  // A food without a weight, like soy milk lacking a density, says its amount.
  return portion.amount > 0
    ? formatIngredientAmount(portion.amount, portion.unit, UnitSystem.METRIC)
    : "";
};

/**
 * RowItems lists what's planned at a row, each item once, in the order
 * first planned, with everyone's share of it, so a shared dinner reads
 * "Curry: Ben 1 serving · Anna 0.5 servings" rather than just its names.
 */
export const rowItems = (row: MealRow): RowItem[] => {
  const items: RowItem[] = [];
  for (const slot of row.slots) {
    for (const portion of slot.planned.portions) {
      const key = portion.name;
      let item = items.find((other) => other.name === key);
      if (item === undefined) {
        item = {
          leftover: portion.leftover,
          name: key,
          shares: [],
          staple: portion.stapleId !== "",
        };
        items.push(item);
      }
      item.shares.push({
        amount: portionAmount(portion),
        personName: slot.person.personName,
      });
    }
  }
  return items;
};

/**
 * RowsOf groups a day's eating times into rows: those of a shared meal
 * together, and others by name, in order of time, those without one last.
 */
export const rowsOf = (day: PlanDay): MealRow[] => {
  const rows: MealRow[] = [];
  for (const person of day.people) {
    for (const planned of person.eatingTimes) {
      const et = planned.eatingTime;
      if (et === undefined) {
        continue;
      }
      /*
       * Eating times group by what they're called, so one person's
       * "Lunch" and another's shared lunch are one row.
       */
      const label =
        et.meal === HouseholdMeal.UNSPECIFIED
          ? et.name
          : householdMealLabel(et.meal);
      const key = label.trim().toLowerCase();
      let row = rows.find((other) => other.key === key);
      if (row === undefined) {
        row = { key, label, meal: et.meal, slots: [], time: et.time };
        rows.push(row);
      }
      if (row.meal === HouseholdMeal.UNSPECIFIED) {
        row.meal = et.meal;
      }
      if (et.time !== "" && (row.time === "" || et.time < row.time)) {
        row.time = et.time;
      }
      row.slots.push({ person, planned });
    }
  }
  const ordered: MealRow[] = [];
  for (const row of rows) {
    const at = ordered.findIndex(
      (other) => row.time !== "" && (other.time === "" || row.time < other.time)
    );
    if (at === -1) {
      ordered.push(row);
    } else {
      ordered.splice(at, 0, row);
    }
  }
  return ordered;
};
