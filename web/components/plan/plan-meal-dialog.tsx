import { WarningList } from "@components/plan/plan-bits";
import { FoodChoice, LeftoversChoice } from "@components/plan/plan-meal-other";
import SuggestionList from "@components/plan/suggestion-list";
import type { PickedRecipe } from "@components/recipes/recipe-picker";
import RecipePicker from "@components/recipes/recipe-picker";
import { SelectField, TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  choiceClass,
  labelClass,
  sectionLabelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { createClient } from "@connectrpc/connect";
import type { ConnectError } from "@connectrpc/connect";
import { useMutation, useTransport } from "@connectrpc/connect-query";
import {
  useMutation as useTanstackMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type { Person } from "gen/people/v1/people_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import { planMeal } from "gen/plans/v1/plans-PlanService_connectquery";
import type {
  PlanMealResponse,
  PlannedCook,
  Staple,
} from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import type { RecipeSummary } from "gen/recipes/v1/recipes_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { formatDay, formatList } from "helpers/format";
import { HOUSEHOLD_MEALS, formatServings, optionValue } from "helpers/labels";
import { formatAmount } from "helpers/nutrients";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import { staplesAt } from "helpers/staples";
import React, { useState } from "react";
import { Link } from "react-router";

/*
 * Tab is what the meal is: a recipe from the suggestions or from all of
 * them, the rest of an earlier cook, or a food.
 */
type Tab = "suggestions" | "recipe" | "leftovers" | "food";

interface PlanMealDialogProps {
  // The dates to choose from, those of the week shown.
  dates: string[];
  // The date chosen first.
  date: string;
  // The meal chosen first, dinner if undefined.
  meal?: HouseholdMeal | undefined;
  people: Person[];
  // The household's staples: people with one at the meal eat it instead.
  staples: Staple[];
  recipes: RecipeSummary[];
  // The recipe chosen first, like the one whose page "Plan this" was on.
  recipe?: PickedRecipe | undefined;
  /*
   * The cook of the meal the recipe replaces, which is deleted with its
   * portions once the new one is planned: swapping a meal's recipe.
   */
  replacing?: { cookId: string; recipeName: string } | undefined;
  // The cooks whose leftovers can be the meal: the week's, and earlier ones.
  cooks?: PlannedCook[] | undefined;
  onClose: () => void;
}

// NO_COOKS is no cooks to eat the rest of, when none are given.
const NO_COOKS: PlannedCook[] = [];

/**
 * PlanMealDialog plans a shared meal: a recipe cooked on a day for
 * breakfast, lunch or dinner, split into a portion for everyone who eats
 * it, within their maximum portions, then showing the split and its
 * warnings; or the rest of an earlier cook, or a food, for everyone. The
 * recipe is chosen from the suggestions for the meal, or from all of them.
 */
const PlanMealDialog: React.FC<PlanMealDialogProps> = ({
  dates,
  date: initialDate,
  meal: initialMeal,
  people,
  staples,
  recipes,
  recipe: initialRecipe,
  replacing,
  cooks = NO_COOKS,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<Tab>(
    initialRecipe === undefined ? "suggestions" : "recipe"
  );
  const [recipe, setRecipe] = useState<PickedRecipe | null>(
    initialRecipe ?? null
  );
  const recipeId = recipe?.id ?? "";
  const [date, setDate] = useState(initialDate);
  const [meal, setMeal] = useState(initialMeal ?? HouseholdMeal.DINNER);
  const eaters = people.filter((person) =>
    person.eatingTimes.some((et) => et.meal === meal)
  );
  /*
   * The people whose choice is flipped from the default: everyone who eats
   * from the meal, but those with a staple at it, who eat their staple.
   */
  const [flipped, setFlipped] = useState<string[]>([]);
  const stapled = new Map(
    eaters.map((person) => [
      person.id,
      staplesAt(staples, person, meal, date).map((staple) => staple.name),
    ])
  );
  const eatsByDefault = (id: string): boolean =>
    (stapled.get(id) ?? []).length === 0;
  const [servings, setServings] = useState("");
  const [notes, setNotes] = useState("");
  const [invalid, setInvalid] = useState("");
  const [result, setResult] = useState<PlanMealResponse | null>(null);
  const transport = useTransport();
  /*
   * EatOut marks the meal eaten out for whoever's chosen, instead of
   * planning it: each person's eating time at it, which planning then
   * leaves alone.
   */
  const eatOut = useTanstackMutation<void, ConnectError, Person[]>({
    mutationFn: async (who) => {
      const client = createClient(PlanService, transport);
      await Promise.all(
        who.flatMap((person) =>
          person.eatingTimes
            .filter((et) => et.meal === meal)
            .slice(0, 1)
            .map((et) =>
              client.setEatingOut({
                date,
                eatingOut: true,
                eatingTimeId: et.id,
                personId: person.id,
              })
            )
        )
      );
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        ShoppingService,
        SuggestionService,
      ]);
      onClose();
    },
  });
  const plan = useMutation(planMeal, {
    onSuccess: async (response) => {
      // A swap deletes the meal's old cook, with its portions.
      if (replacing !== undefined) {
        await createClient(PlanService, transport).deleteCook({
          id: replacing.cookId,
        });
      }
      setResult(response);
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
    },
  });

  if (result !== null) {
    return (
      <Modal
        open
        onClose={onClose}
        title={replacing === undefined ? "Meal planned" : "Meal swapped"}
      >
        <PlanMealResult
          result={result}
          people={people}
          recipes={recipes}
          onClose={onClose}
        />
      </Modal>
    );
  }

  const chosen = eaters.filter(
    (person) => eatsByDefault(person.id) !== flipped.includes(person.id)
  );
  /*
   * The default, everyone without a staple at the meal, is sent as no one,
   * which PlanMeal takes as the same.
   */
  const personIds =
    flipped.length === 0 ? [] : chosen.map((person) => person.id);
  // Each person chosen, at their eating time of the meal.
  const mealEaters = chosen.flatMap((person) =>
    person.eatingTimes
      .filter((et) => et.meal === meal)
      .slice(0, 1)
      .map((et) => ({ eatingTimeId: et.id, personId: person.id }))
  );
  // Planned leftovers or a food replace the meal swapped, then close.
  const planned = async (): Promise<void> => {
    if (replacing !== undefined) {
      await createClient(PlanService, transport).deleteCook({
        id: replacing.cookId,
      });
      await invalidateServices(queryClient, [PlanService, ShoppingService]);
    }
    onClose();
  };
  const save = () => {
    setInvalid("");
    const value = parseOptionalNumber(servings);
    if (value === null) {
      setInvalid("Servings must be a number, or empty for one each.");
      return;
    }
    if (chosen.length === 0) {
      setInvalid("Choose who eats it.");
      return;
    }
    plan.mutate({
      date,
      meal,
      notes,
      personIds,
      recipeId,
      servings: value ?? 0,
    });
  };

  if (recipes.length === 0) {
    return (
      <Modal open onClose={onClose} title="Plan a meal">
        <p className="text-sm text-ink-soft">
          There are no recipes yet.{" "}
          <Link
            to="/recipes/new"
            className="font-medium text-pine hover:underline"
          >
            Add a recipe
          </Link>{" "}
          first.
        </p>
      </Modal>
    );
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={replacing === undefined ? "Plan a meal" : "Swap the meal"}
      wide
    >
      <div className="flex flex-col gap-4">
        <p className="-mt-2 text-sm text-muted">
          {replacing === undefined
            ? "One meal for everyone at it: a recipe, leftovers or a food."
            : `Instead of ${replacing.recipeName}, whose cook and portions are removed.`}
        </p>
        <div className="grid grid-cols-2 gap-2">
          <SelectField
            label="Day"
            value={date}
            options={dates.map((value) => ({
              label: formatDay(value),
              value,
            }))}
            onChange={setDate}
          />
          <SelectField
            label="Meal"
            value={String(meal)}
            options={HOUSEHOLD_MEALS.map((option) => ({
              label: option.label,
              value: String(option.value),
            }))}
            onChange={(value) => {
              setMeal(
                optionValue(HOUSEHOLD_MEALS, value, HouseholdMeal.DINNER)
              );
            }}
          />
        </div>
        <div className="flex flex-col gap-1">
          <span className={sectionLabelClass}>Who eats it</span>
          {eaters.length === 0 ? (
            <p className="text-sm text-warn-dark">
              Nobody eats from this meal: give someone an eating time for it on
              the{" "}
              <Link to="/people" className="font-medium underline">
                People
              </Link>{" "}
              page.
            </p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {eaters.map((person) => {
                const eats = chosen.includes(person);
                return (
                  <button
                    key={person.id}
                    type="button"
                    className={choiceClass(eats)}
                    aria-pressed={eats}
                    title={
                      eatsByDefault(person.id)
                        ? undefined
                        : `Has ${formatList(stapled.get(person.id) ?? [])}`
                    }
                    onClick={() => {
                      setFlipped(
                        flipped.includes(person.id)
                          ? flipped.filter((id) => id !== person.id)
                          : [...flipped, person.id]
                      );
                    }}
                  >
                    {person.name}
                    {!eatsByDefault(person.id) && " · has a staple"}
                  </button>
                );
              })}
            </div>
          )}
          {replacing === undefined && chosen.length > 0 && (
            <div className="mt-1 flex flex-col gap-1">
              <button
                type="button"
                className="self-start text-sm font-bold text-pine hover:underline disabled:opacity-50"
                disabled={eatOut.isPending}
                onClick={() => {
                  eatOut.mutate(chosen);
                }}
              >
                {eatOut.isPending
                  ? "Marking it…"
                  : `Mark ${chosen.length === 1 ? (chosen[0]?.name ?? "them") : "them"} as eating out`}
              </button>
              <ErrorText error={eatOut.error} />
            </div>
          )}
        </div>
        <Segmented
          label="What the meal is"
          value={tab}
          options={[
            { label: "Suggested", value: "suggestions" },
            { label: "Recipe", value: "recipe" },
            { label: "Leftovers", value: "leftovers" },
            { label: "Food", value: "food" },
          ]}
          onChange={setTab}
        />
        {tab === "leftovers" && (
          <LeftoversChoice
            eaters={mealEaters}
            date={date}
            cooks={cooks}
            onPlanned={planned}
          />
        )}
        {tab === "food" && (
          <FoodChoice eaters={mealEaters} date={date} onPlanned={planned} />
        )}
        {tab === "suggestions" && (
          <>
            {eaters.length > 0 && chosen.length > 0 && (
              <SuggestionList
                request={{
                  date,
                  personIds,
                  slot: { case: "meal", value: meal },
                }}
                planningId={
                  plan.isPending ? (plan.variables.recipeId ?? "") : ""
                }
                disabled={plan.isPending}
                onPlan={(suggestion) => {
                  plan.mutate({
                    date,
                    meal,
                    personIds,
                    recipeId: suggestion.recipeId,
                  });
                }}
              />
            )}
            {eaters.length > 0 && chosen.length === 0 && (
              <p className="text-sm text-ink-soft">Choose who eats it.</p>
            )}
            <ErrorText error={plan.error} />
          </>
        )}
        {tab === "recipe" && (
          <form
            className="flex flex-col gap-3"
            onSubmit={(event) => {
              event.preventDefault();
              save();
            }}
          >
            <div className="flex flex-col gap-1">
              <span className={labelClass}>Recipe</span>
              <RecipePicker
                value={recipe}
                onChange={setRecipe}
                label="Recipe"
              />
            </div>
            <div className="grid grid-cols-2 gap-2">
              <TextField
                label="Servings to cook"
                value={servings}
                inputMode="decimal"
                placeholder="One each"
                hint="More are left over, to plan later."
                onChange={setServings}
              />
              <TextField label="Notes" value={notes} onChange={setNotes} />
            </div>
            {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
            <ErrorText error={plan.error} />
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className={secondaryButton}
                onClick={onClose}
              >
                Cancel
              </button>
              <button
                type="submit"
                className={primaryButton}
                disabled={
                  plan.isPending || recipeId === "" || eaters.length === 0
                }
              >
                {plan.isPending ? "Planning…" : "Plan meal"}
              </button>
            </div>
          </form>
        )}
      </div>
    </Modal>
  );
};

interface PlanMealResultProps {
  result: PlanMealResponse;
  people: Person[];
  recipes: RecipeSummary[];
  onClose: () => void;
}

// PlanMealResult shows how a planned meal was split, and its warnings.
const PlanMealResult: React.FC<PlanMealResultProps> = ({
  result,
  people,
  recipes,
  onClose,
}) => {
  const { cook } = result;
  const gramsPerServing =
    recipes.find((recipe) => recipe.id === cook?.recipeId)?.gramsPerServing ??
    0;
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-ink-soft">
        {cook?.recipeName} on {formatDay(cook?.date ?? "")}, cooking{" "}
        {formatServings(cook?.servings ?? 0)}:
      </p>
      <ul className="divide-y divide-line rounded-xl border border-line text-sm">
        {result.portions.map((portion) => {
          const person = people.find((other) => other.id === portion.personId);
          const et = person?.eatingTimes.find(
            (other) => other.id === portion.eatingTimeId
          );
          const servings =
            portion.what.case === "cook" ? portion.what.value.servings : 0;
          return (
            <li
              key={portion.id}
              className="flex flex-wrap justify-between gap-2 px-3 py-2"
            >
              <span>
                <span className="font-medium">{person?.name}</span> · {et?.name}
                {portion.date !== cook?.date && ` · ${formatDay(portion.date)}`}
              </span>
              <span className="text-muted tabular-nums">
                {formatServings(servings)}
                {gramsPerServing > 0 &&
                  ` · ${formatAmount(servings * gramsPerServing)} g`}
              </span>
            </li>
          );
        })}
      </ul>
      {result.unassignedServings > 0 && (
        <p className="text-sm text-muted">
          {formatServings(result.unassignedServings)} left over, to plan as
          leftovers.
        </p>
      )}
      <WarningList warnings={result.warnings} />
      <div className="flex justify-end">
        <button type="button" className={primaryButton} onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  );
};

export default PlanMealDialog;
