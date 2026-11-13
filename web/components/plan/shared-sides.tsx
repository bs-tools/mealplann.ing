import { sideName } from "@components/today/plate";
import { ErrorText } from "@components/ui/query-error";
import { sectionLabelClass } from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { createQueryOptions, useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type { HouseholdMeal } from "gen/people/v1/people_pb";
import type { PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { suggestSides } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { SideSuggestion } from "gen/suggestions/v1/suggestions_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { invalidateServices } from "helpers/query";
import React from "react";

// SharedEater is someone at a shared meal: their eating time at it.
export interface SharedEater {
  personId: string;
  eatingTimeId: string;
}

interface SharedSidesProps {
  eaters: SharedEater[];
  date: string;
  // The shared meal, which a new cook of a side is cooked for.
  meal: HouseholdMeal;
  // The week's cooks, to know what's left of a pot a side is taken from.
  cooks: PlannedCook[];
}

// SideKey identifies a side among each eater's suggestions.
const sideKey = (side: SideSuggestion): string =>
  side.recipeId === "" ? `food:${side.foodId}` : `recipe:${side.recipeId}`;

/**
 * SharedSides offers the sides a shared meal is served with, like rice or
 * a salad, for everyone at once: choosing one adds it to each person's
 * plate, sized for them, from one pot, and replaces the other sides; the
 * one chosen takes it off. A side shows chosen when everyone has it.
 */
const SharedSides: React.FC<SharedSidesProps> = ({
  eaters,
  date,
  meal,
  cooks,
}) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const lists = useQueries({
    queries: eaters.map((eater) =>
      createQueryOptions(
        suggestSides,
        { date, eatingTimeId: eater.eatingTimeId, personId: eater.personId },
        { transport }
      )
    ),
  });
  // Each eater's suggestions, by side, in the order of the first's.
  const perEater = lists.map(
    (list) =>
      new Map(
        (list.data?.sides ?? []).map((side) => [sideKey(side), side] as const)
      )
  );
  const options = lists[0]?.data?.sides ?? [];

  const choose = useMutation<void, ConnectError, string>({
    mutationFn: async (key) => {
      const client = createClient(PlanService, transport);
      const everyone = perEater.every(
        (sides) => sides.get(key)?.planned === true
      );
      // Every side planned for anyone is taken off first.
      await Promise.all(
        perEater.flatMap((sides) =>
          [...sides.values()]
            .filter((side) => side.planned)
            .map(async (side) => {
              /*
               * A side's pot nobody eats from then isn't cooked, but for
               * the side chosen again for everyone, whose pot they share.
               */
              await client.deletePortion({
                deleteUnusedCook: everyone || sideKey(side) !== key,
                id: side.portionId,
              });
            })
        )
      );
      if (everyone) {
        return;
      }
      const shares = eaters.flatMap((eater, index) => {
        const side = perEater[index]?.get(key);
        return side === undefined ? [] : [{ eater, side }];
      });
      const [first] = shares;
      if (first === undefined) {
        return;
      }
      if (first.side.recipeId === "") {
        await Promise.all(
          shares.map(async ({ eater, side }) => {
            await client.createPortion({
              portion: {
                date,
                eatingTimeId: eater.eatingTimeId,
                personId: eater.personId,
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
          })
        );
        return;
      }
      /*
       * A recipe side is one pot for everyone: the one already cooked that
       * day, made bigger if what's left of it is short, or a new cook of
       * what everyone's shares add up to.
       */
      const total = shares.reduce((sum, { side }) => sum + side.servings, 0);
      let { cookId } = first.side;
      if (cookId === "") {
        const created = await client.createCook({
          cook: {
            date,
            meal,
            recipeId: first.side.recipeId,
            servings: Math.max(0.25, Math.ceil(total * 4) / 4),
          },
        });
        cookId = created.cook?.id ?? "";
      } else {
        const pot = cooks.find((other) => other.cook?.id === cookId);
        const short = total - (pot?.leftServings ?? 0);
        if (pot?.cook !== undefined && short > 0) {
          await client.updateCook({
            cook: {
              id: cookId,
              servings: Math.ceil((pot.cook.servings + short) * 4) / 4,
            },
            updateMask: { paths: ["servings"] },
          });
        }
      }
      await Promise.all(
        shares.map(async ({ eater, side }) => {
          await client.createPortion({
            portion: {
              date,
              eatingTimeId: eater.eatingTimeId,
              personId: eater.personId,
              what: {
                case: "cook",
                value: { cookId, servings: side.servings },
              },
            },
          });
        })
      );
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

  if (options.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-col gap-2">
      <h4 className={sectionLabelClass}>Served with</h4>
      <div className="flex flex-wrap gap-2">
        {options.map((side) => {
          const key = sideKey(side);
          const planned = perEater.filter(
            (sides) => sides.get(key)?.planned === true
          ).length;
          let chipClass =
            "bg-white text-ink-soft ring-1 ring-line ring-inset hover:ring-muted";
          if (planned === eaters.length) {
            chipClass = "bg-ink-soft font-bold text-white";
          } else if (planned > 0) {
            chipClass =
              "bg-white font-bold text-ink-soft ring-2 ring-ink-soft ring-inset";
          }
          return (
            <button
              key={key}
              type="button"
              className={`rounded-full px-4 py-2 text-sm ${chipClass} disabled:opacity-50`}
              disabled={choose.isPending || side.grams === 0}
              title={side.reason}
              aria-pressed={planned === eaters.length}
              onClick={() => {
                choose.mutate(key);
              }}
            >
              {sideName(side)}
              {planned > 0 && planned < eaters.length && " · for some"}
            </button>
          );
        })}
      </div>
      <ErrorText error={choose.error} />
    </div>
  );
};

export default SharedSides;
