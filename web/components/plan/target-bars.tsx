import type { Nutrient, Nutrients } from "gen/foods/v1/foods_pb";
import type { NutrientTarget } from "gen/people/v1/people_pb";
import { nutrientInfoOf } from "helpers/nutrients";
import React from "react";

// TargetState is how a day stands against one of a person's daily targets.
export interface TargetState {
  nutrient: Nutrient;
  label: string;
  unit: string;
  value: number;
  // The amount the bar is full at: the maximum, or the minimum without one.
  limit: number;
  // Whether the day is under the minimum, or over the maximum.
  low: boolean;
  high: boolean;
}

// SHORT_LABELS are names that fit under a narrow bar.
const SHORT_LABELS: Record<string, string> = {
  Carbohydrate: "Carbs",
  Energy: "Energy",
};

/**
 * TargetStates returns how a day's nutrients stand against a person's
 * daily targets, in the targets' order, leaving out those of nutrients the
 * app doesn't know.
 */
export const targetStates = (
  targets: NutrientTarget[],
  nutrients: Nutrients | undefined
): TargetState[] =>
  targets.flatMap((target) => {
    const info = nutrientInfoOf(target.nutrient);
    const limit = target.maxPerDay ?? target.minPerDay;
    if (info === undefined || limit === undefined || limit <= 0) {
      return [];
    }
    const value = nutrients?.[info.key] ?? 0;
    return [
      {
        high: target.maxPerDay !== undefined && value > target.maxPerDay,
        label: info.label,
        limit,
        low: target.minPerDay !== undefined && value < target.minPerDay,
        nutrient: target.nutrient,
        unit: info.unit,
        value,
      },
    ];
  });

// BarColor is the fill of a target's bar: amber once it's missed.
const barColor = (state: TargetState): string =>
  state.low || state.high ? "bg-warn" : "bg-pine";

// Fill is how full a target's bar is, as a percentage.
const fill = (state: TargetState): number =>
  Math.min(100, Math.round((state.value / state.limit) * 100));

interface TargetBarsProps {
  targets: NutrientTarget[];
  // The day's nutrients, of everything planned for the person.
  nutrients: Nutrients | undefined;
  // Whether to show narrow bars side by side, without the amounts.
  compact?: boolean | undefined;
  // Called with a missed target that's tapped, to open its fix.
  onMissed?: ((nutrient: Nutrient) => void) | undefined;
}

/**
 * TargetBars shows a person's day against their daily targets, a bar for
 * each: how much of the target is planned, amber once the day is under a
 * minimum or over a maximum. It says at a glance what a list of warnings
 * takes a page to. Nothing shows for someone without targets.
 */
const TargetBars: React.FC<TargetBarsProps> = ({
  targets,
  nutrients,
  compact = false,
  onMissed,
}) => {
  const states = targetStates(targets, nutrients);
  if (states.length === 0) {
    return null;
  }
  if (compact) {
    return (
      <div className="flex grow gap-2">
        {states.map((state) => (
          <div
            key={state.nutrient}
            className="flex min-w-0 grow basis-0 flex-col gap-1"
            title={`${state.label}: ${Math.round(state.value)} of ${Math.round(state.limit)} ${state.unit}`}
          >
            <div className="h-1.5 overflow-hidden rounded-full bg-line">
              <div
                className={`h-full rounded-full ${barColor(state)}`}
                style={{ width: `${fill(state)}%` }}
              />
            </div>
            <span
              className={`truncate text-[11px] leading-none ${state.low || state.high ? "font-bold text-warn-dark" : "text-muted"}`}
            >
              {SHORT_LABELS[state.label] ?? state.label}
            </span>
          </div>
        ))}
      </div>
    );
  }
  return (
    <ul className="flex flex-col gap-2.5">
      {states.map((state) => {
        const missed = state.low || state.high;
        let note = "";
        if (state.low) {
          note = " · under";
        } else if (state.high) {
          note = " · over";
        }
        const row = (
          <>
            <span className="flex items-baseline justify-between gap-2 text-sm">
              <span className="font-medium text-ink-soft">{state.label}</span>
              <span
                className={missed ? "font-bold text-warn-dark" : "text-muted"}
              >
                {Math.round(state.value)} of {Math.round(state.limit)}{" "}
                {state.unit}
                {note}
              </span>
            </span>
            <span className="block h-2 overflow-hidden rounded-full bg-line">
              <span
                className={`block h-full rounded-full ${barColor(state)}`}
                style={{ width: `${fill(state)}%` }}
              />
            </span>
          </>
        );
        return (
          <li key={state.nutrient}>
            {missed && onMissed !== undefined ? (
              <button
                type="button"
                className="flex w-full flex-col gap-1 text-left"
                onClick={() => {
                  onMissed(state.nutrient);
                }}
              >
                {row}
              </button>
            ) : (
              <div className="flex flex-col gap-1">{row}</div>
            )}
          </li>
        );
      })}
    </ul>
  );
};

export default TargetBars;
