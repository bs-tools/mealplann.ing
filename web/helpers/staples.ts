import type { Person } from "gen/people/v1/people_pb";
import type { HouseholdMeal } from "gen/people/v1/people_pb";
import type { Staple } from "gen/plans/v1/plans_pb";
import { Weekday } from "gen/plans/v1/plans_pb";

import { parseDate } from "./dates";

// WEEKDAYS are the weekdays by Date.getDay(), from Sunday.
const WEEKDAYS = [
  Weekday.SUNDAY,
  Weekday.MONDAY,
  Weekday.TUESDAY,
  Weekday.WEDNESDAY,
  Weekday.THURSDAY,
  Weekday.FRIDAY,
  Weekday.SATURDAY,
];

// StapleOccursOn reports whether a staple is eaten on a date (YYYY-MM-DD).
export const stapleOccursOn = (staple: Staple, date: string): boolean => {
  if (staple.startDate !== "" && date < staple.startDate) {
    return false;
  }
  if (staple.endDate !== "" && date > staple.endDate) {
    return false;
  }
  const weekday = WEEKDAYS[parseDate(date).getDay()];
  if (
    staple.weekdays.length > 0 &&
    (weekday === undefined || !staple.weekdays.includes(weekday))
  ) {
    return false;
  }
  return !staple.skippedDates.includes(date);
};

/**
 * StaplesAt returns the staples a person eats at their eating time for a
 * shared meal on a date, which they have instead of the meal.
 */
export const staplesAt = (
  staples: Staple[],
  person: Person,
  meal: HouseholdMeal,
  date: string
): Staple[] => {
  const eatingTimes = new Set(
    person.eatingTimes.filter((et) => et.meal === meal).map((et) => et.id)
  );
  return staples.filter(
    (staple) =>
      staple.personId === person.id &&
      eatingTimes.has(staple.eatingTimeId) &&
      stapleOccursOn(staple, date)
  );
};
