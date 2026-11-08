import { ErrorText } from "@components/ui/query-error";
import { secondaryButton } from "@components/ui/styles";
import {
  createConnectQueryKey,
  useMutation,
  useTransport,
} from "@connectrpc/connect-query";
import { StarIcon } from "@heroicons/react/24/outline";
import { StarIcon as FilledStarIcon } from "@heroicons/react/24/solid";
import { useQueryClient } from "@tanstack/react-query";
import {
  getRecipe,
  updateRecipe,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { Recipe } from "gen/recipes/v1/recipes_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { invalidateServices } from "helpers/query";
import React from "react";

interface FavoriteButtonProps {
  // The recipe to mark or unmark as a favorite.
  recipe: Recipe;
}

/**
 * FavoriteButton marks a recipe as one of the household's favorites, or
 * unmarks it. Suggestions score favorites higher, so they come up more
 * often.
 */
const FavoriteButton: React.FC<FavoriteButtonProps> = ({ recipe }) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const update = useMutation(updateRecipe, {
    onSuccess: async (response) => {
      /*
       * The response is the recipe as saved, so the button shows it at
       * once, while the lists and suggestions, which count favorites,
       * refetch.
       */
      queryClient.setQueryData(
        createConnectQueryKey({
          cardinality: "finite",
          input: { id: recipe.id },
          schema: getRecipe,
          transport,
        }),
        (old) => (old === undefined ? old : { ...old, recipe: response.recipe })
      );
      await invalidateServices(queryClient, [RecipeService, SuggestionService]);
    },
  });
  const label = recipe.favorite ? "Favorite" : "Mark as favorite";
  return (
    <div className="flex flex-col items-end gap-1">
      <button
        type="button"
        className={secondaryButton}
        aria-pressed={recipe.favorite}
        title={
          recipe.favorite
            ? "A household favorite, suggested more often. Click to unmark it."
            : "Mark as a household favorite, to have it suggested more often."
        }
        disabled={update.isPending}
        onClick={() => {
          update.mutate({
            recipe: { favorite: !recipe.favorite, id: recipe.id },
            updateMask: { paths: ["favorite"] },
          });
        }}
      >
        {recipe.favorite ? (
          <FilledStarIcon
            className="size-4 text-amber-500"
            aria-hidden="true"
          />
        ) : (
          <StarIcon className="size-4" aria-hidden="true" />
        )}
        {label}
      </button>
      <ErrorText error={update.error} />
    </div>
  );
};

export default FavoriteButton;
