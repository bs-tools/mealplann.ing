import Loading from "@components/loading";
import FavoriteButton from "@components/recipes/favorite-button";
import NutritionPanel from "@components/recipes/nutrition-panel";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { Badge, PageHeader, Stat } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import { linkClass, primaryButton } from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  ArrowLeftIcon,
  ArrowTopRightOnSquareIcon,
  ExclamationTriangleIcon,
  PencilIcon,
  TrashIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import {
  deleteRecipe,
  getRecipe,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { Recipe } from "gen/recipes/v1/recipes_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { formatHoursMinutes, formatRedirectTarget } from "helpers/format";
import { formatIngredientAmount } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React from "react";
import { Link, useNavigate, useParams } from "react-router";

// FridgeNote says how long a recipe's leftovers keep.
const fridgeNote = (recipe: Recipe): string => {
  const notes: string[] = [];
  if (recipe.fridgeDays !== undefined) {
    notes.push(
      recipe.fridgeDays === 0
        ? "Doesn't keep"
        : `Keeps ${recipe.fridgeDays} ${recipe.fridgeDays === 1 ? "day" : "days"} in the fridge`
    );
  }
  if (recipe.freezable) {
    notes.push(
      recipe.freezerDays === undefined
        ? "Freezes well"
        : `Keeps ${recipe.freezerDays} ${recipe.freezerDays === 1 ? "day" : "days"} frozen`
    );
  }
  return notes.join(" · ");
};

/**
 * RecipePage shows a recipe: its ingredients, steps and nutrition per
 * serving, with the ingredients whose foods lack data flagged.
 */
const RecipePage: React.FC = () => {
  const { id = "" } = useParams();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { isPending, isError, data, error, refetch } = useQuery(getRecipe, {
    id,
  });
  const remove = useMutation(deleteRecipe, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [RecipeService]);
      void navigate("/recipes");
    },
  });
  if (isPending) {
    return <Loading />;
  }
  if (isError) {
    return (
      <QueryError
        error={error}
        onRetry={() => {
          void refetch();
        }}
      />
    );
  }
  const { recipe } = data;
  if (recipe === undefined) {
    return null;
  }
  const issueIndexes = new Set(
    recipe.nutrition?.issues.map((issue) => issue.ingredientIndex) ?? []
  );
  const keeps = fridgeNote(recipe);
  return (
    <div className="flex flex-col gap-4">
      <div>
        <Link
          to="/recipes"
          className="inline-flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900 print:hidden"
        >
          <ArrowLeftIcon className="size-4" aria-hidden="true" />
          Recipes
        </Link>
      </div>
      <PageHeader
        title={recipe.name}
        subtitle={recipe.description === "" ? undefined : recipe.description}
        actions={
          <div className="flex items-start gap-2 print:hidden">
            <FavoriteButton recipe={recipe} />
            <ConfirmButton
              title="Delete recipe?"
              message={`"${recipe.name}" will be deleted. Its foods are kept.`}
              disabled={remove.isPending}
              onConfirm={() => {
                remove.mutate({ id: recipe.id });
              }}
            >
              <TrashIcon className="size-4" aria-hidden="true" />
              Delete
            </ConfirmButton>
            <Link to={`/recipes/${recipe.id}/edit`} className={primaryButton}>
              <PencilIcon className="size-4" aria-hidden="true" />
              Edit
            </Link>
          </div>
        }
      />
      <ErrorText error={remove.error} />
      <div className="flex flex-wrap gap-x-8 gap-y-3">
        <Stat label="Servings" value={recipe.servings} />
        {recipe.prepSeconds > 0 && (
          <Stat label="Prep" value={formatHoursMinutes(recipe.prepSeconds)} />
        )}
        {recipe.cookSeconds > 0 && (
          <Stat label="Cook" value={formatHoursMinutes(recipe.cookSeconds)} />
        )}
        {recipe.nutrition?.perServing?.energyKcal !== undefined && (
          <Stat
            label="Per serving"
            value={`${Math.round(recipe.nutrition.perServing.energyKcal)} kcal`}
          />
        )}
      </div>
      {(recipe.tags.length > 0 || keeps !== "" || recipe.sourceUrl !== "") && (
        <div className="flex flex-wrap items-center gap-2 text-sm text-gray-600">
          {recipe.tags.map((tag) => (
            <Link key={tag} to={`/recipes?tag=${encodeURIComponent(tag)}`}>
              <Badge>{tag}</Badge>
            </Link>
          ))}
          {keeps !== "" && <span>{keeps}</span>}
          {recipe.sourceUrl !== "" && (
            <a
              href={recipe.sourceUrl}
              target="_blank"
              rel="noreferrer"
              className={`${linkClass} inline-flex items-center gap-1`}
            >
              {formatRedirectTarget(recipe.sourceUrl)}
              <ArrowTopRightOnSquareIcon
                className="size-3.5"
                aria-hidden="true"
              />
            </a>
          )}
        </div>
      )}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-5">
        <div className="flex flex-col gap-4 lg:col-span-3">
          <Card title="Ingredients">
            {recipe.ingredients.length === 0 ? (
              <p className="text-sm text-gray-500">No ingredients yet.</p>
            ) : (
              <ul className="flex flex-col gap-2 text-sm">
                {recipe.ingredients.map((ingredient, index) => (
                  <li
                    // Ingredients are only ever shown in order.
                    // oxlint-disable-next-line react/no-array-index-key
                    key={index}
                    className="flex flex-wrap items-baseline gap-x-1.5"
                  >
                    <span className="font-medium text-gray-900 tabular-nums">
                      {formatIngredientAmount(
                        ingredient.amount,
                        ingredient.unit
                      )}
                    </span>
                    <Link
                      to={`/foods/${ingredient.foodId}`}
                      className="text-gray-900 hover:underline"
                    >
                      {ingredient.foodName}
                    </Link>
                    {ingredient.note !== "" && (
                      <span className="text-gray-500">{ingredient.note}</span>
                    )}
                    {ingredient.optional && <Badge>Optional</Badge>}
                    {issueIndexes.has(index) && (
                      <ExclamationTriangleIcon
                        className="size-4 self-center text-amber-600"
                        aria-label="Its food lacks data"
                      />
                    )}
                    {ingredient.originalText !== "" && (
                      <span className="w-full text-xs text-gray-400">
                        “{ingredient.originalText}”
                      </span>
                    )}
                  </li>
                ))}
              </ul>
            )}
          </Card>
          <Card title="Steps">
            {recipe.steps.length === 0 ? (
              <p className="text-sm text-gray-500">No steps yet.</p>
            ) : (
              <ol className="flex list-decimal flex-col gap-3 pl-5 text-sm text-gray-800 marker:font-semibold marker:text-gray-500">
                {recipe.steps.map((step, index) => (
                  // Steps are only ever shown in order.
                  // oxlint-disable-next-line react/no-array-index-key
                  <li key={index} className="pl-1 whitespace-pre-line">
                    {step}
                  </li>
                ))}
              </ol>
            )}
          </Card>
          {recipe.notes !== "" && (
            <Card title="Notes">
              <p className="text-sm whitespace-pre-line text-gray-800">
                {recipe.notes}
              </p>
            </Card>
          )}
        </div>
        <div className="lg:col-span-2">
          <NutritionPanel recipe={recipe} />
        </div>
      </div>
    </div>
  );
};

export default RecipePage;
