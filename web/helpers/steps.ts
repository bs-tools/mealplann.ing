import type { Ingredient, Recipe, Step } from "gen/recipes/v1/recipes_pb";
import type { UnitSystem } from "gen/users/v1/users_pb";

import { formatIngredientAmount, formatServings } from "./labels";

// StepGroup is steps in a row with the same section.
export interface StepGroup {
  section: string;
  // The steps, with their indexes in the recipe.
  steps: { step: Step; index: number }[];
}

// GroupSteps groups steps in a row with the same section.
export const groupSteps = (steps: Step[]): StepGroup[] => {
  const groups: StepGroup[] = [];
  for (const [index, step] of steps.entries()) {
    const last = groups.at(-1);
    if (last?.section === step.section) {
      last.steps.push({ index, step });
    } else {
      groups.push({ section: step.section, steps: [{ index, step }] });
    }
  }
  return groups;
};

/**
 * IngredientAmount formats an ingredient's amount scaled by a factor, in a
 * unit system and its food's measure, e.g. "200 g" or "2 tbsp", or
 * "1 serving" of a component.
 */
export const ingredientAmount = (
  ingredient: Ingredient,
  scale: number,
  system: UnitSystem,
  amount: number = ingredient.amount
): string =>
  ingredient.recipeId === ""
    ? formatIngredientAmount(amount * scale, ingredient.unit, system, {
        gramsPerMilliliter: ingredient.foodGramsPerMilliliter,
        gramsPerPiece: ingredient.foodGramsPerPiece,
        measure: ingredient.foodMeasure,
      })
    : formatServings(amount * scale);

// StepAmount returns how much of the ingredient at a position a step uses.
const stepAmount = (step: Step, position: number): number => {
  const index = step.ingredientIndexes.indexOf(position);
  return step.ingredientAmounts[index] ?? 0;
};

/**
 * StepIngredientAmount formats how much of an ingredient a step of a
 * recipe uses, scaled: its own amount, if it has one, or what the other
 * steps' amounts leave, e.g. "1 tsp" of salt split between the sauce and
 * the pasta water. If several steps share what's left, it says so, e.g.
 * "Part of 2 tbsp".
 */
export const stepIngredientAmount = (
  recipe: Recipe,
  step: Step,
  position: number,
  scale: number,
  system: UnitSystem
): string => {
  const ingredient = recipe.ingredients[position];
  if (ingredient === undefined) {
    return "";
  }
  const own = stepAmount(step, position);
  if (own > 0) {
    return ingredientAmount(ingredient, scale, system, own);
  }
  let given = 0;
  let sharing = 0;
  for (const other of recipe.instructions) {
    if (!other.ingredientIndexes.includes(position)) {
      continue;
    }
    const amount = stepAmount(other, position);
    given += amount;
    if (amount <= 0) {
      sharing++;
    }
  }
  const rest = Math.max(ingredient.amount - given, 0);
  const text = ingredientAmount(ingredient, scale, system, rest);
  return sharing > 1 ? `Part of ${text}` : text;
};

/**
 * StepStart says when a step starts, other than after the step before it,
 * e.g. "Can start right away", "After steps 2 and 4" or "10 min into step
 * 3", or "" for the step before it.
 */
export const stepStart = (step: Step, index: number): string => {
  const offset = formatStepDuration(step.startOffsetSeconds);
  const after =
    step.afterStepIndexes.length > 0
      ? step.afterStepIndexes.map((other) => other + 1)
      : [index];
  const steps = `step${after.length > 1 ? "s" : ""} ${after.join(" and ")}`;
  if (step.fromStart || index === 0) {
    if (offset !== "") {
      return `${offset} after starting`;
    }
    return step.fromStart ? "Can start right away" : "";
  }
  if (offset !== "") {
    return `${offset} into ${steps}`;
  }
  return step.afterStepIndexes.length > 0 ? `After ${steps}` : "";
};

// IngredientName names an ingredient's food or component recipe.
export const ingredientName = (ingredient: Ingredient): string =>
  ingredient.recipeId === "" ? ingredient.foodName : ingredient.recipeName;

/**
 * FormatStepDuration formats a step's duration, e.g. "20 min" or "1h 30m",
 * or "" if unknown.
 */
export const formatStepDuration = (seconds: number): string => {
  if (seconds <= 0) {
    return "";
  }
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) {
    return `${Math.max(minutes, 1)} min`;
  }
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest === 0 ? `${hours}h` : `${hours}h ${rest}m`;
};

// FormatClock formats seconds as m:ss, or h:mm:ss over an hour, for timers.
export const formatClock = (seconds: number): string => {
  const total = Math.max(0, Math.ceil(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = String(total % 60).padStart(2, "0");
  return hours > 0
    ? `${hours}:${String(minutes).padStart(2, "0")}:${secs}`
    : `${minutes}:${secs}`;
};
