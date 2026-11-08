import Loading from "@components/loading";
import DayCard from "@components/plan/day-card";
import EatingTimeSuggestionsDialog from "@components/plan/eating-time-suggestions-dialog";
import PlanMealDialog from "@components/plan/plan-meal-dialog";
import type { Slot } from "@components/plan/portion-dialog";
import PortionDialog from "@components/plan/portion-dialog";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
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
  getWeek,
  skipStaple,
  updateCook,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedPortion, UpdateCookResponse } from "gen/plans/v1/plans_pb";
import { PlanService, Severity } from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import {
  WEEKDAY_NAMES,
  addDays,
  daysOfWeek,
  isDateString,
  startOfWeek,
  today,
} from "helpers/dates";
import { formatGrams, formatList, formatShortDate } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

/**
 * PantryChangeNote says what marking a cook done took out of the pantry, or
 * un-marking it put back, e.g. "Took 600 g of lentils and 150 g of onions
 * out of the pantry."
 */
const pantryChangeNote = (response: UpdateCookResponse): string => {
  const amounts = response.pantryChanges.map(
    (change) => `${formatGrams(change.grams)} of ${change.foodName}`
  );
  if (response.pantryRestored) {
    return amounts.length === 0
      ? ""
      : `Put ${formatList(amounts)} back into the pantry.`;
  }
  if (response.cook?.done !== true) {
    return "";
  }
  return amounts.length === 0
    ? "Cooked. None of its ingredients were in the pantry."
    : `Cooked: took ${formatList(amounts)} out of the pantry.`;
};

// MealPlanning is the shared meal being planned.
interface MealPlanning {
  date: string;
  // The meal, undefined to choose one.
  meal?: HouseholdMeal | undefined;
}

// PortionEdit is the portion being added or changed, and where.
interface PortionEdit {
  slot: Slot;
  portion?: PlannedPortion | undefined;
}

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
  const week = useQuery(
    getWeek,
    { monday },
    { placeholderData: keepPreviousData }
  );
  const people = useQuery(listPeople, {});
  const recipes = useQuery(listRecipes, {});
  const [planning, setPlanning] = useState<MealPlanning | null>(null);
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
  const cookDone = useMutation(updateCook, {
    onSuccess: async (response) => {
      setPantryNote(pantryChangeNote(response));
      await invalidateServices(queryClient, [PlanService, PantryService]);
    },
  });
  const removeCook = useMutation(deleteCook, { onSuccess });
  const pending =
    remove.isPending ||
    skip.isPending ||
    cookDone.isPending ||
    removeCook.isPending;

  const goTo = (nextWeek: string, nextDay?: string) => {
    const next = new URLSearchParams();
    if (nextWeek !== startOfWeek(now)) {
      next.set("week", nextWeek);
    }
    if (nextDay !== undefined) {
      next.set("day", nextDay);
    }
    setParams(next, { replace: true });
  };

  let content: React.ReactNode = null;
  if (week.isPending || people.isPending) {
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
    const warned = (date: string): boolean =>
      week.data.days
        .find((other) => other.date === date)
        ?.people.some(
          (personDay) =>
            personDay.warnings.some(
              (warning) => warning.severity === Severity.WARNING
            ) ||
            personDay.eatingTimes.some((et) =>
              et.warnings.some(
                (warning) => warning.severity === Severity.WARNING
              )
            )
        ) ?? false;
    content = (
      <div className="flex flex-col gap-4">
        <nav
          className="-mx-1 flex gap-1 overflow-x-auto px-1 pb-1 lg:hidden print:hidden"
          aria-label="Days"
        >
          {dates.map((date, index) => (
            <button
              key={date}
              type="button"
              aria-current={date === day ? "date" : undefined}
              className={`flex min-w-11 grow flex-col items-center rounded-md px-1.5 py-1 text-xs ring-1 ring-inset ${
                date === day
                  ? "bg-ink text-white ring-ink"
                  : "bg-white text-gray-700 ring-gray-200"
              }`}
              onClick={() => {
                goTo(monday, date);
              }}
            >
              <span className="font-medium">{WEEKDAY_NAMES[index]}</span>
              <span className="tabular-nums">{Number(date.slice(8))}</span>
              <span
                className={`mt-0.5 size-1.5 rounded-full ${warned(date) ? "bg-amber-500" : "bg-transparent"}`}
                aria-label={warned(date) ? "Has warnings" : undefined}
              />
            </button>
          ))}
        </nav>
        {week.data.days.map((planDay) => (
          <DayCard
            key={planDay.date}
            day={planDay}
            cooks={week.data.cooks.filter(
              (planned) => planned.cook?.date === planDay.date
            )}
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
        subtitle={`Week of ${formatShortDate(monday)} to ${formatShortDate(addDays(monday, 6))}`}
        actions={
          <div className="flex flex-wrap items-center gap-2 print:hidden">
            <div className="flex items-center gap-1">
              <button
                type="button"
                className={iconButton}
                aria-label="Previous week"
                title="Previous week"
                onClick={() => {
                  goTo(addDays(monday, -7));
                }}
              >
                <ChevronLeftIcon className="size-5" />
              </button>
              <button
                type="button"
                className={secondaryButton}
                disabled={monday === startOfWeek(now)}
                onClick={() => {
                  goTo(startOfWeek(now));
                }}
              >
                This week
              </button>
              <button
                type="button"
                className={iconButton}
                aria-label="Next week"
                title="Next week"
                onClick={() => {
                  goTo(addDays(monday, 7));
                }}
              >
                <ChevronRightIcon className="size-5" />
              </button>
            </div>
            <Link
              to={`/plan/templates?week=${monday}`}
              className={secondaryButton}
            >
              <BookmarkSquareIcon className="size-4" aria-hidden="true" />
              Templates
            </Link>
            <Link to="/plan/auto" className={secondaryButton}>
              <SparklesIcon className="size-4" aria-hidden="true" />
              Plan for me
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
      <ErrorText
        error={remove.error ?? skip.error ?? cookDone.error ?? removeCook.error}
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
      {planning !== null && (
        <PlanMealDialog
          dates={dates}
          date={planning.date}
          meal={planning.meal}
          people={people.data?.people ?? []}
          recipes={recipes.data?.recipes ?? []}
          onClose={() => {
            setPlanning(null);
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
          cooks={week.data?.cooks ?? []}
          onClose={() => {
            setEditing(null);
          }}
        />
      )}
    </div>
  );
};

export default PlanPage;
