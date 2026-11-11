import Loading from "@components/loading";
import type { PlanClient } from "@components/today/day-change";
import {
  dayLabel,
  displacedCookOf,
  openSlotsOf,
} from "@components/today/day-change";
import { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  primaryButton,
  sectionLabelClass,
  secondaryButton,
} from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import { moveCook } from "gen/plans/v1/plans-PlanService_connectquery";
import type { GetWeekResponse, PlannedPortion } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { addDays } from "helpers/dates";
import { householdMealLabel } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

interface DisplacedChoiceProps {
  week: GetWeekResponse;
  personId: string;
  date: string;
  today: string;
  // The time of the eating time the portions were at, "HH:MM".
  time: string;
  // The portions and staples not eaten there.
  portions: PlannedPortion[];
  // What to call the meal, e.g. "the lentil soup".
  name: string;
  // Done after the choice, like marking the eating time eaten out.
  then?: ((client: PlanClient) => Promise<void>) | undefined;
  onDone: () => void;
}

/**
 * DisplacedChoice asks what happens to a meal that wasn't eaten, as it's
 * the user's to choose: its cook moves to another day, with the meals
 * that can't come from it any more removed, or isn't cooked; or, when
 * others eat from the cook or it's a leftover, the meal moves to an open
 * slot or is removed. Foods are removed, as they're still in the pantry,
 * and staples skipped.
 */
const DisplacedChoice: React.FC<DisplacedChoiceProps> = ({
  week,
  personId,
  date,
  today,
  time,
  portions,
  name,
  then,
  onDone,
}) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const main = portions.find((portion) => portion.cookId !== "");
  const cook =
    main === undefined ? undefined : displacedCookOf(week, main, date);
  const meal = cook?.cook?.meal ?? HouseholdMeal.UNSPECIFIED;
  const cookDays = [1, 2, 3].map((days) => addDays(date, days));
  const [cookDate, setCookDate] = useState(cookDays[0] ?? date);
  const slots = openSlotsOf(week, personId, date, time, today);
  const [slot, setSlot] = useState(
    slots[0]?.eatingTimeId === undefined
      ? ""
      : `${slots[0].date}|${slots[0].eatingTimeId}`
  );
  const preview = useQuery(
    moveCook,
    { date: cookDate, id: cook?.cook?.id ?? "", meal, preview: true },
    { enabled: cook !== undefined }
  );

  type Choice = "move" | "remove";
  const apply = useMutation<void, ConnectError, Choice>({
    mutationFn: async (choice) => {
      const client = createClient(PlanService, transport);
      // Foods stay in the pantry, and staples are skipped, whatever's chosen.
      await Promise.all(
        portions
          .filter((portion) => portion !== main)
          .map(async (portion) => {
            await (portion.portionId === ""
              ? client.skipStaple({ date, id: portion.stapleId })
              : client.deletePortion({ id: portion.portionId }));
          })
      );
      if (main !== undefined) {
        if (cook?.cook !== undefined) {
          await (choice === "move"
            ? client.moveCook({ date: cookDate, id: cook.cook.id, meal })
            : client.deleteCook({ id: cook.cook.id }));
        } else if (choice === "move" && slot !== "") {
          const [slotDate = "", eatingTimeId = ""] = slot.split("|");
          await client.updatePortion({
            portion: { date: slotDate, eatingTimeId, id: main.portionId },
            updateMask: { paths: ["date", "eating_time_id"] },
          });
        } else {
          await client.deletePortion({ id: main.portionId });
        }
      }
      if (then !== undefined) {
        await then(client);
      }
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
      onDone();
    },
  });

  if (main === undefined) {
    return (
      <div className="flex flex-col gap-3">
        <button
          type="button"
          className={primaryButton}
          disabled={apply.isPending}
          onClick={() => {
            apply.mutate("remove");
          }}
        >
          {apply.isPending ? "Saving…" : "Done"}
        </button>
        <ErrorText error={apply.error} />
      </div>
    );
  }

  if (cook?.cook !== undefined) {
    const others = week.days.flatMap((day) =>
      day.people.flatMap((person) =>
        person.eatingTimes.flatMap((et) =>
          et.portions
            .filter(
              (portion) =>
                portion.cookId === cook.cook?.id &&
                portion.portionId !== main.portionId
            )
            .map(
              () =>
                `${dayLabel(day.date, today)} ${(et.eatingTime?.name ?? "").toLowerCase()}`
            )
        )
      )
    );
    const mealName = householdMealLabel(meal).toLowerCase();
    return (
      <div className="flex flex-col gap-4">
        <p className="text-base text-ink-soft">
          It&apos;s {dayLabel(date, today).toLowerCase()}&apos;s cook of{" "}
          {cook.cook.recipeName}.
        </p>
        <section className="flex flex-col gap-3 rounded-2xl border-2 border-ink-soft bg-white p-4">
          <h3 className="font-bold text-ink-soft">Cook it another day</h3>
          <Segmented
            label="Day to cook it"
            value={cookDate}
            options={cookDays.map((day) => ({
              label: `${dayLabel(day, today).slice(0, 3)}${mealName === "" ? "" : ` ${mealName}`}`,
              value: day,
            }))}
            onChange={setCookDate}
          />
          {preview.isPending && <Loading />}
          {(preview.data?.removedPortions.length ?? 0) > 0 && (
            <p className="grid grid-cols-[1.5rem_minmax(0,1fr)] items-center gap-2 rounded-xl bg-warn-tint p-3 text-sm text-warn-dark">
              <ExclamationTriangleIcon
                className="size-5 justify-self-center"
                aria-hidden="true"
              />
              <span>
                {preview.data?.removedPortions
                  .map((portion) => `${dayLabel(portion.date, today)}'s`)
                  .join(", ")}{" "}
                {preview.data?.removedPortions.length === 1
                  ? "meal is"
                  : "meals are"}{" "}
                removed: they came from this cook, which would now be ready
                later.
              </span>
            </p>
          )}
          <button
            type="button"
            className={primaryButton}
            disabled={apply.isPending}
            onClick={() => {
              apply.mutate("move");
            }}
          >
            Move to {dayLabel(cookDate, today)}
          </button>
        </section>
        <section className="flex flex-col gap-2 rounded-2xl border border-line bg-white p-4">
          <h3 className="font-bold text-ink-soft">Don&apos;t cook it</h3>
          {others.length > 0 && (
            <p className="text-sm text-muted">
              Also removed: {others.join(", ")}.
            </p>
          )}
          <button
            type="button"
            className={`${secondaryButton} self-start`}
            disabled={apply.isPending}
            onClick={() => {
              apply.mutate("remove");
            }}
          >
            Don&apos;t cook {cook.cook.recipeName}
          </button>
        </section>
        <ErrorText error={apply.error} />
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {slots.length > 0 && (
        <section className="flex flex-col gap-3 rounded-2xl border-2 border-ink-soft bg-white p-4">
          <h3 className="font-bold text-ink-soft">Move it to</h3>
          <div className="flex flex-col gap-2">
            {slots.slice(0, 4).map((open) => {
              const value = `${open.date}|${open.eatingTimeId}`;
              return (
                <label
                  key={value}
                  className={`flex items-center gap-3 rounded-xl px-3 py-2.5 text-base ${slot === value ? "bg-ink-soft font-bold text-white" : "bg-ground-dark/60 text-ink-soft"}`}
                >
                  <input
                    type="radio"
                    name="slot"
                    className="sr-only"
                    checked={slot === value}
                    onChange={() => {
                      setSlot(value);
                    }}
                  />
                  {open.label}
                </label>
              );
            })}
          </div>
          <button
            type="button"
            className={primaryButton}
            disabled={apply.isPending || slot === ""}
            onClick={() => {
              apply.mutate("move");
            }}
          >
            Move {name}
          </button>
        </section>
      )}
      <button
        type="button"
        className={secondaryButton}
        disabled={apply.isPending}
        onClick={() => {
          apply.mutate("remove");
        }}
      >
        Remove it from the plan
      </button>
      <p className={sectionLabelClass}>
        {main.leftover ? "The leftover stays in the fridge." : ""}
      </p>
      <ErrorText error={apply.error} />
    </div>
  );
};

export default DisplacedChoice;
