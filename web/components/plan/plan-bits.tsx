import {
  ExclamationTriangleIcon,
  InformationCircleIcon,
} from "@heroicons/react/24/outline";
import type { Warning } from "gen/plans/v1/plans_pb";
import { Severity } from "gen/plans/v1/plans_pb";
import type { ScoreComponent } from "gen/suggestions/v1/suggestions_pb";
import { ScoreKind } from "gen/suggestions/v1/suggestions_pb";
import { formatAmount } from "helpers/nutrients";
import React from "react";

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
        let color = "text-warn-dark";
        if (info) {
          color = "text-heat";
        } else if (warning.uncertain) {
          color = "text-warn-dark/80";
        }
        return (
          <li
            key={`${warning.kind}-${warning.nutrient}-${warning.cookId}-${warning.message}`}
            className={`flex gap-1.5 text-xs leading-snug ${color}`}
          >
            <Icon
              className={`mt-px size-3.5 shrink-0 ${info ? "text-heat" : "text-warn"}`}
              aria-label={info ? "Note" : "Warning"}
            />
            <span>{warning.message}</span>
          </li>
        );
      })}
    </ul>
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
  [ScoreKind.TASTES]: "Tastes",
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
          ? "bg-pine-50 text-ink-soft ring-pine-200"
          : "bg-warn-tint text-warn-dark ring-warn-line"
      }`}
      title={KIND_LABELS[component.kind]}
    >
      <span
        className={`shrink-0 font-semibold tabular-nums ${positive ? "text-pine" : "text-warn"}`}
      >
        {formatPoints(component.points)}
      </span>
      <span className="min-w-0">{component.reason}</span>
    </li>
  );
};
