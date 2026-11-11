import Loading from "@components/loading";
import type { FixTarget } from "@components/plan/day-card";
import DayCard from "@components/plan/day-card";
import EatingTimeSuggestionsDialog from "@components/plan/eating-time-suggestions-dialog";
import FixesDialog from "@components/plan/fixes-dialog";
import MonthView from "@components/plan/month-view";
import PlanMealDialog from "@components/plan/plan-meal-dialog";
import type { Slot } from "@components/plan/portion-dialog";
import PortionDialog from "@components/plan/portion-dialog";
import WeekStrip, { fillOf, openSlotsOf } from "@components/plan/week-strip";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  iconButton,
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  ArchiveBoxIcon,
  BookmarkSquareIcon,
  ChevronLeftIcon,
  ChevronRightIcon,
  PlusIcon,
  SparklesIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import {
  deleteCook,
  deletePortion,
  getPlanCost,
  getWeek,
  listStaples,
  setStapleEaten,
  skipStaple,
  setEatingOut,
  unskipStaple,
  updateCook,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type {
  GetPlanCostResponse,
  PlannedPortion,
  SetStapleEatenResponse,
  UpdateCookResponse,
} from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { listShoppingLists } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import {
  addDays,
  addMonths,
  daysBetween,
  daysOfWeek,
  isDateString,
  parseDate,
  startOfWeek,
  today,
} from "helpers/dates";
import {
  formatDollars,
  formatGrams,
  formatList,
  formatMonth,
  formatShortDate,
} from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

/**
 * PantryChangeNote says what marking a cook done or a staple eaten took out
 * of the pantry, or un-marking it put back, e.g. "Cooked: took 600 g of
 * lentils and 150 g of onions out of the pantry." Done is whether it was
 * marked, doneLabel what that's called, like "Cooked", and noneNote what to
 * say when none of it was in the pantry.
 */
const pantryChangeNote = (
  response: UpdateCookResponse | SetStapleEatenResponse,
  done: boolean,
  doneLabel: string,
  noneNote: string
): string => {
  const amounts = response.pantryChanges.map(
    (change) => `${formatGrams(change.grams)} of ${change.foodName}`
  );
  if (response.pantryRestored) {
    return amounts.length === 0
      ? ""
      : `Put ${formatList(amounts)} back into the pantry.`;
  }
  if (!done) {
    return "";
  }
  return amounts.length === 0
    ? `${doneLabel}. ${noneNote}`
    : `${doneLabel}: took ${formatList(amounts)} out of the pantry.`;
};

// WeekdayOf names a date's day of the week, e.g. "Friday".
const weekdayOf = (date: string): string =>
  parseDate(date).toLocaleDateString(undefined, { weekday: "long" });

// MealPlanning is the shared meal being planned.
interface MealPlanning {
  date: string;
  // The meal, undefined to choose one.
  meal?: HouseholdMeal | undefined;
  // The recipe, undefined to choose one.
  recipeId?: string | undefined;
}

// PortionEdit is the portion being added or changed, and where.
interface PortionEdit {
  slot: Slot;
  portion?: PlannedPortion | undefined;
}

interface WeekCostProps {
  cost: GetPlanCostResponse;
}

/**
 * WeekCost says what the week's food costs, from the foods' prices: in
 * all, and what's left to buy beyond the pantry, and which foods have no
 * price.
 */
const WeekCost: React.FC<WeekCostProps> = ({ cost }) => (
  <p className="flex flex-wrap items-baseline gap-x-3 gap-y-1 rounded-md bg-white px-3 py-2 text-sm text-gray-700 ring-1 ring-gray-200">
    <span>
      This week&apos;s food:{" "}
      <span className="font-semibold text-gray-900 tabular-nums">
        ≈ {formatDollars(cost.totalCents)}
      </span>
    </span>
    <span>
      Left to buy:{" "}
      <span className="font-semibold text-gray-900 tabular-nums">
        ≈ {formatDollars(cost.toBuyCents)}
      </span>
    </span>
    {cost.unpricedFoodNames.length > 0 && (
      <span className="text-xs text-gray-500">
        Without {formatList(cost.unpricedFoodNames.slice(0, 5))}
        {cost.unpricedFoodNames.length > 5 &&
          ` and ${cost.unpricedFoodNames.length - 5} more`}
        , which {cost.unpricedFoodNames.length === 1 ? "has" : "have"} no price
        yet
      </span>
    )}
  </p>
);

/**
 * PlanPage shows a week of the plan, from Monday: each day's cooks and
 * everyone's eating times with what they eat, their totals against their
 * targets, and the warnings. Phones show one day at a time.
 */
const PlanPage: React.FC = () => {
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const now = today();
  const weekParam = params.get("week") ?? "";
  const monday = startOfWeek(isDateString(weekParam) ? weekParam : now);
  const dates = daysOfWeek(monday);
  const dayParam = params.get("day") ?? "";
  let day = monday;
  if (dates.includes(dayParam)) {
    day = dayParam;
  } else if (dates.includes(now)) {
    day = now;
  }
  const month = params.get("view") === "month";
  const lists = useQuery(listShoppingLists, { pageSize: 10 });
  const week = useQuery(
    getWeek,
    { monday },
    { placeholderData: keepPreviousData }
  );
  const people = useQuery(listPeople, {});
  const recipes = useQuery(listRecipes, {});
  // What the week costs, from the foods' prices.
  const cost = useQuery(
    getPlanCost,
    { endDate: dates.at(-1) ?? monday, startDate: monday },
    { placeholderData: keepPreviousData }
  );
  const costs = new Map(
    (cost.data?.cooks ?? []).map((cook) => [cook.cookId, cook.totalCents])
  );
  // A recipe's "Plan this" links here with ?plan=, to plan that recipe.
  const [planning, setPlanning] = useState<MealPlanning | null>(() => {
    const recipeId = params.get("plan") ?? "";
    return recipeId === "" ? null : { date: day, recipeId };
  });
  const [editing, setEditing] = useState<PortionEdit | null>(null);
  // The empty eating time recipes are suggested for, or null.
  const [suggesting, setSuggesting] = useState<Slot | null>(null);

  // What marking a cook done last did to the pantry, empty if nothing.
  const [pantryNote, setPantryNote] = useState("");
  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [PlanService]);
  };
  const remove = useMutation(deletePortion, { onSuccess });
  const skip = useMutation(skipStaple, { onSuccess });
  const unskip = useMutation(unskipStaple, { onSuccess });
  // The day or eating time to suggest fixes for, if any.
  const [fixing, setFixing] = useState<FixTarget | undefined>(undefined);
  // Eating out changes what Plan for me suggests too.
  const eatOut = useMutation(setEatingOut, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService, SuggestionService]);
    },
  });
  // The staples, to show those skipped on each day, to bring back.
  const staples = useQuery(listStaples, {});
  const cookDone = useMutation(updateCook, {
    onSuccess: async (response) => {
      setPantryNote(
        pantryChangeNote(
          response,
          response.cook?.done === true,
          "Cooked",
          "None of its ingredients were in the pantry."
        )
      );
      await invalidateServices(queryClient, [PlanService, PantryService]);
    },
  });
  const stapleEaten = useMutation(setStapleEaten, {
    onSuccess: async (response, request) => {
      setPantryNote(
        pantryChangeNote(
          response,
          request.eaten === true,
          `Ate ${response.staple?.name ?? "it"}`,
          "None of it was in the pantry."
        )
      );
      await invalidateServices(queryClient, [PlanService, PantryService]);
    },
  });
  const removeCook = useMutation(deleteCook, { onSuccess });
  const pending =
    remove.isPending ||
    skip.isPending ||
    unskip.isPending ||
    eatOut.isPending ||
    cookDone.isPending ||
    stapleEaten.isPending ||
    removeCook.isPending;

  const goTo = (nextWeek: string, nextDay?: string, nextMonth = false) => {
    const next = new URLSearchParams();
    if (nextMonth) {
      next.set("view", "month");
    }
    if (nextWeek !== startOfWeek(now)) {
      next.set("week", nextWeek);
    }
    if (nextDay !== undefined) {
      next.set("day", nextDay);
    }
    setParams(next, { replace: true });
  };

  let content: React.ReactNode = null;
  if (month) {
    content = (
      <MonthView
        month={day}
        today={now}
        lists={lists.data?.lists ?? []}
        onChoose={(date) => {
          goTo(startOfWeek(date), date);
        }}
      />
    );
  } else if (week.isPending || people.isPending) {
    content = <Loading />;
  } else if (week.isError) {
    content = (
      <QueryError
        error={week.error}
        onRetry={() => {
          void week.refetch();
        }}
      />
    );
  } else if (people.isError) {
    content = (
      <QueryError
        error={people.error}
        onRetry={() => {
          void people.refetch();
        }}
      />
    );
  } else if (people.data.people.length === 0) {
    content = (
      <EmptyState title="No one to plan for yet">
        Add the people you cook for, with when they eat, on the{" "}
        <Link to="/people" className={linkClass}>
          People
        </Link>{" "}
        page.
      </EmptyState>
    );
  } else {
    /*
     * The days from today on with eating times left to plan: the banner
     * offers to plan them, from the first to the last.
     */
    const open = week.data.days.filter(
      (planDay) => planDay.date >= now && openSlotsOf(planDay) > 0
    );
    const openSlots = open.reduce(
      (sum, planDay) => sum + openSlotsOf(planDay),
      0
    );
    const [firstOpen] = open;
    const lastOpen = open.at(-1);
    let openTitle = "";
    if (firstOpen !== undefined && lastOpen !== undefined) {
      const allEmpty = open.every((planDay) => fillOf(planDay) <= 0);
      if (!allEmpty) {
        openTitle = `${openSlots} ${openSlots === 1 ? "meal" : "meals"} to plan`;
      } else if (firstOpen === lastOpen) {
        openTitle = `${weekdayOf(firstOpen.date)} is open`;
      } else {
        openTitle = `${weekdayOf(firstOpen.date)} to ${weekdayOf(lastOpen.date)} are open`;
      }
    }
    const shop = lists.data?.lists.find(
      (list) => !list.complete && list.startDate >= now
    );
    content = (
      <div className="flex flex-col gap-4">
        <WeekStrip
          dates={dates}
          today={now}
          days={week.data.days}
          chosen={day}
          onChoose={(date) => {
            goTo(monday, date);
            document
              .querySelector(`#day-${date}`)
              ?.scrollIntoView({ behavior: "smooth", block: "start" });
          }}
        />
        {firstOpen !== undefined && lastOpen !== undefined && (
          <div className="flex flex-col gap-2.5 rounded-2xl bg-ink-soft p-5 text-white print:hidden">
            <p className="text-lg font-bold">{openTitle}</p>
            <p className="text-sm text-gray-300">
              {openSlots} {openSlots === 1 ? "meal" : "meals"}
              {shop !== undefined &&
              shop.startDate > firstOpen.date &&
              daysBetween(now, shop.startDate) <= 7
                ? `, before ${weekdayOf(shop.startDate)}'s shop.`
                : " left to plan."}
            </p>
            <Link
              to={`/plan/auto?start=${firstOpen.date}&days=${daysBetween(firstOpen.date, lastOpen.date) + 1}`}
              className="self-start rounded-xl bg-pine-200 px-4 py-2.5 text-sm font-bold text-pine-dark"
            >
              Plan them for me
            </Link>
          </div>
        )}
        {cost.data !== undefined && cost.data.totalCents > 0 && (
          <WeekCost cost={cost.data} />
        )}
        {week.data.days.map((planDay) => (
          <DayCard
            key={planDay.date}
            day={planDay}
            cooks={week.data.cooks.filter(
              (planned) => planned.cook?.date === planDay.date
            )}
            weekCooks={week.data.cooks}
            people={people.data.people}
            disabled={pending}
            shownOnPhones={planDay.date === day}
            onAdd={(slot) => {
              setEditing({ slot });
            }}
            onSuggest={(slot, eatingTime) => {
              /*
               * An eating time of a shared meal plans the meal for
               * everyone; others plan a portion of their own.
               */
              const meal = eatingTime?.meal ?? HouseholdMeal.UNSPECIFIED;
              if (meal === HouseholdMeal.UNSPECIFIED) {
                setSuggesting(slot);
              } else {
                setPlanning({ date: slot.date, meal });
              }
            }}
            onEdit={(slot, portion) => {
              setEditing({ portion, slot });
            }}
            onDelete={(portion) => {
              remove.mutate({ id: portion.portionId });
            }}
            onSkip={(portion, date) => {
              skip.mutate({ date, id: portion.stapleId });
            }}
            onStapleEaten={(portion, date, eaten) => {
              stapleEaten.mutate({ date, eaten, id: portion.stapleId });
            }}
            staples={staples.data?.staples ?? []}
            costs={costs}
            onUnskip={(staple, date) => {
              unskip.mutate({ date, id: staple.id });
            }}
            onFix={setFixing}
            onEatingOut={(slot, eatingOut) => {
              eatOut.mutate({
                date: slot.date,
                eatingOut,
                eatingTimeId: slot.eatingTimeId,
                personId: slot.personId,
              });
            }}
            onCookDone={(planned, done) => {
              cookDone.mutate({
                cook: { done, id: planned.cook?.id ?? "" },
                updateMask: { paths: ["done"] },
              });
            }}
            onCookDelete={(planned) => {
              removeCook.mutate({ id: planned.cook?.id ?? "" });
            }}
          />
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Plan"
        subtitle={
          month
            ? formatMonth(day)
            : `${formatShortDate(monday)} – ${formatShortDate(addDays(monday, 6))}`
        }
        actions={
          <div className="flex flex-wrap items-center gap-2 print:hidden">
            <Link
              to={`/plan/templates?week=${monday}`}
              className={secondaryButton}
              title="Templates"
            >
              <BookmarkSquareIcon className="size-4" aria-hidden="true" />
              <span className="sr-only sm:not-sr-only">Templates</span>
            </Link>
            <Link
              to="/plan/auto"
              className={secondaryButton}
              title="Plan for me"
            >
              <SparklesIcon className="size-4" aria-hidden="true" />
              <span className="sr-only sm:not-sr-only">Plan for me</span>
            </Link>
            <button
              type="button"
              className={primaryButton}
              disabled={recipes.isPending}
              onClick={() => {
                setPlanning({ date: day });
              }}
            >
              <PlusIcon className="size-4" aria-hidden="true" />
              Plan a meal
            </button>
          </div>
        }
      />
      <div className="flex items-center gap-2 print:hidden">
        <Segmented
          label="View"
          className="grow sm:max-w-xs"
          value={month ? "month" : "week"}
          options={[
            { label: "Week", value: "week" },
            { label: "Month", value: "month" },
          ]}
          onChange={(view) => {
            goTo(monday, day, view === "month");
          }}
        />
        <button
          type="button"
          className={iconButton}
          aria-label={month ? "Previous month" : "Previous week"}
          title={month ? "Previous month" : "Previous week"}
          onClick={() => {
            if (month) {
              const previous = addMonths(day, -1);
              goTo(startOfWeek(previous), previous, true);
            } else {
              goTo(addDays(monday, -7));
            }
          }}
        >
          <ChevronLeftIcon className="size-5" />
        </button>
        <button
          type="button"
          className={`${secondaryButton} px-3`}
          disabled={
            month
              ? day.slice(0, 7) === now.slice(0, 7)
              : monday === startOfWeek(now)
          }
          onClick={() => {
            goTo(startOfWeek(now), undefined, month);
          }}
        >
          {month ? "This month" : "This week"}
        </button>
        <button
          type="button"
          className={iconButton}
          aria-label={month ? "Next month" : "Next week"}
          title={month ? "Next month" : "Next week"}
          onClick={() => {
            if (month) {
              const next = addMonths(day, 1);
              goTo(startOfWeek(next), next, true);
            } else {
              goTo(addDays(monday, 7));
            }
          }}
        >
          <ChevronRightIcon className="size-5" />
        </button>
      </div>
      <ErrorText
        error={
          remove.error ??
          skip.error ??
          eatOut.error ??
          cookDone.error ??
          removeCook.error
        }
      />
      {pantryNote !== "" && (
        /*
         * It floats at the bottom of the screen, as the cook ticked is
         * often far down the page.
         */
        <div
          role="status"
          className="fixed inset-x-3 bottom-3 z-[1000] flex items-start gap-2 rounded-md border border-brand-200 bg-brand-50 px-3 py-2 text-sm text-gray-800 shadow-lg sm:right-4 sm:left-auto sm:max-w-md print:hidden"
        >
          <ArchiveBoxIcon
            className="mt-0.5 size-4 shrink-0 text-brand-700"
            aria-hidden="true"
          />
          <p className="grow">
            {pantryNote}{" "}
            <Link to="/pantry" className={linkClass}>
              See the pantry
            </Link>
          </p>
          <button
            type="button"
            className={iconButton}
            aria-label="Dismiss"
            title="Dismiss"
            onClick={() => {
              setPantryNote("");
            }}
          >
            <XMarkIcon className="size-4" />
          </button>
        </div>
      )}
      {content}
      {planning !== null &&
        (planning.recipeId === undefined || recipes.data !== undefined) && (
          <PlanMealDialog
            dates={dates}
            date={planning.date}
            meal={planning.meal}
            people={people.data?.people ?? []}
            staples={staples.data?.staples ?? []}
            recipes={recipes.data?.recipes ?? []}
            recipe={
              planning.recipeId === undefined
                ? undefined
                : {
                    id: planning.recipeId,
                    name:
                      recipes.data?.recipes.find(
                        (other) => other.id === planning.recipeId
                      )?.name ?? "",
                  }
            }
            onClose={() => {
              setPlanning(null);
              if (params.has("plan")) {
                const next = new URLSearchParams(params);
                next.delete("plan");
                setParams(next, { replace: true });
              }
            }}
          />
        )}
      {suggesting !== null && (
        <EatingTimeSuggestionsDialog
          slot={suggesting}
          people={people.data?.people ?? []}
          onClose={() => {
            setSuggesting(null);
          }}
        />
      )}
      {editing !== null && (
        <PortionDialog
          slot={editing.slot}
          portion={editing.portion}
          people={people.data?.people ?? []}
          dates={dates}
          cooks={[
            ...(week.data?.earlierLeftovers ?? []),
            ...(week.data?.cooks ?? []),
          ]}
          onClose={() => {
            setEditing(null);
          }}
        />
      )}
      {fixing !== undefined && (
        <FixesDialog
          personId={fixing.personId}
          date={fixing.date}
          eatingTimeId={fixing.eatingTimeId}
          title={fixing.title}
          portions={fixing.portions}
          onClose={() => {
            setFixing(undefined);
          }}
        />
      )}
    </div>
  );
};

export default PlanPage;
