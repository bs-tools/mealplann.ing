import Loading from "@components/loading";
import RecipeForm from "@components/recipes/recipe-form";
import QueryError from "@components/ui/query-error";
import { skipToken, useQuery } from "@connectrpc/connect-query";
import {
  getRecipe,
  listRecipes,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import React from "react";
import { useParams } from "react-router";

/**
 * RecipeEditPage creates a recipe at /recipes/new, or edits one at
 * /recipes/:id/edit.
 */
const RecipeEditPage: React.FC = () => {
  const { id } = useParams();
  // The household's tags are offered for reuse, so both start together.
  const recipe = useQuery(getRecipe, id === undefined ? skipToken : { id });
  const recipes = useQuery(listRecipes, {});
  if ((id !== undefined && recipe.isPending) || recipes.isPending) {
    return <Loading />;
  }
  if (recipe.isError) {
    return (
      <QueryError
        error={recipe.error}
        onRetry={() => {
          void recipe.refetch();
        }}
      />
    );
  }
  if (recipes.isError) {
    return (
      <QueryError
        error={recipes.error}
        onRetry={() => {
          void recipes.refetch();
        }}
      />
    );
  }
  return (
    <RecipeForm
      key={id ?? "new"}
      recipe={recipe.data?.recipe}
      allTags={recipes.data.tags}
    />
  );
};

export default RecipeEditPage;
