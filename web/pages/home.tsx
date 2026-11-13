import AccountMenu from "@components/header/account-menu";
import Loading from "@components/loading";
import WarningsLink from "@components/plan/warnings-link";
import { weekWarnings } from "@components/plan/week-warnings";
import MealSheet from "@components/today/meal-sheet";
import { CookDot, EffortDot, effortOf } from "@components/today/today-bits";
import type { Effort } from "@components/today/today-bits";
import { EmptyState } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import { linkClass, primaryButton } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import type { EatingTime } from "gen/people/v1/people_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import { getWeek } from "gen/plans/v1/plans-PlanService_connectquery";
import type {
  PersonDay,
  PlannedCook,
  PlannedEatingTime,
} from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { listShoppingLists } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import type { User } from "gen/users/v1/users_pb";
import {
  addDays,
  daysBetween,
  parseDate,
  startOfWeek,
  today,
} from "helpers/dates";
import { formatWeekday } from "helpers/format";
import { formatServings } from "helpers/labels";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

// A cook of the day, with when to start it and how long it takes.
interface CookBlock {
  cook: PlannedCook;
  name: string;
  minutes: number;
  // When to start, "HH:MM", or "" if unknown.
  start: string;
}

// An entry of the day's line: an eating time, or time spent cooking.
type Entry =
  | { kind: "meal"; time: string; planned: PlannedEatingTime }
  | { kind: "cook"; time: string; block: CookBlock };

// MinutesOf returns the minutes since midnight of "HH:MM", or -1.
const minutesOf = (time: string): number => {
  const [hours, minutes] = time.split(":").map(Number);
  if (hours === undefined || minutes === undefined || Number.isNaN(hours)) {
    return -1;
  }
  return hours * 60 + minutes;
};

/**
 * ByTime returns items in order of their time of day, "HH:MM", keeping the
 * order of items at the same time, or without one, which go first.
 */
const byTime = <Item,>(
  items: Item[],
  timeOfItem: (item: Item) => string
): Item[] => {
  const ordered: Item[] = [];
  for (const item of items) {
    const at = minutesOf(timeOfItem(item));
    const index = ordered.findIndex(
      (other) => minutesOf(timeOfItem(other)) > at
    );
    ordered.splice(index < 0 ? ordered.length : index, 0, item);
  }
  return ordered;
};

// TimeOf formats minutes since midnight as "HH:MM".
const timeOf = (minutes: number): string => {
  const at = Math.max(minutes, 0);
  return `${String(Math.floor(at / 60)).padStart(2, "0")}:${String(at % 60).padStart(2, "0")}`;
};

// EffortText says how much work a meal is, e.g. "Heat · leftovers".
const effortText = (effort: Effort, planned: PlannedEatingTime): string => {
  switch (effort) {
    case "ready": {
      return "Ready to eat";
    }
    case "heat": {
      return "Heat · leftovers";
    }
    case "cook": {
      return "Freshly cooked";
    }
    case "out": {
      return planned.eatingOutNotes === ""
        ? "Eating out"
        : `Eating out · ${planned.eatingOutNotes}`;
    }
    default: {
      return "Nothing planned";
    }
  }
};

// EFFORT_TEXT_CLASS colors each effort's words.
const EFFORT_TEXT_CLASS: Record<Effort, string> = {
  cook: "text-cook-dark",
  heat: "text-heat",
  none: "text-muted",
  out: "text-muted",
  ready: "text-ready",
};

/**
 * CookBlocksOf returns the day's cooks for a person's meals, with when to
 * start them: their total time before the eating time of their meal, or,
 * for a cook of no shared meal, like a side cooked for one, of the first
 * of the person's eating times that day that eats from it; unknown for
 * cooks made ahead.
 */
const cookBlocksOf = (
  cooks: PlannedCook[],
  date: string,
  eatingTimes: EatingTime[],
  day: PersonDay | undefined,
  minutesOfRecipe: Map<string, { minutes: number; name: string }>
): CookBlock[] => {
  const blocks: CookBlock[] = [];
  for (const planned of cooks) {
    const { cook } = planned;
    if (cook === undefined || cook.date !== date || cook.done) {
      continue;
    }
    const recipe = minutesOfRecipe.get(cook.recipeId);
    const minutes = recipe?.minutes ?? 0;
    const eatingTime =
      cook.meal === HouseholdMeal.UNSPECIFIED
        ? day?.eatingTimes.find((eating) =>
            eating.portions.some((portion) => portion.cookId === cook.id)
          )?.eatingTime
        : eatingTimes.find((et) => et.meal === cook.meal);
    const mealAt = minutesOf(eatingTime?.time ?? "");
    blocks.push({
      cook: planned,
      minutes,
      name: recipe?.name ?? "A recipe",
      start: mealAt < 0 ? "" : timeOf(mealAt - Math.max(minutes, 15)),
    });
  }
  return blocks;
};

interface DayLineProps {
  day: PersonDay;
  blocks: CookBlock[];
  date: string;
  // Opens a meal, to change it.
  onMeal: (planned: PlannedEatingTime) => void;
}

/**
 * DayLine is a person's day as a vertical line of their meals and the
 * cooking before them, in time order, each marked with how much work it is.
 */
const DayLine: React.FC<DayLineProps> = ({ day, blocks, date, onMeal }) => {
  let entries: Entry[] = [
    ...day.eatingTimes.map((planned): Entry => ({
      kind: "meal",
      planned,
      time: planned.eatingTime?.time ?? "",
    })),
    ...blocks.map((block): Entry => ({
      block,
      kind: "cook",
      time: block.start,
    })),
  ];
  // Entries go in time order; those without a time keep their place.
  entries = byTime(entries, (entry) => entry.time);
  return (
    <ol className="relative flex flex-col">
      {/* The line runs through the middle of the markers' column. */}
      <span
        className="absolute top-4 bottom-4 left-[calc(3rem+0.75rem+0.75rem-1px)] w-0.5 bg-line"
        aria-hidden="true"
      />
      {entries.map((entry) => {
        if (entry.kind === "cook") {
          const { block } = entry;
          return (
            <li
              key={`cook-${block.cook.cook?.id ?? ""}`}
              className="flex items-start gap-3 py-2.5"
            >
              <span className="w-12 shrink-0 pt-3.5 text-sm text-muted tabular-nums">
                {block.start}
              </span>
              <span className="flex w-6 justify-center pt-3">
                <CookDot />
              </span>
              <div className="flex min-w-0 grow flex-col gap-1.5 rounded-2xl bg-cook-tint p-3.5">
                <span className="text-xs font-bold tracking-[0.08em] text-cook-dark uppercase">
                  Cooking
                  {block.minutes > 0 && ` · ${block.minutes} min`}
                </span>
                <span className="font-medium text-ink-soft">
                  <Link
                    to={`/recipes/${block.cook.cook?.recipeId ?? ""}`}
                    className="hover:text-cook-dark"
                  >
                    {block.name}
                  </Link>
                  {block.cook.cook !== undefined &&
                    `, ${formatServings(block.cook.cook.servings)}`}
                </span>
                <Link
                  to={`/recipes/${block.cook.cook?.recipeId ?? ""}/cook?cook=${block.cook.cook?.id ?? ""}&servings=${block.cook.cook?.servings ?? ""}`}
                  className={`${primaryButton} mt-1 self-start`}
                >
                  Start cooking
                </Link>
              </div>
            </li>
          );
        }
        const { planned } = entry;
        const et = planned.eatingTime;
        const effort = effortOf(planned, date);
        const eaten =
          planned.portions.length > 0 &&
          planned.portions.every((portion) => portion.eaten);
        return (
          <li key={et?.id} className="flex items-start gap-3 py-2.5">
            <span className="w-12 shrink-0 pt-0.5 text-sm text-muted tabular-nums">
              {et?.time}
            </span>
            <span className="flex w-6 justify-center">
              <EffortDot effort={effort} eaten={eaten} />
            </span>
            <div className="flex min-w-0 grow flex-col gap-1">
              <button
                type="button"
                className="flex flex-col gap-1 rounded-xl text-left outline-none focus-visible:ring-2 focus-visible:ring-pine"
                onClick={() => {
                  onMeal(planned);
                }}
              >
                <span className="text-xs font-medium text-muted">
                  {et?.name}
                </span>
                {planned.portions.length === 0 ? (
                  <span className="text-base text-muted">
                    {effortText(effort, planned)}
                  </span>
                ) : (
                  <>
                    <span
                      className={`text-base font-medium ${eaten ? "text-muted line-through" : "text-ink-soft"}`}
                    >
                      {planned.portions
                        .map((portion) => portion.name)
                        .join(", ")}
                    </span>
                    <span
                      className={`text-sm ${eaten ? "text-ready" : EFFORT_TEXT_CLASS[effort]}`}
                    >
                      {eaten ? "Eaten" : effortText(effort, planned)}
                    </span>
                  </>
                )}
              </button>
            </div>
          </li>
        );
      })}
    </ol>
  );
};

interface HomePageProps {
  user: User;
}

/**
 * HomePage is today: one person's day as a line of meals and the cooking
 * before them, with when the household shops next, and how many warnings
 * the day has, linking to the page that lists them.
 */
const HomePage: React.FC<HomePageProps> = ({ user }) => {
  const now = today();
  const [params, setParams] = useSearchParams();
  // The meal opened, to change it, by its eating time's ID.
  const [openMeal, setOpenMeal] = useState("");
  const week = useQuery(getWeek, { monday: startOfWeek(now) });
  const people = useQuery(listPeople, {});
  const recipes = useQuery(listRecipes, {});
  const lists = useQuery(listShoppingLists, { pageSize: 10 });

  const title = (
    <div className="flex items-start justify-between gap-3">
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium text-muted">
          {parseDate(now).toLocaleDateString(undefined, {
            day: "numeric",
            month: "long",
            weekday: "long",
          })}
        </p>
        <h1 className="text-[28px] leading-tight font-bold tracking-tight text-ink-soft">
          Today
        </h1>
      </div>
      <AccountMenu user={user} className="lg:hidden" />
    </div>
  );

  if (week.isPending || people.isPending) {
    return (
      <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
        {title}
        <Loading />
      </div>
    );
  }
  const failed = week.isError ? week.error : people.error;
  if (week.isError || people.isError) {
    return (
      <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
        {title}
        {failed !== null && (
          <QueryError
            error={failed}
            onRetry={() => {
              void week.refetch();
              void people.refetch();
            }}
          />
        )}
      </div>
    );
  }

  const days = week.data.days.find((day) => day.date === now)?.people ?? [];
  // The signed in user's own day comes first, unless another is chosen.
  const mine = people.data.people.find((person) => person.userId === user.id);
  const chosen = params.get("person") ?? mine?.id ?? days[0]?.personId ?? "";
  const day = days.find((other) => other.personId === chosen) ?? days[0];
  const person = people.data.people.find((other) => other.id === day?.personId);

  const minutesOfRecipe = new Map<string, { minutes: number; name: string }>();
  for (const recipe of recipes.data?.recipes ?? []) {
    minutesOfRecipe.set(recipe.id, {
      minutes: Math.round((recipe.prepSeconds + recipe.cookSeconds) / 60),
      name: recipe.name,
    });
  }
  const blocks =
    day === undefined
      ? []
      : cookBlocksOf(
          week.data.cooks,
          now,
          person?.eatingTimes ?? [],
          day,
          minutesOfRecipe
        ).filter((block) => {
          // A shared meal's cook is on the line of those who eat from it.
          const meal = block.cook.cook?.meal ?? HouseholdMeal.UNSPECIFIED;
          const eats =
            meal === HouseholdMeal.UNSPECIFIED ||
            (person?.eatingTimes ?? []).some((et) => et.meal === meal);
          // Once its portions today are eaten, there's no cooking left.
          const todays = day.eatingTimes
            .flatMap((planned) => planned.portions)
            .filter((portion) => portion.cookId === block.cook.cook?.id);
          return (
            eats &&
            (todays.length === 0 || todays.some((portion) => !portion.eaten))
          );
        });
  const cookingMinutes = blocks.reduce((sum, block) => sum + block.minutes, 0);
  const [firstBlock] = byTime(
    blocks.filter((block) => block.start !== ""),
    (block) => block.start
  );
  const list = lists.data?.lists.find(
    (other) => !other.complete && other.startDate >= now
  );
  let shopText = "";
  if (list !== undefined && daysBetween(now, list.startDate) <= 7) {
    shopText =
      list.startDate === now
        ? "Shop today"
        : `Shop ${list.startDate === addDays(now, 1) ? "tomorrow" : formatWeekday(list.startDate)}`;
  }

  return (
    <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
      {title}
      {(cookingMinutes > 0 || shopText !== "") && (
        <div className="-mt-1 flex flex-wrap gap-2">
          {cookingMinutes > 0 && (
            <span className="rounded-full bg-cook-tint px-3 py-1.5 text-sm font-medium text-cook-dark">
              {/*
               * The day's cooking in all: one cook at its time, or several
               * in all from the first, so it adds up to the cooks below.
               */}
              {blocks.length === 1
                ? `${cookingMinutes} min cooking`
                : `${blocks.length} cooks, ${cookingMinutes} min in all`}
              {firstBlock !== undefined &&
                (blocks.length === 1
                  ? ` at ${firstBlock.start}`
                  : `, from ${firstBlock.start}`)}
            </span>
          )}
          {shopText !== "" && (
            <Link
              to="/shopping"
              className="rounded-full bg-heat-tint px-3 py-1.5 text-sm font-medium text-heat"
            >
              {shopText}
            </Link>
          )}
        </div>
      )}
      {days.length > 1 && (
        <Segmented
          label="Whose day"
          value={day?.personId ?? ""}
          options={days.map((other) => ({
            label: other.personName,
            value: other.personId,
          }))}
          onChange={(personId) => {
            setParams({ person: personId }, { replace: true });
          }}
        />
      )}
      {day === undefined ? (
        <EmptyState title="Nobody to plan for yet">
          Add the people you cook for, with when they eat, on the{" "}
          <Link to="/people" className={linkClass}>
            People
          </Link>{" "}
          page, then plan the week.
        </EmptyState>
      ) : (
        <>
          <WarningsLink
            count={
              weekWarnings(week.data, now).filter(
                (item) =>
                  item.date === now &&
                  (item.person === undefined ||
                    item.person.personId === day.personId)
              ).length
            }
            date={now}
            about="today"
          />
          <DayLine
            day={day}
            blocks={blocks}
            date={now}
            onMeal={(planned) => {
              setOpenMeal(planned.eatingTime?.id ?? "");
            }}
          />
          {day.eatingTimes.some(
            (planned) => planned.portions.length === 0 && !planned.eatingOut
          ) && (
            <Link
              to="/plan"
              className="rounded-2xl border-2 border-dashed border-gray-400 px-4 py-3.5 text-center text-base text-ink-soft"
            >
              Plan the rest of today
            </Link>
          )}
          <Link
            to={`/today/change?person=${day.personId}&date=${now}`}
            className="rounded-2xl border-2 border-dashed border-gray-400 px-4 py-3.5 text-center text-base text-ink-soft"
          >
            + I ate something else
          </Link>
        </>
      )}
      {openMeal !== "" &&
        day !== undefined &&
        (() => {
          const planned = day.eatingTimes.find(
            (other) => other.eatingTime?.id === openMeal
          );
          return planned === undefined ? null : (
            <MealSheet
              week={week.data}
              day={day}
              planned={planned}
              date={now}
              today={now}
              onClose={() => {
                setOpenMeal("");
              }}
            />
          );
        })()}
    </div>
  );
};

export default HomePage;
