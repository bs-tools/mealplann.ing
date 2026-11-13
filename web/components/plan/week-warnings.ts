import type {
  GetWeekResponse,
  PersonDay,
  PlannedCook,
  Warning,
} from "gen/plans/v1/plans_pb";
import { Severity, WarningKind } from "gen/plans/v1/plans_pb";

// WeekWarning is a warning of the plan, with what it's about.
export interface WeekWarning {
  key: string;
  date: string;
  warning: Warning;
  // The person whose day or eating time it's about, if any.
  person?: PersonDay | undefined;
  // The eating time it's about, if any, as its ID and name.
  eatingTimeId: string;
  eatingTimeName: string;
  // The cook it's about, if any, like leftovers kept too long.
  cook?: PlannedCook | undefined;
}

/**
 * IsTargetWarning reports whether a warning is of a day missing a daily
 * target, which the bars of the day's targets show already.
 */
export const isTargetWarning = (warning: Warning): boolean =>
  warning.kind === WarningKind.TARGET_LOW ||
  warning.kind === WarningKind.TARGET_HIGH;

// Acting reports whether a warning is one to act on, rather than a note.
const acting = (warning: Warning): boolean =>
  warning.severity === Severity.WARNING;

/**
 * WeekWarnings returns the warnings of a week's plan, those to act on
 * rather than notes, from a date on: each person's days and eating times,
 * and the cooks. They're shown together on their own page rather than
 * beside each meal, where they crowded out the plan.
 */
export const weekWarnings = (
  week: GetWeekResponse,
  from = ""
): WeekWarning[] => {
  const warnings: WeekWarning[] = [];
  for (const day of week.days) {
    if (day.date < from) {
      continue;
    }
    for (const person of day.people) {
      for (const planned of person.eatingTimes) {
        for (const warning of planned.warnings.filter(acting)) {
          warnings.push({
            date: day.date,
            eatingTimeId: planned.eatingTime?.id ?? "",
            eatingTimeName: planned.eatingTime?.name ?? "",
            key: `${day.date}-${person.personId}-${planned.eatingTime?.id ?? ""}-${warning.kind}-${warning.nutrient}`,
            person,
            warning,
          });
        }
      }
      for (const warning of person.warnings.filter(acting)) {
        warnings.push({
          date: day.date,
          eatingTimeId: "",
          eatingTimeName: "",
          key: `${day.date}-${person.personId}-${warning.kind}-${warning.nutrient}`,
          person,
          warning,
        });
      }
    }
    for (const planned of week.cooks) {
      if (planned.cook?.date !== day.date) {
        continue;
      }
      for (const warning of planned.warnings.filter(acting)) {
        warnings.push({
          cook: planned,
          date: day.date,
          eatingTimeId: "",
          eatingTimeName: "",
          key: `${planned.cook.id}-${warning.kind}-${warning.message}`,
          warning,
        });
      }
    }
  }
  return warnings;
};
