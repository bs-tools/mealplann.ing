import Loading from "@components/loading";
import BackLink from "@components/ui/back-link";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import { linkClass, primaryButton } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { suggestFromPantry } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import { today } from "helpers/dates";
import { formatHoursMinutes, formatList } from "helpers/format";
import { formatServings } from "helpers/labels";
import { MAIN_MEALS } from "helpers/suggestions";
import React from "react";
import { Link, useSearchParams } from "react-router";

type Show = "meals" | "all";

/**
 * PantryCookPage lists the recipes the pantry has everything for, so
 * they're cooked without shopping, those using food to use soon first,
 * each to add to the plan, at whichever meal and day: lunches and
 * dinners, or everything, sides and snacks too.
 */
const PantryCookPage: React.FC = () => {
  const [params, setParams] = useSearchParams();
  const show: Show = params.get("show") === "all" ? "all" : "meals";
  const recipes = useQuery(suggestFromPantry, {
    date: today(),
    meals: show === "meals" ? MAIN_MEALS : [],
  });
  let content: React.ReactNode = null;
  if (recipes.isPending) {
    content = <Loading />;
  } else if (recipes.isError) {
    content = (
      <QueryError
        error={recipes.error}
        onRetry={() => {
          void recipes.refetch();
        }}
      />
    );
  } else if (recipes.data.recipes.length === 0) {
    content = (
      <EmptyState title="Nothing to cook with only the pantry">
        {show === "meals"
          ? "Every lunch and dinner needs something from the shop, or isn't tagged lunch or dinner. "
          : "Every recipe needs something from the shop. "}
        See the{" "}
        <Link to="/recipes" className={linkClass}>
          recipes
        </Link>
        .
      </EmptyState>
    );
  } else {
    content = (
      <ul className="flex flex-col gap-3">
        {recipes.data.recipes.map((recipe) => (
          <li
            key={recipe.recipeId}
            className="flex flex-col gap-2 rounded-2xl border border-line bg-white p-4"
          >
            <div className="flex items-baseline justify-between gap-3">
              <Link
                to={`/recipes/${recipe.recipeId}`}
                className="text-lg font-bold text-ink-soft hover:underline"
              >
                {recipe.recipeName}
              </Link>
              {recipe.totalSeconds > 0 && (
                <span className="shrink-0 text-sm text-muted tabular-nums">
                  {formatHoursMinutes(recipe.totalSeconds)}
                </span>
              )}
            </div>
            <p className="text-sm text-muted">
              {recipe.useSoonFoodNames.length > 0
                ? `Uses the ${formatList(recipe.useSoonFoodNames.map((name) => name.toLowerCase()))} before ${recipe.useSoonFoodNames.length === 1 ? "it goes" : "they go"}. `
                : ""}
              Makes {formatServings(recipe.servings)}.
            </p>
            <Link
              to={`/plan?plan=${recipe.recipeId}`}
              className={`${primaryButton} self-start`}
            >
              Add to plan
            </Link>
          </li>
        ))}
      </ul>
    );
  }
  return (
    <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
      <BackLink to="/pantry">Pantry</BackLink>
      <PageHeader title="You can cook these now" />
      <Segmented
        label="Which recipes"
        value={show}
        options={[
          { label: "Lunches and dinners", value: "meals" },
          { label: "Everything", value: "all" },
        ]}
        onChange={(next) => {
          setParams(next === "all" ? { show: "all" } : {}, { replace: true });
        }}
      />
      {content}
    </div>
  );
};

export default PantryCookPage;
