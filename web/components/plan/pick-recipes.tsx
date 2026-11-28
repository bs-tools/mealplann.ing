import Loading from "@components/loading";
import type { PickedRecipe } from "@components/recipes/recipe-picker";
import RecipePicker from "@components/recipes/recipe-picker";
import { EmptyState } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  cardClass,
  linkClass,
  primaryButton,
  secondaryButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { createClient } from "@connectrpc/connect";
import type { ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useTransport } from "@connectrpc/connect-query";
import { CheckIcon } from "@heroicons/react/24/outline";
import {
  useMutation as useTanstackMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  deleteCook,
  listAnytimeCooks,
  updateCook,
} from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService } from "gen/plans/v1/plans_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { suggestPicks } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { today } from "helpers/dates";
import { formatList } from "helpers/format";
import { formatServings } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useNavigate } from "react-router";

// MAX_PICKS is how many recipes are picked for one shop.
const MAX_PICKS = 5;

/**
 * PickRecipes picks up to five recipes to buy for in one shop and cook any
 * time after, suggesting those using food to use soon and sharing
 * ingredients with the picks; and lists those picked before, still to
 * cook, to cook now or drop.
 */
const PickRecipes: React.FC = () => {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const transport = useTransport();
  const now = today();
  const [picked, setPicked] = useState<PickedRecipe[]>([]);
  const [added, setAdded] = useState(0);
  const toCook = useQuery(listAnytimeCooks, {});
  // The recipes, which the plan has read, for the servings of one picked.
  const allRecipes = useQuery(listRecipes, {});
  const suggestions = useQuery(
    suggestPicks,
    { date: now, pickedRecipeIds: picked.map((recipe) => recipe.id) },
    { placeholderData: (previous) => previous }
  );
  const invalidate = async (): Promise<void> => {
    await invalidateServices(queryClient, [
      PlanService,
      ShoppingService,
      SuggestionService,
      PantryService,
    ]);
  };
  const add = useTanstackMutation<void, ConnectError, PickedRecipe[]>({
    mutationFn: async (recipes) => {
      const client = createClient(PlanService, transport);
      /*
       * The picks are cooks of today, the earliest they're cooked, so the
       * shopping list from today buys for them.
       */
      await Promise.all(
        recipes.map((recipe) => {
          const servings =
            allRecipes.data?.recipes.find((other) => other.id === recipe.id)
              ?.servings ?? 0;
          return client.createCook({
            cook: {
              anytime: true,
              date: now,
              recipeId: recipe.id,
              servings: servings > 0 ? servings : 2,
            },
          });
        })
      );
    },
    onSuccess: async (_result, recipes) => {
      setAdded(recipes.length);
      setPicked([]);
      await invalidate();
    },
  });
  const cookNow = useMutation(updateCook, { onSuccess: invalidate });
  const drop = useMutation(deleteCook, { onSuccess: invalidate });

  let suggested: React.ReactNode = null;
  if (suggestions.isPending) {
    suggested = <Loading />;
  } else if (suggestions.isError) {
    suggested = (
      <QueryError
        error={suggestions.error}
        onRetry={() => {
          void suggestions.refetch();
        }}
      />
    );
  } else if (suggestions.data.suggestions.length === 0) {
    suggested = (
      <EmptyState title="No lunches or dinners to suggest">
        Tag recipes lunch or dinner to have them suggested here.
      </EmptyState>
    );
  } else {
    suggested = (
      <ul className="flex flex-col gap-2">
        {suggestions.data.suggestions.map((suggestion) => (
          <li key={suggestion.recipeId}>
            <button
              type="button"
              className={`${cardClass} flex w-full items-start gap-3 px-4 py-3 text-left hover:border-muted disabled:opacity-50`}
              disabled={picked.length >= MAX_PICKS}
              onClick={() => {
                setAdded(0);
                setPicked([
                  ...picked,
                  { id: suggestion.recipeId, name: suggestion.recipeName },
                ]);
              }}
            >
              <span
                className="mt-0.5 size-6 shrink-0 rounded-lg border-2 border-gray-400"
                aria-hidden="true"
              />
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="text-base font-bold text-ink-soft">
                  {suggestion.recipeName}
                </span>
                <span className="text-sm text-muted">
                  {suggestion.reasons.join(" · ")}
                </span>
              </span>
            </button>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <div className="flex flex-col gap-5">
      {(toCook.data?.cooks.length ?? 0) > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className={sectionLabelClass}>Bought for, to cook any time</h2>
          <ul className="flex flex-col gap-2">
            {toCook.data?.cooks.map((cook) => (
              <li
                key={cook.id}
                className={`${cardClass} flex flex-wrap items-center gap-3 px-4 py-3`}
              >
                <span className="flex min-w-0 grow flex-col">
                  <Link
                    to={`/recipes/${cook.recipeId}`}
                    className="font-bold text-ink-soft hover:underline"
                  >
                    {cook.recipeName}
                  </Link>
                  <span className="text-sm text-muted">
                    {formatServings(cook.servings)} · picked{" "}
                    {cook.date === now ? "today" : cook.date}
                  </span>
                </span>
                <button
                  type="button"
                  className={primaryButton}
                  disabled={cookNow.isPending}
                  onClick={() => {
                    // Cooked now, it's today's cook, and no longer any time.
                    cookNow.mutate(
                      {
                        cook: { anytime: false, date: now, id: cook.id },
                        updateMask: { paths: ["date", "anytime"] },
                      },
                      {
                        onSuccess: () => {
                          void navigate(
                            `/recipes/${cook.recipeId}/cook?${new URLSearchParams(
                              { cook: cook.id, servings: String(cook.servings) }
                            ).toString()}`
                          );
                        },
                      }
                    );
                  }}
                >
                  Cook now
                </button>
                <button
                  type="button"
                  className={secondaryButton}
                  disabled={drop.isPending}
                  onClick={() => {
                    drop.mutate({ id: cook.id });
                  }}
                >
                  Drop
                </button>
              </li>
            ))}
          </ul>
          <ErrorText error={cookNow.error ?? drop.error} />
        </section>
      )}
      <section className="flex flex-col gap-3">
        <div className="flex flex-col gap-1">
          <h2 className={sectionLabelClass}>Pick recipes to buy for</h2>
          <p className="text-sm text-muted">
            Up to {MAX_PICKS}, to shop for once and cook whenever suits, best
            first for what&apos;s going off and what they share.
          </p>
        </div>
        {picked.length > 0 && (
          <ul className="flex flex-col gap-2">
            {picked.map((recipe) => (
              <li key={recipe.id}>
                <button
                  type="button"
                  className={`${cardClass} flex w-full items-center gap-3 border-2 border-pine px-4 py-3 text-left`}
                  aria-pressed="true"
                  onClick={() => {
                    setPicked(picked.filter((other) => other.id !== recipe.id));
                  }}
                >
                  <span className="flex size-6 shrink-0 items-center justify-center rounded-lg bg-pine text-white">
                    <CheckIcon className="size-4" aria-hidden="true" />
                  </span>
                  <span className="text-base font-bold text-ink-soft">
                    {recipe.name}
                  </span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {picked.length < MAX_PICKS && (
          <RecipePicker
            label="Pick another recipe"
            value={null}
            onChange={(recipe) => {
              if (!picked.some((other) => other.id === recipe.id)) {
                setAdded(0);
                setPicked([...picked, recipe]);
              }
            }}
          />
        )}
        {suggested}
      </section>
      {added > 0 && (
        <p className="rounded-2xl bg-pine-50 p-4 text-sm text-pine-dark">
          {added === 1 ? "It's" : "They're"} on the plan, to cook any time.{" "}
          <Link to="/shopping" className={linkClass}>
            Make the shopping list
          </Link>{" "}
          from today to buy for {added === 1 ? "it" : "them"}.
        </p>
      )}
      {picked.length > 0 && (
        <div className="sticky bottom-20 flex flex-col gap-2 rounded-2xl border border-line bg-white p-4 shadow-sm lg:bottom-4">
          <p className="text-sm text-muted">
            <span className="font-bold text-ink-soft">
              {picked.length} picked
            </span>
            {(suggestions.data?.pickedToBuy.length ?? 0) === 0
              ? " · nothing to buy"
              : ` · to buy: ${formatList(suggestions.data?.pickedToBuy ?? [])}`}
          </p>
          <button
            type="button"
            className={primaryButton}
            disabled={add.isPending}
            onClick={() => {
              add.mutate(picked);
            }}
          >
            Add to the plan, to cook any time
          </button>
          <ErrorText error={add.error} />
        </div>
      )}
    </div>
  );
};

export default PickRecipes;
