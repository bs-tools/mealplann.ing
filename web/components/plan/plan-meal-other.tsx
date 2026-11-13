import FoodPicker from "@components/foods/food-picker";
import type { PickedFood } from "@components/foods/food-picker";
import { SelectField, TextField } from "@components/ui/fields";
import { ErrorText } from "@components/ui/query-error";
import {
  pillPrimaryButton,
  primaryButton,
  sectionLabelClass,
} from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { listFrozenLeftovers } from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { addDays, today } from "helpers/dates";
import { formatShortDate } from "helpers/format";
import { AMOUNT_UNITS, formatServings, optionValue } from "helpers/labels";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

// MealEater is someone a meal is planned for: their eating time at it.
export interface MealEater {
  personId: string;
  eatingTimeId: string;
}

// CookedOn says when a cook was, relative to today for the last two days.
const cookedOn = (date: string): string => {
  const now = today();
  if (date === now) {
    return "today";
  }
  return date === addDays(now, -1) ? "yesterday" : formatShortDate(date);
};

/**
 * ShareOf is each eater's share of what's left of a cook: a serving each,
 * or as much as there is, in quarters, but at least a quarter.
 */
const shareOf = (left: number, eaters: number): number =>
  Math.max(0.25, Math.min(1, Math.floor((left / eaters) * 4) / 4));

interface LeftoversChoiceProps {
  eaters: MealEater[];
  date: string;
  // The cooks to eat the rest of: the week's, and earlier ones that keep.
  cooks: PlannedCook[];
  // Called once it's planned, e.g. to close the dialog.
  onPlanned: () => Promise<void>;
}

/**
 * LeftoversChoice plans the meal as the rest of an earlier cook, from the
 * fridge or the freezer, a share of it for each person at the meal: a
 * serving each, or a fair share of what's left.
 */
export const LeftoversChoice: React.FC<LeftoversChoiceProps> = ({
  eaters,
  date,
  cooks,
  onPlanned,
}) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const freezer = useQuery(listFrozenLeftovers, {});
  const fridge = cooks.filter(
    (planned) =>
      planned.leftServings > 0 &&
      planned.cook !== undefined &&
      // A cook of the same day only has leftovers once it's cooked.
      (planned.cook.date < date ||
        (planned.cook.date === date && planned.cook.done))
  );
  const frozen = (freezer.data?.leftovers ?? []).filter(
    (leftover) => leftover.remainingServings > 0 && !leftover.expired
  );
  const plan = useMutation<
    void,
    ConnectError,
    { cookId: string; left: number; fromFreezer: boolean }
  >({
    mutationFn: async ({ cookId, left, fromFreezer }) => {
      const client = createClient(PlanService, transport);
      const servings = shareOf(left, eaters.length);
      // Each share is taken in turn, as they come out of the same cook.
      for (const eater of eaters) {
        // oxlint-disable-next-line no-await-in-loop
        await (fromFreezer
          ? client.takeFromFreezer({
              cookId,
              date,
              eatingTimeId: eater.eatingTimeId,
              personId: eater.personId,
              servings,
            })
          : client.createPortion({
              portion: {
                date,
                eatingTimeId: eater.eatingTimeId,
                personId: eater.personId,
                what: { case: "cook", value: { cookId, servings } },
              },
            }));
      }
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
      await onPlanned();
    },
  });

  // Nothing shows without leftovers: the suggestions follow.
  if (fridge.length === 0 && frozen.length === 0) {
    return null;
  }
  const rows = [
    ...fridge.map((planned) => ({
      cookId: planned.cook?.id ?? "",
      detail: `Cooked ${cookedOn(planned.cook?.date ?? "")} · ${formatServings(planned.leftServings)} left`,
      fromFreezer: false,
      left: planned.leftServings,
      name: planned.cook?.recipeName ?? "",
    })),
    ...frozen.map((leftover) => ({
      cookId: leftover.cook?.id ?? "",
      detail: `Frozen ${formatShortDate(leftover.cook?.date ?? "")} · ${formatServings(leftover.remainingServings)} left`,
      fromFreezer: true,
      left: leftover.remainingServings,
      name: leftover.cook?.recipeName ?? "",
    })),
  ];
  return (
    <div className="flex flex-col gap-2">
      <h3 className={sectionLabelClass}>Leftovers to finish</h3>
      <ul className="divide-y divide-line rounded-2xl border border-line bg-white px-4">
        {rows.map((row) => (
          <li
            key={`${row.fromFreezer ? "freezer" : "fridge"}-${row.cookId}`}
            className="flex items-center gap-3 py-3"
          >
            <div className="flex min-w-0 grow flex-col gap-0.5">
              <span className="leading-snug font-bold text-ink-soft">
                {row.name}
              </span>
              <span className="text-sm text-muted">{row.detail}</span>
            </div>
            <button
              type="button"
              className={`${pillPrimaryButton} shrink-0`}
              disabled={plan.isPending || eaters.length === 0}
              onClick={() => {
                plan.mutate(row);
              }}
            >
              {plan.isPending && plan.variables.cookId === row.cookId
                ? "Planning…"
                : "Plan this"}
            </button>
          </li>
        ))}
      </ul>
      <ErrorText error={plan.error} />
    </div>
  );
};

interface FoodChoiceProps {
  eaters: MealEater[];
  date: string;
  // Called once it's planned, e.g. to close the dialog.
  onPlanned: () => Promise<void>;
}

/**
 * FoodChoice plans the meal as an amount of a food for each person at it,
 * like a soup from a carton, or a ready meal.
 */
export const FoodChoice: React.FC<FoodChoiceProps> = ({
  eaters,
  date,
  onPlanned,
}) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const [food, setFood] = useState<PickedFood | null>(null);
  const [amount, setAmount] = useState("");
  const [unit, setUnit] = useState(AmountUnit.GRAMS);
  const [invalid, setInvalid] = useState("");
  const plan = useMutation<
    void,
    ConnectError,
    { foodId: string; value: number }
  >({
    mutationFn: async ({ foodId, value }) => {
      const client = createClient(PlanService, transport);
      await Promise.all(
        eaters.map(async (eater) => {
          await client.createPortion({
            portion: {
              date,
              eatingTimeId: eater.eatingTimeId,
              personId: eater.personId,
              what: { case: "food", value: { amount: value, foodId, unit } },
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
      await onPlanned();
    },
  });
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        setInvalid("");
        const value = parseOptionalNumber(amount);
        if (food === null) {
          setInvalid("Choose a food.");
          return;
        }
        if (value === null || value === undefined || value <= 0) {
          setInvalid("The amount must be a number more than 0.");
          return;
        }
        plan.mutate({ foodId: food.id, value });
      }}
    >
      <div className="grid grid-cols-[1fr_5.5rem_5.5rem] items-end gap-2">
        <div className="flex flex-col gap-1">
          <span className="block text-xs font-medium text-muted">Food</span>
          <FoodPicker value={food} onChange={setFood} label="Food" />
        </div>
        <TextField
          label="Each"
          value={amount}
          inputMode="decimal"
          onChange={setAmount}
        />
        <SelectField
          label="Unit"
          value={String(unit)}
          options={AMOUNT_UNITS.map((option) => ({
            label: option.label,
            value: String(option.value),
          }))}
          onChange={(value) => {
            setUnit(optionValue(AMOUNT_UNITS, value, AmountUnit.GRAMS));
          }}
        />
      </div>
      {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
      <ErrorText error={plan.error} />
      <button
        type="submit"
        className={`${primaryButton} self-end`}
        disabled={plan.isPending || eaters.length === 0}
      >
        {plan.isPending ? "Planning…" : "Plan meal"}
      </button>
    </form>
  );
};
