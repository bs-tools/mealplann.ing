import { WarningList } from "@components/plan/plan-bits";
import SuggestionList from "@components/plan/suggestion-list";
import { CheckboxField, SelectField, TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  choiceClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import type { Person } from "gen/people/v1/people_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import { planMeal } from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlanMealResponse, Staple } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import type { RecipeSummary } from "gen/recipes/v1/recipes_pb";
import { formatDay, formatList } from "helpers/format";
import { HOUSEHOLD_MEALS, formatServings, optionValue } from "helpers/labels";
import { formatAmount } from "helpers/nutrients";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import { staplesAt } from "helpers/staples";
import React, { useState } from "react";
import { Link } from "react-router";

// Tab is how the recipe is chosen: from the suggestions, or from all.
type Tab = "suggestions" | "recipe";

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
  onClose: () => void;
}

/**
 * PlanMealDialog plans a shared meal: a recipe cooked on a day for
 * breakfast, lunch or dinner, split into a portion for everyone who eats
 * it, within their maximum portions. The recipe is chosen from the
 * suggestions for the meal, or from all of them. It then shows the split
 * and its warnings.
 */
const PlanMealDialog: React.FC<PlanMealDialogProps> = ({
  dates,
  date: initialDate,
  meal: initialMeal,
  people,
  staples,
  recipes,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<Tab>("suggestions");
  const [recipeId, setRecipeId] = useState(recipes[0]?.id ?? "");
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
  const plan = useMutation(planMeal, {
    onSuccess: async (response) => {
      setResult(response);
      await invalidateServices(queryClient, [PlanService]);
    },
  });

  if (result !== null) {
    return (
      <Modal open onClose={onClose} title="Meal planned">
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
        <p className="text-sm text-gray-700">
          There are no recipes yet.{" "}
          <Link
            to="/recipes/new"
            className="font-medium text-brand-600 hover:underline"
          >
            Add a recipe
          </Link>{" "}
          first.
        </p>
      </Modal>
    );
  }

  return (
    <Modal open onClose={onClose} title="Plan a meal" wide>
      <div className="flex flex-col gap-3">
        <p className="text-sm text-gray-600">
          One cook for everyone who eats the shared meal. Portions over
          someone&apos;s maximum are split, with the rest at their next small
          meal.
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
          <span className={labelClass}>Who eats it</span>
          {eaters.length === 0 ? (
            <p className="text-sm text-amber-800">
              Nobody eats from this meal: give someone an eating time for it on
              the{" "}
              <Link to="/people" className="font-medium underline">
                People
              </Link>{" "}
              page.
            </p>
          ) : (
            <div className="flex flex-wrap gap-x-4 gap-y-1">
              {eaters.map((person) => (
                <CheckboxField
                  key={person.id}
                  label={
                    eatsByDefault(person.id) ? (
                      person.name
                    ) : (
                      <>
                        {person.name}{" "}
                        <span className="text-xs text-gray-500">
                          has {formatList(stapled.get(person.id) ?? [])}
                        </span>
                      </>
                    )
                  }
                  checked={chosen.includes(person)}
                  onChange={() => {
                    setFlipped(
                      flipped.includes(person.id)
                        ? flipped.filter((id) => id !== person.id)
                        : [...flipped, person.id]
                    );
                  }}
                />
              ))}
            </div>
          )}
        </div>
        <div
          className="flex gap-1 border-t border-gray-100 pt-3"
          role="tablist"
          aria-label="How to choose the recipe"
        >
          <button
            type="button"
            role="tab"
            aria-selected={tab === "suggestions"}
            className={choiceClass(tab === "suggestions")}
            onClick={() => {
              setTab("suggestions");
            }}
          >
            Suggestions
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "recipe"}
            className={choiceClass(tab === "recipe")}
            onClick={() => {
              setTab("recipe");
            }}
          >
            Choose a recipe
          </button>
        </div>
        {tab === "suggestions" ? (
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
              <p className="text-sm text-gray-700">Choose who eats it.</p>
            )}
            <ErrorText error={plan.error} />
          </>
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(event) => {
              event.preventDefault();
              save();
            }}
          >
            <SelectField
              label="Recipe"
              value={recipeId}
              options={recipes.map((recipe) => ({
                label: `${recipe.name} (${formatAmount(recipe.gramsPerServing)} g a serving)`,
                value: recipe.id,
              }))}
              onChange={setRecipeId}
            />
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
            {invalid !== "" && (
              <p className="text-sm text-red-700">{invalid}</p>
            )}
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
      <p className="text-sm text-gray-700">
        {cook?.recipeName} on {formatDay(cook?.date ?? "")}, cooking{" "}
        {formatServings(cook?.servings ?? 0)}:
      </p>
      <ul className="divide-y divide-gray-100 rounded-md border border-gray-200 text-sm">
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
              <span className="text-gray-600 tabular-nums">
                {formatServings(servings)}
                {gramsPerServing > 0 &&
                  ` · ${formatAmount(servings * gramsPerServing)} g`}
              </span>
            </li>
          );
        })}
      </ul>
      {result.unassignedServings > 0 && (
        <p className="text-sm text-gray-600">
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
