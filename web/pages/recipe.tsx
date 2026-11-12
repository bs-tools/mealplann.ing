import Loading from "@components/loading";
import RecipeBalance from "@components/recipes/balance";
import FavoriteButton from "@components/recipes/favorite-button";
import NutritionPanel from "@components/recipes/nutrition-panel";
import SuggestMenu from "@components/recipes/suggest-feedback";
import BackLink from "@components/ui/back-link";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { Badge, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  iconButton,
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  ArrowTopRightOnSquareIcon,
  CalendarDaysIcon,
  ChartBarIcon,
  ExclamationTriangleIcon,
  DocumentDuplicateIcon,
  PencilIcon,
  PlayIcon,
  TrashIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import {
  deleteRecipe,
  getRecipe,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { Recipe } from "gen/recipes/v1/recipes_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import {
  formatDollars,
  formatHoursMinutes,
  formatList,
  formatRedirectTarget,
} from "helpers/format";
import { formatIngredientAmount, formatServings } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import {
  formatStepDuration,
  groupSteps,
  ingredientAmount,
  ingredientName,
  stepStart,
} from "helpers/steps";
import { useUnitSystem } from "helpers/user";
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
  if (recipe.prepAheadDays > 0) {
    const days =
      recipe.prepAheadDays === 1
        ? "the day before"
        : `${recipe.prepAheadDays} days ahead`;
    notes.push(
      recipe.prepAheadNote === ""
        ? `Starts ${days}`
        : `${recipe.prepAheadNote} ${days}`
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
  const system = useUnitSystem();
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
  const totalSeconds = recipe.prepSeconds + recipe.cookSeconds;
  const meta = [
    totalSeconds > 0 ? formatHoursMinutes(totalSeconds) : "",
    formatServings(recipe.servings),
    recipe.yieldAmount > 0
      ? `makes ${formatIngredientAmount(recipe.yieldAmount, recipe.yieldUnit, system)}`
      : "",
    recipe.nutrition?.perServing?.energyKcal === undefined
      ? ""
      : `${Math.round(recipe.nutrition.perServing.energyKcal)} kcal a serving`,
    recipe.cost !== undefined && recipe.cost.totalCents > 0
      ? `≈ ${formatDollars(recipe.cost.centsPerServing)} a serving`
      : "",
  ].filter((part) => part !== "");
  return (
    <div className="flex flex-col gap-4">
      <BackLink to="/recipes">Recipes</BackLink>
      <PageHeader
        title={recipe.name}
        subtitle={
          <span className="flex flex-col gap-1">
            <span className="tabular-nums">
              {meta.join(" · ")}
              {recipe.sourceUrl !== "" && (
                <>
                  {" · "}
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
                </>
              )}
            </span>
            {recipe.description !== "" && (
              <span className="text-ink-soft">{recipe.description}</span>
            )}
          </span>
        }
        actions={
          <div className="flex items-center gap-1 print:hidden">
            <Link
              to={`/recipes/${recipe.id}/edit`}
              className={iconButton}
              aria-label="Edit"
              title="Edit"
            >
              <PencilIcon className="size-5" />
            </Link>
            <Link
              to={`/recipes/new?from=${recipe.id}`}
              className={iconButton}
              aria-label="Duplicate"
              title="Start a new recipe from this one, to make a variation of it"
            >
              <DocumentDuplicateIcon className="size-5" />
            </Link>
            <Link
              to={`/recipes/${recipe.id}/timeline`}
              className={iconButton}
              aria-label="Timeline"
              title="Timeline"
            >
              <ChartBarIcon className="size-5" />
            </Link>
            <ConfirmButton
              className={iconButton}
              title="Delete recipe?"
              message={`"${recipe.name}" will be deleted. Its foods are kept.`}
              disabled={remove.isPending}
              onConfirm={() => {
                remove.mutate({ id: recipe.id });
              }}
            >
              <TrashIcon className="size-5" aria-label="Delete" />
            </ConfirmButton>
          </div>
        }
      />
      <div className="-mt-2 flex flex-wrap items-center gap-2 print:hidden">
        {!recipe.draft && (
          <Link to={`/plan?plan=${recipe.id}`} className={primaryButton}>
            <CalendarDaysIcon className="size-4" aria-hidden="true" />
            Plan this
          </Link>
        )}
        <Link to={`/recipes/${recipe.id}/cook`} className={secondaryButton}>
          <PlayIcon className="size-4" aria-hidden="true" />
          Cook now
        </Link>
        <FavoriteButton recipe={recipe} />
        <SuggestMenu recipe={recipe} />
      </div>
      <ErrorText error={remove.error} />
      {recipe.draft && (
        <p className="rounded-xl bg-warn-tint px-3 py-2 text-sm text-warn-dark">
          This is a draft: it isn&apos;t planned or suggested.{" "}
          <Link to={`/recipes/${recipe.id}/edit`} className={linkClass}>
            Edit it
          </Link>{" "}
          to choose a food for each ingredient, and save it to finish it.
        </p>
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
                    {(ingredient.foodId !== "" ||
                      ingredient.recipeId !== "") && (
                      <span className="font-medium text-gray-900 tabular-nums">
                        {ingredientAmount(ingredient, 1, system)}
                      </span>
                    )}
                    {ingredient.recipeId === "" && ingredient.foodId === "" && (
                      <>
                        <span className="text-gray-900">
                          {ingredient.originalText}
                        </span>
                        <Badge>No food yet</Badge>
                      </>
                    )}
                    {ingredient.recipeId === "" && ingredient.foodId !== "" && (
                      <Link
                        to={`/foods/${ingredient.foodId}`}
                        className="text-gray-900 hover:underline"
                      >
                        {ingredient.foodName}
                      </Link>
                    )}
                    {ingredient.recipeId !== "" && (
                      <>
                        <span className="text-gray-500">of</span>
                        <Link
                          to={`/recipes/${ingredient.recipeId}`}
                          className="font-medium text-brand-700 hover:underline"
                        >
                          {ingredient.recipeName}
                        </Link>
                        <Badge>Recipe</Badge>
                        {ingredient.unit !== AmountUnit.UNSPECIFIED && (
                          <span className="text-xs text-gray-500">
                            {ingredient.componentServings > 0
                              ? formatServings(ingredient.componentServings)
                              : "servings unknown: set what it makes"}
                          </span>
                        )}
                      </>
                    )}
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
                    {ingredient.originalText !== "" &&
                      ingredient.foodId !== "" && (
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
            {recipe.instructions.length === 0 ? (
              <p className="text-sm text-gray-500">No steps yet.</p>
            ) : (
              <div className="flex flex-col gap-4">
                {groupSteps(recipe.instructions).map((group) => (
                  <div
                    key={group.steps[0]?.index}
                    className="flex flex-col gap-2"
                  >
                    {group.section !== "" && (
                      <h3 className="text-xs font-semibold tracking-wide text-gray-500 uppercase">
                        {group.section}
                      </h3>
                    )}
                    <ol className="flex flex-col gap-3 text-sm text-gray-800">
                      {group.steps.map(({ step, index }) => {
                        const used = step.ingredientIndexes
                          .map((position) => recipe.ingredients[position])
                          .filter((ingredient) => ingredient !== undefined)
                          .map(ingredientName);
                        const duration = formatStepDuration(
                          step.durationSeconds
                        );
                        return (
                          <li key={index} className="flex gap-2">
                            <span className="w-5 shrink-0 text-right font-semibold text-gray-500 tabular-nums">
                              {index + 1}.
                            </span>
                            <div className="flex flex-col gap-0.5">
                              <span className="whitespace-pre-line">
                                {step.text}
                              </span>
                              {(used.length > 0 ||
                                duration !== "" ||
                                stepStart(step, index) !== "") && (
                                <span className="text-xs text-gray-500">
                                  {[
                                    stepStart(step, index),
                                    duration === ""
                                      ? ""
                                      : `${duration}${step.waiting ? " waiting" : ""}`,
                                    formatList(used),
                                  ]
                                    .filter((part) => part !== "")
                                    .join(" · ")}
                                </span>
                              )}
                            </div>
                          </li>
                        );
                      })}
                    </ol>
                  </div>
                ))}
              </div>
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
        <div className="order-first flex flex-col gap-4 lg:order-none lg:col-span-2">
          <RecipeBalance perServing={recipe.nutrition?.perServing} />
          {recipe.servedWith.length > 0 && (
            <p className="text-sm text-muted">
              Served with{" "}
              {recipe.servedWith
                .map(
                  (side) =>
                    `${side.foodName.toLowerCase()} (${side.gramsPerServing} g)`
                )
                .join(", ")}
              : each meal suggests the one that fits the day.
            </p>
          )}
          <NutritionPanel recipe={recipe} />
          {(recipe.tags.length > 0 || keeps !== "") && (
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted">
              {recipe.tags.map((tag) => (
                <Link
                  key={tag}
                  to={`/recipes?tag=${encodeURIComponent(tag)}`}
                  className="rounded-full bg-ground-dark px-3 py-1 text-sm text-ink-soft hover:bg-gray-200"
                >
                  {tag}
                </Link>
              ))}
              {keeps !== "" && <span>{keeps}</span>}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};

export default RecipePage;
