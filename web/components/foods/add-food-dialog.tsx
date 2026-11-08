import Loading from "@components/loading";
import { TextField } from "@components/ui/fields";
import { Badge, EmptyState } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  inputClass,
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { skipToken, useMutation, useQuery } from "@connectrpc/connect-query";
import { MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import {
  createFood,
  searchFoodData,
} from "gen/foods/v1/foods-FoodService_connectquery";
import type { Food, FoodData } from "gen/foods/v1/foods_pb";
import { FoodService } from "gen/foods/v1/foods_pb";
import { useDebouncedValue } from "helpers/hooks";
import { foodSourceInfo } from "helpers/labels";
import { formatNutrient } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

type Tab = "search" | "custom";

// FoodDataKey identifies a reference food among both sources' foods.
const foodDataKey = (data: FoodData): string =>
  `${data.source}-${data.sourceId}`;

// TabClass styles a tab of the dialog.
const tabClass = (active: boolean): string =>
  `rounded-md px-3 py-1.5 text-sm font-medium ${
    active ? "bg-ink text-white" : "text-gray-600 hover:bg-gray-100"
  }`;

interface AddFoodDialogProps {
  // The text to search for first, e.g. what was typed in a food picker.
  initialQuery?: string | undefined;
  onClose: () => void;
  // Called with the created food, after which the dialog is closed.
  onCreated: (food: Food) => void;
}

/**
 * AddFoodDialog adds a food to the household: from a reference food of
 * Health Canada's Canadian Nutrient File or USDA FoodData Central, with its
 * nutrients, or as a custom food whose nutrients are entered from its label
 * afterwards.
 */
const AddFoodDialog: React.FC<AddFoodDialogProps> = ({
  initialQuery = "",
  onClose,
  onCreated,
}) => {
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<Tab>("search");
  const [query, setQuery] = useState(initialQuery);
  const [customName, setCustomName] = useState(initialQuery);
  // The reference food picked to add, and the name to add it as.
  const [picked, setPicked] = useState<FoodData | null>(null);
  const [name, setName] = useState("");
  const search = useDebouncedValue(query.trim());
  const results = useQuery(
    searchFoodData,
    search === "" ? skipToken : { pageSize: 20, query: search },
    { placeholderData: keepPreviousData }
  );
  const create = useMutation(createFood, {
    onSuccess: async (response) => {
      await invalidateServices(queryClient, [FoodService]);
      if (response.food !== undefined) {
        onCreated(response.food);
      }
    },
  });

  let list: React.ReactNode = null;
  if (search === "") {
    list = (
      <p className="text-sm text-gray-500">
        Search for an ingredient, e.g. &quot;chickpeas cooked&quot; or
        &quot;spinach raw&quot;.
      </p>
    );
  } else if (results.isPending) {
    list = <Loading />;
  } else if (results.isError) {
    list = (
      <QueryError
        error={results.error}
        onRetry={() => {
          void results.refetch();
        }}
      />
    );
  } else if (results.data.foods.length === 0) {
    list = (
      <EmptyState title="No foods found">
        Try other words, or add a custom food.
      </EmptyState>
    );
  } else {
    list = (
      <ul className="flex max-h-96 flex-col divide-y divide-gray-100 overflow-y-auto">
        {results.data.foods.map((data) => (
          <FoodDataRow
            key={foodDataKey(data)}
            data={data}
            picked={
              picked !== null && foodDataKey(picked) === foodDataKey(data)
            }
            onPick={() => {
              setPicked(data);
              setName(data.description);
              create.reset();
            }}
            onClose={onClose}
          />
        ))}
      </ul>
    );
  }

  return (
    <Modal open onClose={onClose} title="Add a food" wide>
      <div className="flex flex-col gap-4">
        <div className="flex gap-1" role="tablist">
          <button
            type="button"
            role="tab"
            aria-selected={tab === "search"}
            className={tabClass(tab === "search")}
            onClick={() => {
              setTab("search");
            }}
          >
            Nutrient databases
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "custom"}
            className={tabClass(tab === "custom")}
            onClick={() => {
              setTab("custom");
            }}
          >
            Custom
          </button>
        </div>
        {tab === "search" ? (
          <>
            <label className="relative block">
              <span className="sr-only">
                Search the Canadian Nutrient File and USDA FoodData Central
              </span>
              <MagnifyingGlassIcon className="pointer-events-none absolute top-2 left-2.5 size-4 text-gray-400" />
              <input
                className={`${inputClass} pl-8`}
                value={query}
                placeholder="Search Canadian and USDA foods"
                autoFocus
                onChange={(event) => {
                  setQuery(event.target.value);
                }}
              />
            </label>
            {picked !== null && (
              <form
                className="flex flex-col gap-2 rounded-md border border-brand-200 bg-brand-50 p-3"
                onSubmit={(event) => {
                  event.preventDefault();
                  /*
                   * Submitting mustn't reach a form the dialog is opened
                   * from, like a recipe's, as React events bubble through
                   * portals.
                   */
                  event.stopPropagation();
                  create.mutate({
                    food: { name: name.trim() },
                    source: picked.source,
                    sourceId: picked.sourceId,
                  });
                }}
              >
                <TextField
                  label="Name in your household"
                  value={name}
                  hint="A short name reads best in recipes and the shopping list, e.g. “Chickpeas, cooked”."
                  autoFocus
                  onChange={setName}
                />
                <div className="flex flex-wrap gap-2">
                  <button
                    type="submit"
                    className={primaryButton}
                    disabled={name.trim() === "" || create.isPending}
                  >
                    {create.isPending ? "Adding…" : "Add food"}
                  </button>
                  <button
                    type="button"
                    className={ghostButton}
                    onClick={() => {
                      setPicked(null);
                    }}
                  >
                    Cancel
                  </button>
                </div>
                <ErrorText error={create.error} />
              </form>
            )}
            {list}
          </>
        ) : (
          <form
            className="flex flex-col gap-3"
            onSubmit={(event) => {
              event.preventDefault();
              event.stopPropagation();
              create.mutate({ food: { name: customName.trim() } });
            }}
          >
            <p className="text-sm text-gray-600">
              For products the nutrient databases don&apos;t have, like a brand
              of tofu or plant milk. You&apos;ll enter its nutrients per 100 g
              from its label next.
            </p>
            <TextField
              label="Name"
              value={customName}
              placeholder="e.g. Smoked tofu (brand)"
              autoFocus
              onChange={setCustomName}
            />
            <div>
              <button
                type="submit"
                className={primaryButton}
                disabled={customName.trim() === "" || create.isPending}
              >
                {create.isPending ? "Adding…" : "Add food"}
              </button>
            </div>
            <ErrorText error={create.error} />
          </form>
        )}
        <div className="flex justify-end">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </Modal>
  );
};

interface FoodDataRowProps {
  data: FoodData;
  // Whether it's the food picked to add.
  picked: boolean;
  onPick: () => void;
  onClose: () => void;
}

// FoodDataRow shows a reference food found, to add, with its source.
const FoodDataRow: React.FC<FoodDataRowProps> = ({
  data,
  picked,
  onPick,
  onClose,
}) => {
  const { nutrients } = data;
  const source = foodSourceInfo(data.source, data.sourceId);
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
  return (
    <li
      className={`flex items-start justify-between gap-3 py-2 ${picked ? "bg-brand-50" : ""}`}
    >
      <div className="min-w-0">
        <p className="text-sm font-medium text-gray-900">{data.description}</p>
        <p className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs text-gray-500">
          {source !== undefined && (
            <Badge className={source.badgeClass} title={source.name}>
              {source.badge}
            </Badge>
          )}
          <span>
            {data.category}
            {data.dataType === "foundation_food" ? " · Foundation" : ""}
          </span>
        </p>
        {facts.length > 0 && (
          <p className="text-xs text-gray-600 tabular-nums">
            Per 100 g: {facts.join(" · ")}
          </p>
        )}
      </div>
      {data.foodId === "" ? (
        <button
          type="button"
          className={`${secondaryButton} shrink-0`}
          onClick={onPick}
        >
          Add
        </button>
      ) : (
        <Link
          to={`/foods/${data.foodId}`}
          className={`${linkClass} shrink-0 text-sm`}
          onClick={onClose}
        >
          Added
        </Link>
      )}
    </li>
  );
};

export default AddFoodDialog;
