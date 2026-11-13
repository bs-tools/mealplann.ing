import { ErrorText } from "@components/ui/query-error";
import { sectionLabelClass } from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import {
  ChevronRightIcon,
  PlusIcon,
  SparklesIcon,
} from "@heroicons/react/24/outline";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Nutrients } from "gen/foods/v1/foods_pb";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { listStaples } from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedPortion, Staple } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { suggestSides } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { SideSuggestion } from "gen/suggestions/v1/suggestions_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { formatGrams } from "helpers/format";
import { formatServings } from "helpers/labels";
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

/**
 * StapleSkips are a person's staples at an eating time skipped on a date,
 * and the mutations to skip and bring one back.
 */
export interface StapleSkips {
  skipped: Staple[];
  skip: (stapleId: string) => void;
  unskip: (stapleId: string) => void;
  pending: boolean;
  error: ConnectError | null;
}

/**
 * UseStapleSkips returns a person's staples at an eating time skipped on a
 * date, and skips and brings them back, so a staple like a breakfast tea
 * is skipped, or brought back, on the meal it's part of.
 */
export const useStapleSkips = (
  personId: string,
  eatingTimeId: string,
  date: string
): StapleSkips => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const staples = useQuery(listStaples, {});
  const change = useMutation<
    void,
    ConnectError,
    { stapleId: string; skip: boolean }
  >({
    mutationFn: async ({ stapleId, skip }) => {
      const client = createClient(PlanService, transport);
      await (skip
        ? client.skipStaple({ date, id: stapleId })
        : client.unskipStaple({ date, id: stapleId }));
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        ShoppingService,
        SuggestionService,
      ]);
    },
  });
  return {
    error: change.error,
    pending: change.isPending,
    skip: (stapleId) => {
      change.mutate({ skip: true, stapleId });
    },
    skipped: (staples.data?.staples ?? []).filter(
      (staple) =>
        staple.personId === personId &&
        staple.eatingTimeId === eatingTimeId &&
        staple.skippedDates.includes(date)
    ),
    unskip: (stapleId) => {
      change.mutate({ skip: false, stapleId });
    },
  };
};

interface PortionListProps {
  portions: PlannedPortion[];
  // Opens a portion to change or remove it, if the list allows it.
  onSelect?: (portion: PlannedPortion) => void;
  // Adds something to the meal, if the list allows it.
  onAdd?: (() => void) | undefined;
  // Skips the meal's staples on its date, and brings them back, if allowed.
  staples?: StapleSkips | undefined;
}

/**
 * PortionList lists what a meal is: each recipe or food, with its weight,
 * so the sheet's title can be just the meal, like "Breakfast". Given
 * onSelect and onAdd, each portion opens to change it, and a last row adds
 * more, like a piece of fruit to a light lunch. Given staples, a staple,
 * like a morning tea, is skipped that day from its row, and a skipped one
 * shows, struck through, to bring back; staples are otherwise changed on
 * the person's page.
 */
export const PortionList: React.FC<PortionListProps> = ({
  portions,
  onSelect,
  onAdd,
  staples,
}) => (
  <ul className="flex flex-col divide-y divide-line rounded-2xl border border-line bg-white">
    {portions.map((portion) => {
      const row = (
        <>
          <span className="min-w-0 grow text-ink-soft">{portion.name}</span>
          {portion.grams > 0 && (
            <span className="shrink-0 text-muted tabular-nums">
              {formatGrams(portion.grams)}
            </span>
          )}
        </>
      );
      return (
        <li
          key={portion.portionId === "" ? portion.stapleId : portion.portionId}
        >
          {onSelect === undefined || portion.portionId === "" ? (
            <div className="flex items-baseline gap-3 px-4 py-2.5 text-sm">
              {row}
              {staples !== undefined && portion.stapleId !== "" ? (
                <button
                  type="button"
                  className="shrink-0 text-xs font-bold text-pine hover:underline disabled:opacity-50"
                  disabled={staples.pending}
                  title={`Skip ${portion.name} this day only; it's back the next`}
                  onClick={() => {
                    staples.skip(portion.stapleId);
                  }}
                >
                  Skip
                </button>
              ) : (
                onSelect !== undefined && (
                  <span className="shrink-0 text-xs text-muted">Staple</span>
                )
              )}
            </div>
          ) : (
            <button
              type="button"
              className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm hover:bg-ground/60"
              aria-label={`Change ${portion.name}`}
              onClick={() => {
                onSelect(portion);
              }}
            >
              {row}
              <ChevronRightIcon
                className="size-4 shrink-0 text-muted/70"
                aria-hidden="true"
              />
            </button>
          )}
        </li>
      );
    })}
    {staples?.skipped.map((staple) => (
      <li
        key={staple.id}
        className="flex items-baseline gap-3 px-4 py-2.5 text-sm"
      >
        <span className="min-w-0 grow text-muted line-through">
          {staple.name}
        </span>
        <span className="shrink-0 text-xs text-muted">Skipped</span>
        <button
          type="button"
          className="shrink-0 text-xs font-bold text-pine hover:underline disabled:opacity-50"
          disabled={staples.pending}
          onClick={() => {
            staples.unskip(staple.id);
          }}
        >
          Bring back
        </button>
      </li>
    ))}
    {onAdd !== undefined && (
      <li>
        <button
          type="button"
          className="flex w-full items-center gap-2 px-4 py-2.5 text-left text-sm font-bold text-pine hover:bg-pine-50/60"
          onClick={onAdd}
        >
          <PlusIcon className="size-4" aria-hidden="true" />
          Add to this meal
        </button>
      </li>
    )}
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

// SideName names a side, a food or a recipe.
export const sideName = (side: SideSuggestion): string =>
  side.recipeId === "" ? side.foodName : side.recipeName;

// SideAmount says how much of a side to add, e.g. "150 g" or "1½ servings".
const sideAmount = (side: SideSuggestion): string =>
  side.recipeId === ""
    ? `${formatAmount(side.grams)} g`
    : `${formatServings(side.servings)}${side.grams > 0 ? `, ${formatAmount(side.grams)} g` : ""}`;

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
      if (side.recipeId === "") {
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
        return;
      }
      /*
       * A recipe side is cooked: a share of the pot already cooked that day
       * for someone else, made bigger if it must be, or a new cook of it.
       */
      if (side.cookId !== "") {
        if (side.cookServingsNeeded > 0) {
          await client.updateCook({
            cook: { id: side.cookId, servings: side.cookServingsNeeded },
            updateMask: { paths: ["servings"] },
          });
        }
        await client.createPortion({
          portion: {
            date,
            eatingTimeId,
            personId,
            what: {
              case: "cook",
              value: { cookId: side.cookId, servings: side.servings },
            },
          },
        });
        return;
      }
      await client.createPortion({
        newCook: { recipeId: side.recipeId, servings: 0 },
        portion: {
          date,
          eatingTimeId,
          personId,
          what: {
            case: "cook",
            value: { cookId: "", servings: side.servings },
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
              key={side.recipeId === "" ? side.foodId : side.recipeId}
              type="button"
              className={`rounded-full px-4 py-2 text-sm ${chipClass} disabled:opacity-50`}
              disabled={choose.isPending || side.grams === 0}
              title={side.reason}
              aria-pressed={side.planned}
              onClick={() => {
                choose.mutate(side);
              }}
            >
              {sideName(side)}
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
              {sideName(suggested)}, {sideAmount(suggested)}
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
