import Loading from "@components/loading";
import { RecipeLeftovers } from "@components/pantry/in-the-kitchen";
import RecipeBalance from "@components/recipes/balance";
import FavoriteButton from "@components/recipes/favorite-button";
import NutritionPanel, {
  issueProblem,
} from "@components/recipes/nutrition-panel";
import SuggestMenu from "@components/recipes/suggest-feedback";
import { tintOf } from "@components/recipes/tint";
import BackLink from "@components/ui/back-link";
import Card from "@components/ui/card";
import HintIcon from "@components/ui/hint-icon";
import { Badge } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import { menuItemClass } from "@components/ui/more-menu";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  dangerButton,
  linkClass,
  primaryButton,
  secondaryButton,
  sectionLabelClass,
  stickyBarClass,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import {
  ArrowTopRightOnSquareIcon,
  EllipsisHorizontalIcon,
  ExclamationTriangleIcon,
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
import { formatAmount } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import {
  formatStepDuration,
  groupSteps,
  ingredientAmount,
  ingredientName,
  stepStart,
} from "helpers/steps";
import { useUnitSystem } from "helpers/user";
import React, { useState } from "react";
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
interface ServedWithChipsProps {
  recipe: Recipe;
}

/**
 * ServedWithChips shows the sides a recipe is served with as chips, a
 * recipe side linking to its recipe, with how much goes with a serving:
 * each meal of it suggests the one that fits the day.
 */
const ServedWithChips: React.FC<ServedWithChipsProps> = ({ recipe }) => {
  if (recipe.servedWith.length === 0) {
    return null;
  }
  const chipClass =
    "inline-flex items-baseline gap-1.5 rounded-full bg-white px-4 py-2 text-sm text-ink-soft ring-1 ring-line ring-inset";
  return (
    <section className="flex flex-col gap-2">
      <h2 className={sectionLabelClass}>Served with</h2>
      <ul className="flex flex-wrap gap-2">
        {recipe.servedWith.map((side) => {
          const amount =
            side.recipeId === ""
              ? `${formatAmount(side.gramsPerServing)} g`
              : formatServings(side.servingsPerServing);
          return (
            <li key={side.recipeId === "" ? side.foodId : side.recipeId}>
              {side.recipeId === "" ? (
                <span className={chipClass}>
                  {side.foodName}
                  <span className="text-xs text-muted">{amount}</span>
                </span>
              ) : (
                <Link
                  to={`/recipes/${side.recipeId}`}
                  className={`${chipClass} font-bold hover:ring-muted`}
                >
                  {side.recipeName}
                  <span className="text-xs font-normal text-muted">
                    {amount}
                  </span>
                </Link>
              )}
            </li>
          );
        })}
      </ul>
      <p className="text-xs text-muted">
        A meal of it suggests the side that fits the day best, for each serving.
      </p>
    </section>
  );
};

const RecipePage: React.FC = () => {
  const [deleting, setDeleting] = useState(false);
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
  // The ingredients lacking data, by index, to say what each lacks.
  const issues = new Map(
    recipe.nutrition?.issues.map((issue) => [issue.ingredientIndex, issue]) ??
      []
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
      <div
        className={`relative -mt-1 flex h-36 items-start justify-end rounded-3xl p-3 sm:h-44 print:hidden ${tintOf(recipe.id)}`}
      >
        <Menu as="div" className="relative">
          <MenuButton
            className="flex size-10 items-center justify-center rounded-full bg-white/80 text-ink-soft backdrop-blur hover:bg-white"
            aria-label="More"
            title="More"
          >
            <EllipsisHorizontalIcon className="size-6" />
          </MenuButton>
          <MenuItems
            transition
            anchor="bottom end"
            className="z-[1100] mt-1 w-60 rounded-2xl border border-line bg-white py-1 font-ui shadow-lg transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
          >
            <MenuItem>
              <Link to={`/recipes/${recipe.id}/edit`} className={menuItemClass}>
                Edit
              </Link>
            </MenuItem>
            <MenuItem>
              <Link
                to={`/recipes/new?from=${recipe.id}`}
                className={menuItemClass}
              >
                Start a variation of it
              </Link>
            </MenuItem>
            <MenuItem>
              <Link
                to={`/recipes/${recipe.id}/timeline`}
                className={menuItemClass}
              >
                Timeline
              </Link>
            </MenuItem>
            <MenuItem>
              <button
                type="button"
                className={`${menuItemClass} text-error`}
                onClick={() => {
                  setDeleting(true);
                }}
              >
                Delete
              </button>
            </MenuItem>
          </MenuItems>
        </Menu>
      </div>
      <div className="flex flex-col gap-1">
        <h1 className="text-[28px] leading-tight font-bold tracking-tight text-ink-soft">
          {recipe.name}
        </h1>
        <p className="text-sm text-muted tabular-nums">
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
        </p>
        {recipe.description !== "" && (
          <p className="text-sm text-ink-soft">{recipe.description}</p>
        )}
      </div>
      {deleting && (
        <Modal
          open
          title="Delete recipe?"
          onClose={() => {
            setDeleting(false);
          }}
        >
          <div className="flex flex-col gap-4">
            <p className="text-sm text-ink-soft">
              &ldquo;{recipe.name}&rdquo; will be deleted. Its foods are kept.
            </p>
            <div className="flex gap-2">
              <button
                type="button"
                className={dangerButton}
                disabled={remove.isPending}
                onClick={() => {
                  remove.mutate({ id: recipe.id });
                }}
              >
                {remove.isPending ? "Deleting…" : "Delete"}
              </button>
              <button
                type="button"
                className={secondaryButton}
                onClick={() => {
                  setDeleting(false);
                }}
              >
                Keep it
              </button>
            </div>
          </div>
        </Modal>
      )}
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
      <ServedWithChips recipe={recipe} />
      <RecipeLeftovers recipeId={recipe.id} />
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-5">
        <div className="flex flex-col gap-4 lg:col-span-3">
          <Card title="Ingredients">
            {recipe.ingredients.length === 0 ? (
              <p className="text-sm text-muted">No ingredients yet.</p>
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
                      <span className="font-medium text-ink-soft tabular-nums">
                        {ingredientAmount(ingredient, 1, system)}
                      </span>
                    )}
                    {ingredient.recipeId === "" && ingredient.foodId === "" && (
                      <>
                        <span className="text-ink-soft">
                          {ingredient.originalText}
                        </span>
                        <Badge>No food yet</Badge>
                      </>
                    )}
                    {ingredient.recipeId === "" && ingredient.foodId !== "" && (
                      <Link
                        to={`/foods/${ingredient.foodId}`}
                        className="text-ink-soft hover:underline"
                      >
                        {ingredient.foodName}
                      </Link>
                    )}
                    {/* The foods it can be made with instead. */}
                    {ingredient.alternatives.map((alternative) => (
                      <React.Fragment key={alternative.foodId}>
                        <span className="text-muted">or</span>
                        <Link
                          to={`/foods/${alternative.foodId}`}
                          className="text-ink-soft hover:underline"
                        >
                          {alternative.foodName}
                        </Link>
                      </React.Fragment>
                    ))}
                    {ingredient.recipeId !== "" && (
                      <>
                        <span className="text-muted">of</span>
                        <Link
                          to={`/recipes/${ingredient.recipeId}`}
                          className="font-medium text-pine hover:underline"
                        >
                          {ingredient.recipeName}
                        </Link>
                        <Badge>Recipe</Badge>
                        {ingredient.unit !== AmountUnit.UNSPECIFIED && (
                          <span className="text-xs text-muted">
                            {ingredient.componentServings > 0
                              ? formatServings(ingredient.componentServings)
                              : "servings unknown: set what it makes"}
                          </span>
                        )}
                      </>
                    )}
                    {ingredient.note !== "" && (
                      <span className="text-muted">{ingredient.note}</span>
                    )}
                    {ingredient.optional && <Badge>Optional</Badge>}
                    {issues.has(index) && (
                      <HintIcon
                        icon={ExclamationTriangleIcon}
                        className="text-warn"
                        hint={`${ingredient.foodName === "" ? ingredient.originalText : ingredient.foodName} ${issueProblem(recipe, issues.get(index))}`}
                      />
                    )}
                    {ingredient.originalText !== "" &&
                      ingredient.foodId !== "" && (
                        <span className="w-full text-xs text-muted/70">
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
              <p className="text-sm text-muted">No steps yet.</p>
            ) : (
              <div className="flex flex-col gap-4">
                {groupSteps(recipe.instructions).map((group) => (
                  <div
                    key={group.steps[0]?.index}
                    className="flex flex-col gap-2"
                  >
                    {group.section !== "" && (
                      <h3 className="text-xs font-semibold tracking-wide text-muted uppercase">
                        {group.section}
                      </h3>
                    )}
                    <ol className="flex flex-col gap-3 text-sm text-ink-soft">
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
                            <span className="w-5 shrink-0 text-right font-semibold text-muted tabular-nums">
                              {index + 1}.
                            </span>
                            <div className="flex flex-col gap-0.5">
                              <span className="whitespace-pre-line">
                                {step.text}
                              </span>
                              {(used.length > 0 ||
                                duration !== "" ||
                                stepStart(step, index) !== "") && (
                                <span className="text-xs text-muted">
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
              <p className="text-sm whitespace-pre-line text-ink-soft">
                {recipe.notes}
              </p>
            </Card>
          )}
        </div>
        <div className="order-first flex flex-col gap-4 lg:order-none lg:col-span-2">
          <RecipeBalance perServing={recipe.nutrition?.perServing} />
          <NutritionPanel recipe={recipe} />
          {(recipe.tags.length > 0 || keeps !== "") && (
            <div className="flex flex-wrap items-center gap-2 text-sm text-muted">
              {recipe.tags.map((tag) => (
                <Link
                  key={tag}
                  to={`/recipes?tag=${encodeURIComponent(tag)}`}
                  className="rounded-full bg-ground-dark px-3 py-1 text-sm text-ink-soft hover:bg-ground-dark"
                >
                  {tag}
                </Link>
              ))}
              {keeps !== "" && <span>{keeps}</span>}
            </div>
          )}
          <div className="flex flex-wrap items-start gap-2 print:hidden">
            <FavoriteButton recipe={recipe} />
            <SuggestMenu recipe={recipe} />
          </div>
        </div>
      </div>
      <div className={`${stickyBarClass} flex gap-2`}>
        {!recipe.draft && (
          <Link
            to={`/plan?plan=${recipe.id}`}
            className={`${primaryButton} grow py-3.5 text-base`}
          >
            Plan this
          </Link>
        )}
        <Link
          to={`/recipes/${recipe.id}/cook`}
          className={`${secondaryButton} py-3.5 text-base ${recipe.draft ? "grow" : ""}`}
        >
          Cook now
        </Link>
      </div>
    </div>
  );
};

export default RecipePage;
