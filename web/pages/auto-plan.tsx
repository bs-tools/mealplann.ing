import type { Criteria } from "@components/autoplan/criteria-panel";
import CriteriaPanel, { MEALS } from "@components/autoplan/criteria-panel";
import PlanSlotCard from "@components/autoplan/plan-slot-card";
import Card from "@components/ui/card";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { skipToken, useMutation, useQuery } from "@connectrpc/connect-query";
import {
  AdjustmentsHorizontalIcon,
  ArrowLeftIcon,
  ArrowPathIcon,
  CheckIcon,
  ExclamationTriangleIcon,
  SparklesIcon,
} from "@heroicons/react/24/outline";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import { planMeals } from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService } from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { suggestPlan } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { PlanSlot, Suggestion } from "gen/suggestions/v1/suggestions_pb";
import { Emphasis, PlanSlotState } from "gen/suggestions/v1/suggestions_pb";
import { addDays, startOfWeek, today } from "helpers/dates";
import { formatShortDate, formatWeekday } from "helpers/format";
import { useDebouncedValue } from "helpers/hooks";
import { invalidateServices } from "helpers/query";
import { readStorage, writeStorage } from "helpers/storage";
import type { Leftover } from "helpers/suggestions";
import { planKey, suggestionKey } from "helpers/suggestions";
import React, { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";

// CRITERIA_KEY is where the criteria are remembered, but for the start.
const CRITERIA_KEY = "mealplanning.autoPlanCriteria";

/**
 * DefaultCriteria are the criteria of a first plan: the next week, with
 * every criterion at normal.
 */
const defaultCriteria = (): Criteria => ({
  days: 7,
  excludeTags: [],
  favorites: Emphasis.NORMAL,
  maxMinutes: 0,
  meals: [],
  nutrition: Emphasis.NORMAL,
  ownEatingTimes: true,
  pantry: Emphasis.NORMAL,
  shopping: Emphasis.NORMAL,
  start: today(),
  variety: Emphasis.NORMAL,
});

// LoadCriteria returns the remembered criteria, starting today.
const loadCriteria = (): Criteria => {
  const criteria = defaultCriteria();
  const stored = readStorage(CRITERIA_KEY);
  if (stored === null) {
    return criteria;
  }
  try {
    const parsed: unknown = JSON.parse(stored);
    if (typeof parsed === "object" && parsed !== null) {
      return { ...criteria, ...(parsed as Partial<Criteria>), start: today() };
    }
  } catch {
    // A corrupt value is ignored.
  }
  return criteria;
};

// RandomSeed returns a new seed for the search: 64 random bits, not 0.
const randomSeed = (): bigint => {
  const words = new Uint32Array(2);
  globalThis.crypto.getRandomValues(words);
  const seed = (BigInt(words[0] ?? 0) << BigInt(32)) | BigInt(words[1] ?? 0);
  return seed === BigInt(0) ? BigInt(1) : seed;
};

// MealName returns the name of a shared meal, e.g. "Dinner".
const mealName = (meal: HouseholdMeal): string =>
  MEALS.find(([other]) => other === meal)?.[1] ?? "";

/**
 * SlotName names a slot: its shared meal, e.g. "Dinner", or the person's
 * eating time, e.g. "Cleo · Mid-morning".
 */
const slotName = (slot: PlanSlot): string =>
  slot.eatingTimeId === ""
    ? mealName(slot.meal)
    : `${slot.personName} · ${slot.eatingTimeName}`;

/**
 * Choice is the user's choice for a slot: a recipe to keep, or "" to leave
 * it empty. A slot of one person's eating time has their IDs.
 */
interface Choice {
  date: string;
  meal: HouseholdMeal;
  personId: string;
  eatingTimeId: string;
  recipeId: string;
  /*
   * The earlier meal it's the rest of, for a leftover kept at one person's
   * eating time.
   */
  leftoverOf?: Leftover | undefined;
}

// SaveLabel labels the button adding a plan's meals to the plan.
const saveLabel = (saving: boolean, meals: number): string => {
  if (saving) {
    return "Adding…";
  }
  if (meals === 0) {
    return "Add to the plan";
  }
  return `Add ${meals} ${meals === 1 ? "meal" : "meals"} to the plan`;
};

/**
 * RepeatNotes say which meals, or people's own eating times, have fewer
 * recipes fitting everyone's rules than days, so the plan repeats them,
 * e.g. "Only 2 recipes fit everyone's rules at breakfast, so they repeat."
 */
const repeatNotes = (slots: PlanSlot[]): string[] => {
  const groups = new Map<string, PlanSlot[]>();
  for (const slot of slots) {
    const name =
      slot.eatingTimeId === ""
        ? mealName(slot.meal).toLowerCase()
        : `${slot.personName}'s ${slot.eatingTimeName.toLowerCase()}`;
    groups.set(name, [...(groups.get(name) ?? []), slot]);
  }
  return [...groups.entries()].flatMap(([name, group]) => {
    if (group.length < 2) {
      return [];
    }
    const fitting = Math.max(...group.map((slot) => slot.fittingRecipeCount));
    if (fitting >= group.length) {
      return [];
    }
    if (fitting === 0) {
      return [`No recipe fits everyone's rules at ${name}.`];
    }
    if (fitting === 1) {
      return [`Only 1 recipe fits everyone's rules at ${name}, so it repeats.`];
    }
    return [
      `Only ${fitting} recipes fit everyone's rules at ${name}, so they repeat.`,
    ];
  });
};

/**
 * SlotKey identifies a slot of a plan by its date and meal, or its date and
 * the person's eating time.
 */
const slotKey = (slot: PlanSlot | Choice): string =>
  `${slot.date}|${slot.meal}|${slot.eatingTimeId}`;

// THINKING are what the planner says it's doing while it searches.
const THINKING = [
  "Reading your recipes…",
  "Checking the pantry…",
  "Sharing packs between meals…",
  "Balancing everyone's targets…",
  "Keeping the shop short…",
  "Trying another few hundred plans…",
];

// Thinking shows the planner at work, before its first plan arrives.
const Thinking: React.FC<{ slots: number }> = ({ slots }) => {
  const [step, setStep] = useState(0);
  useEffect(() => {
    const interval = globalThis.setInterval(() => {
      setStep((current) => (current + 1) % THINKING.length);
    }, 700);
    return () => {
      globalThis.clearInterval(interval);
    };
  }, []);
  return (
    <div className="flex flex-col gap-4" role="status">
      <p className="flex items-center gap-2 text-sm text-gray-700">
        <SparklesIcon
          className="size-5 text-brand-600 motion-safe:animate-pulse"
          aria-hidden="true"
        />
        {THINKING[step]}
      </p>
      <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: Math.min(slots, 8) }, (_unused, index) => (
          <li
            key={index}
            className="h-28 rounded-md bg-gray-100 motion-safe:animate-pulse"
            style={{ animationDelay: `${index * 120}ms` }}
          />
        ))}
      </ul>
    </div>
  );
};

/**
 * AutoPlanPage suggests a whole plan of shared meals for the next days,
 * from criteria the user sets: they keep, swap or clear meals, shuffle the
 * rest, and add the plan to the week.
 */
const AutoPlanPage: React.FC = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const people = useQuery(listPeople, {});
  const recipes = useQuery(listRecipes, {});
  const [criteria, setCriteria] = useState<Criteria>(loadCriteria);
  const [showCriteria, setShowCriteria] = useState(false);
  const [seed, setSeed] = useState<bigint>(randomSeed);
  /*
   * The slots the user chose: a recipe to keep, or "" to leave it empty.
   * Keeping a recipe only changes choices, so the plan stays as it is;
   * replanned is what the last plan was asked for with, so the plan is
   * only replanned when the user shuffles, swaps, clears or changes the
   * criteria.
   */
  const [choices, setChoices] = useState<Map<string, Choice>>(new Map());
  const [replanned, setReplanned] = useState<Map<string, Choice>>(new Map());

  // The meals someone eats, which the meals planned default to.
  const eaten = MEALS.map(([meal]) => meal).filter((meal) =>
    (people.data?.people ?? []).some((person) =>
      person.eatingTimes.some((et) => et.meal === meal)
    )
  );
  const meals = criteria.meals.filter((meal) => eaten.includes(meal));
  const plannedMeals = meals.length > 0 ? meals : eaten;
  // Whether someone eats at times of their own, like snacks.
  const hasOwnEatingTimes = (people.data?.people ?? []).some((person) =>
    person.eatingTimes.some((et) => et.meal === HouseholdMeal.UNSPECIFIED)
  );
  const ownEatingTimes = hasOwnEatingTimes && criteria.ownEatingTimes;
  const end = addDays(criteria.start, criteria.days - 1);

  const request = useDebouncedValue(
    {
      choices: [...replanned.values()].filter(
        (choice) =>
          choice.date >= criteria.start &&
          choice.date <= end &&
          (choice.eatingTimeId === ""
            ? plannedMeals.includes(choice.meal)
            : ownEatingTimes)
      ),
      criteria: {
        excludeTags: criteria.excludeTags,
        favorites: criteria.favorites,
        maxTotalSeconds: criteria.maxMinutes * 60,
        nutrition: criteria.nutrition,
        pantry: criteria.pantry,
        shopping: criteria.shopping,
        variety: criteria.variety,
      },
      days: criteria.days,
      meals: plannedMeals,
      seed,
      skipPersonalEatingTimes: !ownEatingTimes,
      startDate: criteria.start,
    },
    350
  );
  const plan = useQuery(
    suggestPlan,
    people.isSuccess && (plannedMeals.length > 0 || ownEatingTimes)
      ? request
      : skipToken,
    {
      placeholderData: keepPreviousData,
      refetchOnWindowFocus: false,
      retry: false,
      // The same request plans the same, so there's no need to plan again.
      staleTime: Infinity,
    }
  );
  const save = useMutation(planMeals, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
      void navigate(
        `/plan?week=${startOfWeek(criteria.start)}&day=${criteria.start}`
      );
    },
  });

  const changeCriteria = (next: Criteria) => {
    setCriteria(next);
    const { start: _start, ...remembered } = next;
    writeStorage(CRITERIA_KEY, JSON.stringify(remembered));
    setReplanned(new Map(choices));
  };
  /*
   * Choose keeps a suggestion at a slot, leaves it empty with "", or lets
   * it be planned again with undefined.
   */
  const choose = (slot: PlanSlot, pick: Suggestion | "" | undefined) => {
    const next = new Map(choices);
    const key = slotKey(slot);
    if (pick === undefined) {
      next.delete(key);
    } else {
      next.set(key, {
        date: slot.date,
        eatingTimeId: slot.eatingTimeId,
        leftoverOf: pick === "" ? undefined : pick.leftoverOf,
        meal: slot.meal,
        personId: slot.personId,
        recipeId: pick === "" ? "" : pick.recipeId,
      });
    }
    setChoices(next);
    return next;
  };

  const slots = plan.data?.slots ?? [];
  const toSave = slots.filter(
    (slot) =>
      slot.suggestion !== undefined &&
      (slot.state === PlanSlotState.SUGGESTED ||
        slot.state === PlanSlotState.KEPT)
  );
  const maxScore = Math.max(
    0,
    ...toSave.map((slot) => slot.suggestion?.score ?? 0)
  );
  const breaking = toSave.filter(
    (slot) => slot.suggestion?.fitsRules === false
  ).length;
  const busy = plan.isFetching || save.isPending;
  const repeats = repeatNotes(toSave);
  let content: React.ReactNode = null;
  if (people.isPending) {
    content = <Thinking slots={criteria.days} />;
  } else if (people.isError) {
    content = (
      <QueryError
        error={people.error}
        onRetry={() => {
          void people.refetch();
        }}
      />
    );
  } else if (eaten.length === 0 && !hasOwnEatingTimes) {
    content = (
      <EmptyState title="No shared meals to plan">
        Set which meals people eat together on the{" "}
        <Link to="/people" className={linkClass}>
          People
        </Link>{" "}
        page.
      </EmptyState>
    );
  } else if (plan.isError && plan.data === undefined) {
    content = (
      <QueryError
        error={plan.error}
        onRetry={() => {
          void plan.refetch();
        }}
      />
    );
  } else if (plan.data === undefined) {
    content = <Thinking slots={criteria.days * plannedMeals.length} />;
  } else {
    const byDate = new Map<string, PlanSlot[]>();
    for (const slot of slots) {
      byDate.set(slot.date, [...(byDate.get(slot.date) ?? []), slot]);
    }
    let index = 0;
    content = (
      <div className="relative flex flex-col gap-3">
        {plan.isError && <ErrorText error={plan.error} />}
        <p className="flex flex-wrap items-center gap-x-1.5 text-sm text-gray-600">
          <SparklesIcon className="size-4 text-brand-600" aria-hidden="true" />
          <span>
            Tried {plan.data.plansTried.toLocaleString()}{" "}
            {plan.data.plansTried === 1 ? "plan" : "plans"} with{" "}
            {plan.data.recipeCount}{" "}
            {plan.data.recipeCount === 1 ? "recipe" : "recipes"} and kept the
            best.
          </span>
          {toSave.length > 0 && (
            <span>
              {breaking === 0
                ? "Every meal fits everyone's rules."
                : `${breaking} ${breaking === 1 ? "meal breaks" : "meals break"} someone's rules: swap ${breaking === 1 ? "it" : "them"} if you can.`}
            </span>
          )}
        </p>
        {repeats.length > 0 && (
          <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-950">
            <ExclamationTriangleIcon
              className="mt-0.5 size-4 shrink-0 text-amber-600"
              aria-hidden="true"
            />
            <p>
              {repeats.join(" ")} For more variety,{" "}
              <Link to="/recipes/new" className={linkClass}>
                add recipes
              </Link>{" "}
              that fit, or check the rules on the{" "}
              <Link to="/people" className={linkClass}>
                People
              </Link>{" "}
              page.
            </p>
          </div>
        )}
        <div
          className={`grid grid-cols-1 gap-3 transition-opacity sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 ${plan.isFetching ? "opacity-50" : ""}`}
          aria-busy={plan.isFetching}
        >
          {[...byDate.entries()].map(([date, daySlots]) => (
            <section key={date} className="flex flex-col gap-2">
              <h2 className="text-sm font-semibold text-gray-900">
                {formatWeekday(date)}{" "}
                <span className="font-normal text-gray-500">
                  {formatShortDate(date)}
                </span>
              </h2>
              <ul className="flex flex-col gap-2">
                {daySlots.map((slot) => {
                  const key = slotKey(slot);
                  const planned =
                    slot.suggestion === undefined
                      ? ""
                      : suggestionKey(slot.suggestion);
                  const choice = choices.get(key);
                  const locked =
                    choice !== undefined &&
                    choice.recipeId !== "" &&
                    planKey(choice.recipeId, choice.leftoverOf) === planned;
                  index++;
                  return (
                    <PlanSlotCard
                      // A new recipe at the slot rises in again.
                      key={`${key}|${planned}|${slot.state}`}
                      slot={slot}
                      mealName={slotName(slot)}
                      maxScore={maxScore}
                      locked={locked}
                      index={index}
                      disabled={busy}
                      onToggleLock={() => {
                        choose(slot, locked ? undefined : slot.suggestion);
                      }}
                      onSwap={(swapped) => {
                        setReplanned(choose(slot, swapped));
                      }}
                      onSkip={() => {
                        setReplanned(choose(slot, ""));
                      }}
                      onFill={() => {
                        setReplanned(choose(slot, undefined));
                      }}
                    />
                  );
                })}
              </ul>
            </section>
          ))}
        </div>
        {plan.isFetching && (
          <div className="pointer-events-none absolute inset-x-0 top-10 flex justify-center">
            <span className="flex items-center gap-2 rounded-full bg-white px-3 py-1.5 text-sm text-gray-700 shadow-md ring-1 ring-gray-200">
              <SparklesIcon
                className="size-4 text-brand-600 motion-safe:animate-spin"
                aria-hidden="true"
              />
              Rethinking the plan…
            </span>
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <div>
        <Link
          to="/plan"
          className="inline-flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900 print:hidden"
        >
          <ArrowLeftIcon className="size-4" aria-hidden="true" />
          Plan
        </Link>
      </div>
      <PageHeader
        title="Plan for me"
        subtitle={`${formatShortDate(criteria.start)} to ${formatShortDate(end)}: a recipe for each meal, chosen together to share ingredients, use the pantry and fit everyone's rules.`}
        actions={
          <div className="flex flex-wrap items-center gap-2 print:hidden">
            <button
              type="button"
              className={secondaryButton}
              aria-expanded={showCriteria}
              onClick={() => {
                setShowCriteria(!showCriteria);
              }}
            >
              <AdjustmentsHorizontalIcon
                className="size-4"
                aria-hidden="true"
              />
              Criteria
            </button>
            <button
              type="button"
              className={secondaryButton}
              disabled={busy || plan.data === undefined}
              title="Plan the meals you didn't keep again, differently"
              onClick={() => {
                setReplanned(new Map(choices));
                setSeed(randomSeed());
              }}
            >
              <ArrowPathIcon
                className={`size-4 ${plan.isFetching ? "motion-safe:animate-spin" : ""}`}
                aria-hidden="true"
              />
              Shuffle
            </button>
            <button
              type="button"
              className={primaryButton}
              disabled={busy || toSave.length === 0}
              onClick={() => {
                save.mutate({
                  meals: toSave.map((slot) => ({
                    date: slot.date,
                    eatingTimeId: slot.eatingTimeId,
                    leftoverOf: slot.suggestion?.leftoverOf,
                    meal: slot.meal,
                    personId: slot.personId,
                    recipeId: slot.suggestion?.recipeId ?? "",
                  })),
                });
              }}
            >
              <CheckIcon className="size-4" aria-hidden="true" />
              {saveLabel(save.isPending, toSave.length)}
            </button>
          </div>
        }
      />
      <ErrorText error={save.error} />
      {showCriteria && (
        <Card title="What matters this time">
          <CriteriaPanel
            criteria={{ ...criteria, meals: plannedMeals, ownEatingTimes }}
            meals={eaten}
            hasOwnEatingTimes={hasOwnEatingTimes}
            tags={recipes.data?.tags ?? []}
            onChange={changeCriteria}
          />
        </Card>
      )}
      {content}
    </div>
  );
};

export default AutoPlanPage;
