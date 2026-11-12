import { ErrorText } from "@components/ui/query-error";
import { sectionLabelClass } from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import {
  ExclamationTriangleIcon,
  SparklesIcon,
} from "@heroicons/react/24/outline";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Nutrients } from "gen/foods/v1/foods_pb";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type { PlannedPortion, Warning } from "gen/plans/v1/plans_pb";
import { PlanService, Severity } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { suggestSides } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { SideSuggestion } from "gen/suggestions/v1/suggestions_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { formatGrams } from "helpers/format";
import { formatAmount } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import React from "react";

/*
 * MACROS are the parts of a meal's energy the plate shows, with their
 * kilocalories per gram, to weigh each by the energy it brings.
 */
const MACROS: {
  key: "proteinGrams" | "carbohydrateGrams" | "fatGrams";
  label: string;
  kcalPerGram: number;
  color: string;
}[] = [
  { color: "#2f6b4f", kcalPerGram: 4, key: "proteinGrams", label: "Protein" },
  {
    color: "#b8895a",
    kcalPerGram: 4,
    key: "carbohydrateGrams",
    label: "Carbohydrate",
  },
  { color: "#7c93b5", kcalPerGram: 9, key: "fatGrams", label: "Fat" },
];

interface PlateProps {
  // What the meal has in all: its portions together, at their sizes.
  nutrients: Nutrients | undefined;
}

/**
 * Plate shows a meal's macronutrients as a plate: protein, carbohydrate
 * and fat each a slice by the energy it brings, with the meal's energy in
 * the middle and each one's grams and share beside it.
 */
export const Plate: React.FC<PlateProps> = ({ nutrients }) => {
  const parts = MACROS.map((macro) => {
    const grams = nutrients?.[macro.key] ?? 0;
    return {
      color: macro.color,
      grams,
      kcal: grams * macro.kcalPerGram,
      key: macro.key,
      label: macro.label,
    };
  });
  const total = parts.reduce((sum, part) => sum + part.kcal, 0);
  if (total <= 0) {
    return null;
  }
  const stops: string[] = [];
  parts.reduce((from, part) => {
    const to = from + (part.kcal / total) * 100;
    stops.push(`${part.color} ${from}% ${to}%`);
    return to;
  }, 0);
  const energy = nutrients?.energyKcal;
  return (
    <div className="flex items-center gap-5">
      <div
        className="relative size-32 shrink-0 rounded-full p-2 ring-1 ring-line ring-inset"
        aria-hidden="true"
      >
        <div
          className="size-full rounded-full"
          style={{ background: `conic-gradient(${stops.join(", ")})` }}
        />
        <div className="absolute inset-[30%] flex flex-col items-center justify-center rounded-full bg-white leading-tight">
          <span className="text-base font-bold text-ink-soft tabular-nums">
            {formatAmount(Math.round(energy ?? total))}
          </span>
          <span className="text-[11px] text-muted">kcal</span>
        </div>
      </div>
      <ul className="flex min-w-0 flex-col gap-2">
        {parts.map((part) => (
          <li key={part.key} className="flex items-center gap-2 text-sm">
            <span
              className="size-2.5 shrink-0 rounded-full"
              style={{ background: part.color }}
            />
            <span className="font-medium text-ink-soft">{part.label}</span>
            <span className="text-muted tabular-nums">
              {formatAmount(Math.round(part.grams))} g ·{" "}
              {Math.round((part.kcal / total) * 100)}%
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
};

interface PortionListProps {
  portions: PlannedPortion[];
}

/**
 * PortionList lists what a meal is: each recipe or food, with its weight,
 * so the sheet's title can be just the meal, like "Breakfast".
 */
export const PortionList: React.FC<PortionListProps> = ({ portions }) => (
  <ul className="flex flex-col divide-y divide-line rounded-2xl border border-line bg-white">
    {portions.map((portion) => (
      <li
        key={portion.portionId === "" ? portion.stapleId : portion.portionId}
        className="flex items-baseline justify-between gap-3 px-4 py-2.5 text-sm"
      >
        <span className="min-w-0 text-ink-soft">{portion.name}</span>
        {portion.grams > 0 && (
          <span className="shrink-0 text-muted tabular-nums">
            {formatGrams(portion.grams)}
          </span>
        )}
      </li>
    ))}
  </ul>
);

/**
 * SplitFix splits a fix's description into its action, short enough for a
 * button, like "Have each portion smaller, at 55%", and the detail after
 * its colon, of each portion's new size.
 */
export const splitFix = (
  description: string
): { action: string; detail: string } => {
  const at = description.indexOf(": ");
  return at < 0
    ? { action: description, detail: "" }
    : {
        action: description.slice(0, at),
        detail: description.slice(at + 2),
      };
};

interface MealWarningsProps {
  warnings: Warning[];
}

// MealWarnings shows an eating time's warnings, each a note to act on.
export const MealWarnings: React.FC<MealWarningsProps> = ({ warnings }) => (
  <>
    {warnings
      .filter((warning) => warning.severity === Severity.WARNING)
      .map((warning) => (
        <p
          key={`${warning.kind}-${warning.nutrient}-${warning.message}`}
          className="grid grid-cols-[1.5rem_minmax(0,1fr)] items-center gap-2 rounded-xl bg-warn-tint p-3 text-sm text-warn-dark"
        >
          <ExclamationTriangleIcon
            className="size-5 justify-self-center"
            aria-hidden="true"
          />
          <span>{warning.message}</span>
        </p>
      ))}
  </>
);

interface SidesCardProps {
  personId: string;
  eatingTimeId: string;
  date: string;
}

/**
 * SidesCard offers the sides the meal's recipe is served with as chips,
 * the planned one filled and the best for the day suggested with why, to
 * add one, swap it for another, or take it off.
 */
export const SidesCard: React.FC<SidesCardProps> = ({
  personId,
  eatingTimeId,
  date,
}) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const sides = useQuery(suggestSides, { date, eatingTimeId, personId });
  const list = sides.data?.sides ?? [];
  const planned = list.filter((side) => side.planned);

  // Choose adds a side, replacing the others planned, or takes it off.
  const choose = useMutation<void, ConnectError, SideSuggestion>({
    mutationFn: async (side) => {
      const client = createClient(PlanService, transport);
      await Promise.all(
        planned.map(async (other) => {
          await client.deletePortion({ id: other.portionId });
        })
      );
      if (side.planned) {
        return;
      }
      await client.createPortion({
        portion: {
          date,
          eatingTimeId,
          personId,
          what: {
            case: "food",
            value: {
              amount: side.grams,
              foodId: side.foodId,
              unit: AmountUnit.GRAMS,
            },
          },
        },
      });
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
    },
  });

  if (list.length === 0) {
    return null;
  }
  // The best side, if it fits and isn't planned, is suggested with why.
  const [best] = list;
  const suggested =
    best !== undefined &&
    !best.planned &&
    best.grams > 0 &&
    best.warnings.length === 0
      ? best
      : undefined;
  return (
    <section className="flex flex-col gap-3">
      <h3 className={sectionLabelClass}>Served with</h3>
      <div className="flex flex-wrap gap-2">
        {list.map((side) => {
          let chipClass =
            "bg-white text-ink-soft ring-1 ring-line ring-inset hover:ring-muted";
          if (side.planned) {
            chipClass = "bg-ink-soft font-bold text-white";
          } else if (side === suggested) {
            chipClass =
              "bg-white font-bold text-pine ring-2 ring-pine ring-inset";
          }
          return (
            <button
              key={side.foodId}
              type="button"
              className={`rounded-full px-4 py-2 text-sm ${chipClass} disabled:opacity-50`}
              disabled={choose.isPending || side.grams === 0}
              title={side.reason}
              aria-pressed={side.planned}
              onClick={() => {
                choose.mutate(side);
              }}
            >
              {side.foodName}
              {side === suggested && " · suggested"}
            </button>
          );
        })}
      </div>
      {suggested !== undefined && (
        <p className="grid grid-cols-[1.5rem_minmax(0,1fr)] items-center gap-2 rounded-2xl bg-ready-tint p-4 text-sm text-ink-soft">
          <SparklesIcon
            className="size-5 justify-self-center text-ready"
            aria-hidden="true"
          />
          <span>
            <span className="font-bold text-ready">
              {suggested.foodName}, {formatAmount(suggested.grams)} g
            </span>
            <br />
            {suggested.reason.replace(/^./u, (first) => first.toUpperCase())}.
          </span>
        </p>
      )}
      <ErrorText error={choose.error} />
    </section>
  );
};
