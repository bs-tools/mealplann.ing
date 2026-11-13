import type { Effort } from "@components/today/today-bits";
import { effortOf } from "@components/today/today-bits";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import type {
  PersonDay,
  PlanDay,
  PlannedEatingTime,
} from "gen/plans/v1/plans_pb";
import { householdMealLabel } from "helpers/labels";

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

// RowNames lists what's planned at a row, each name once.
export const rowNames = (row: MealRow): string[] => {
  const names: string[] = [];
  for (const slot of row.slots) {
    for (const portion of slot.planned.portions) {
      if (!names.includes(portion.name)) {
        names.push(portion.name);
      }
    }
  }
  return names;
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
