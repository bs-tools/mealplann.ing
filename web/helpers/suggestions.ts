import { HouseholdMeal } from "gen/people/v1/people_pb";
import type { Suggestion } from "gen/suggestions/v1/suggestions_pb";

import { formatWeekday } from "./format";
import { householdMealLabel } from "./labels";

// Leftover is an earlier shared meal that a portion is the rest of.
export interface Leftover {
  date: string;
  meal: HouseholdMeal;
}

/**
 * PlanKey identifies what a recipe planned at a slot is: a new cook of it,
 * or the rest of an earlier meal's cook of it.
 */
export const planKey = (recipeId: string, leftover?: Leftover): string =>
  leftover === undefined
    ? recipeId
    : `${recipeId}|${leftover.date}|${leftover.meal}`;

// SuggestionKey identifies what a suggestion plans (see planKey).
export const suggestionKey = (suggestion: Suggestion): string =>
  planKey(suggestion.recipeId, suggestion.leftoverOf);

// LeftoverLabel names the meal a leftover is the rest of, e.g. "Rest of
// Mon's dinner".
export const leftoverLabel = (leftover: Leftover): string =>
  `Rest of ${formatWeekday(leftover.date)}'s ${householdMealLabel(leftover.meal).toLowerCase()}`;

/*
 * MAIN_MEALS are the meals the pantry suggests recipes for by default:
 * lunches and dinners, rather than sides, snacks, drinks or basics like a
 * pot of beans, which are always there to cook.
 */
export const MAIN_MEALS = [HouseholdMeal.LUNCH, HouseholdMeal.DINNER];
