import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import Loading from "@components/loading";
import Card from "@components/ui/card";
import { PageHeader } from "@components/ui/misc";
import { ErrorText } from "@components/ui/query-error";
import {
  inputClass,
  labelClass,
  linkClass,
  primaryButton,
} from "@components/ui/styles";
import { skipToken, useMutation, useQuery } from "@connectrpc/connect-query";
import {
  ArrowLeftIcon,
  CheckCircleIcon,
  ExclamationTriangleIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import {
  createRecipe,
  previewImport,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { PreviewImportResponse } from "gen/recipes/v1/recipes_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { AMOUNT_UNITS, optionValue } from "helpers/labels";
import { parseQuantity } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

// AUTO_PICK_SCORE is how alike a food must be to be picked for the user.
const AUTO_PICK_SCORE = 0.75;

// The food choices besides the matches: another food, or none.
const OTHER = "other";
const NONE = "";

// Row is an imported ingredient as the user reviews it.
interface Row {
  originalText: string;
  // The food chosen: a match's ID, OTHER, or NONE to leave it unmatched.
  choice: string;
  // The food picked with OTHER.
  other: PickedFood | null;
  amount: string;
  unit: AmountUnit;
}

interface ReviewProps {
  preview: PreviewImportResponse;
}

/**
 * Review shows an imported recipe's ingredients, each with the household's
 * foods most like it to choose from, any other food, or none, and creates
 * the recipe: a draft if some are left unmatched.
 */
const Review: React.FC<ReviewProps> = ({ preview }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { recipe } = preview;
  const [name, setName] = useState(recipe?.name ?? "");
  const [servings, setServings] = useState(String(recipe?.servings ?? 1));
  const [rows, setRows] = useState<Row[]>(() =>
    (recipe?.ingredients ?? []).map((ingredient, index) => {
      const best = preview.ingredients[index]?.matches[0];
      return {
        amount: ingredient.amount > 0 ? String(ingredient.amount) : "",
        choice:
          best !== undefined && best.score >= AUTO_PICK_SCORE
            ? best.foodId
            : NONE,
        originalText: ingredient.originalText,
        other: null,
        unit:
          ingredient.unit === AmountUnit.UNSPECIFIED
            ? AmountUnit.GRAMS
            : ingredient.unit,
      };
    })
  );
  /*
   * The ingredients matched to a food on their own: they're folded into a
   * line, to review only if wanted, so the list shows what needs a choice.
   */
  const [autoMatched] = useState(
    () =>
      new Set(
        rows.flatMap((row, index) => (row.choice === NONE ? [] : [index]))
      )
  );
  const [reviewing, setReviewing] = useState(false);
  const [invalid, setInvalid] = useState("");
  const create = useMutation(createRecipe, {
    onSuccess: async (response) => {
      await invalidateServices(queryClient, [RecipeService]);
      const { recipe: created } = response;
      void navigate(
        created?.draft === true
          ? `/recipes/${created.id}/edit`
          : `/recipes/${created?.id ?? ""}`
      );
    },
  });
  const update = (index: number, change: Partial<Row>) => {
    setRows(
      rows.map((row, other) => (other === index ? { ...row, ...change } : row))
    );
  };
  // FoodOf returns the food chosen for a row, "" for none.
  const foodOf = (row: Row): string =>
    row.choice === OTHER ? (row.other?.id ?? "") : row.choice;
  const matched = rows.filter((row) => foodOf(row) !== "").length;

  const save = () => {
    setInvalid("");
    const count = Number(servings);
    if (name.trim() === "" || !Number.isInteger(count) || count < 1) {
      setInvalid("Give the recipe a name and a whole number of servings.");
      return;
    }
    const ingredients = [];
    for (const row of rows) {
      const amount = parseQuantity(row.amount);
      if (amount === null) {
        setInvalid(
          `"${row.amount}" isn't an amount: give a number, like 1.5 or 1 1/2.`
        );
        return;
      }
      const foodId = foodOf(row);
      // A food needs an amount; without one, the ingredient stays as written.
      if (foodId !== "" && amount !== undefined && amount > 0) {
        ingredients.push({
          amount,
          foodId,
          originalText: row.originalText,
          unit: row.unit,
        });
      } else {
        ingredients.push({
          amount: amount ?? 0,
          originalText: row.originalText,
          unit: amount === undefined ? AmountUnit.UNSPECIFIED : row.unit,
        });
      }
    }
    const draft = ingredients.some((ingredient) => !("foodId" in ingredient));
    create.mutate({
      recipe: {
        cookSeconds: recipe?.cookSeconds ?? 0,
        description: recipe?.description ?? "",
        draft,
        ingredients,
        instructions: recipe?.instructions ?? [],
        name: name.trim(),
        prepSeconds: recipe?.prepSeconds ?? 0,
        servings: count,
        sourceUrl: recipe?.sourceUrl ?? "",
        tags: recipe?.tags ?? [],
      },
    });
  };

  return (
    <div className="flex flex-col gap-4">
      <Card title="Recipe">
        <div className="flex flex-wrap gap-3">
          <label className="flex grow flex-col gap-1">
            <span className={labelClass}>Name</span>
            <input
              className={inputClass}
              value={name}
              onChange={(event) => {
                setName(event.target.value);
              }}
            />
          </label>
          <label className="flex w-28 flex-col gap-1">
            <span className={labelClass}>Servings</span>
            <input
              className={inputClass}
              inputMode="numeric"
              value={servings}
              onChange={(event) => {
                setServings(event.target.value);
              }}
            />
          </label>
        </div>
        {recipe !== undefined && recipe.sourceUrl !== "" && (
          <p className="mt-2 text-xs text-gray-500">
            From{" "}
            <a
              href={recipe.sourceUrl}
              className={linkClass}
              target="_blank"
              rel="noreferrer"
            >
              {recipe.sourceUrl}
            </a>
            {recipe.instructions.length > 0 &&
              `, with ${recipe.instructions.length} ${recipe.instructions.length === 1 ? "step" : "steps"}`}
          </p>
        )}
      </Card>
      {autoMatched.size > 0 && (
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-2xl bg-pine-50 px-4 py-3 text-sm text-pine-dark">
          <CheckCircleIcon className="size-5 shrink-0" aria-hidden="true" />
          <span className="grow font-medium">
            {autoMatched.size}{" "}
            {autoMatched.size === 1 ? "ingredient" : "ingredients"} matched your
            foods
          </span>
          <button
            type="button"
            className={linkClass}
            onClick={() => {
              setReviewing(!reviewing);
            }}
          >
            {reviewing ? "Hide them" : "Review them"}
          </button>
        </div>
      )}
      <Card
        title={
          autoMatched.size === rows.length && !reviewing
            ? "Ingredients"
            : `Choose the food for ${rows.length - autoMatched.size === 0 ? "each" : rows.length - autoMatched.size} more`
        }
      >
        {autoMatched.size === rows.length && !reviewing ? (
          <p className="text-sm text-muted">
            Every ingredient has a food: save the recipe.
          </p>
        ) : null}
        <ul className="-my-2 divide-y divide-gray-100">
          {rows.map((row, index) => {
            if (autoMatched.has(index) && !reviewing) {
              return null;
            }
            const matches = preview.ingredients[index]?.matches ?? [];
            const foodId = foodOf(row);
            return (
              <li
                // The rows are the recipe's, in order, and never move.
                // oxlint-disable-next-line react/no-array-index-key
                key={index}
                className="flex flex-col gap-2 py-3 sm:flex-row sm:items-start"
              >
                <div className="flex min-w-0 grow flex-col gap-1">
                  <span className="flex items-start gap-1.5 text-sm font-medium text-gray-900">
                    {foodId === "" ? (
                      <ExclamationTriangleIcon
                        className="mt-0.5 size-4 shrink-0 text-amber-600"
                        aria-label="Unmatched"
                      />
                    ) : (
                      <CheckCircleIcon
                        className="mt-0.5 size-4 shrink-0 text-brand-600"
                        aria-label="Matched"
                      />
                    )}
                    {row.originalText}
                  </span>
                  <select
                    className={`${inputClass} sm:max-w-sm`}
                    aria-label={`Food of ${row.originalText}`}
                    value={row.choice}
                    onChange={(event) => {
                      update(index, { choice: event.target.value });
                    }}
                  >
                    {matches.map((match, rank) => (
                      <option key={match.foodId} value={match.foodId}>
                        {match.foodName}
                        {rank === 0 ? " (closest)" : ""}
                      </option>
                    ))}
                    <option value={OTHER}>Another food…</option>
                    <option value={NONE}>
                      {matches.length === 0
                        ? "No food is like it: leave it unmatched"
                        : "Leave it unmatched"}
                    </option>
                  </select>
                  {row.choice === OTHER && (
                    <div className="sm:max-w-sm">
                      <FoodPicker
                        value={row.other}
                        label={`Another food for ${row.originalText}`}
                        onChange={(food) => {
                          update(index, { other: food });
                        }}
                      />
                    </div>
                  )}
                </div>
                <div className="flex shrink-0 gap-2">
                  <input
                    className={`${inputClass} w-20`}
                    aria-label={`Amount of ${row.originalText}`}
                    value={row.amount}
                    placeholder="Amount"
                    onChange={(event) => {
                      update(index, { amount: event.target.value });
                    }}
                  />
                  <select
                    className={`${inputClass} w-28`}
                    aria-label={`Unit of ${row.originalText}`}
                    value={row.unit}
                    onChange={(event) => {
                      update(index, {
                        unit: optionValue(
                          AMOUNT_UNITS,
                          event.target.value,
                          AmountUnit.GRAMS
                        ),
                      });
                    }}
                  >
                    {AMOUNT_UNITS.map((option) => (
                      <option key={option.value} value={option.value}>
                        {option.label}
                      </option>
                    ))}
                  </select>
                </div>
              </li>
            );
          })}
        </ul>
      </Card>
      <div className="flex flex-wrap items-center justify-end gap-3">
        <p className="text-sm text-gray-600">
          {matched === rows.length
            ? "Every ingredient has a food: it's saved as a recipe."
            : "It's saved as a draft, to match the rest in the editor."}
        </p>
        <button
          type="button"
          className={primaryButton}
          disabled={create.isPending}
          onClick={save}
        >
          {create.isPending ? "Saving…" : "Save recipe"}
        </button>
      </div>
      {invalid !== "" && <p className="text-sm text-red-700">{invalid}</p>}
      <ErrorText error={create.error} />
    </div>
  );
};

/**
 * RecipeImportPage imports a recipe from a web page: it reads it, offers
 * the household's foods most like each ingredient to review, and saves it.
 */
const RecipeImportPage: React.FC = () => {
  const [params, setParams] = useSearchParams();
  const submitted = params.get("url") ?? "";
  const [url, setUrl] = useState(submitted);
  const preview = useQuery(
    previewImport,
    submitted === "" ? skipToken : { url: submitted },
    { refetchOnWindowFocus: false, retry: false, staleTime: Infinity }
  );
  return (
    <div className="flex flex-col gap-4">
      <div>
        <Link
          to="/recipes"
          className="inline-flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900"
        >
          <ArrowLeftIcon className="size-4" aria-hidden="true" />
          Recipes
        </Link>
      </div>
      <PageHeader
        title="Import a recipe"
        subtitle="Read a recipe from a web page, like a food blog's, and choose the food of each ingredient."
      />
      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          setParams({ url: url.trim() });
        }}
      >
        <label className="flex min-w-64 grow flex-col gap-1">
          <span className={labelClass}>Address of the recipe&apos;s page</span>
          <input
            type="url"
            className={inputClass}
            value={url}
            placeholder="https://"
            required
            onChange={(event) => {
              setUrl(event.target.value);
            }}
          />
        </label>
        <button
          type="submit"
          className={primaryButton}
          disabled={url.trim() === "" || preview.isFetching}
        >
          {preview.isFetching ? "Reading…" : "Read recipe"}
        </button>
      </form>
      {preview.isFetching && <Loading />}
      <ErrorText error={preview.error} />
      {preview.data !== undefined && !preview.isFetching && (
        <Review key={submitted} preview={preview.data} />
      )}
    </div>
  );
};

export default RecipeImportPage;
