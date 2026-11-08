import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import Loading from "@components/loading";
import FreezerCard from "@components/pantry/freezer-card";
import PantryItemDialog from "@components/pantry/pantry-item-dialog";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { Badge, EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import { iconButton, linkClass, primaryButton } from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  PencilSquareIcon,
  PlusIcon,
  TrashIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { updateFood } from "gen/foods/v1/foods-FoodService_connectquery";
import { FoodService, StoreSection } from "gen/foods/v1/foods_pb";
import {
  listPantry,
  removeFromPantry,
} from "gen/pantry/v1/pantry-PantryService_connectquery";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { daysBetween, today } from "helpers/dates";
import { formatNearDate, formatStock } from "helpers/format";
import { STORE_SECTIONS } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

// SOON_DAYS is how many days left count as soon to expire.
const SOON_DAYS = 3;

/**
 * SECTIONS are the store sections in the order of a typical shop, then the
 * foods without one.
 */
const SECTIONS = [
  ...STORE_SECTIONS,
  { label: "Other foods", value: StoreSection.UNSPECIFIED },
];

interface ExpiryBadgeProps {
  item: PantryItem;
  now: string;
}

// ExpiryBadge says when a lot should be used by, highlighting it if soon.
const ExpiryBadge: React.FC<ExpiryBadgeProps> = ({ item, now }) => {
  if (item.useByDate === "") {
    return null;
  }
  const days = daysBetween(now, item.useByDate);
  const opened = item.openedDate === "" ? "" : "Opened · ";
  if (days < 0) {
    return (
      <Badge className="bg-red-50 text-red-800 ring-red-200">
        {opened}Expired {formatNearDate(item.useByDate)}
      </Badge>
    );
  }
  if (days <= SOON_DAYS) {
    let when = `in ${days} days`;
    if (days === 0) {
      when = "today";
    } else if (days === 1) {
      when = "tomorrow";
    }
    return (
      <Badge className="bg-amber-50 text-amber-800 ring-amber-200">
        {opened}Use {when}
      </Badge>
    );
  }
  return (
    <span className="text-xs text-gray-500">
      {opened}Until {formatNearDate(item.useByDate)}
    </span>
  );
};

/**
 * PantryPage lists what the household has in stock, by store section, with
 * what to use soon highlighted, and the foods it always has.
 */
const PantryPage: React.FC = () => {
  const queryClient = useQueryClient();
  const pantry = useQuery(listPantry, {});
  // The lot being edited, null for a new one, undefined if none.
  const [editing, setEditing] = useState<PantryItem | null | undefined>(
    undefined
  );
  const [stockedFood, setStockedFood] = useState<PickedFood | null>(null);
  const remove = useMutation(removeFromPantry, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PantryService]);
    },
  });
  const setAlwaysInStock = useMutation(updateFood, {
    onSuccess: async () => {
      setStockedFood(null);
      await invalidateServices(queryClient, [PantryService, FoodService]);
    },
  });
  const markAlwaysInStock = (foodId: string, always: boolean) => {
    setAlwaysInStock.mutate({
      food: { alwaysInStock: always, id: foodId },
      updateMask: { paths: ["always_in_stock"] },
    });
  };
  const now = today();

  let content: React.ReactNode = null;
  if (pantry.isPending) {
    content = <Loading />;
  } else if (pantry.isError) {
    content = (
      <QueryError
        error={pantry.error}
        onRetry={() => {
          void pantry.refetch();
        }}
      />
    );
  } else {
    const { items, alwaysInStockFoods } = pantry.data;
    const soon = items.filter(
      (item) =>
        item.useByDate !== "" && daysBetween(now, item.useByDate) <= SOON_DAYS
    ).length;
    content = (
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="flex flex-col gap-4 lg:col-span-2">
          {soon > 0 && (
            <p className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
              {soon === 1
                ? "1 thing to use soon"
                : `${soon} things to use soon`}
              : plan them early in the week.
            </p>
          )}
          {items.length === 0 ? (
            <EmptyState title="The pantry is empty">
              Add what you have, or check items off a{" "}
              <Link to="/shopping" className={linkClass}>
                shopping list
              </Link>{" "}
              and they come here when you&apos;re done.
            </EmptyState>
          ) : (
            SECTIONS.map((section) => {
              const lots = items.filter(
                (item) => item.storeSection === section.value
              );
              if (lots.length === 0) {
                return null;
              }
              return (
                <Card key={section.value} title={section.label}>
                  <ul className="-my-2 divide-y divide-gray-100">
                    {lots.map((item) => (
                      <li
                        key={item.id}
                        className="flex items-center gap-2 py-2 text-sm"
                      >
                        <div className="flex min-w-0 grow flex-col">
                          <span className="font-medium text-gray-900">
                            {item.foodName}
                          </span>
                          <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                            <span className="text-gray-600 tabular-nums">
                              {formatStock(item.grams, item.gramsPerPiece)}
                            </span>
                            <ExpiryBadge item={item} now={now} />
                            {item.notes !== "" && (
                              <span className="text-xs text-gray-500 italic">
                                {item.notes}
                              </span>
                            )}
                          </span>
                        </div>
                        <button
                          type="button"
                          className={iconButton}
                          aria-label={`Edit ${item.foodName}`}
                          title="Edit"
                          onClick={() => {
                            setEditing(item);
                          }}
                        >
                          <PencilSquareIcon className="size-5" />
                        </button>
                        <ConfirmButton
                          className={iconButton}
                          title={`Remove ${item.foodName}?`}
                          message="Remove it from the pantry when it's used up or thrown away."
                          confirmLabel="Remove"
                          disabled={remove.isPending}
                          onConfirm={() => {
                            remove.mutate({ id: item.id });
                          }}
                        >
                          <TrashIcon
                            className="size-5"
                            aria-label={`Remove ${item.foodName}`}
                          />
                        </ConfirmButton>
                      </li>
                    ))}
                  </ul>
                </Card>
              );
            })
          )}
        </div>
        <div className="flex flex-col gap-4">
          <FreezerCard />
          <Card title="Always in stock">
            <div className="flex flex-col gap-3 text-sm">
              <p className="text-gray-600">
                Things you always have, like salt and spices. They aren&apos;t
                weighed here, and the shopping list leaves them out.
              </p>
              {alwaysInStockFoods.length > 0 && (
                <ul className="flex flex-wrap gap-1.5">
                  {alwaysInStockFoods.map((food) => (
                    <li
                      key={food.foodId}
                      className="inline-flex items-center gap-1 rounded-full bg-gray-100 py-0.5 pr-1 pl-2.5 text-gray-800"
                    >
                      {food.foodName}
                      <button
                        type="button"
                        className="rounded-full p-0.5 text-gray-500 hover:bg-gray-200 hover:text-gray-900"
                        aria-label={`Track ${food.foodName} again`}
                        title="Track it again"
                        disabled={setAlwaysInStock.isPending}
                        onClick={() => {
                          markAlwaysInStock(food.foodId, false);
                        }}
                      >
                        <XMarkIcon className="size-3.5" />
                      </button>
                    </li>
                  ))}
                </ul>
              )}
              <FoodPicker
                value={stockedFood}
                label="Food always in stock"
                onChange={(food) => {
                  setStockedFood(food);
                  markAlwaysInStock(food.id, true);
                }}
              />
            </div>
          </Card>
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <PageHeader
        title="Pantry"
        subtitle="What's in stock. Shopping adds to it, and cooking takes from it."
        actions={
          <button
            type="button"
            className={primaryButton}
            onClick={() => {
              setEditing(null);
            }}
          >
            <PlusIcon className="size-4" aria-hidden="true" />
            Add
          </button>
        }
      />
      <ErrorText error={remove.error ?? setAlwaysInStock.error} />
      {content}
      {editing !== undefined && (
        <PantryItemDialog
          item={editing ?? undefined}
          onClose={() => {
            setEditing(undefined);
          }}
        />
      )}
    </div>
  );
};

export default PantryPage;
