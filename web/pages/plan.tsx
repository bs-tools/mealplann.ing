import Loading from "@components/loading";
import EatingTimeSuggestionsDialog from "@components/plan/eating-time-suggestions-dialog";
import MonthView from "@components/plan/month-view";
import type { DayWarning } from "@components/plan/plan-day-list";
import PlanDayList from "@components/plan/plan-day-list";
import PlanMealDialog from "@components/plan/plan-meal-dialog";
import type { MealRow } from "@components/plan/plan-rows";
import PlannedMealSheet from "@components/plan/planned-meal-sheet";
import type { Slot } from "@components/plan/portion-dialog";
import PortionDialog from "@components/plan/portion-dialog";
import WeekStrip, { fillOf, openSlotsOf } from "@components/plan/week-strip";
import WarningSheet from "@components/today/warning-sheet";
import { EmptyState } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import { iconButton, linkClass } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  EllipsisHorizontalIcon,
} from "@heroicons/react/24/outline";
import { keepPreviousData } from "@tanstack/react-query";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import {
  getPlanCost,
  getWeek,
  listStaples,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedPortion } from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { listShoppingLists } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
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
import { formatDollars, formatMonth, formatShortDate } from "helpers/format";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

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

// MealOpen is the planned meal shown in its sheet.
interface MealOpen {
  date: string;
  row: MealRow;
}

// DayWarningOpen is a warning about a person's day, opened to fix it.
interface DayWarningOpen {
  date: string;
  dayWarning: DayWarning;
}

const menuItemClass =
  "block w-full px-4 py-2.5 text-left text-sm text-ink-soft data-[focus]:bg-ground-dark/60 data-[focus]:outline-hidden";

/**
 * PlanPage shows the plan by week or month. A week is its strip of days,
 * an offer to plan the open ones, and from the day chosen on, a row for
 * each meal, opening it as a plate, or planning it.
 */
const PlanPage: React.FC = () => {
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
  // A recipe's "Plan this" links here with ?plan=, to plan that recipe.
  const [planning, setPlanning] = useState<MealPlanning | null>(() => {
    const recipeId = params.get("plan") ?? "";
    return recipeId === "" ? null : { date: day, recipeId };
  });
  const [editing, setEditing] = useState<PortionEdit | null>(null);
  // The empty eating time recipes are suggested for, or null.
  const [suggesting, setSuggesting] = useState<Slot | null>(null);
  const [opened, setOpened] = useState<MealOpen | null>(null);
  const [dayWarning, setDayWarning] = useState<DayWarningOpen | null>(null);
  const staples = useQuery(listStaples, {});

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

  /*
   * The days from today on with eating times left to plan: the banner
   * offers to plan them, from the first to the last.
   */
  const open = (week.data?.days ?? []).filter(
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
  const banner = firstOpen !== undefined && lastOpen !== undefined && (
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
  );

  let content: React.ReactNode = null;
  if (month) {
    content = (
      <div className="flex flex-col gap-5">
        <MonthView
          month={day}
          today={now}
          lists={lists.data?.lists ?? []}
          onChoose={(date) => {
            goTo(startOfWeek(date), date);
          }}
        />
        {banner}
      </div>
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
    content = (
      <div className="flex flex-col gap-5">
        <WeekStrip
          dates={dates}
          today={now}
          days={week.data.days}
          chosen={day}
          onChoose={(date) => {
            goTo(monday, date);
          }}
        />
        {banner}
        <PlanDayList
          days={week.data.days.filter((planDay) => planDay.date >= day)}
          today={now}
          onOpen={(planDay, row) => {
            setOpened({ date: planDay.date, row });
          }}
          onPlan={(planDay, row) => {
            /*
             * A shared meal plans the meal for everyone; other eating
             * times are suggested recipes for one person's.
             */
            if (row.meal === HouseholdMeal.UNSPECIFIED) {
              const [first] = row.slots;
              setSuggesting({
                date: planDay.date,
                eatingTimeId: first?.planned.eatingTime?.id ?? "",
                personId: first?.person.personId ?? "",
              });
            } else {
              setPlanning({ date: planDay.date, meal: row.meal });
            }
          }}
          onDayWarning={(planDay, warning) => {
            setDayWarning({ date: planDay.date, dayWarning: warning });
          }}
        />
        {cost.data !== undefined && cost.data.totalCents > 0 && (
          <p className="text-sm text-muted">
            This week&apos;s food is about {formatDollars(cost.data.totalCents)}
            , with {formatDollars(cost.data.toBuyCents)} left to buy.
          </p>
        )}
      </div>
    );
  }

  const openedDay = week.data?.days.find(
    (planDay) => planDay.date === opened?.date
  );
  const warnedDay = week.data?.days.find(
    (planDay) => planDay.date === dayWarning?.date
  );
  const warnedPerson = warnedDay?.people.find(
    (person) => person.personId === dayWarning?.dayWarning.person.personId
  );
  const shift = (step: number) => {
    if (month) {
      const next = addMonths(day, step);
      goTo(startOfWeek(next), next, true);
    } else {
      goTo(addDays(monday, 7 * step));
    }
  };
  const current = month
    ? day.slice(0, 7) === now.slice(0, 7)
    : monday === startOfWeek(now);

  return (
    <div className="mx-auto flex w-full max-w-xl flex-col gap-5">
      <div className="flex items-start justify-between gap-3">
        <div className="flex flex-col gap-1">
          <p className="text-sm font-medium text-muted">
            {month
              ? formatMonth(day)
              : `${formatShortDate(monday)} – ${formatShortDate(addDays(monday, 6))}`}
          </p>
          <h1 className="text-[28px] leading-tight font-bold tracking-tight text-ink-soft">
            Plan
          </h1>
        </div>
        <div className="flex items-center gap-1 print:hidden">
          <button
            type="button"
            className={iconButton}
            aria-label={month ? "Previous month" : "Previous week"}
            title={month ? "Previous month" : "Previous week"}
            onClick={() => {
              shift(-1);
            }}
          >
            <ChevronLeftIcon className="size-5" />
          </button>
          <button
            type="button"
            className={iconButton}
            aria-label={month ? "Next month" : "Next week"}
            title={month ? "Next month" : "Next week"}
            onClick={() => {
              shift(1);
            }}
          >
            <ChevronRightIcon className="size-5" />
          </button>
          <Menu as="div" className="relative">
            <MenuButton className={iconButton} aria-label="More" title="More">
              <EllipsisHorizontalIcon className="size-6" />
            </MenuButton>
            <MenuItems
              transition
              anchor="bottom end"
              className="z-[1100] mt-1 w-56 rounded-2xl border border-line bg-white py-1 font-ui shadow-lg transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
            >
              <MenuItem>
                <button
                  type="button"
                  className={menuItemClass}
                  disabled={recipes.isPending}
                  onClick={() => {
                    setPlanning({ date: day });
                  }}
                >
                  Plan a meal
                </button>
              </MenuItem>
              <MenuItem>
                <Link to="/plan/auto" className={menuItemClass}>
                  Plan for me
                </Link>
              </MenuItem>
              <MenuItem>
                <Link
                  to={`/plan/templates?week=${monday}`}
                  className={menuItemClass}
                >
                  Templates
                </Link>
              </MenuItem>
              {!current && (
                <MenuItem>
                  <button
                    type="button"
                    className={menuItemClass}
                    onClick={() => {
                      goTo(startOfWeek(now), undefined, month);
                    }}
                  >
                    {month ? "This month" : "This week"}
                  </button>
                </MenuItem>
              )}
            </MenuItems>
          </Menu>
        </div>
      </div>
      <Segmented
        label="View"
        className="print:hidden"
        value={month ? "month" : "week"}
        options={[
          { label: "Week", value: "week" },
          { label: "Month", value: "month" },
        ]}
        onChange={(view) => {
          goTo(monday, day, view === "month");
        }}
      />
      {content}
      {opened !== null &&
        openedDay !== undefined &&
        week.data !== undefined && (
          <PlannedMealSheet
            week={week.data}
            day={openedDay}
            row={opened.row}
            onEdit={(slot, portion) => {
              setOpened(null);
              setEditing({ portion, slot });
            }}
            onClose={() => {
              setOpened(null);
            }}
          />
        )}
      {dayWarning !== null && warnedPerson !== undefined && (
        <WarningSheet
          warning={dayWarning.dayWarning.warning}
          personId={warnedPerson.personId}
          date={dayWarning.date}
          eatingTimeId=""
          portions={warnedPerson.eatingTimes.flatMap(
            (planned) => planned.portions
          )}
          onClose={() => {
            setDayWarning(null);
          }}
        />
      )}
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
    </div>
  );
};

export default PlanPage;
