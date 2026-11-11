import Loading from "@components/loading";
import ImportRecipeDialog from "@components/recipes/import-recipe-dialog";
import { Badge, EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import {
  cardClass,
  choiceClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  ArrowDownTrayIcon,
  InformationCircleIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  StarIcon,
  XMarkIcon,
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

// MAX_TAG_MATCHES is how many matching tags the search suggests.
const MAX_TAG_MATCHES = 6;

/**
 * SplitTag splits a tag into its category and value, like "cuisine" and
 * "Korean" for "cuisine: Korean", or no category for a tag without one.
 */
const splitTag = (tag: string): [string, string] => {
  const at = tag.indexOf(":");
  if (at <= 0) {
    return ["", tag];
  }
  return [tag.slice(0, at).trim(), tag.slice(at + 1).trim()];
};

// TagText shows a tag, its category muted, like "cuisine: Korean".
const TagText: React.FC<{ tag: string }> = ({ tag }) => {
  const [category, value] = splitTag(tag);
  return (
    <span>
      {category !== "" && <span className="text-muted">{category}: </span>}
      {value}
    </span>
  );
};

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

  /*
   * The tags matching what's typed, to add as filters: by name, or by
   * category and value for tags like "cuisine: Korean".
   */
  const typed = query.trim().toLowerCase();
  const allTags = data?.tags ?? [];
  const matchingTags =
    typed === ""
      ? []
      : allTags
          .filter(
            (tag) =>
              !tags.includes(tag) &&
              tag
                .toLowerCase()
                .replaceAll(/\s*:\s*/gu, ":")
                .includes(typed.replaceAll(/\s*:\s*/gu, ":"))
          )
          .slice(0, MAX_TAG_MATCHES);
  // The categories of the tags, to browse by, else the tags themselves.
  const categories = [
    ...new Set(
      allTags.map((tag) => {
        const [category] = splitTag(tag);
        return category;
      })
    ),
  ].filter((category) => category !== "");

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
  } else if (data.recipes.length === 0 && matchingTags.length > 0) {
    // What's typed is a tag to choose, not a recipe's name.
    content = null;
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
      <div className="flex flex-col gap-2 sm:max-w-xl">
        <label className="flex flex-wrap items-center gap-1.5 rounded-xl border-2 border-pine bg-white px-3 py-2">
          <MagnifyingGlassIcon
            className="size-5 shrink-0 text-pine"
            aria-hidden="true"
          />
          {tags.map((tag) => (
            <button
              key={tag}
              type="button"
              className="inline-flex items-center gap-1 rounded-full bg-pine-50 px-2.5 py-1 text-sm text-pine-dark"
              aria-label={`Remove ${tag}`}
              onClick={() => {
                setFilters(
                  query.trim(),
                  tags.filter((other) => other !== tag)
                );
              }}
            >
              <TagText tag={tag} />
              <XMarkIcon className="size-3.5" aria-hidden="true" />
            </button>
          ))}
          <span className="sr-only">Search recipes and tags</span>
          <input
            type="search"
            className="min-w-24 grow border-0 bg-transparent py-1 text-base text-ink-soft outline-none placeholder:text-gray-500"
            value={query}
            placeholder={tags.length === 0 ? "Search recipes and tags" : ""}
            onChange={(event) => {
              setQuery(event.target.value);
              setFilters(event.target.value.trim(), tags);
            }}
          />
        </label>
        {matchingTags.length > 0 && (
          <ul className="flex flex-col rounded-2xl border border-line bg-white p-1.5">
            {matchingTags.map((tag) => (
              <li key={tag}>
                <button
                  type="button"
                  className="w-full rounded-xl px-3 py-2 text-left text-base text-ink-soft hover:bg-ground-dark/60"
                  onClick={() => {
                    setQuery("");
                    setFilters("", [...tags, tag]);
                  }}
                >
                  <TagText tag={tag} />
                </button>
              </li>
            ))}
          </ul>
        )}
        <div
          className="flex flex-wrap items-center gap-1.5"
          aria-label="Browse by"
        >
          <button
            type="button"
            aria-pressed={favoritesOnly}
            className={`${choiceClass(favoritesOnly)} inline-flex items-center gap-1`}
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
          {categories.map((category) => (
            <button
              key={category}
              type="button"
              className={choiceClass(false)}
              onClick={() => {
                setQuery(`${category}:`);
              }}
            >
              {category}
            </button>
          ))}
        </div>
      </div>
      {content}
    </div>
  );
};

interface RecipeCardProps {
  recipe: RecipeSummary;
}

// RecipeCard shows a recipe in the list: its time and main nutrients.
const RecipeCard: React.FC<RecipeCardProps> = ({ recipe }) => {
  const totalSeconds = recipe.prepSeconds + recipe.cookSeconds;
  const facts: string[] = [];
  if (totalSeconds > 0) {
    facts.push(formatHoursMinutes(totalSeconds));
  }
  if (recipe.energyKcalPerServing !== undefined) {
    facts.push(`${formatAmount(recipe.energyKcalPerServing)} kcal`);
  }
  if (recipe.proteinGramsPerServing !== undefined) {
    facts.push(`${formatAmount(recipe.proteinGramsPerServing)} g protein`);
  }
  return (
    <li>
      <Link
        to={`/recipes/${recipe.id}`}
        className={`${cardClass} flex h-full flex-col gap-1.5 p-4 hover:border-gray-300`}
      >
        <h2 className="flex items-start gap-1.5 text-base font-bold text-ink-soft">
          <span className="grow">{recipe.name}</span>
          {recipe.favorite && (
            <FilledStarIcon
              className="mt-0.5 size-4 shrink-0 text-amber-500"
              aria-label="Favorite"
            />
          )}
        </h2>
        {recipe.draft && (
          <span className="self-start">
            <Badge>Draft</Badge>
          </span>
        )}
        <p className="flex flex-wrap items-center gap-x-1.5 text-sm text-muted tabular-nums">
          {facts.length > 0 ? facts.join(" · ") : "No nutrition yet"}
          {recipe.hasIssues && (
            <InformationCircleIcon
              className="size-4 shrink-0"
              aria-label="Some ingredients lack data"
            />
          )}
        </p>
      </Link>
    </li>
  );
};

export default RecipesPage;
