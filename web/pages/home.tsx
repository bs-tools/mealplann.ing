import AccountMenu from "@components/header/account-menu";
import Loading from "@components/loading";
import EatingTimeDialog from "@components/plan/eating-time-dialog";
import PlanMealDialog from "@components/plan/plan-meal-dialog";
import type { MealRow, RowSlot } from "@components/plan/plan-rows";
import { rowsOf } from "@components/plan/plan-rows";
import PrepTasks from "@components/plan/prep-tasks";
import TargetBars, { targetStates } from "@components/plan/target-bars";
import WarningsLink from "@components/plan/warnings-link";
import { isTargetWarning, weekWarnings } from "@components/plan/week-warnings";
import CookDone from "@components/today/cook-done";
import MealSheet from "@components/today/meal-sheet";
import { effortOf } from "@components/today/today-bits";
import type { Effort } from "@components/today/today-bits";
import { EmptyState } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import { linkClass, primaryButton } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { ShoppingCartIcon } from "@heroicons/react/24/outline";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import type { EatingTime } from "gen/people/v1/people_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import {
  getWeek,
  listStaples,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type {
  GetWeekResponse,
  PersonDay,
  PlanDay,
  PlannedCook,
  PlannedEatingTime,
} from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { listShoppingLists } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import type { User } from "gen/users/v1/users_pb";
import {
  addDays,
  daysBetween,
  daysOfWeek,
  parseDate,
  startOfWeek,
  today,
} from "helpers/dates";
import { formatList, formatWeekday } from "helpers/format";
import { formatServings } from "helpers/labels";
import { useNutritionGoals } from "helpers/user";
import React, { useState } from "react";
import { Link } from "react-router";

// A cook of the day, with when to start it and how long it takes.
interface CookBlock {
  cook: PlannedCook;
  name: string;
  minutes: number;
  // When to start, "HH:MM", or "" if unknown.
  start: string;
}

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
 * of the person's eating times that day that eats from it; before the
 * cook it's made for, for a component; unknown for cooks made ahead.
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
    // A pick to cook any time isn't today's until it's cooked now.
    if (cook === undefined || cook.date !== date || cook.done || cook.anytime) {
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
  /*
   * A cook made for another cook, like beans for a chili, starts with or
   * before the cook it's for, rather than at the top of the day. A few rounds
   * place those made for cooks that are made for others in turn.
   */
  for (let round = 0; round < 3; round += 1) {
    for (const block of blocks) {
      if (block.start !== "") {
        continue;
      }
      const consumers = (block.cook.cook?.supplying ?? []).flatMap((supply) => {
        const consumer = blocks.find(
          (other) => other.cook.cook?.id === supply.consumerCookId
        );
        return consumer === undefined || consumer.start === ""
          ? []
          : [consumer];
      });
      if (consumers.length > 0) {
        /*
         * One no longer than its dish cooks alongside it, like tofu
         * crumbles for a chili; a longer one, like a pot of beans, is
         * started early enough to be done as the dish starts.
         */
        block.start = timeOf(
          Math.min(
            ...consumers.map(
              (consumer) =>
                minutesOf(consumer.start) -
                (block.minutes <= consumer.minutes
                  ? 0
                  : Math.max(block.minutes, 15))
            )
          )
        );
      }
    }
  }
  return blocks;
};

interface CookCardProps {
  block: CookBlock;
  date: string;
  // The week, for everyone's portions of a cook marked cooked and eaten.
  week: GetWeekResponse;
}

// CookCard is a cook of the day: what, for what, and to start or finish.
const CookCard: React.FC<CookCardProps> = ({ block, date, week }) => (
  <li className="flex flex-col gap-1.5 rounded-2xl bg-cook-tint p-3.5">
    <span className="text-xs font-bold tracking-[0.08em] text-cook-dark uppercase">
      {block.start === "" ? "Cooking" : `${block.start} · Cooking`}
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
    {/* What a cook made for another cook goes into. */}
    {(block.cook.cook?.supplying.length ?? 0) > 0 && (
      <span className="text-sm text-cook-dark">
        For{" "}
        {formatList([
          ...new Set(
            (block.cook.cook?.supplying ?? []).map(
              (supply) => supply.consumerRecipeName
            )
          ),
        ])}
      </span>
    )}
    {/* Foods to buy before it can be cooked. */}
    {block.cook.missingFoodNames.length > 0 && (
      <Link
        to="/shopping"
        className="flex items-start gap-1.5 text-sm font-medium text-warn-dark"
      >
        <ShoppingCartIcon
          className="mt-0.5 size-4 shrink-0"
          aria-hidden="true"
        />
        Shop first: {formatList(block.cook.missingFoodNames)}
      </Link>
    )}
    <div className="mt-1 flex flex-wrap items-center gap-2">
      <Link
        to={`/recipes/${block.cook.cook?.recipeId ?? ""}/cook?cook=${block.cook.cook?.id ?? ""}&servings=${block.cook.cook?.servings ?? ""}`}
        className={primaryButton}
      >
        Start cooking
      </Link>
      <CookDone planned={block.cook} week={week} date={date} />
    </div>
  </li>
);

// MealCellProps is one person's eating time in a row of the day.
interface MealCellProps {
  slot: RowSlot | undefined;
  date: string;
  onOpen: (slot: RowSlot) => void;
}

/**
 * MealCell is one person's part of a meal of the day: what they have and
 * how much work it is, that they're eating out, or nothing yet, opening
 * it to change it.
 */
const MealCell: React.FC<MealCellProps> = ({ slot, date, onOpen }) => {
  if (slot === undefined) {
    return <span className="text-sm text-muted">—</span>;
  }
  const { planned } = slot;
  const effort = effortOf(planned, date);
  const eaten =
    planned.portions.length > 0 &&
    planned.portions.every((portion) => portion.eaten);
  return (
    <button
      type="button"
      className="flex min-w-0 flex-col gap-0.5 rounded-lg text-left outline-none focus-visible:ring-2 focus-visible:ring-pine"
      aria-label={`${slot.person.personName}'s ${planned.eatingTime?.name ?? "meal"}`}
      onClick={() => {
        onOpen(slot);
      }}
    >
      {planned.portions.length === 0 ? (
        <span className="text-sm text-muted">
          {planned.eatingOut ? effortText(effort, planned) : "Nothing yet"}
        </span>
      ) : (
        <>
          <span
            className={`text-sm font-medium ${eaten ? "text-muted line-through" : "text-ink-soft"}`}
          >
            {planned.portions.map((portion) => portion.name).join(", ")}
          </span>
          <span
            className={`text-xs ${eaten ? "text-ready" : EFFORT_TEXT_CLASS[effort]}`}
          >
            {eaten ? "Eaten" : effortText(effort, planned)}
          </span>
        </>
      )}
    </button>
  );
};

// SameMeal reports whether two people have the same at an eating time.
const sameMeal = (
  first: PlannedEatingTime,
  other: PlannedEatingTime
): boolean => {
  const names = new Set(first.portions.map((portion) => portion.name));
  return (
    first.portions.length === other.portions.length &&
    other.portions.every((portion) => names.has(portion.name))
  );
};

interface HouseholdDayProps {
  day: PlanDay;
  blocks: CookBlock[];
  date: string;
  week: GetWeekResponse;
  // Opens someone's eating time, planned or not.
  onOpen: (slot: RowSlot) => void;
  // Plans a shared meal for those at it.
  onPlan: (meal: HouseholdMeal) => void;
}

/**
 * HouseholdDay is the household's day: a column per person and a row per
 * meal, in time order, with the cooking before them. A meal everyone at it
 * has the same of reads as one cell across them, said to be shared; an
 * empty shared meal is planned for everyone at it, naming them.
 */
const HouseholdDay: React.FC<HouseholdDayProps> = ({
  day,
  blocks,
  date,
  week,
  onOpen,
  onPlan,
}) => {
  const { people } = day;
  const columns = {
    gridTemplateColumns: `5.5rem repeat(${people.length}, minmax(0, 1fr))`,
  };
  type Item =
    | { kind: "row"; time: string; row: MealRow }
    | { kind: "cook"; time: string; block: CookBlock };
  const items = byTime<Item>(
    [
      ...rowsOf(day).map((row): Item => ({ kind: "row", row, time: row.time })),
      ...blocks.map((block): Item => ({
        block,
        kind: "cook",
        time: block.start,
      })),
    ],
    (item) => item.time
  );
  return (
    <div className="flex flex-col gap-2.5">
      {people.length > 1 && (
        <div
          className="grid gap-2 px-3 text-xs font-bold tracking-[0.08em] text-muted uppercase"
          style={columns}
        >
          <span />
          {people.map((person) => (
            <span key={person.personId} className="truncate">
              {person.personName}
            </span>
          ))}
        </div>
      )}
      <ol className="flex flex-col gap-2.5">
        {items.map((item) => {
          if (item.kind === "cook") {
            return (
              <CookCard
                key={`cook-${item.block.cook.cook?.id ?? ""}`}
                block={item.block}
                date={date}
                week={week}
              />
            );
          }
          const { row } = item;
          const slotOf = (personId: string): RowSlot | undefined =>
            row.slots.find((slot) => slot.person.personId === personId);
          const filled = row.slots.filter(
            (slot) => slot.planned.portions.length > 0
          );
          const home = row.slots.filter((slot) => !slot.planned.eatingOut);
          const shared = row.meal !== HouseholdMeal.UNSPECIFIED;
          // Everyone at it has the same: one cell across them.
          const same =
            shared &&
            filled.length > 1 &&
            filled.length === row.slots.length &&
            filled.every((slot) =>
              sameMeal(slot.planned, filled[0]?.planned ?? slot.planned)
            );
          const empty = shared && filled.length === 0 && home.length > 0;
          const names = home.map((slot) => slot.person.personName);
          let forWhom = formatList(names);
          if (names.length === 2 && home.length === row.slots.length) {
            forWhom = "both";
          }
          return (
            <li
              key={row.key}
              className={`flex flex-col gap-2 rounded-2xl px-3 py-3 ${
                empty
                  ? "border-2 border-dashed border-gray-400"
                  : "border border-line bg-white"
              }`}
            >
              <div className="grid items-start gap-2" style={columns}>
                <span className="flex flex-col">
                  <span className="text-sm font-bold text-ink-soft">
                    {row.label}
                  </span>
                  <span className="text-xs text-muted tabular-nums">
                    {row.time}
                  </span>
                </span>
                {same && filled[0] !== undefined && (
                  <div style={{ gridColumn: `span ${people.length}` }}>
                    <MealCell slot={filled[0]} date={date} onOpen={onOpen} />
                  </div>
                )}
                {!same &&
                  !empty &&
                  people.map((person) => (
                    <MealCell
                      key={person.personId}
                      slot={slotOf(person.personId)}
                      date={date}
                      onOpen={onOpen}
                    />
                  ))}
                {empty && (
                  <button
                    type="button"
                    className="self-center text-left text-base font-bold text-pine hover:underline"
                    style={{ gridColumn: `span ${people.length}` }}
                    onClick={() => {
                      onPlan(row.meal);
                    }}
                  >
                    + Plan {row.label.toLowerCase()} for {forWhom}
                  </button>
                )}
              </div>
              {same && (
                <span className="pl-[6rem] text-xs font-bold text-pine">
                  Shared ·{" "}
                  {people.length === 2 ? "both of you" : formatList(names)}
                </span>
              )}
              {empty && home.length < row.slots.length && (
                <span className="pl-[6rem] text-xs text-muted">
                  {formatList(
                    row.slots
                      .filter((slot) => slot.planned.eatingOut)
                      .map((slot) => slot.person.personName)
                  )}{" "}
                  eating out
                </span>
              )}
            </li>
          );
        })}
      </ol>
    </div>
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
  // The eating time opened, to change it, by its person and ID.
  const [openMeal, setOpenMeal] = useState<{
    personId: string;
    eatingTimeId: string;
  } | null>(null);
  // The shared meal being planned, for everyone at it.
  const [planning, setPlanning] = useState<HouseholdMeal | null>(null);
  const week = useQuery(getWeek, { monday: startOfWeek(now) });
  const people = useQuery(listPeople, {});
  const goals = useNutritionGoals();
  const recipes = useQuery(listRecipes, {});
  const lists = useQuery(listShoppingLists, { pageSize: 10 });
  const staples = useQuery(listStaples, {});
  const close = () => {
    setOpenMeal(null);
  };

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

  const planDay = week.data.days.find((other) => other.date === now);
  const days = planDay?.people ?? [];

  const minutesOfRecipe = new Map<string, { minutes: number; name: string }>();
  for (const recipe of recipes.data?.recipes ?? []) {
    minutesOfRecipe.set(recipe.id, {
      minutes: Math.round((recipe.prepSeconds + recipe.cookSeconds) / 60),
      name: recipe.name,
    });
  }
  /*
   * The day's cooks, once each, for everyone's meals: a shared meal's cook
   * is timed by the first person who eats from it.
   */
  const blocks: CookBlock[] = [];
  for (const day of days) {
    const person = people.data.people.find(
      (other) => other.id === day.personId
    );
    for (const block of cookBlocksOf(
      week.data.cooks,
      now,
      person?.eatingTimes ?? [],
      day,
      minutesOfRecipe
    )) {
      if (blocks.some((other) => other.cook.cook?.id === block.cook.cook?.id)) {
        continue;
      }
      const meal = block.cook.cook?.meal ?? HouseholdMeal.UNSPECIFIED;
      const eats =
        meal === HouseholdMeal.UNSPECIFIED ||
        (person?.eatingTimes ?? []).some((et) => et.meal === meal);
      // Once its portions today are eaten, there's no cooking left.
      const todays = days
        .flatMap((other) => other.eatingTimes)
        .flatMap((planned) => planned.portions)
        .filter((portion) => portion.cookId === block.cook.cook?.id);
      if (
        eats &&
        (todays.length === 0 || todays.some((portion) => !portion.eaten))
      ) {
        blocks.push(block);
      }
    }
  }
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
  const cooks = [...week.data.earlierLeftovers, ...week.data.cooks];
  const opened = days.find((day) => day.personId === openMeal?.personId);
  const openedMeal = opened?.eatingTimes.find(
    (planned) => planned.eatingTime?.id === openMeal?.eatingTimeId
  );

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
      {planDay === undefined || days.length === 0 ? (
        <EmptyState title="Nobody to plan for yet">
          Add the people you cook for, with when they eat, on the{" "}
          <Link to="/people" className={linkClass}>
            People
          </Link>{" "}
          page, then plan the week.
        </EmptyState>
      ) : (
        <>
          {/* Each person's day against their targets, then what else is off. */}
          {goals &&
            days.map((day) => {
              const targets =
                people.data.people.find((other) => other.id === day.personId)
                  ?.dailyTargets ?? [];
              return targetStates(targets, day.nutrients).length > 0 ? (
                <Link
                  key={day.personId}
                  to="/plan/warnings"
                  aria-label={`${day.personName}'s day against their targets`}
                  className="flex flex-col gap-2 rounded-2xl border border-line bg-white px-4 py-3.5"
                >
                  <span className="text-sm font-medium text-ink-soft">
                    {day.personName}
                  </span>
                  <TargetBars targets={targets} nutrients={day.nutrients} />
                </Link>
              ) : null;
            })}
          <WarningsLink
            count={
              weekWarnings(week.data, now).filter(
                (item) => item.date === now && !isTargetWarning(item.warning)
              ).length
            }
            date={now}
            about="today"
          />
          {/* Tomorrow's cooks that need shopping for first, in one card. */}
          {(() => {
            const short = week.data.cooks.filter(
              (planned) =>
                planned.cook?.date === addDays(now, 1) &&
                planned.missingFoodNames.length > 0
            );
            if (short.length === 0) {
              return null;
            }
            const names = [
              ...new Set(short.flatMap((planned) => planned.missingFoodNames)),
            ];
            return (
              <Link
                to="/shopping"
                className="flex items-start gap-2.5 rounded-2xl bg-warn-tint px-4 py-3"
              >
                <ShoppingCartIcon
                  className="mt-0.5 size-5 shrink-0 text-warn-dark"
                  aria-hidden="true"
                />
                <span className="flex min-w-0 flex-col">
                  <span className="text-sm font-medium text-ink-soft">
                    Shop for tomorrow: {formatList(names)}
                  </span>
                  <span className="text-sm text-warn-dark">
                    For{" "}
                    {formatList(
                      short.map((planned) => planned.cook?.recipeName ?? "")
                    )}
                  </span>
                </span>
              </Link>
            );
          })()}
          {/* What to start today for a later day's cooking. */}
          <PrepTasks tasks={planDay.prepTasks} />
          <HouseholdDay
            day={planDay}
            blocks={blocks}
            date={now}
            week={week.data}
            onOpen={(slot) => {
              setOpenMeal({
                eatingTimeId: slot.planned.eatingTime?.id ?? "",
                personId: slot.person.personId,
              });
            }}
            onPlan={setPlanning}
          />
          {days.some((day) =>
            day.eatingTimes.some(
              (planned) => planned.portions.length === 0 && !planned.eatingOut
            )
          ) && (
            <Link
              to={`/plan/auto?start=${now}&days=1`}
              className="rounded-2xl border-2 border-dashed border-gray-400 px-4 py-3.5 text-center text-base text-ink-soft"
            >
              Plan the rest of today
            </Link>
          )}
        </>
      )}
      {planning !== null && (
        <PlanMealDialog
          cooks={cooks}
          dates={daysOfWeek(startOfWeek(now))}
          date={now}
          meal={planning}
          people={people.data.people}
          staples={staples.data?.staples ?? []}
          recipes={recipes.data?.recipes ?? []}
          onClose={() => {
            setPlanning(null);
          }}
        />
      )}
      {opened !== undefined &&
        openedMeal !== undefined &&
        (() => {
          /*
           * A meal with nothing planned is planned as on the Plan page, and
           * so is a shared meal eaten out, which planning ends.
           */
          const meal = openedMeal.eatingTime?.meal ?? HouseholdMeal.UNSPECIFIED;
          if (
            openedMeal.portions.length === 0 &&
            (!openedMeal.eatingOut || meal !== HouseholdMeal.UNSPECIFIED)
          ) {
            return meal === HouseholdMeal.UNSPECIFIED ? (
              <EatingTimeDialog
                slot={{
                  date: now,
                  eatingTimeId: openedMeal.eatingTime?.id ?? "",
                  personId: opened.personId,
                }}
                people={people.data.people}
                dates={daysOfWeek(startOfWeek(now))}
                cooks={cooks}
                onClose={close}
              />
            ) : (
              <PlanMealDialog
                cooks={cooks}
                dates={daysOfWeek(startOfWeek(now))}
                date={now}
                meal={meal}
                people={people.data.people}
                staples={staples.data?.staples ?? []}
                recipes={recipes.data?.recipes ?? []}
                onClose={close}
              />
            );
          }
          return (
            <MealSheet
              week={week.data}
              day={opened}
              planned={openedMeal}
              date={now}
              today={now}
              onClose={close}
            />
          );
        })()}
    </div>
  );
};

export default HomePage;
