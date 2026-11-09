import Loading from "@components/loading";
import ImportRecipeDialog from "@components/recipes/import-recipe-dialog";
import { Badge, EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import {
  cardClass,
  inputClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  ArrowDownTrayIcon,
  ClockIcon,
  ExclamationTriangleIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  StarIcon,
} from "@heroicons/react/24/outline";
import { StarIcon as FilledStarIcon } from "@heroicons/react/24/solid";
import { keepPreviousData } from "@tanstack/react-query";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { RecipeSummary } from "gen/recipes/v1/recipes_pb";
import { formatHoursMinutes } from "helpers/format";
import { useDebouncedValue } from "helpers/hooks";
import { formatAmount } from "helpers/nutrients";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

/**
 * RecipesPage lists the household's recipes, filtered by a search, tags
 * and whether they're favorites, kept in the URL, with their main
 * nutrients per serving.
 */
const RecipesPage: React.FC = () => {
  const [params, setParams] = useSearchParams();
  const [query, setQuery] = useState(params.get("q") ?? "");
  const [importing, setImporting] = useState(false);
  const tags = params.getAll("tag");
  const favoritesOnly = params.get("favorites") === "1";
  const search = useDebouncedValue(query.trim());
  const { isPending, isError, data, error, refetch } = useQuery(
    listRecipes,
    { favoritesOnly, query: search, tags },
    { placeholderData: keepPreviousData }
  );

  const setFilters = (
    nextQuery: string,
    nextTags: string[],
    nextFavoritesOnly = favoritesOnly
  ) => {
    const next = new URLSearchParams();
    if (nextQuery !== "") {
      next.set("q", nextQuery);
    }
    for (const tag of nextTags) {
      next.append("tag", tag);
    }
    if (nextFavoritesOnly) {
      next.set("favorites", "1");
    }
    setParams(next, { replace: true });
  };

  let content: React.ReactNode = null;
  if (isPending) {
    content = <Loading />;
  } else if (isError) {
    content = (
      <QueryError
        error={error}
        onRetry={() => {
          void refetch();
        }}
      />
    );
  } else if (data.recipes.length === 0) {
    if (search === "" && tags.length === 0 && !favoritesOnly) {
      content = (
        <EmptyState title="No recipes yet">
          Add your first recipe, or ask your AI agent to enter some.
        </EmptyState>
      );
    } else if (search === "" && tags.length === 0) {
      content = (
        <EmptyState title="No favorites yet">
          Mark the recipes your household likes as favorites, on their pages, to
          have them suggested more often.
        </EmptyState>
      );
    } else {
      content = (
        <EmptyState title="No recipes found">
          Try another search, or fewer filters.
        </EmptyState>
      );
    }
  } else {
    content = (
      <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {data.recipes.map((recipe) => (
          <RecipeCard key={recipe.id} recipe={recipe} />
        ))}
      </ul>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Recipes"
        subtitle="Your household's recipes, with nutrition per serving."
        actions={
          <>
            <button
              type="button"
              className={secondaryButton}
              onClick={() => {
                setImporting(true);
              }}
            >
              <ArrowDownTrayIcon className="size-4" aria-hidden="true" />
              Import
            </button>
            <Link to="/recipes/new" className={primaryButton}>
              <PlusIcon className="size-4" aria-hidden="true" />
              New recipe
            </Link>
          </>
        }
      />
      <ImportRecipeDialog
        open={importing}
        onClose={() => {
          setImporting(false);
        }}
      />
      <div className="flex flex-col gap-2">
        <label className="relative block sm:max-w-sm">
          <span className="sr-only">Search recipes</span>
          <MagnifyingGlassIcon className="pointer-events-none absolute top-2 left-2.5 size-4 text-gray-400" />
          <input
            type="search"
            className={`${inputClass} pl-8`}
            value={query}
            placeholder="Search recipes"
            onChange={(event) => {
              setQuery(event.target.value);
              setFilters(event.target.value.trim(), tags);
            }}
          />
        </label>
        <div className="flex flex-wrap gap-1.5" aria-label="Filters">
          <button
            type="button"
            aria-pressed={favoritesOnly}
            className={`${filterClass(favoritesOnly)} inline-flex items-center gap-1`}
            onClick={() => {
              setFilters(query.trim(), tags, !favoritesOnly);
            }}
          >
            {favoritesOnly ? (
              <FilledStarIcon className="size-3.5" aria-hidden="true" />
            ) : (
              <StarIcon className="size-3.5" aria-hidden="true" />
            )}
            Favorites
          </button>
          {data?.tags.map((tag) => {
            const active = tags.includes(tag);
            return (
              <button
                key={tag}
                type="button"
                aria-pressed={active}
                className={filterClass(active)}
                onClick={() => {
                  setFilters(
                    query.trim(),
                    active
                      ? tags.filter((other) => other !== tag)
                      : [...tags, tag]
                  );
                }}
              >
                {tag}
              </button>
            );
          })}
        </div>
      </div>
      {content}
    </div>
  );
};

// FilterClass styles a filter of the list, filled when it's on.
const filterClass = (active: boolean): string =>
  `rounded-full px-2.5 py-0.5 text-xs font-medium ring-1 ring-inset ${
    active
      ? "bg-brand-600 text-white ring-brand-600"
      : "bg-white text-gray-700 ring-gray-300 hover:bg-gray-50"
  }`;

interface RecipeCardProps {
  recipe: RecipeSummary;
}

// RecipeCard shows a recipe in the list.
const RecipeCard: React.FC<RecipeCardProps> = ({ recipe }) => {
  const facts: string[] = [];
  if (recipe.energyKcalPerServing !== undefined) {
    facts.push(`${formatAmount(recipe.energyKcalPerServing)} kcal`);
  }
  if (recipe.proteinGramsPerServing !== undefined) {
    facts.push(`${formatAmount(recipe.proteinGramsPerServing)} g protein`);
  }
  if (recipe.carbohydrateGramsPerServing !== undefined) {
    facts.push(`${formatAmount(recipe.carbohydrateGramsPerServing)} g carbs`);
  }
  const totalSeconds = recipe.prepSeconds + recipe.cookSeconds;
  return (
    <li>
      <Link
        to={`/recipes/${recipe.id}`}
        className={`${cardClass} flex h-full flex-col gap-2 p-4 hover:border-gray-300 hover:shadow`}
      >
        <div>
          <h2 className="font-semibold text-gray-900">
            {recipe.name}
            {recipe.favorite && (
              <FilledStarIcon
                className="ml-1 inline size-4 align-text-bottom text-amber-500"
                aria-label="Favorite"
              />
            )}
            {recipe.draft && (
              <span className="ml-2 align-middle">
                <Badge>Draft</Badge>
              </span>
            )}
          </h2>
          {recipe.description !== "" && (
            <p className="line-clamp-2 text-sm text-gray-600">
              {recipe.description}
            </p>
          )}
        </div>
        <p className="flex flex-wrap items-center gap-x-1.5 text-xs text-gray-500">
          <span>
            {recipe.servings} {recipe.servings === 1 ? "serving" : "servings"}
          </span>
          {totalSeconds > 0 && (
            <span className="inline-flex items-center gap-1">
              · <ClockIcon className="size-3.5" aria-hidden="true" />
              {formatHoursMinutes(totalSeconds)}
            </span>
          )}
        </p>
        <p className="text-sm text-gray-800 tabular-nums">
          {facts.length > 0 ? (
            <>
              <span className="text-xs text-gray-500">Per serving: </span>
              {facts.join(" · ")}
            </>
          ) : (
            <span className="text-xs text-gray-500">No nutrition yet</span>
          )}
          {recipe.hasIssues && (
            <ExclamationTriangleIcon
              className="ml-1 inline size-4 text-amber-600"
              aria-label="Some ingredients lack data"
            />
          )}
        </p>
        {recipe.tags.length > 0 && (
          <div className="mt-auto flex flex-wrap gap-1">
            {recipe.tags.map((tag) => (
              <Badge key={tag}>{tag}</Badge>
            ))}
          </div>
        )}
      </Link>
    </li>
  );
};

export default RecipesPage;
