import AddFoodDialog from "@components/foods/add-food-dialog";
import Loading from "@components/loading";
import Card from "@components/ui/card";
import { Badge, EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import {
  inputClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useInfiniteQuery } from "@connectrpc/connect-query";
import {
  BoltIcon,
  MagnifyingGlassIcon,
  PlusIcon,
} from "@heroicons/react/24/outline";
import { keepPreviousData } from "@tanstack/react-query";
import { listFoods } from "gen/foods/v1/foods-FoodService_connectquery";
import type { Food } from "gen/foods/v1/foods_pb";
import { useDebouncedValue } from "helpers/hooks";
import { foodSourceInfo, storeSectionLabel } from "helpers/labels";
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
        <Card>
          <ul className="-my-2 divide-y divide-gray-100">
            {list.map((food) => (
              <FoodRow key={food.id} food={food} />
            ))}
          </ul>
          {foods.hasNextPage && (
            <div className="mt-3 flex justify-center">
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
        </Card>
      );
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Foods"
        subtitle="The ingredients of your recipes, with their nutrients per 100 g."
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
      <label className="relative block sm:max-w-sm">
        <span className="sr-only">Search foods</span>
        <MagnifyingGlassIcon className="pointer-events-none absolute top-2 left-2.5 size-4 text-gray-400" />
        <input
          type="search"
          className={`${inputClass} pl-8`}
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

// FoodRow shows a food in the list, with its main nutrients.
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
  const source = foodSourceInfo(food.source, food.sourceId);
  return (
    <li>
      <Link
        to={`/foods/${food.id}`}
        className="-mx-2 flex flex-wrap items-center justify-between gap-x-4 gap-y-1 rounded px-2 py-2 hover:bg-gray-50"
      >
        <div className="min-w-0">
          <p className="font-medium text-gray-900">{food.name}</p>
          <p className="text-xs text-gray-500 tabular-nums">
            {facts.length > 0
              ? `Per 100 g: ${facts.join(" · ")}`
              : "No nutrients yet"}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          {food.fastCarb && (
            <Badge
              className="bg-amber-50 text-amber-800 ring-amber-200"
              title="Quickly absorbed carbohydrate"
            >
              <BoltIcon className="size-3" aria-hidden="true" />
              Fast carb
            </Badge>
          )}
          {section !== "" && <Badge>{section}</Badge>}
          {source === undefined ? (
            <Badge>Custom</Badge>
          ) : (
            <Badge className={source.badgeClass} title={source.name}>
              {source.badge}
            </Badge>
          )}
        </div>
      </Link>
    </li>
  );
};

export default FoodsPage;
