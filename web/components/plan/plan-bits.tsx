import ConfirmButton from "@components/ui/confirm-button";
import { iconButton } from "@components/ui/styles";
import {
  ArrowUturnLeftIcon,
  BuildingStorefrontIcon,
  ExclamationTriangleIcon,
  InformationCircleIcon,
  PencilIcon,
  PlusIcon,
  SparklesIcon,
  TrashIcon,
  WrenchScrewdriverIcon,
} from "@heroicons/react/24/outline";
import type { Nutrients } from "gen/foods/v1/foods_pb";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import type { Person } from "gen/people/v1/people_pb";
import { EatingTimeKind } from "gen/people/v1/people_pb";
import type {
  PersonDay,
  PlannedEatingTime,
  PlannedPortion,
  Staple,
  Warning,
} from "gen/plans/v1/plans_pb";
import { Severity, WarningKind } from "gen/plans/v1/plans_pb";
import type { ScoreComponent } from "gen/suggestions/v1/suggestions_pb";
import { ScoreKind } from "gen/suggestions/v1/suggestions_pb";
import type { UnitSystem } from "gen/users/v1/users_pb";
import { formatShortDate } from "helpers/format";
import { formatServings } from "helpers/labels";
import { formatPackages, formatVolume, formatWeight } from "helpers/measures";
import { formatAmount, nutrientInfoOf } from "helpers/nutrients";
import { useUnitSystem } from "helpers/user";
import React from "react";
import { Link } from "react-router";

interface WarningListProps {
  warnings: Warning[];
}

// WarningList shows warnings, each saying what to change.
export const WarningList: React.FC<WarningListProps> = ({ warnings }) => {
  if (warnings.length === 0) {
    return null;
  }
  return (
    <ul className="flex flex-col gap-1">
      {warnings.map((warning) => {
        const info = warning.severity === Severity.INFO;
        const Icon = info ? InformationCircleIcon : ExclamationTriangleIcon;
        let color = "text-amber-900";
        if (info) {
          color = "text-sky-900";
        } else if (warning.uncertain) {
          color = "text-amber-800/80";
        }
        return (
          <li
            key={`${warning.kind}-${warning.nutrient}-${warning.cookId}-${warning.message}`}
            className={`flex gap-1.5 text-xs leading-snug ${color}`}
          >
            <Icon
              className={`mt-px size-3.5 shrink-0 ${info ? "text-sky-600" : "text-amber-600"}`}
              aria-label={info ? "Note" : "Warning"}
            />
            <span>{warning.message}</span>
          </li>
        );
      })}
    </ul>
  );
};

// PortionAmount describes how much of a planned portion is eaten.
const portionAmount = (portion: PlannedPortion, system: UnitSystem): string => {
  const parts: string[] = [];
  if (portion.foodId === "") {
    parts.push(formatServings(portion.servings));
  } else if (portion.unit === AmountUnit.PIECES) {
    parts.push(
      `${formatAmount(portion.amount)} ${portion.amount === 1 ? "piece" : "pieces"}`
    );
  } else if (portion.unit === AmountUnit.MILLILITERS) {
    parts.push(formatVolume(portion.amount, system));
  } else if (portion.unit === AmountUnit.PACKAGES) {
    parts.push(formatPackages(portion.amount));
  }
  if (portion.grams > 0) {
    parts.push(
      `${portion.unknownWeight ? "≥ " : ""}${formatWeight(portion.grams, system)}`
    );
  } else if (portion.unknownWeight) {
    parts.push("weight unknown");
  }
  return parts.join(" · ");
};

/**
 * PortionPath returns the page of what a portion or staple is: its recipe,
 * or its food.
 */
export const portionPath = (portion: PlannedPortion): string => {
  if (portion.recipeId !== "") {
    return `/recipes/${portion.recipeId}`;
  }
  return `/foods/${portion.foodId}`;
};

/**
 * PortionName names a portion or staple, linking to its recipe or food, for
 * more about it.
 */
export const PortionName: React.FC<{ portion: PlannedPortion }> = ({
  portion,
}) => (
  <Link
    to={portionPath(portion)}
    className="hover:underline"
    title={`See ${portion.name}`}
  >
    {portion.name}
  </Link>
);

interface PortionRowProps {
  portion: PlannedPortion;
  // Whether a mutation is running, which disables the buttons.
  disabled: boolean;
  onEdit: () => void;
  onDelete: () => void;
  onSkip: () => void;
  // Marks a staple eaten, or not, on the day.
  onEaten: (eaten: boolean) => void;
}

/**
 * PortionRow shows a portion or staple at an eating time, with its amount
 * and where it comes from, and buttons to change it.
 */
export const PortionRow: React.FC<PortionRowProps> = ({
  portion,
  disabled,
  onEdit,
  onDelete,
  onSkip,
  onEaten,
}) => {
  const staple = portion.stapleId !== "";
  const system = useUnitSystem();
  return (
    <li className="group flex items-start gap-1">
      {staple && (
        <input
          type="checkbox"
          className="mt-0.5 size-4 shrink-0 rounded border-gray-300 accent-brand-700 print:hidden"
          checked={portion.eaten}
          disabled={disabled}
          aria-label={`${portion.name} is eaten`}
          title={
            portion.eaten
              ? "Eaten: it was taken out of the pantry. Uncheck to put it back."
              : "Mark as eaten, taking it out of the pantry"
          }
          onChange={(event) => {
            onEaten(event.target.checked);
          }}
        />
      )}
      <div className="min-w-0 grow">
        <p
          className={`text-sm leading-snug text-gray-900 ${portion.eaten ? "line-through decoration-gray-400" : ""}`}
        >
          <PortionName portion={portion} />
          {portion.leftover && (
            <span
              className="ml-1 inline-flex items-center gap-0.5 rounded bg-violet-50 px-1 text-[11px] font-medium text-violet-800"
              title={`Cooked on ${formatShortDate(portion.cookDate)}`}
            >
              <ArrowUturnLeftIcon className="size-3" aria-hidden="true" />
              {formatShortDate(portion.cookDate)}
            </span>
          )}
          {staple && (
            <span className="ml-1 rounded bg-gray-100 px-1 text-[11px] font-medium text-gray-600">
              staple
            </span>
          )}
        </p>
        <p className="text-xs text-gray-500 tabular-nums">
          {portionAmount(portion, system)}
          {portion.notes !== "" && ` · ${portion.notes}`}
        </p>
      </div>
      <div className="flex shrink-0 print:hidden">
        {staple && !portion.eaten && (
          <button
            type="button"
            className="rounded px-1.5 py-1 text-xs font-medium whitespace-nowrap text-gray-600 hover:bg-gray-100 hover:text-gray-900 disabled:opacity-50"
            disabled={disabled}
            aria-label={`Skip ${portion.name} on this day`}
            title="Leave it out on this day only. It stays on the other days, and you can bring it back."
            onClick={onSkip}
          >
            Skip today
          </button>
        )}
        {!staple && (
          <>
            <button
              type="button"
              className={iconButton}
              disabled={disabled}
              aria-label={`Change or move ${portion.name}`}
              title="Change or move"
              onClick={onEdit}
            >
              <PencilIcon className="size-4" />
            </button>
            <ConfirmButton
              className={iconButton}
              title={`Remove ${portion.name}?`}
              message="It's removed from this eating time. The cook stays, with a serving more left over."
              confirmLabel="Remove"
              disabled={disabled}
              onConfirm={onDelete}
            >
              <TrashIcon
                className="size-4"
                aria-label={`Remove ${portion.name}`}
              />
            </ConfirmButton>
          </>
        )}
      </div>
    </li>
  );
};

/**
 * MacroLine summarizes the energy and macronutrients of an eating time or
 * a day, e.g. "412 kcal · 18 g protein · 40 g carbs", with "≥" for lower
 * bounds.
 */
export const macroLine = (
  nutrients: Nutrients | undefined,
  incomplete: string[] = []
): string => {
  const parts: string[] = [];
  const add = (
    value: number | undefined,
    field: string,
    unit: string,
    label: string
  ) => {
    if (value === undefined) {
      return;
    }
    const bound = incomplete.includes(field) ? "≥ " : "";
    parts.push(`${bound}${formatAmount(value)}${unit} ${label}`.trim());
  };
  add(nutrients?.energyKcal, "energy_kcal", " kcal", "");
  add(nutrients?.proteinGrams, "protein_grams", " g", "protein");
  add(nutrients?.carbohydrateGrams, "carbohydrate_grams", " g", "carbs");
  add(nutrients?.fatGrams, "fat_grams", " g", "fat");
  return parts.join(" · ");
};

interface EatingTimeBlockProps {
  planned: PlannedEatingTime;
  disabled: boolean;
  onAdd: () => void;
  // Suggests recipes for it while it's empty.
  onSuggest: () => void;
  onEdit: (portion: PlannedPortion) => void;
  onDelete: (portion: PlannedPortion) => void;
  onSkip: (portion: PlannedPortion) => void;
  onEaten: (portion: PlannedPortion, eaten: boolean) => void;
  // The staples skipped at it on its date, which can be brought back.
  skipped: Staple[];
  onUnskip: (staple: Staple) => void;
  // Marks the person eating out at it, or eating at home again.
  onEatingOut: (eatingOut: boolean) => void;
  // Suggests changes that fix its warnings.
  onFix: () => void;
}

/**
 * EatingTimeBlock shows an eating time of a person's day: what's planned at
 * it, its weight and macronutrients, and its warnings.
 */
export const EatingTimeBlock: React.FC<EatingTimeBlockProps> = ({
  planned,
  disabled,
  onAdd,
  onSuggest,
  onEdit,
  onDelete,
  onSkip,
  onEaten,
  skipped,
  onUnskip,
  onEatingOut,
  onFix,
}) => {
  const et = planned.eatingTime;
  const main = et?.kind === EatingTimeKind.MAIN;
  const empty = planned.portions.length === 0;
  const warned = planned.warnings.some(
    (warning) => warning.severity === Severity.WARNING
  );
  return (
    <li
      className={`rounded-md border px-2.5 py-2 ${
        warned ? "border-amber-200 bg-amber-50/40" : "border-gray-200 bg-white"
      } ${main ? "" : "ml-3"}`}
    >
      <div className="flex items-center gap-2">
        <h4
          className={`text-xs tracking-wide uppercase ${main ? "font-semibold text-gray-700" : "font-medium text-gray-500"}`}
        >
          {et?.name}
        </h4>
        {et !== undefined && et.time !== "" && (
          <span className="text-xs text-gray-400 tabular-nums">{et.time}</span>
        )}
        <span className="grow" />
        {!empty && (
          <span className="text-xs text-gray-500 tabular-nums">
            {formatAmount(planned.grams)} g
          </span>
        )}
        {!planned.eatingOut && (
          <button
            type="button"
            className={`${iconButton} print:hidden`}
            disabled={disabled}
            aria-label={`Add to ${et?.name ?? "eating time"}`}
            title="Add a food or leftovers"
            onClick={onAdd}
          >
            <PlusIcon className="size-4" />
          </button>
        )}
      </div>
      {planned.eatingOut && (
        <div className="flex items-center gap-2">
          <p className="flex min-w-0 grow items-center gap-1 text-sm text-gray-600">
            <BuildingStorefrontIcon
              className="size-4 shrink-0 text-gray-500"
              aria-hidden="true"
            />
            <span>
              Eating out
              {planned.eatingOutNotes !== "" && (
                <span className="text-gray-500">
                  {" "}
                  · {planned.eatingOutNotes}
                </span>
              )}
            </span>
          </p>
          <button
            type="button"
            className="inline-flex shrink-0 items-center gap-0.5 rounded px-1.5 py-1 text-xs font-medium text-brand-700 hover:bg-brand-50 disabled:opacity-50 print:hidden"
            disabled={disabled}
            aria-label={`Plan ${et?.name ?? "this eating time"} at home`}
            title="Plan it at home again, with its staples"
            onClick={() => {
              onEatingOut(false);
            }}
          >
            <ArrowUturnLeftIcon className="size-3.5" aria-hidden="true" />
            Eat at home
          </button>
        </div>
      )}
      {!planned.eatingOut && empty && (
        <div className="flex items-center gap-2">
          <p className="text-xs text-gray-400">Nothing planned</p>
          <button
            type="button"
            className="inline-flex items-center gap-0.5 rounded px-1 text-xs font-medium text-brand-700 hover:bg-brand-50 disabled:opacity-50 print:hidden"
            disabled={disabled}
            title={`Suggest recipes for ${et?.name ?? "this eating time"}`}
            onClick={onSuggest}
          >
            <SparklesIcon className="size-3.5" aria-hidden="true" />
            Suggest
          </button>
          <button
            type="button"
            className="inline-flex items-center gap-0.5 rounded px-1 text-xs font-medium text-gray-600 hover:bg-gray-100 disabled:opacity-50 print:hidden"
            disabled={disabled}
            title="Eating out: leave it out of the plan, with its staples"
            aria-label={`Eating out at ${et?.name ?? "this eating time"}`}
            onClick={() => {
              onEatingOut(true);
            }}
          >
            <BuildingStorefrontIcon className="size-3.5" aria-hidden="true" />
            Eating out
          </button>
        </div>
      )}
      {!empty && (
        <ul className="mt-1 flex flex-col gap-1">
          {planned.portions.map((portion) => (
            <PortionRow
              key={
                portion.portionId === ""
                  ? `staple-${portion.stapleId}`
                  : portion.portionId
              }
              portion={portion}
              disabled={disabled}
              onEdit={() => {
                onEdit(portion);
              }}
              onDelete={() => {
                onDelete(portion);
              }}
              onSkip={() => {
                onSkip(portion);
              }}
              onEaten={(eaten) => {
                onEaten(portion, eaten);
              }}
            />
          ))}
        </ul>
      )}
      {skipped.length > 0 && (
        <ul className="mt-1 flex flex-col gap-1">
          {skipped.map((staple) => (
            <li
              key={staple.id}
              className="flex items-center gap-1 text-sm text-gray-400"
            >
              <span className="min-w-0 grow">
                <span className="line-through">{staple.name}</span>{" "}
                <span className="text-xs">skipped today</span>
              </span>
              <button
                type="button"
                className="inline-flex shrink-0 items-center gap-0.5 rounded px-1.5 py-1 text-xs font-medium text-brand-700 hover:bg-brand-50 disabled:opacity-50 print:hidden"
                disabled={disabled}
                aria-label={`Bring back ${staple.name} on this day`}
                onClick={() => {
                  onUnskip(staple);
                }}
              >
                <ArrowUturnLeftIcon className="size-3.5" aria-hidden="true" />
                Bring back
              </button>
            </li>
          ))}
        </ul>
      )}
      {!empty && macroLine(planned.nutrients) !== "" && (
        <p className="mt-1 text-[11px] text-gray-500 tabular-nums">
          {macroLine(planned.nutrients)}
        </p>
      )}
      {planned.warnings.length > 0 && (
        <div className="mt-1.5">
          <WarningList warnings={planned.warnings} />
        </div>
      )}
      {warned && !empty && <FixButton disabled={disabled} onClick={onFix} />}
    </li>
  );
};

/**
 * NothingPlanned says why a day has nothing planned: that it's all eaten
 * out, that only meals out are planned, or that nothing is.
 */
const nothingPlanned = (day: PersonDay): string => {
  const out = day.eatingTimes.filter((planned) => planned.eatingOut).length;
  if (out === 0) {
    return "Nothing planned";
  }
  return out === day.eatingTimes.length
    ? "Eating out"
    : "Nothing planned at home";
};

interface FixButtonProps {
  disabled: boolean;
  onClick: () => void;
}

// FixButton opens the changes suggested to fix warnings.
const FixButton: React.FC<FixButtonProps> = ({ disabled, onClick }) => (
  <button
    type="button"
    className="mt-1 inline-flex items-center gap-0.5 rounded px-1 text-xs font-medium text-brand-700 hover:bg-brand-50 disabled:opacity-50 print:hidden"
    disabled={disabled}
    title="Suggest changes that fix the warnings"
    onClick={onClick}
  >
    <WrenchScrewdriverIcon className="size-3.5" aria-hidden="true" />
    Suggest a fix
  </button>
);

interface DayTotalsProps {
  day: PersonDay;
  person: Person | undefined;
  disabled: boolean;
  // Suggests changes to the day that fix its warnings.
  onFix: () => void;
}

/**
 * DayTotals shows a person's day against their daily targets, compactly:
 * each target with how much is planned, flagged when it's off.
 */
export const DayTotals: React.FC<DayTotalsProps> = ({
  day,
  person,
  disabled,
  onFix,
}) => {
  const targets = person?.dailyTargets ?? [];
  const flagged = new Set(
    day.warnings
      .filter(
        (warning) =>
          warning.kind === WarningKind.TARGET_LOW ||
          warning.kind === WarningKind.TARGET_HIGH
      )
      .map((warning) => warning.nutrient)
  );
  return (
    <div className="flex flex-col gap-1.5 rounded-md bg-gray-50 px-2.5 py-2">
      <p className="text-xs text-gray-600 tabular-nums">
        <span className="font-semibold text-gray-700">Day</span>{" "}
        {day.grams > 0
          ? `${formatAmount(day.grams)} g · ${macroLine(day.nutrients, day.incompleteNutrients)}`
          : nothingPlanned(day)}
      </p>
      {targets.length > 0 && day.grams > 0 && (
        <ul className="flex flex-wrap gap-1">
          {targets.map((target) => {
            const info = nutrientInfoOf(target.nutrient);
            if (info === undefined) {
              return null;
            }
            const value = day.nutrients?.[info.key] ?? 0;
            const bound = day.incompleteNutrients.includes(info.field)
              ? "≥ "
              : "";
            const { minPerDay: min, maxPerDay: max } = target;
            let limit = "";
            if (min !== undefined && max !== undefined) {
              limit = `${formatAmount(min)}–${formatAmount(max)}`;
            } else if (min === undefined) {
              limit = `≤ ${formatAmount(max ?? 0)}`;
            } else {
              limit = formatAmount(min);
            }
            const off = flagged.has(target.nutrient);
            return (
              <li
                key={target.nutrient}
                className={`rounded px-1.5 py-0.5 text-[11px] tabular-nums ring-1 ring-inset ${
                  off
                    ? "bg-amber-50 text-amber-900 ring-amber-200"
                    : "bg-white text-gray-700 ring-gray-200"
                }`}
                title={`${info.label} planned, of the daily target`}
              >
                {info.label} {bound}
                {formatAmount(value)} / {limit} {info.unit}
              </li>
            );
          })}
        </ul>
      )}
      <WarningList warnings={day.warnings} />
      {day.grams > 0 &&
        day.warnings.some(
          (warning) => warning.severity === Severity.WARNING
        ) && <FixButton disabled={disabled} onClick={onFix} />}
    </div>
  );
};

// KIND_LABELS name the kinds of score components, for their titles.
export const KIND_LABELS: Record<ScoreKind, string> = {
  [ScoreKind.UNSPECIFIED]: "",
  [ScoreKind.SHARED_INGREDIENTS]: "Shared ingredients",
  [ScoreKind.PACK_LEFTOVERS]: "Rest of a pack",
  [ScoreKind.PANTRY]: "Pantry",
  [ScoreKind.NUTRITION]: "Nutrition",
  [ScoreKind.RULES]: "Rules",
  [ScoreKind.KEEPING]: "Keeping",
  [ScoreKind.VARIETY]: "Variety",
  [ScoreKind.MEAL_TAG]: "Meal",
  [ScoreKind.FAVORITE]: "Favorite",
  [ScoreKind.LEFTOVER]: "Leftover",
  [ScoreKind.COOKED]: "Cooked lately",
};

// FormatPoints formats a component's points with their sign, e.g. "+6".
export const formatPoints = (points: number): string =>
  `${points > 0 ? "+" : "−"}${formatAmount(Math.abs(points))}`;

interface ComponentChipProps {
  component: ScoreComponent;
}

// ComponentChip shows a part of a suggestion's score: its points and why.
export const ComponentChip: React.FC<ComponentChipProps> = ({ component }) => {
  const positive = component.points > 0;
  return (
    <li
      className={`inline-flex max-w-full items-start gap-1 rounded px-1.5 py-0.5 text-xs leading-snug ring-1 ring-inset ${
        positive
          ? "bg-brand-50 text-gray-800 ring-brand-200"
          : "bg-amber-50 text-amber-950 ring-amber-200"
      }`}
      title={KIND_LABELS[component.kind]}
    >
      <span
        className={`shrink-0 font-semibold tabular-nums ${positive ? "text-brand-700" : "text-amber-700"}`}
      >
        {formatPoints(component.points)}
      </span>
      <span className="min-w-0">{component.reason}</span>
    </li>
  );
};
