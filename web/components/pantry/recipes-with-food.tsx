import Loading from "@components/loading";
import { tintOf } from "@components/recipes/tint";
import Modal from "@components/ui/modal";
import QueryError from "@components/ui/query-error";
import { linkClass } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { ChevronRightIcon } from "@heroicons/react/24/outline";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { startOfWeek, today } from "helpers/dates";
import React from "react";
import { Link } from "react-router";

interface RecipesWithFoodProps {
  // The lot to use up, before it goes off.
  item: PantryItem;
  onClose: () => void;
}

/**
 * RecipesWithFood lists the recipes using a lot's food, to plan one of them
 * and use it up: choosing a recipe plans it from today, on the Plan page,
 * like a recipe's "Plan this".
 */
const RecipesWithFood: React.FC<RecipesWithFoodProps> = ({ item, onClose }) => {
  const recipes = useQuery(listRecipes, {
    foodIds: [item.foodId],
    summary: true,
  });
  const now = today();
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
      <p className="text-sm text-muted">
        No recipe uses {item.foodName} yet.{" "}
        <Link to="/recipes/new" className={linkClass}>
          Add a recipe
        </Link>{" "}
        with it, or plan it as a food on the Plan page.
      </p>
    );
  } else {
    content = (
      <ul className="-my-1">
        {recipes.data.recipes.map((recipe) => (
          <li key={recipe.id}>
            <Link
              to={`/plan?week=${startOfWeek(now)}&day=${now}&plan=${recipe.id}`}
              className="flex items-center gap-3 border-b border-line py-3 last:border-b-0"
            >
              <span
                className={`size-9 shrink-0 rounded-lg ${tintOf(recipe.id)}`}
                aria-hidden="true"
              />
              <span className="min-w-0 grow truncate text-base font-bold text-ink-soft">
                {recipe.name}
              </span>
              <ChevronRightIcon
                className="size-5 shrink-0 text-muted"
                aria-hidden="true"
              />
            </Link>
          </li>
        ))}
      </ul>
    );
  }
  return (
    <Modal open title={`Recipes with ${item.foodName}`} onClose={onClose}>
      <div className="flex flex-col gap-3">
        <p className="-mt-2 text-sm text-muted">
          Plan one to use it up before it goes off.
        </p>
        {content}
      </div>
    </Modal>
  );
};

export default RecipesWithFood;
