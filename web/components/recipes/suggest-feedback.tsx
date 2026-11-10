import { ErrorText } from "@components/ui/query-error";
import { secondaryButton } from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { useMutation } from "@connectrpc/connect-query";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import { HandThumbDownIcon } from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { updateRecipe } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { Recipe, RecipeSummary } from "gen/recipes/v1/recipes_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { addDays, today } from "helpers/dates";
import { formatShortDate } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React from "react";

/**
 * SNOOZES are how long a recipe can be left out of suggestions for, in
 * days, with their labels.
 */
export const SNOOZES = [
  { days: 14, label: "Not for 2 weeks" },
  { days: 60, label: "Not for 2 months" },
];

// Feedback is what the household wants suggested of a recipe.
export interface Feedback {
  // Whether it's never suggested.
  notSuggested: boolean;
  // The date it's not suggested before, "" for none.
  snoozedUntil: string;
}

// SuggestFeedback saves feedback on recipes.
interface SuggestFeedback {
  save: (recipeId: string, feedback: Feedback) => void;
  isPending: boolean;
  error: ConnectError | null;
}

/**
 * UseSuggestFeedback saves whether a recipe is suggested, refreshing the
 * recipes and suggestions.
 */
export const useSuggestFeedback = (): SuggestFeedback => {
  const queryClient = useQueryClient();
  const update = useMutation(updateRecipe, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [RecipeService, SuggestionService]);
    },
  });
  const save = (recipeId: string, feedback: Feedback) => {
    update.mutate({
      recipe: {
        id: recipeId,
        notSuggested: feedback.notSuggested,
        snoozedUntil: feedback.snoozedUntil,
      },
      updateMask: { paths: ["not_suggested", "snoozed_until"] },
    });
  };
  return { error: update.error, isPending: update.isPending, save };
};

/**
 * SnoozeFor returns the feedback that leaves a recipe out of suggestions
 * for some days from today.
 */
export const snoozeFor = (days: number): Feedback => ({
  notSuggested: false,
  snoozedUntil: addDays(today(), days),
});

// NEVER is the feedback that leaves a recipe out of suggestions for good.
export const NEVER: Feedback = { notSuggested: true, snoozedUntil: "" };

// SUGGEST is the feedback that suggests a recipe again.
export const SUGGEST: Feedback = { notSuggested: false, snoozedUntil: "" };

/**
 * SuggestStatus says whether a recipe is left out of suggestions, e.g.
 * "Not suggested before Nov 24", or "" if it's suggested.
 */
export const suggestStatus = (recipe: Recipe | RecipeSummary): string => {
  if (recipe.notSuggested) {
    return "Never suggested";
  }
  if (recipe.snoozedUntil !== "" && recipe.snoozedUntil > today()) {
    return `Not suggested before ${formatShortDate(recipe.snoozedUntil)}`;
  }
  return "";
};

interface SuggestMenuProps {
  recipe: Recipe;
}

/**
 * SuggestMenu sets whether a recipe is suggested: never, not for a while,
 * or again, like after the household tired of it.
 */
const SuggestMenu: React.FC<SuggestMenuProps> = ({ recipe }) => {
  const { save, isPending, error } = useSuggestFeedback();
  const status = suggestStatus(recipe);
  return (
    <div className="flex flex-col items-end gap-1">
      <Menu as="div" className="relative">
        <MenuButton
          className={secondaryButton}
          disabled={isPending}
          title="Whether Plan for me and suggestions pick it"
        >
          <HandThumbDownIcon className="size-4" aria-hidden="true" />
          {status === "" ? "Suggested" : status}
        </MenuButton>
        <MenuItems
          transition
          className="absolute right-0 z-[1100] mt-1 w-56 origin-top-right rounded-md bg-white py-1 text-sm shadow-lg ring-1 ring-black/5 transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
        >
          {status !== "" && (
            <MenuItem>
              <button
                type="button"
                className="w-full px-3 py-2 text-left data-[focus]:bg-gray-100"
                onClick={() => {
                  save(recipe.id, SUGGEST);
                }}
              >
                Suggest it again
              </button>
            </MenuItem>
          )}
          {SNOOZES.map((snooze) => (
            <MenuItem key={snooze.days}>
              <button
                type="button"
                className="w-full px-3 py-2 text-left data-[focus]:bg-gray-100"
                onClick={() => {
                  save(recipe.id, snoozeFor(snooze.days));
                }}
              >
                {snooze.label}
              </button>
            </MenuItem>
          ))}
          <MenuItem>
            <button
              type="button"
              className="w-full px-3 py-2 text-left data-[focus]:bg-gray-100"
              onClick={() => {
                save(recipe.id, NEVER);
              }}
            >
              Never suggest it
            </button>
          </MenuItem>
        </MenuItems>
      </Menu>
      <ErrorText error={error} />
    </div>
  );
};

export default SuggestMenu;
