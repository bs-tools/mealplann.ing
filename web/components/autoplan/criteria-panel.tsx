import { inputClass, labelClass } from "@components/ui/styles";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import { Emphasis } from "gen/suggestions/v1/suggestions_pb";
import React from "react";

// Criteria are what the user wants of a suggested plan.
export interface Criteria {
  // The first date (YYYY-MM-DD) and how many days.
  start: string;
  days: number;
  // The shared meals to plan.
  meals: HouseholdMeal[];
  // Whether to plan each person's own eating times, like snacks.
  ownEatingTimes: boolean;
  variety: Emphasis;
  favorites: Emphasis;
  pantry: Emphasis;
  shopping: Emphasis;
  nutrition: Emphasis;
  // The longest a recipe may take, in minutes, 0 for any time.
  maxMinutes: number;
  // The tags of recipes not to plan.
  excludeTags: string[];
}

// DAY_OPTIONS are the numbers of days to plan offered.
const DAY_OPTIONS = [3, 5, 7, 10, 14];

// MINUTE_OPTIONS are the longest times offered, 0 for any.
const MINUTE_OPTIONS = [0, 20, 30, 45, 60, 90];

// MEALS are the shared meals, in a day's order, with their names.
export const MEALS: [HouseholdMeal, string][] = [
  [HouseholdMeal.BREAKFAST, "Breakfast"],
  [HouseholdMeal.LUNCH, "Lunch"],
  [HouseholdMeal.DINNER, "Dinner"],
];

// EMPHASES are the emphases offered, in order, with their names.
const EMPHASES: [Emphasis, string][] = [
  [Emphasis.OFF, "Off"],
  [Emphasis.LOW, "Less"],
  [Emphasis.NORMAL, "Normal"],
  [Emphasis.HIGH, "More"],
];

// EmphasisKey names the criteria that take an emphasis.
type EmphasisKey =
  | "variety"
  | "favorites"
  | "pantry"
  | "shopping"
  | "nutrition";

// EMPHASIS_ROWS describe the criteria that take an emphasis.
const EMPHASIS_ROWS: { key: EmphasisKey; label: string; hint: string }[] = [
  {
    hint: "Avoid the same recipe on nearby days.",
    key: "variety",
    label: "Variety",
  },
  {
    hint: "Plan the recipes you starred more often.",
    key: "favorites",
    label: "Favorites",
  },
  {
    hint: "Use up the pantry and the rest of packs.",
    key: "pantry",
    label: "Use what's there",
  },
  {
    hint: "Share ingredients, so there's less to buy and less goes off.",
    key: "shopping",
    label: "Short shop",
  },
  {
    hint: "Meet everyone's daily targets.",
    key: "nutrition",
    label: "Nutrition",
  },
];

// Segment styles a button of a segmented control, filled when chosen.
const segment = (active: boolean): string =>
  `px-2 py-1 text-xs font-medium first:rounded-l-md last:rounded-r-md ring-1 ring-inset -ml-px first:ml-0 ${
    active
      ? "relative z-10 bg-ink text-white ring-ink"
      : "bg-white text-gray-700 ring-gray-300 hover:bg-gray-50"
  }`;

// Chip styles a toggle among several, filled when on.
const chip = (active: boolean): string =>
  `rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${
    active
      ? "bg-brand-600 text-white ring-brand-600"
      : "bg-white text-gray-700 ring-gray-300 hover:bg-gray-50"
  }`;

interface CriteriaPanelProps {
  criteria: Criteria;
  // The meals someone eats, which are the ones offered.
  meals: HouseholdMeal[];
  // Whether someone has eating times of their own, like snacks.
  hasOwnEatingTimes: boolean;
  // The tags of the household's recipes, to exclude some.
  tags: string[];
  onChange: (criteria: Criteria) => void;
}

/**
 * CriteriaPanel sets what a suggested plan should be like: its days and
 * meals, how much each criterion counts, the longest a recipe may take,
 * and the tags to leave out.
 */
const CriteriaPanel: React.FC<CriteriaPanelProps> = ({
  criteria,
  meals,
  hasOwnEatingTimes,
  tags,
  onChange,
}) => {
  const set = (change: Partial<Criteria>) => {
    onChange({ ...criteria, ...change });
  };
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-4">
        <label className="flex flex-col gap-1">
          <span className={labelClass}>From</span>
          <input
            type="date"
            className={`${inputClass} w-40`}
            value={criteria.start}
            required
            onChange={(event) => {
              if (event.target.value !== "") {
                set({ start: event.target.value });
              }
            }}
          />
        </label>
        <div className="flex flex-col gap-1">
          <span className={labelClass}>Days</span>
          <div className="flex" role="group" aria-label="Days">
            {DAY_OPTIONS.map((days) => (
              <button
                key={days}
                type="button"
                aria-pressed={criteria.days === days}
                className={`${segment(criteria.days === days)} min-w-9 py-1.5 text-sm tabular-nums`}
                onClick={() => {
                  set({ days });
                }}
              >
                {days}
              </button>
            ))}
          </div>
        </div>
        <div className="flex flex-col gap-1">
          <span className={labelClass}>Meals</span>
          <div
            className="flex flex-wrap gap-1.5"
            role="group"
            aria-label="Meals"
          >
            {MEALS.filter(([meal]) => meals.includes(meal)).map(
              ([meal, name]) => {
                const active = criteria.meals.includes(meal);
                return (
                  <button
                    key={meal}
                    type="button"
                    aria-pressed={active}
                    className={`${chip(active)} py-1 text-sm`}
                    onClick={() => {
                      const next = active
                        ? criteria.meals.filter((other) => other !== meal)
                        : [...criteria.meals, meal];
                      // At least one meal stays, or there's nothing to plan.
                      if (next.length > 0) {
                        set({ meals: next });
                      }
                    }}
                  >
                    {name}
                  </button>
                );
              }
            )}
            {hasOwnEatingTimes && (
              <button
                type="button"
                aria-pressed={criteria.ownEatingTimes}
                title="Each person's eating times that aren't shared meals, like snacks"
                className={`${chip(criteria.ownEatingTimes)} py-1 text-sm`}
                onClick={() => {
                  set({ ownEatingTimes: !criteria.ownEatingTimes });
                }}
              >
                Snacks
              </button>
            )}
          </div>
        </div>
        <label className="flex flex-col gap-1">
          <span className={labelClass}>Takes at most</span>
          <select
            className={`${inputClass} w-32`}
            value={criteria.maxMinutes}
            onChange={(event) => {
              set({ maxMinutes: Number(event.target.value) });
            }}
          >
            {MINUTE_OPTIONS.map((minutes) => (
              <option key={minutes} value={minutes}>
                {minutes === 0 ? "Any time" : `${minutes} minutes`}
              </option>
            ))}
          </select>
        </label>
      </div>
      <div className="grid grid-cols-1 gap-x-6 gap-y-2 sm:grid-cols-2 xl:grid-cols-3">
        {EMPHASIS_ROWS.map((row) => (
          <div
            key={row.key}
            className="flex items-center justify-between gap-3"
            title={row.hint}
          >
            <span className="text-sm text-gray-800">{row.label}</span>
            <div className="flex" role="group" aria-label={row.label}>
              {EMPHASES.map(([emphasis, name]) => {
                const current =
                  criteria[row.key] === Emphasis.UNSPECIFIED
                    ? Emphasis.NORMAL
                    : criteria[row.key];
                return (
                  <button
                    key={emphasis}
                    type="button"
                    aria-pressed={current === emphasis}
                    className={segment(current === emphasis)}
                    onClick={() => {
                      set({ [row.key]: emphasis });
                    }}
                  >
                    {name}
                  </button>
                );
              })}
            </div>
          </div>
        ))}
      </div>
      {tags.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className={labelClass}>Leave out recipes tagged</span>
          <div className="flex flex-wrap gap-1.5">
            {tags.map((tag) => {
              const active = criteria.excludeTags.includes(tag);
              return (
                <button
                  key={tag}
                  type="button"
                  aria-pressed={active}
                  className={
                    active
                      ? "rounded-full bg-amber-100 px-2.5 py-0.5 text-xs font-medium text-amber-900 line-through ring-1 ring-amber-300 ring-inset"
                      : chip(false)
                  }
                  onClick={() => {
                    set({
                      excludeTags: active
                        ? criteria.excludeTags.filter((other) => other !== tag)
                        : [...criteria.excludeTags, tag],
                    });
                  }}
                >
                  {tag}
                </button>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
};

export default CriteriaPanel;
