import { CheckIcon, ClockIcon, FireIcon } from "@heroicons/react/24/outline";
import type { PlannedEatingTime, Warning } from "gen/plans/v1/plans_pb";
import { WarningKind } from "gen/plans/v1/plans_pb";
import { nutrientInfoOf } from "helpers/nutrients";
import React from "react";

/**
 * Effort is how much work a meal is before it's eaten: none, as it's ready
 * to eat, heating leftovers, or cooking. Eating out and nothing planned
 * take none either, but show differently.
 */
export type Effort = "ready" | "heat" | "cook" | "out" | "none";

// EFFORT_ORDER ranks efforts, so a meal takes its parts' most.
const EFFORT_ORDER: Effort[] = ["none", "out", "ready", "heat", "cook"];

/**
 * EffortOf returns how much work an eating time's portions are on a date:
 * a share of a cook cooked that day is cooking, a share of an earlier one
 * is heating leftovers, and foods and staples are ready.
 */
export const effortOf = (planned: PlannedEatingTime, date: string): Effort => {
  if (planned.eatingOut) {
    return "out";
  }
  let effort: Effort = "none";
  for (const portion of planned.portions) {
    let own: Effort = "ready";
    if (portion.cookId !== "") {
      own = portion.cookDate === date && !portion.leftover ? "cook" : "heat";
    }
    if (EFFORT_ORDER.indexOf(own) > EFFORT_ORDER.indexOf(effort)) {
      effort = own;
    }
  }
  return effort;
};

interface EffortDotProps {
  effort: Effort;
  // Whether it's eaten, which shows as a tick.
  eaten?: boolean;
}

/**
 * EffortDot is the round marker of a meal on the day's line, its color and
 * icon saying how much work it is.
 */
export const EffortDot: React.FC<EffortDotProps> = ({
  effort,
  eaten = false,
}) => {
  const base =
    "relative z-10 flex size-6 shrink-0 items-center justify-center rounded-full";
  if (eaten || effort === "ready") {
    return (
      <span className={`${base} bg-ready-tint text-ready`} aria-hidden="true">
        <CheckIcon className="size-3.5 stroke-[2.6]" />
      </span>
    );
  }
  switch (effort) {
    case "heat": {
      return (
        <span className={`${base} bg-heat-tint text-heat`} aria-hidden="true">
          <ClockIcon className="size-3.5 stroke-[2.4]" />
        </span>
      );
    }
    case "cook": {
      return (
        <span
          className={`${base} border-2 border-cook bg-white`}
          aria-hidden="true"
        />
      );
    }
    default: {
      return (
        <span
          className={`${base} border-2 border-dashed border-gray-400 bg-ground`}
          aria-hidden="true"
        />
      );
    }
  }
};

// CookDot is the marker of time spent cooking on the day's line.
export const CookDot: React.FC = () => (
  <span
    className="relative z-10 flex size-6 shrink-0 items-center justify-center rounded-full bg-cook text-white"
    aria-hidden="true"
  >
    <FireIcon className="size-3.5 stroke-[2.4]" />
  </span>
);

/**
 * WarningTitle names a warning in a few words, for a button that opens it,
 * like "Over daily energy"; the message says the rest.
 */
export const warningTitle = (warning: Warning): string => {
  const nutrient = (
    nutrientInfoOf(warning.nutrient)?.label ?? "target"
  ).toLowerCase();
  switch (warning.kind) {
    case WarningKind.TARGET_HIGH: {
      return `Over daily ${nutrient}`;
    }
    case WarningKind.TARGET_LOW: {
      return `Under daily ${nutrient}`;
    }
    case WarningKind.PROTEIN_LOW: {
      return "Short of protein";
    }
    case WarningKind.PORTION_TOO_LARGE: {
      return "Portion too large";
    }
    case WarningKind.CARBOHYDRATE_HIGH: {
      return "Too much carbohydrate";
    }
    case WarningKind.CARBOHYDRATE_UNPAIRED: {
      return "Carbs without protein or fat";
    }
    case WarningKind.SUGARS_HIGH: {
      return "Too much sugar";
    }
    case WarningKind.FAST_CARBS: {
      return "Fast carbs";
    }
    case WarningKind.LEFTOVER_PAST_FRIDGE_DAYS:
    case WarningKind.LEFTOVER_PAST_FREEZER_DAYS: {
      return "Kept too long";
    }
    case WarningKind.LEFTOVER_FREEZE: {
      return "Freeze the rest";
    }
    case WarningKind.LEFTOVER_THAW: {
      return "Thaw it first";
    }
    default: {
      return "Check the plan";
    }
  }
};
