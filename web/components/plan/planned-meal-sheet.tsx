import CookActions from "@components/plan/cook-actions";
import PlanMealDialog from "@components/plan/plan-meal-dialog";
import type { MealRow, RowSlot } from "@components/plan/plan-rows";
import { portionAmount } from "@components/plan/plan-rows";
import PortionDialog from "@components/plan/portion-dialog";
import SharedSides from "@components/plan/shared-sides";
import WarningsLink from "@components/plan/warnings-link";
import {
  Plate,
  PortionList,
  SidesCard,
  useStapleSkips,
} from "@components/today/plate";
import { ConfirmDialog } from "@components/ui/confirm-button";
import Modal from "@components/ui/modal";
import MoreMenu from "@components/ui/more-menu";
import { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  pillButton,
  pillDangerButton,
  secondaryButton,
  sectionLabelClass,
} from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useMutation, useQuery, useTransport } from "@connectrpc/connect-query";
import { ChevronRightIcon } from "@heroicons/react/24/outline";
import {
  useMutation as useTanstackMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import {
  deletePortion,
  listStaples,
  setEatingOut,
  updateCook,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type {
  GetWeekResponse,
  PlanDay,
  PlannedPortion,
} from "gen/plans/v1/plans_pb";
import { PlanService, Severity } from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { parseDate } from "helpers/dates";
import { formatServings } from "helpers/labels";
import { formatAmount } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

// CookUse is what some servings of a cook go to, for its bar.
interface CookUse {
  key: string;
  servings: number;
  label: string;
  // Whether it's the meal shown, which stands out.
  own: boolean;
  kind: "eaten" | "frozen" | "left";
}

/**
 * CookUsesOf returns what a cook's servings go to over the week: each
 * portion, in order of date, then the freezer and what's left over.
 */
const cookUsesOf = (
  week: GetWeekResponse,
  cookId: string,
  shown: PlannedPortion[]
): CookUse[] => {
  const uses: CookUse[] = week.days.flatMap((day) =>
    day.people.flatMap((person) =>
      person.eatingTimes.flatMap((planned) =>
        planned.portions
          .filter(
            (portion) => portion.cookId === cookId && portion.portionId !== ""
          )
          .map((portion): CookUse => ({
            key: portion.portionId,
            kind: "eaten",
            label: `${parseDate(day.date).toLocaleDateString(undefined, { weekday: "short" })} · ${person.personName} ${formatAmount(portion.servings)}`,
            own: shown.some((other) => other.portionId === portion.portionId),
            servings: portion.servings,
          }))
      )
    )
  );
  const planned = week.cooks.find((other) => other.cook?.id === cookId);
  const frozen = planned?.cook?.frozenServings ?? 0;
  if (frozen > 0) {
    uses.push({
      key: "frozen",
      kind: "frozen",
      label: `Freezer ${formatAmount(frozen)}`,
      own: false,
      servings: frozen,
    });
  }
  const left = planned?.leftServings ?? 0;
  if (left > 0.01) {
    uses.push({
      key: "left",
      kind: "left",
      label: `Not planned ${formatAmount(left)}`,
      own: false,
      servings: left,
    });
  }
  return uses;
};

const USE_CLASS: Record<CookUse["kind"], string> = {
  eaten: "bg-cook/35",
  frozen: "bg-heat-tint",
  left: "border-2 border-dashed border-muted/40",
};

interface CookBarProps {
  week: GetWeekResponse;
  cookId: string;
  portions: PlannedPortion[];
}

/**
 * CookBar shows what a cook's servings go to: the meal shown, the other
 * meals that eat from it, the freezer and what's left, so it's clear what
 * changing one does to the rest. Each part is as wide as its servings, so
 * a cook making more than is planned shows mostly as unplanned: the bar
 * offers to cook only what's planned, as nobody eats the rest.
 */
const CookBar: React.FC<CookBarProps> = ({ week, cookId, portions }) => {
  const queryClient = useQueryClient();
  const planned = week.cooks.find((other) => other.cook?.id === cookId);
  const cook = planned?.cook;
  const uses = cookUsesOf(week, cookId, portions);
  const shrink = useMutation(updateCook, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
    },
  });
  if (planned === undefined || cook === undefined || uses.length < 2) {
    return null;
  }
  const left = planned.leftServings;
  /*
   * What's planned of it, rounded up to a quarter serving, as recipes
   * scale by quarters; a cook already made can't be made smaller.
   */
  const needed = Math.ceil((cook.servings - left) * 4 - 0.001) / 4;
  const canShrink = !cook.done && left > 0.01 && needed > 0;
  return (
    <section className="flex flex-col gap-2">
      <h3 className={sectionLabelClass}>
        This cook makes {formatAmount(cook.servings)}
        {left > 0.01 && ` · ${formatAmount(cook.servings - left)} planned`}
      </h3>
      <div className="flex gap-1">
        {uses.map((use) => (
          <span
            key={use.key}
            className={`h-6 min-w-2 rounded-xl ${use.own ? "bg-cook" : USE_CLASS[use.kind]}`}
            style={{ flexBasis: 0, flexGrow: use.servings }}
          />
        ))}
      </div>
      <ul className="flex flex-wrap gap-x-3 gap-y-1 text-xs">
        {uses.map((use) => (
          <li
            key={use.key}
            className={`flex items-center gap-1.5 ${use.own ? "font-bold text-ink-soft" : "text-muted"}`}
          >
            <span
              className={`size-2.5 shrink-0 rounded-full ${use.own ? "bg-cook" : USE_CLASS[use.kind]}`}
              aria-hidden="true"
            />
            {use.label}
          </li>
        ))}
      </ul>
      {canShrink && (
        <div className="flex flex-col gap-1">
          <button
            type="button"
            className={`${pillButton} self-start`}
            disabled={shrink.isPending}
            onClick={() => {
              shrink.mutate({
                cook: { id: cook.id, servings: needed },
                updateMask: { paths: ["servings"] },
              });
            }}
          >
            {shrink.isPending
              ? "Changing it…"
              : `Cook only ${formatServings(needed)}`}
          </button>
          <ErrorText error={shrink.error} />
        </div>
      )}
    </section>
  );
};

interface PlannedMealSheetProps {
  week: GetWeekResponse;
  day: PlanDay;
  row: MealRow;
  onClose: () => void;
}

/**
 * PlannedMealSheet shows a planned meal: as a plate, with the sides it's
 * served with, its warnings and their fixes, and what its cook's servings
 * go to, for each person who eats it. Each portion opens to change or
 * remove it, and more can be added to the meal.
 */
const PlannedMealSheet: React.FC<PlannedMealSheetProps> = ({
  week,
  day,
  row,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const people = useQuery(listPeople, {});
  /*
   * The portion being changed, null to add one, or undefined while
   * neither: its dialog takes the sheet's place until it's closed.
   */
  const [editing, setEditing] = useState<PlannedPortion | null | undefined>(
    undefined
  );
  const eaters = row.slots.filter(
    (slot) => slot.planned.eatingOut || slot.planned.portions.length > 0
  );
  const [personId, setPersonId] = useState(eaters[0]?.person.personId ?? "");
  /*
   * The slot is found again in the refreshed week, as changes to it come
   * back there.
   */
  const fresh = (slot: RowSlot | undefined): RowSlot | undefined => {
    if (slot === undefined) {
      return undefined;
    }
    const person = week.days
      .find((other) => other.date === day.date)
      ?.people.find((other) => other.personId === slot.person.personId);
    const planned = person?.eatingTimes.find(
      (other) => other.eatingTime?.id === slot.planned.eatingTime?.id
    );
    return person === undefined || planned === undefined
      ? slot
      : { person, planned };
  };
  const slot = fresh(
    eaters.find((other) => other.person.personId === personId) ?? eaters[0]
  );
  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [
      PlanService,
      PantryService,
      ShoppingService,
      SuggestionService,
    ]);
  };
  const remove = useMutation(deletePortion, { onSuccess });
  const stapleSkips = useStapleSkips(
    slot?.person.personId ?? "",
    slot?.planned.eatingTime?.id ?? "",
    day.date
  );
  const eatIn = useMutation(setEatingOut, { onSuccess });
  const transport = useTransport();
  const staples = useQuery(listStaples, {});
  const recipes = useQuery(listRecipes, {});
  const [swapping, setSwapping] = useState(false);
  const [removingMeal, setRemovingMeal] = useState(false);

  /*
   * The shared meal's cook: the one everyone at a shared meal eats from,
   * cooked for it.
   */
  const shared =
    row.meal === HouseholdMeal.UNSPECIFIED
      ? undefined
      : week.cooks.find(
          (planned) =>
            planned.cook !== undefined &&
            planned.cook.date === day.date &&
            planned.cook.meal === row.meal &&
            eaters.some((other) =>
              other.planned.portions.some(
                (portion) => portion.cookId === planned.cook?.id
              )
            )
        );
  /*
   * RemoveMeal takes the meal off for everyone: their portions at it, and
   * its cook, unless another meal eats the rest of it, which it then only
   * makes enough for.
   */
  const removeMeal = useTanstackMutation<void, ConnectError>({
    mutationFn: async () => {
      const client = createClient(PlanService, transport);
      const here = eaters.flatMap((other) =>
        other.planned.portions.filter((portion) => portion.portionId !== "")
      );
      const cookId = shared?.cook?.id ?? "";
      const elsewhere = week.days.flatMap((planDay) =>
        planDay.people.flatMap((person) =>
          person.eatingTimes.flatMap((planned) =>
            planned.portions.filter(
              (portion) =>
                portion.cookId === cookId &&
                cookId !== "" &&
                !here.some((other) => other.portionId === portion.portionId)
            )
          )
        )
      );
      await Promise.all(
        here
          .filter(
            (portion) => elsewhere.length > 0 || portion.cookId !== cookId
          )
          .map(async (portion) =>
            client.deletePortion({ id: portion.portionId })
          )
      );
      if (cookId === "") {
        return;
      }
      if (elsewhere.length === 0) {
        await client.deleteCook({ id: cookId });
        return;
      }
      const left = elsewhere.reduce(
        (sum, portion) => sum + portion.servings,
        0
      );
      await client.updateCook({
        cook: { id: cookId, servings: Math.ceil(left * 4) / 4 },
        updateMask: { paths: ["servings"] },
      });
    },
    onSuccess: async () => {
      await onSuccess();
      onClose();
    },
  });

  const weekday = parseDate(day.date).toLocaleDateString(undefined, {
    weekday: "long",
  });
  const kicker = `${weekday}${row.time === "" ? "" : ` · ${row.time}`}`;
  let content: React.ReactNode = null;
  if (slot === undefined) {
    content = null;
  } else if (slot.planned.eatingOut) {
    content = (
      <div className="flex flex-col gap-3">
        <p className="text-base text-ink-soft">
          {slot.person.personName} is eating out
          {slot.planned.eatingOutNotes === ""
            ? "."
            : `: ${slot.planned.eatingOutNotes}`}
        </p>
        <button
          type="button"
          className={`${secondaryButton} self-start`}
          disabled={eatIn.isPending}
          onClick={() => {
            eatIn.mutate({
              date: day.date,
              eatingOut: false,
              eatingTimeId: slot.planned.eatingTime?.id ?? "",
              personId: slot.person.personId,
            });
          }}
        >
          Not eating out
        </button>
        <ErrorText error={eatIn.error} />
      </div>
    );
  } else {
    const { portions } = slot.planned;
    const main = portions.find((portion) => portion.cookId !== "");
    const cook = week.cooks.find(
      (other) => main !== undefined && other.cook?.id === main.cookId
    );
    const eatingTimeId = slot.planned.eatingTime?.id ?? "";
    // The person's day's warnings, listed on the warnings page.
    const warnings = [
      ...slot.person.warnings,
      ...slot.person.eatingTimes.flatMap((planned) => planned.warnings),
    ].filter((warning) => warning.severity === Severity.WARNING);
    content = (
      <div className="flex flex-col gap-5">
        <PortionList
          portions={portions}
          onSelect={setEditing}
          staples={stapleSkips}
          onAdd={() => {
            setEditing(null);
          }}
        />
        <Plate nutrients={slot.planned.nutrients} />
        {shared === undefined && main !== undefined && (
          <SidesCard
            personId={slot.person.personId}
            eatingTimeId={eatingTimeId}
            date={day.date}
          />
        )}
        <WarningsLink
          count={warnings.length}
          date={day.date}
          about={`for ${slot.person.personName}'s day`}
        />
        {shared === undefined && main !== undefined && (
          <CookBar week={week} cookId={main.cookId} portions={portions} />
        )}
        {shared === undefined &&
          cook !== undefined &&
          main?.leftover === false && (
            <CookActions
              planned={cook}
              weekCooks={week.cooks}
              onDeleted={onClose}
            />
          )}
        <div className="flex flex-wrap gap-2">
          {shared === undefined &&
            main !== undefined &&
            main.recipeId !== "" && (
              <Link to={`/recipes/${main.recipeId}`} className={pillButton}>
                The recipe
              </Link>
            )}
          <button
            type="button"
            className={pillDangerButton}
            disabled={remove.isPending}
            onClick={() => {
              void Promise.all(
                portions
                  .filter((portion) => portion.portionId !== "")
                  .map(async (portion) =>
                    remove.mutateAsync({
                      deleteUnusedCook: true,
                      id: portion.portionId,
                    })
                  )
              ).then(onClose);
            }}
          >
            {eaters.length > 1
              ? `Remove ${slot.person.personName}'s portion`
              : "Remove"}
          </button>
        </div>
        <ErrorText error={remove.error} />
      </div>
    );
  }

  if (editing !== undefined && slot !== undefined) {
    return (
      <PortionDialog
        slot={{
          date: day.date,
          eatingTimeId: slot.planned.eatingTime?.id ?? "",
          personId: slot.person.personId,
        }}
        portion={editing ?? undefined}
        people={people.data?.people ?? []}
        dates={week.days.map((other) => other.date)}
        cooks={[...week.earlierLeftovers, ...week.cooks]}
        mealName={
          eaters.length > 1
            ? `${slot.person.personName}'s ${(slot.planned.eatingTime?.name ?? "meal").toLowerCase()}`
            : (slot.planned.eatingTime?.name ?? "this meal")
        }
        onClose={() => {
          setEditing(undefined);
        }}
      />
    );
  }

  if (swapping && shared?.cook !== undefined) {
    return (
      <PlanMealDialog
        cooks={[...week.earlierLeftovers, ...week.cooks]}
        dates={week.days.map((other) => other.date)}
        date={day.date}
        meal={row.meal}
        people={people.data?.people ?? []}
        staples={staples.data?.staples ?? []}
        recipes={recipes.data?.recipes ?? []}
        replacing={{
          cookId: shared.cook.id,
          recipeName: shared.cook.recipeName,
        }}
        onClose={onClose}
      />
    );
  }

  // Everyone's portions at the meal, to show their shares of its cook.
  const mealPortions = eaters.flatMap((other) => other.planned.portions);
  return (
    <Modal open onClose={onClose} title={row.label}>
      <div className="flex flex-col gap-5">
        <p className="-mt-3 text-sm text-muted">{kicker}</p>
        {shared?.cook !== undefined && (
          <section className="flex flex-col gap-4 rounded-2xl border border-line bg-white p-4">
            <div className="flex items-start gap-3">
              <div className="flex min-w-0 grow flex-col gap-0.5">
                <h3 className={sectionLabelClass}>For everyone</h3>
                <Link
                  to={`/recipes/${shared.cook.recipeId}`}
                  className="text-lg leading-snug font-bold text-ink-soft hover:underline"
                >
                  {shared.cook.recipeName}
                </Link>
              </div>
              <MoreMenu
                label="More for the meal"
                items={[
                  {
                    disabled:
                      recipes.data === undefined || people.data === undefined,
                    label: "Swap the recipe",
                    onClick: () => {
                      setSwapping(true);
                    },
                  },
                  {
                    danger: true,
                    disabled: removeMeal.isPending,
                    label: "Remove the meal",
                    onClick: () => {
                      setRemovingMeal(true);
                    },
                  },
                ]}
              />
            </div>
            <ul className="-my-1 flex flex-col divide-y divide-line">
              {eaters.map((other) => {
                const share = other.planned.portions.find(
                  (portion) => portion.cookId === shared.cook?.id
                );
                return (
                  <li key={other.person.personId}>
                    <button
                      type="button"
                      className="flex w-full items-center gap-3 py-2.5 text-left text-sm disabled:cursor-default"
                      disabled={share === undefined}
                      aria-label={`Change ${other.person.personName}'s portion`}
                      onClick={() => {
                        setPersonId(other.person.personId);
                        setEditing(share);
                      }}
                    >
                      <span className="grow font-medium text-ink-soft">
                        {other.person.personName}
                      </span>
                      <span className="text-muted tabular-nums">
                        {share === undefined
                          ? "something else"
                          : portionAmount(share)}
                      </span>
                      {share !== undefined && (
                        <ChevronRightIcon
                          className="size-4 shrink-0 text-muted/70"
                          aria-hidden="true"
                        />
                      )}
                    </button>
                  </li>
                );
              })}
            </ul>
            <SharedSides
              eaters={eaters
                .filter((other) => !other.planned.eatingOut)
                .map((other) => ({
                  eatingTimeId: other.planned.eatingTime?.id ?? "",
                  personId: other.person.personId,
                }))}
              date={day.date}
              meal={row.meal}
              cooks={week.cooks}
            />
            <CookBar
              week={week}
              cookId={shared.cook.id}
              portions={mealPortions}
            />
            <CookActions
              planned={shared}
              weekCooks={week.cooks}
              onDeleted={onClose}
              hideDelete
            />
            <ErrorText error={removeMeal.error} />
            <ConfirmDialog
              open={removingMeal}
              title={`Remove ${row.label.toLowerCase()}?`}
              message={`Everyone's portions at it are removed, and ${shared.cook.recipeName} isn't cooked, unless another meal eats the rest of it.`}
              confirmLabel="Remove"
              onClose={() => {
                setRemovingMeal(false);
              }}
              onConfirm={() => {
                removeMeal.mutate();
              }}
            />
          </section>
        )}
        {eaters.length > 1 ? (
          <section className="flex flex-col gap-3">
            <h3 className={sectionLabelClass}>Each plate</h3>
            <Segmented
              label="Whose"
              value={slot?.person.personId ?? ""}
              options={eaters.map((other) => ({
                label: other.person.personName,
                value: other.person.personId,
              }))}
              onChange={setPersonId}
            />
            {content}
          </section>
        ) : (
          content
        )}
      </div>
    </Modal>
  );
};

export default PlannedMealSheet;
