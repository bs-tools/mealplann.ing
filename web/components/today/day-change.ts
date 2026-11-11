import type { Client } from "@connectrpc/connect";
import type {
  GetWeekResponse,
  PlannedCook,
  PlannedPortion,
} from "gen/plans/v1/plans_pb";
import type { PlanService } from "gen/plans/v1/plans_pb";
import { parseDate } from "helpers/dates";

// PlanClient is a client of the plan's service.
export type PlanClient = Client<typeof PlanService>;

// OpenSlot is an eating time of a person with nothing planned, on a date.
export interface OpenSlot {
  date: string;
  eatingTimeId: string;
  // What to call it, e.g. "Friday lunch".
  label: string;
}

// DayLabel names a date relative to today, e.g. "Today" or "Friday".
export const dayLabel = (date: string, today: string): string => {
  if (date === today) {
    return "Today";
  }
  return parseDate(date).toLocaleDateString(undefined, { weekday: "long" });
};

/**
 * OpenSlotsOf returns a person's eating times with nothing planned, and not
 * eaten out, from a date and time on, through the week: where a meal they
 * didn't eat can move to.
 */
export const openSlotsOf = (
  week: GetWeekResponse,
  personId: string,
  date: string,
  afterTime: string,
  today: string
): OpenSlot[] => {
  const slots: OpenSlot[] = [];
  for (const day of week.days) {
    if (day.date < date) {
      continue;
    }
    const person = day.people.find((other) => other.personId === personId);
    for (const planned of person?.eatingTimes ?? []) {
      const et = planned.eatingTime;
      if (
        et === undefined ||
        planned.eatingOut ||
        planned.portions.length > 0 ||
        (day.date === date && et.time !== "" && et.time <= afterTime)
      ) {
        continue;
      }
      slots.push({
        date: day.date,
        eatingTimeId: et.id,
        label: `${dayLabel(day.date, today)} ${et.name.toLowerCase()}`,
      });
    }
  }
  return slots;
};

/**
 * DisplacedCookOf returns the cook a portion that isn't eaten displaces:
 * its cook, cooked that day and not done yet, if nobody else eats from it
 * that day. Then the question is whether to cook it another day, not where
 * the portion goes. Otherwise undefined.
 */
export const displacedCookOf = (
  week: GetWeekResponse,
  portion: PlannedPortion,
  date: string
): PlannedCook | undefined => {
  if (portion.cookId === "" || portion.leftover) {
    return undefined;
  }
  const planned = week.cooks.find((other) => other.cook?.id === portion.cookId);
  if (
    planned?.cook === undefined ||
    planned.cook.done ||
    planned.cook.date !== date
  ) {
    return undefined;
  }
  const others = week.days
    .find((day) => day.date === date)
    ?.people.flatMap((person) =>
      person.eatingTimes.flatMap((et) => et.portions)
    )
    .filter(
      (other) =>
        other.cookId === portion.cookId && other.portionId !== portion.portionId
    );
  return (others ?? []).length === 0 ? planned : undefined;
};

/**
 * MarkEaten marks portions and staples eaten on a date, or not: a portion
 * of a food, or a staple, comes out of the pantry.
 */
export const markEaten = async (
  client: PlanClient,
  portions: PlannedPortion[],
  date: string,
  eaten: boolean
): Promise<void> => {
  await Promise.all(
    portions.map(async (portion) => {
      await (portion.portionId === ""
        ? client.setStapleEaten({ date, eaten, id: portion.stapleId })
        : client.setPortionEaten({ eaten, id: portion.portionId }));
    })
  );
};

/**
 * ScalePortions changes how much of each portion was eaten, by a factor:
 * the servings of a share of a cook, or the amount of a food. Staples stay
 * as they are.
 */
export const scalePortions = async (
  client: PlanClient,
  portions: PlannedPortion[],
  factor: number
): Promise<void> => {
  await Promise.all(
    portions
      .filter((portion) => portion.portionId !== "")
      .map(async (portion) =>
        portion.cookId === ""
          ? client.updatePortion({
              portion: {
                id: portion.portionId,
                what: {
                  case: "food",
                  value: {
                    amount: Math.round(portion.amount * factor * 100) / 100,
                    foodId: portion.foodId,
                    unit: portion.unit,
                  },
                },
              },
              updateMask: { paths: ["food"] },
            })
          : client.updatePortion({
              portion: {
                id: portion.portionId,
                what: {
                  case: "cook",
                  value: {
                    cookId: portion.cookId,
                    servings: Math.round(portion.servings * factor * 20) / 20,
                  },
                },
              },
              updateMask: { paths: ["cook"] },
            })
      )
  );
};
