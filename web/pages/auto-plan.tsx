import type { Criteria } from "@components/autoplan/criteria-panel";
import CriteriaPanel, { MEALS } from "@components/autoplan/criteria-panel";
import PlanSlotCard from "@components/autoplan/plan-slot-card";
import { useSuggestFeedback } from "@components/recipes/suggest-feedback";
import BackLink from "@components/ui/back-link";
import Card from "@components/ui/card";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  linkClass,
  primaryButton,
  choiceClass,
  ghostButton,
  sectionLabelClass,
  stickyBarClass,
} from "@components/ui/styles";
import { skipToken, useMutation, useQuery } from "@connectrpc/connect-query";
import {
  AdjustmentsHorizontalIcon,
  ArrowPathIcon,
  ExclamationTriangleIcon,
  SparklesIcon,
} from "@heroicons/react/24/outline";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import type { Person } from "gen/people/v1/people_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import {
  planMeals,
  setEatingOut,
} from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService } from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { RecipeSide } from "gen/recipes/v1/recipes_pb";
import { suggestPlan } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { PlanSlot, Suggestion } from "gen/suggestions/v1/suggestions_pb";
import {
  Emphasis,
  PlanSlotState,
  SuggestionService,
} from "gen/suggestions/v1/suggestions_pb";
import { addDays, isDateString, startOfWeek, today } from "helpers/dates";
import { formatShortDate, formatWeekday } from "helpers/format";
import { useDebouncedValue } from "helpers/hooks";
import { formatServings } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import { readStorage, writeStorage } from "helpers/storage";
import type { Leftover } from "helpers/suggestions";
import { planKey, suggestionKey } from "helpers/suggestions";
import React, { useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

// CRITERIA_KEY is where the criteria are remembered, but for the start.
const CRITERIA_KEY = "mealplanning.autoPlanCriteria";

/**
 * DefaultCriteria are the criteria of a first plan: the next week, with
 * every criterion at normal.
 */
const defaultCriteria = (): Criteria => ({
  cooking: Emphasis.NORMAL,
  cookingWeekdays: [],
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
  // The side kept with a new cook, or noSide to keep it with none.
  side?: RecipeSide | undefined;
  noSide?: boolean;
}

/**
 * SideChoice keeps a suggestion's side with its recipe, or no side if it
 * has none, so planning again doesn't change it. A leftover keeps what it
 * was served with.
 */
const sideChoice = (
  suggestion: Suggestion
): { side?: RecipeSide; noSide?: boolean } => {
  if (suggestion.leftoverOf !== undefined) {
    return {};
  }
  return suggestion.side === undefined
    ? { noSide: true }
    : { side: suggestion.side };
};

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
      <p className="flex items-center gap-2 text-sm text-ink-soft">
        <SparklesIcon
          className="size-5 text-pine motion-safe:animate-pulse"
          aria-hidden="true"
        />
        {THINKING[step]}
      </p>
      <ul className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: Math.min(slots, 8) }, (_unused, index) => (
          <li
            key={index}
            className="h-28 rounded-xl bg-ground-dark motion-safe:animate-pulse"
            style={{ animationDelay: `${index * 120}ms` }}
          />
        ))}
      </ul>
    </div>
  );
};

interface SharedShareProps {
  person: Person;
  slot: PlanSlot;
  disabled: boolean;
  // Marks the person eating out at their eating time of the meal.
  onEatingOut: (eatingTimeId: string) => void;
}

/**
 * SharedShare is one person's share of a shared meal of the plan, under
 * their name: how much they have, and at which eating time if not the
 * meal's, or that they're not at it, like for their own breakfast; and
 * to mark them eating out, which plans the meal for the others.
 */
const SharedShare: React.FC<SharedShareProps> = ({
  person,
  slot,
  disabled,
  onEatingOut,
}) => {
  const portions = (slot.suggestion?.portions ?? []).filter(
    (portion) => portion.personId === person.id
  );
  const eatingTime = person.eatingTimes.find((et) => et.meal === slot.meal);
  if (portions.length === 0) {
    return (
      <span className="px-3 text-sm text-muted">
        {eatingTime === undefined ? "Doesn't eat it" : "Not at it"}
      </span>
    );
  }
  return (
    <div className="flex flex-col items-start gap-0.5 px-3 text-sm">
      {portions.map((portion) => (
        <span
          key={`${portion.date}-${portion.eatingTimeId}`}
          className="text-ink-soft"
        >
          {formatServings(portion.servings)}
          {portion.eatingTimeId === eatingTime?.id
            ? ""
            : ` at ${portion.eatingTimeName}`}
        </span>
      ))}
      {eatingTime !== undefined && (
        <button
          type="button"
          className="text-xs font-bold text-pine hover:underline disabled:opacity-50 print:hidden"
          disabled={disabled}
          onClick={() => {
            onEatingOut(eatingTime.id);
          }}
        >
          Eating out
        </button>
      )}
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
  const [params] = useSearchParams();
  /*
   * The plan's start and days can come from the link that opened the page,
   * like the plan's "Plan them for me" for the week's open days.
   */
  const [criteria, setCriteria] = useState<Criteria>(() => {
    const loaded = loadCriteria();
    const start = params.get("start") ?? "";
    if (isDateString(start) && start >= today()) {
      loaded.start = start;
    }
    const days = Number(params.get("days") ?? "");
    if (Number.isInteger(days) && days >= 1 && days <= 14) {
      loaded.days = days;
    }
    return loaded;
  });
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
  /*
   * The day replanned for nutrition, if any, with the other days' meals
   * pinned as they were, so only that day changes. Pins aren't kept
   * meals: shuffling or changing the criteria lets them go.
   */
  const [focus, setFocus] = useState<string | undefined>(undefined);
  const [pins, setPins] = useState<Map<string, Choice>>(new Map());

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
      choices: [...new Map([...pins, ...replanned]).values()].filter(
        (choice) =>
          choice.date >= criteria.start &&
          choice.date <= end &&
          (choice.eatingTimeId === ""
            ? plannedMeals.includes(choice.meal)
            : ownEatingTimes)
      ),
      criteria: {
        cooking: criteria.cooking,
        cookingWeekdays: criteria.cookingWeekdays,
        excludeTags: criteria.excludeTags,
        favorites: criteria.favorites,
        maxTotalSeconds: criteria.maxMinutes * 60,
        // A day replanned for nutrition weighs it over sharing the shop.
        nutrition: focus === undefined ? criteria.nutrition : Emphasis.HIGH,
        pantry: criteria.pantry,
        shopping: focus === undefined ? criteria.shopping : Emphasis.LOW,
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

  // Feedback on a recipe is saved on it, and planned around from then on.
  const feedback = useSuggestFeedback();
  // Eating out is saved in the plan, and planned around from then on.
  const eatOut = useMutation(setEatingOut, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService, SuggestionService]);
    },
  });
  const markEatingOut = (slot: PlanSlot, eatingOut: boolean) => {
    eatOut.mutate(
      slot.eatingTimeId === ""
        ? { date: slot.date, eatingOut, meal: slot.meal }
        : {
            date: slot.date,
            eatingOut,
            eatingTimeId: slot.eatingTimeId,
            personId: slot.personId,
          }
    );
  };

  const changeCriteria = (next: Criteria) => {
    setCriteria(next);
    const { start: _start, ...remembered } = next;
    writeStorage(CRITERIA_KEY, JSON.stringify(remembered));
    setReplanned(new Map(choices));
    setFocus(undefined);
    setPins(new Map());
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
        ...(pick === "" ? {} : sideChoice(pick)),
      });
    }
    setChoices(next);
    return next;
  };
  /*
   * ChooseSide keeps a slot's recipe with another side, or none with null,
   * and plans the rest again around it.
   */
  const chooseSide = (slot: PlanSlot, side: RecipeSide | null) => {
    if (slot.suggestion === undefined) {
      return;
    }
    const next = new Map(choices);
    next.set(slotKey(slot), {
      date: slot.date,
      eatingTimeId: slot.eatingTimeId,
      meal: slot.meal,
      noSide: side === null,
      personId: slot.personId,
      recipeId: slot.suggestion.recipeId,
      side: side ?? undefined,
    });
    setChoices(next);
    setReplanned(next);
  };

  const slots = plan.data?.slots ?? [];
  /*
   * ReplanDay plans a day's meals again, putting nutrition first, with the
   * other days' meals pinned as they are.
   */
  const replanDay = (date: string) => {
    const next = new Map<string, Choice>();
    for (const slot of slots) {
      if (slot.date !== date && slot.suggestion !== undefined) {
        next.set(slotKey(slot), {
          date: slot.date,
          eatingTimeId: slot.eatingTimeId,
          leftoverOf: slot.suggestion.leftoverOf,
          meal: slot.meal,
          personId: slot.personId,
          recipeId: slot.suggestion.recipeId,
          ...sideChoice(slot.suggestion),
        });
      }
    }
    setPins(next);
    setFocus(date);
    setReplanned(new Map(choices));
    setSeed(randomSeed());
  };
  const toSave = slots.filter(
    (slot) =>
      slot.suggestion !== undefined &&
      (slot.state === PlanSlotState.SUGGESTED ||
        slot.state === PlanSlotState.KEPT)
  );
  const breaking = toSave.filter(
    (slot) => slot.suggestion?.fitsRules === false
  ).length;
  const busy =
    plan.isFetching || save.isPending || eatOut.isPending || feedback.isPending;
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
    // Everyone in the household, a column each.
    const household = people.data?.people ?? [];
    const columnsStyle = {
      gridTemplateColumns: `repeat(${Math.max(household.length, 1)}, minmax(9rem, 1fr))`,
    };
    const byDate = new Map<string, PlanSlot[]>();
    for (const slot of slots) {
      byDate.set(slot.date, [...(byDate.get(slot.date) ?? []), slot]);
    }
    let index = 0;
    content = (
      <div className="relative flex flex-col gap-3">
        {plan.isError && <ErrorText error={plan.error} />}
        <p className="text-sm text-muted">
          <span>
            Tried {plan.data.plansTried.toLocaleString()}{" "}
            {plan.data.plansTried === 1 ? "plan" : "plans"} with{" "}
            {plan.data.recipeCount}{" "}
            {plan.data.recipeCount === 1 ? "recipe" : "recipes"} and kept the
            best.
          </span>
          {toSave.length > 0 && (
            <span>
              {" "}
              {breaking === 0
                ? "Every meal fits everyone's rules."
                : `${breaking} ${breaking === 1 ? "meal breaks" : "meals break"} someone's rules: swap ${breaking === 1 ? "it" : "them"} if you can.`}
            </span>
          )}
        </p>
        {repeats.length > 0 && (
          <div className="flex items-start gap-3 rounded-2xl border border-warn-line bg-warn-tint p-4 text-sm text-ink-soft">
            <ExclamationTriangleIcon
              className="size-5 shrink-0 text-warn-dark"
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
          className={`grid grid-cols-1 gap-5 transition-opacity ${household.length > 1 ? "" : "lg:grid-cols-2"} ${plan.isFetching ? "opacity-50" : ""}`}
          aria-busy={plan.isFetching}
        >
          {[...byDate.entries()].map(([date, daySlots]) => (
            <section key={date} className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-2">
                <h2 className="text-base font-bold text-ink-soft">
                  {formatWeekday(date)}{" "}
                  <span className="font-normal text-muted">
                    {formatShortDate(date)}
                  </span>
                </h2>
                <button
                  type="button"
                  className={`${ghostButton} -mr-2 print:hidden`}
                  disabled={busy}
                  title="Plan this day's meals again, putting everyone's targets first, and keeping the other days as they are, but for leftovers of this day's meals"
                  onClick={() => {
                    replanDay(date);
                  }}
                >
                  Re-plan for nutrition
                </button>
              </div>
              <div className="-mx-4 overflow-x-auto px-4 sm:mx-0 sm:px-0">
                <div className="grid gap-2" style={columnsStyle}>
                  {household.map((person) => (
                    <span
                      key={person.id}
                      className={`${sectionLabelClass} truncate px-3`}
                    >
                      {person.name}
                    </span>
                  ))}
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
                    const card = (
                      <PlanSlotCard
                        compact={slot.eatingTimeId !== ""}
                        // A new recipe at the slot rises in again.
                        key={`${key}|${planned}|${slot.state}`}
                        slot={slot}
                        mealName={
                          slot.eatingTimeId === ""
                            ? slotName(slot)
                            : slot.eatingTimeName
                        }
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
                        onEatingOut={(eatingOut) => {
                          markEatingOut(slot, eatingOut);
                        }}
                        sides={
                          recipes.data?.recipes.find(
                            (other) => other.id === slot.suggestion?.recipeId
                          )?.servedWith ?? []
                        }
                        onSide={(side) => {
                          chooseSide(slot, side);
                        }}
                        onFeedback={(value) => {
                          if (slot.suggestion !== undefined) {
                            // A kept recipe is let go, to plan another.
                            choose(slot, undefined);
                            feedback.save(slot.suggestion.recipeId, value);
                          }
                        }}
                      />
                    );
                    // One person's eating time is in their column alone.
                    if (slot.eatingTimeId !== "") {
                      const column =
                        household.findIndex(
                          (person) => person.id === slot.personId
                        ) + 1;
                      return (
                        <div
                          key={key}
                          className="rounded-2xl border border-line bg-white px-3"
                          style={{ gridColumn: String(Math.max(column, 1)) }}
                        >
                          {card}
                        </div>
                      );
                    }
                    /*
                     * A shared meal spans everyone, with each person's
                     * share under their name, or that they're not at it.
                     */
                    return (
                      <div
                        key={key}
                        className="col-span-full rounded-2xl border border-line bg-white"
                      >
                        <div className="px-3">{card}</div>
                        {slot.suggestion !== undefined && (
                          <div
                            className="grid gap-2 border-t border-line py-2"
                            style={columnsStyle}
                          >
                            {household.map((person) => (
                              <SharedShare
                                key={person.id}
                                person={person}
                                slot={slot}
                                disabled={busy}
                                onEatingOut={(eatingTimeId) => {
                                  eatOut.mutate({
                                    date: slot.date,
                                    eatingOut: true,
                                    eatingTimeId,
                                    personId: person.id,
                                  });
                                }}
                              />
                            ))}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              </div>
            </section>
          ))}
        </div>
        {plan.isFetching && (
          <div className="pointer-events-none absolute inset-x-0 top-10 flex justify-center">
            <span className="flex items-center gap-2 rounded-full bg-white px-3 py-1.5 text-sm text-ink-soft shadow-md ring-1 ring-line">
              <SparklesIcon
                className="size-4 text-pine motion-safe:animate-spin"
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
      <BackLink to="/plan">Plan</BackLink>
      <PageHeader
        kicker={`${formatShortDate(criteria.start)} to ${formatShortDate(end)}`}
        title="Plan for me"
        subtitle="A recipe for each meal, chosen together to share ingredients, use the pantry and fit everyone's rules."
      />
      <div className="-mt-3 flex flex-wrap gap-2 print:hidden">
        <button
          type="button"
          className={`${choiceClass(showCriteria)} inline-flex items-center gap-1.5`}
          aria-expanded={showCriteria}
          onClick={() => {
            setShowCriteria(!showCriteria);
          }}
        >
          <AdjustmentsHorizontalIcon className="size-4" aria-hidden="true" />
          What matters
        </button>
        <button
          type="button"
          className={`${choiceClass(false)} inline-flex items-center gap-1.5 disabled:opacity-50`}
          disabled={busy || plan.data === undefined}
          title="Plan the meals you didn't keep again, differently"
          onClick={() => {
            setReplanned(new Map(choices));
            setFocus(undefined);
            setPins(new Map());
            setSeed(randomSeed());
          }}
        >
          <ArrowPathIcon
            className={`size-4 ${plan.isFetching ? "motion-safe:animate-spin" : ""}`}
            aria-hidden="true"
          />
          Shuffle
        </button>
      </div>
      <ErrorText error={save.error ?? eatOut.error ?? feedback.error} />
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
      {toSave.length > 0 && (
        <div className={stickyBarClass}>
          <button
            type="button"
            className={`${primaryButton} w-full py-3.5 text-base`}
            disabled={busy}
            onClick={() => {
              save.mutate({
                meals: toSave.map((slot) => ({
                  cookDate: slot.suggestion?.cookDate ?? "",
                  date: slot.date,
                  eatingTimeId: slot.eatingTimeId,
                  leftoverOf: slot.suggestion?.leftoverOf,
                  meal: slot.meal,
                  personId: slot.personId,
                  recipeId: slot.suggestion?.recipeId ?? "",
                  side: slot.suggestion?.side,
                })),
              });
            }}
          >
            {saveLabel(save.isPending, toSave.length)}
          </button>
        </div>
      )}
    </div>
  );
};

export default AutoPlanPage;
