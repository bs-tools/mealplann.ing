import AddFoodDialog from "@components/foods/add-food-dialog";
import Loading from "@components/loading";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import { primaryButton, secondaryButton } from "@components/ui/styles";
import { useInfiniteQuery } from "@connectrpc/connect-query";
import {
  MagnifyingGlassIcon,
  PlusIcon,
  ChevronRightIcon,
} from "@heroicons/react/24/outline";
import { keepPreviousData } from "@tanstack/react-query";
import { listFoods } from "gen/foods/v1/foods-FoodService_connectquery";
import type { Food } from "gen/foods/v1/foods_pb";
import { useDebouncedValue } from "helpers/hooks";
import { storeSectionLabel } from "helpers/labels";
import { formatNutrient } from "helpers/nutrients";
import React, { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";

// PAGE_SIZE is how many foods are shown at once.
const PAGE_SIZE = 50;

/**
 * FoodsPage lists the household's foods, the ingredients of its recipes,
 * and adds new ones.
 */
const FoodsPage: React.FC = () => {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [query, setQuery] = useState(params.get("q") ?? "");
  const [adding, setAdding] = useState(false);
  const search = useDebouncedValue(query.trim());
  const foods = useInfiniteQuery(
    listFoods,
    { pageSize: PAGE_SIZE, pageToken: "", query: search },
    {
      getNextPageParam: (last) =>
        last.nextPageToken === "" ? undefined : last.nextPageToken,
      pageParamKey: "pageToken",
      placeholderData: keepPreviousData,
    }
  );

  let content: React.ReactNode = null;
  if (foods.isPending) {
    content = <Loading />;
  } else if (foods.isError) {
    content = (
      <QueryError
        error={foods.error}
        onRetry={() => {
          void foods.refetch();
        }}
      />
    );
  } else {
    const list = foods.data.pages.flatMap((page) => page.foods);
    if (list.length === 0) {
      content =
        search === "" ? (
          <EmptyState title="No foods yet">
            Add the ingredients you cook with, from USDA FoodData Central or
            from a product&apos;s label.
          </EmptyState>
        ) : (
          <EmptyState title="No foods found">
            No food&apos;s name contains &quot;{search}&quot;.
          </EmptyState>
        );
    } else {
      content = (
        <div className="rounded-2xl border border-line bg-white px-4">
          <ul className="divide-y divide-line">
            {list.map((food) => (
              <FoodRow key={food.id} food={food} />
            ))}
          </ul>
          {foods.hasNextPage && (
            <div className="flex justify-center border-t border-line py-3">
              <button
                type="button"
                className={secondaryButton}
                disabled={foods.isFetchingNextPage}
                onClick={() => {
                  void foods.fetchNextPage();
                }}
              >
                {foods.isFetchingNextPage ? "Loading…" : "Show more"}
              </button>
            </div>
          )}
        </div>
      );
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        kicker="Your kitchen"
        title="Foods"
        subtitle="The ingredients of your recipes. Nutrients are per 100 g."
        actions={
          <button
            type="button"
            className={primaryButton}
            onClick={() => {
              setAdding(true);
            }}
          >
            <PlusIcon className="size-4" aria-hidden="true" />
            Add food
          </button>
        }
      />
      <label className="flex items-center gap-2 rounded-xl border-2 border-pine bg-white px-3 py-2">
        <MagnifyingGlassIcon
          className="size-5 shrink-0 text-pine"
          aria-hidden="true"
        />
        <span className="sr-only">Search foods</span>
        <input
          type="search"
          className="min-w-0 grow border-0 bg-transparent py-1 text-base text-ink-soft outline-none placeholder:text-muted"
          value={query}
          placeholder="Search foods"
          onChange={(event) => {
            setQuery(event.target.value);
            const next = new URLSearchParams();
            if (event.target.value.trim() !== "") {
              next.set("q", event.target.value.trim());
            }
            setParams(next, { replace: true });
          }}
        />
      </label>
      {content}
      {adding && (
        <AddFoodDialog
          initialQuery={query.trim()}
          onClose={() => {
            setAdding(false);
          }}
          onCreated={(food) => {
            setAdding(false);
            void navigate(`/foods/${food.id}`);
          }}
        />
      )}
    </div>
  );
};

interface FoodRowProps {
  food: Food;
}

// FoodRow shows a food in the list, with its store section and main nutrients.
const FoodRow: React.FC<FoodRowProps> = ({ food }) => {
  const { nutrients } = food;
  const facts: string[] = [];
  if (nutrients?.energyKcal !== undefined) {
    facts.push(formatNutrient(nutrients.energyKcal, "kcal"));
  }
  if (nutrients?.proteinGrams !== undefined) {
    facts.push(`${formatNutrient(nutrients.proteinGrams, "g")} protein`);
  }
  if (nutrients?.carbohydrateGrams !== undefined) {
    facts.push(`${formatNutrient(nutrients.carbohydrateGrams, "g")} carbs`);
  }
  const section = storeSectionLabel(food.storeSection);
  return (
    <li>
      <Link
        to={`/foods/${food.id}`}
        className="flex items-center gap-3 py-3 hover:bg-ground/60"
      >
        <div className="flex min-w-0 grow flex-col gap-0.5">
          <p className="truncate font-bold text-ink-soft">{food.name}</p>
          <p className="truncate text-sm text-muted tabular-nums">
            {[
              section,
              facts.length > 0 ? facts.join(" · ") : "No nutrients yet",
            ]
              .filter((part) => part !== "")
              .join(" · ")}
          </p>
        </div>
        {food.fastCarb && (
          <span
            className="shrink-0 rounded-full bg-warn-tint px-2.5 py-1 text-xs font-bold text-warn-dark"
            title="Quickly absorbed carbohydrate"
          >
            Fast carb
          </span>
        )}
        <ChevronRightIcon
          className="size-5 shrink-0 text-muted/70"
          aria-hidden="true"
        />
      </Link>
    </li>
  );
};

export default FoodsPage;
