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
import { AmountUnit } from "gen/foods/v1/foods_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type { PlannedPortion, Warning } from "gen/plans/v1/plans_pb";
import { PlanService, Severity } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { suggestSides } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { SideSuggestion } from "gen/suggestions/v1/suggestions_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { formatAmount } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import React from "react";

/*
 * PLATE_COLORS are the colors of the plate's parts, in order: the main
 * meal, then sides and the rest.
 */
const PLATE_COLORS = [
  "var(--color-pine)",
  "var(--color-cook)",
  "var(--color-heat)",
  "var(--color-leaf)",
];

const plateColor = (index: number): string =>
  PLATE_COLORS[index % PLATE_COLORS.length] ?? "var(--color-muted)";

interface PlateProps {
  portions: PlannedPortion[];
}

/**
 * Plate shows a meal as a plate: each portion a slice by its weight, with
 * what it is and how much beside it.
 */
export const Plate: React.FC<PlateProps> = ({ portions }) => {
  const weighed = portions.filter((portion) => portion.grams > 0);
  const total = weighed.reduce((sum, portion) => sum + portion.grams, 0);
  if (total === 0) {
    return null;
  }
  const stops: string[] = [];
  weighed.reduce((from, portion, index) => {
    const to = from + (portion.grams / total) * 100;
    stops.push(`${plateColor(index)} ${from}% ${to}%`);
    return to;
  }, 0);
  return (
    <div className="flex items-center gap-5">
      <div
        className="relative size-28 shrink-0 rounded-full p-2 ring-1 ring-line ring-inset"
        aria-hidden="true"
      >
        <div
          className="size-full rounded-full"
          style={{ background: `conic-gradient(${stops.join(", ")})` }}
        />
        <div className="absolute inset-[38%] rounded-full bg-white" />
      </div>
      <ul className="flex min-w-0 flex-col gap-1.5">
        {weighed.map((portion, index) => (
          <li
            key={
              portion.portionId === "" ? portion.stapleId : portion.portionId
            }
            className="flex items-center gap-2 text-sm"
          >
            <span
              className="size-2.5 shrink-0 rounded-full"
              style={{ background: plateColor(index) }}
            />
            <span className="min-w-0 truncate font-medium text-ink-soft">
              {portion.name}
            </span>
            <span className="shrink-0 text-muted">
              {formatAmount(Math.round(portion.grams))} g
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
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
