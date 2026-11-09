import type { Ingredient, Recipe } from "gen/recipes/v1/recipes_pb";

import { formatIngredientAmount, formatServings } from "./labels";

/**
 * StepView is a step to show: a step of a recipe's instructions, or one of
 * its plain steps in the old format, which has only its text.
 */
export interface StepView {
  text: string;
  section: string;
  ingredientIndexes: number[];
  durationSeconds: number;
  waiting: boolean;
  afterStepIndexes: number[];
  fromStart: boolean;
}

// RecipeSteps returns a recipe's steps, from its instructions if it has any.
export const recipeSteps = (recipe: Recipe): StepView[] => {
  if (recipe.instructions.length > 0) {
    return recipe.instructions.map((step) => ({
      afterStepIndexes: step.afterStepIndexes,
      durationSeconds: step.durationSeconds,
      fromStart: step.fromStart,
      ingredientIndexes: step.ingredientIndexes,
      section: step.section,
      text: step.text,
      waiting: step.waiting,
    }));
  }
  return recipe.steps.map((text) => ({
    afterStepIndexes: [],
    durationSeconds: 0,
    fromStart: false,
    ingredientIndexes: [],
    section: "",
    text,
    waiting: false,
  }));
};

// StepGroup is steps in a row with the same section.
export interface StepGroup {
  section: string;
  // The steps, with their indexes in the recipe.
  steps: { step: StepView; index: number }[];
}

// GroupSteps groups steps in a row with the same section.
export const groupSteps = (steps: StepView[]): StepGroup[] => {
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
 * IngredientAmount formats an ingredient's amount scaled by a factor, e.g.
 * "200 g", or "1 serving" of a component.
 */
export const ingredientAmount = (
  ingredient: Ingredient,
  scale: number
): string =>
  ingredient.recipeId === ""
    ? formatIngredientAmount(ingredient.amount * scale, ingredient.unit)
    : formatServings(ingredient.amount * scale);

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
