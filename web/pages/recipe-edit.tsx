import Loading from "@components/loading";
import RecipeForm from "@components/recipes/recipe-form";
import QueryError from "@components/ui/query-error";
import { skipToken, useQuery } from "@connectrpc/connect-query";
import {
  getRecipe,
  listRecipes,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import React from "react";
import { useParams, useSearchParams } from "react-router";

/**
 * RecipeEditPage creates a recipe at /recipes/new, optionally as a copy of
 * another with ?from=its ID, or edits one at /recipes/:id/edit.
 */
const RecipeEditPage: React.FC = () => {
  const { id } = useParams();
  const [params] = useSearchParams();
  // The recipe edited, or else copied.
  const sourceID = id ?? params.get("from") ?? undefined;
  // The household's tags are offered for reuse, so both start together.
  const recipe = useQuery(
    getRecipe,
    sourceID === undefined ? skipToken : { id: sourceID }
  );
  const recipes = useQuery(listRecipes, {});
  if ((sourceID !== undefined && recipe.isPending) || recipes.isPending) {
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
      key={id ?? `new-${sourceID ?? ""}`}
      recipe={id === undefined ? undefined : recipe.data?.recipe}
      copyOf={id === undefined ? recipe.data?.recipe : undefined}
      allTags={recipes.data.tags}
    />
  );
};

export default RecipeEditPage;
