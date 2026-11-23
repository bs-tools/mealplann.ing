import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import Loading from "@components/loading";
import type { Leftover, LeftoverStep } from "@components/pantry/leftovers";
import {
  LeftoverRow,
  LeftoverSheet,
  matchesLeftover,
  useLeftovers,
} from "@components/pantry/leftovers";
import PantryItemDialog from "@components/pantry/pantry-item-dialog";
import RecipesWithFood from "@components/pantry/recipes-with-food";
import { tintOf } from "@components/recipes/tint";
import Card from "@components/ui/card";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  linkClass,
  pillButton,
  pillPrimaryButton,
  primaryButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  ChevronRightIcon,
  FireIcon,
  MagnifyingGlassIcon,
  PlusIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { updateFood } from "gen/foods/v1/foods-FoodService_connectquery";
import { FoodService } from "gen/foods/v1/foods_pb";
import {
  listPantry,
  updatePantryItem,
} from "gen/pantry/v1/pantry-PantryService_connectquery";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { PantryService, Storage } from "gen/pantry/v1/pantry_pb";
import { suggestFromPantry } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { daysBetween, parseDate, today } from "helpers/dates";
import { formatGrams, formatNearDate, formatStock } from "helpers/format";
import { invalidateServices } from "helpers/query";
import { MAIN_MEALS } from "helpers/suggestions";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

// SOON_DAYS is how many days left count as soon to expire.
const SOON_DAYS = 3;

// Place is a tab of the pantry: where food is kept.
type Place = "fridge" | "freezer" | "cupboard";

const PLACES: { value: Place; label: string }[] = [
  { label: "Fridge", value: "fridge" },
  { label: "Freezer", value: "freezer" },
  { label: "Cupboard", value: "cupboard" },
];

/**
 * MatchesSearch reports whether a lot matches a search: whether each of
 * its words is in its food's name, label or notes, ignoring case.
 */
const matchesSearch = (item: PantryItem, search: string): boolean => {
  const text = [item.foodName, item.label, item.notes].join(" ").toLowerCase();
  return search
    .toLowerCase()
    .split(/\s+/u)
    .every((word) => text.includes(word));
};

// PlaceOf returns the tab of a lot: the cupboard if it isn't said.
const placeOf = (item: PantryItem): Place => {
  switch (item.storage) {
    case Storage.FRIDGE: {
      return "fridge";
    }
    case Storage.FREEZER: {
      return "freezer";
    }
    default: {
      return "cupboard";
    }
  }
};

// UseByText says when a lot is to be used by, e.g. "Use by Friday".
const useByText = (item: PantryItem, now: string): string => {
  if (item.useByDate === "") {
    return item.opened ? "Opened" : "";
  }
  const days = daysBetween(now, item.useByDate);
  if (days < 0) {
    return `Expired ${formatNearDate(item.useByDate)}`;
  }
  if (days === 0) {
    return "Use today";
  }
  if (days === 1) {
    return "Use tomorrow";
  }
  if (days < 7) {
    return `Use by ${parseDate(item.useByDate).toLocaleDateString(undefined, { weekday: "long" })}`;
  }
  return `Use by ${formatNearDate(item.useByDate)}`;
};

interface LotRowProps {
  item: PantryItem;
  now: string;
  onOpen: () => void;
  /*
   * Called to plan a recipe using the lot, or to freeze it, offered for a
   * lot to use soon, so it's used before it goes off, but not once it has.
   */
  onPlan?: (() => void) | undefined;
  onFreeze?: (() => void) | undefined;
  // Whether it's being frozen.
  freezing?: boolean | undefined;
}

/**
 * LotRow shows a lot: its food, when to use it by, and how much, in the
 * units it's bought in. Tapping it opens it, to correct it.
 */
const LotRow: React.FC<LotRowProps> = ({
  item,
  now,
  onOpen,
  onPlan,
  onFreeze,
  freezing = false,
}) => {
  const days =
    item.useByDate === "" ? Infinity : daysBetween(now, item.useByDate);
  const when = useByText(item, now);
  // Food past its date is for throwing out, not planning or freezing.
  const usable = days >= 0;
  return (
    <li className="border-b border-line">
      <button
        type="button"
        className="flex w-full items-center gap-3 py-3.5 text-left"
        onClick={onOpen}
      >
        <span
          className={`size-11 shrink-0 rounded-xl ${tintOf(item.foodId)}`}
          aria-hidden="true"
        />
        <span className="flex min-w-0 grow flex-col">
          <span className="text-base font-bold text-ink-soft">
            {item.foodName}
            {item.label !== "" && (
              <span className="font-normal text-muted"> · {item.label}</span>
            )}
          </span>
          {when !== "" && (
            <span
              className={`text-sm ${days <= SOON_DAYS ? "font-bold text-warn-dark" : "text-muted"}`}
            >
              {when}
              {item.notes !== "" && ` · ${item.notes}`}
            </span>
          )}
        </span>
        <span
          className="shrink-0 text-base font-bold text-ink-soft tabular-nums"
          title={
            item.estimated
              ? "The amount is a guess: check it"
              : formatGrams(item.grams)
          }
        >
          {item.estimated ? "≈ " : ""}
          {formatStock(item.grams, item.gramsPerPiece, item.packageGrams)}
        </span>
      </button>
      {usable && (onPlan !== undefined || onFreeze !== undefined) && (
        <div className="-mt-1 flex flex-wrap gap-2 pb-3.5 pl-14">
          {onPlan !== undefined && (
            <button
              type="button"
              className={pillPrimaryButton}
              onClick={onPlan}
            >
              Plan a recipe
            </button>
          )}
          {onFreeze !== undefined && (
            <button
              type="button"
              className={pillButton}
              disabled={freezing}
              onClick={onFreeze}
            >
              {freezing ? "Freezing…" : "Freeze"}
            </button>
          )}
        </div>
      )}
    </li>
  );
};

/**
 * PantryPage lists what the household has, by where it's kept: what to use
 * soon first, with how much and until when. Shopping adds to it and eating
 * takes from it; a lot is opened to correct it, and Add adds one by hand.
 */
const PantryPage: React.FC = () => {
  const queryClient = useQueryClient();
  const [params, setParams] = useSearchParams();
  const now = today();
  const place =
    PLACES.find((option) => option.value === params.get("place"))?.value ??
    "fridge";
  const pantry = useQuery(listPantry, {});
  // Only lunches and dinners count, not toast or a pot of beans.
  const cookable = useQuery(suggestFromPantry, {
    date: now,
    meals: MAIN_MEALS,
  });
  // The lot being corrected, null to add one, undefined if none.
  const [editing, setEditing] = useState<PantryItem | null | undefined>(
    undefined
  );
  const [stockedFood, setStockedFood] = useState<PickedFood | null>(null);
  /*
   * The search the lots are filtered by, "" for none. A search looks in
   * every place, so a food is found wherever it's kept.
   */
  const [search, setSearch] = useState(params.get("q") ?? "");
  const leftovers = useLeftovers();
  const [openLeftover, setOpenLeftover] = useState<{
    leftover: Leftover;
    step?: LeftoverStep | undefined;
  } | null>(null);
  // The lot to plan a recipe with, to use it up, or null.
  const [planningLot, setPlanningLot] = useState<PantryItem | null>(null);
  /*
   * Freeze moves a lot to the freezer, frozen today, so it keeps for the
   * food's freezer shelf life instead of going off.
   */
  const freeze = useMutation(updatePantryItem, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PantryService, SuggestionService]);
    },
  });
  /*
   * The lot just frozen whose use-by date didn't change, as its food has no
   * freezer shelf life to count from, or null: the page says to set one.
   */
  const [frozenAsIs, setFrozenAsIs] = useState<PantryItem | null>(null);
  const freezeLot = (item: PantryItem) => {
    setFrozenAsIs(null);
    freeze.mutate(
      {
        item: { id: item.id, storage: Storage.FREEZER },
        updateMask: { paths: ["storage"] },
      },
      {
        onSuccess: (resp) => {
          if (
            resp.item !== undefined &&
            resp.item.useByDate !== "" &&
            resp.item.useByDate === item.useByDate
          ) {
            setFrozenAsIs(resp.item);
          }
        },
      }
    );
  };
  const searching = search.trim() !== "";
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
    const { items: allItems, alwaysInStockFoods } = pantry.data;
    const here = allItems.filter((item) => placeOf(item) === place);
    const isSoon = (item: PantryItem): boolean =>
      item.useByDate !== "" && daysBetween(now, item.useByDate) <= SOON_DAYS;
    const soon = here.filter(isSoon);
    // Soonest first, so what to use first is on top.
    const ordered: PantryItem[] = [];
    for (const item of soon) {
      const at = ordered.findIndex((other) => other.useByDate > item.useByDate);
      ordered.splice(at < 0 ? ordered.length : at, 0, item);
    }
    const rest = here.filter((item) => !isSoon(item));
    const leftoversIn = (where: Place): Leftover[] => {
      if (where === "fridge") {
        return leftovers.fridge;
      }
      return where === "freezer" ? leftovers.frozen : [];
    };
    const leftoverRow = (leftover: Leftover): React.ReactNode => (
      <LeftoverRow
        key={leftover.key}
        leftover={leftover}
        onOpen={(step) => {
          setOpenLeftover({ leftover, step });
        }}
      />
    );
    const leftoversHere = leftoversIn(place);
    const lotRow = (item: PantryItem): React.ReactNode => (
      <LotRow
        key={item.id}
        item={item}
        now={now}
        onOpen={() => {
          setEditing(item);
        }}
      />
    );
    let lots: React.ReactNode = null;
    if (searching) {
      const found = allItems.filter((item) =>
        matchesSearch(item, search.trim())
      );
      const foundLeftovers = [...leftovers.fridge, ...leftovers.frozen].filter(
        (leftover) => matchesLeftover(leftover, search.trim())
      );
      lots =
        found.length === 0 && foundLeftovers.length === 0 ? (
          <p className="text-sm text-muted">
            Nothing in the pantry matches &ldquo;{search.trim()}&rdquo;.
          </p>
        ) : (
          PLACES.map((option) => {
            const there = found.filter(
              (item) => placeOf(item) === option.value
            );
            const leftThere = leftoversIn(option.value).filter((leftover) =>
              foundLeftovers.includes(leftover)
            );
            return (
              there.length + leftThere.length > 0 && (
                <section key={option.value} className="flex flex-col">
                  <h2 className={sectionLabelClass}>{option.label}</h2>
                  <ul>
                    {leftThere.map(leftoverRow)}
                    {there.map(lotRow)}
                  </ul>
                </section>
              )
            );
          })
        );
    } else if (allItems.length === 0 && leftoversHere.length === 0) {
      lots = (
        <EmptyState title="The pantry is empty">
          Check items off a{" "}
          <Link to="/shopping" className={linkClass}>
            shopping list
          </Link>{" "}
          and they come here when you&apos;re done.
        </EmptyState>
      );
    } else if (here.length === 0 && leftoversHere.length === 0) {
      lots = <p className="text-sm text-muted">Nothing here.</p>;
    } else {
      lots = (
        <>
          {leftoversHere.length > 0 && (
            <section className="flex flex-col">
              <h2 className={sectionLabelClass}>Leftovers</h2>
              <ul>{leftoversHere.map(leftoverRow)}</ul>
            </section>
          )}
          {ordered.length > 0 && (
            <section className="flex flex-col">
              <h2 className={sectionLabelClass}>Use soon</h2>
              <ul>
                {ordered.map((item) => (
                  <LotRow
                    key={item.id}
                    item={item}
                    now={now}
                    onOpen={() => {
                      setEditing(item);
                    }}
                    onPlan={() => {
                      setPlanningLot(item);
                    }}
                    onFreeze={
                      item.storage === Storage.FREEZER
                        ? undefined
                        : () => {
                            freezeLot(item);
                          }
                    }
                    freezing={
                      freeze.isPending && freeze.variables.item?.id === item.id
                    }
                  />
                ))}
              </ul>
            </section>
          )}
          {rest.length > 0 && (
            <section className="flex flex-col">
              {(ordered.length > 0 || leftoversHere.length > 0) && (
                <h2 className={sectionLabelClass}>
                  {ordered.length > 0 ? "Everything else" : "Food"}
                </h2>
              )}
              <ul>
                {rest.map((item) => (
                  <LotRow
                    key={item.id}
                    item={item}
                    now={now}
                    onOpen={() => {
                      setEditing(item);
                    }}
                  />
                ))}
              </ul>
            </section>
          )}
        </>
      );
    }
    content = (
      <div className="flex flex-col gap-4">
        {lots}
        {!searching && place === "cupboard" && (
          <Card title="Always in stock">
            <div className="flex flex-col gap-3 text-sm">
              <p className="text-muted">
                Things you always have, like salt and spices. They aren&apos;t
                counted here, and the shopping list leaves them out.
              </p>
              {alwaysInStockFoods.length > 0 && (
                <ul className="flex flex-wrap gap-1.5">
                  {alwaysInStockFoods.map((food) => (
                    <li
                      key={food.foodId}
                      className="inline-flex items-center gap-1 rounded-full bg-ground-dark py-1 pr-1 pl-3 text-ink-soft"
                    >
                      {food.foodName}
                      <button
                        type="button"
                        className="rounded-full p-0.5 text-muted hover:bg-ground-dark hover:text-ink-soft"
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
        )}
      </div>
    );
  }

  const cookableCount = cookable.data?.recipes.length ?? 0;
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
      <PageHeader
        title="Pantry"
        actions={
          <Link to="/pantry/add" className={primaryButton}>
            <PlusIcon className="size-4" aria-hidden="true" />
            Add
          </Link>
        }
      />
      <label className="flex items-center gap-2 rounded-xl border-2 border-pine bg-white px-3 py-2">
        <MagnifyingGlassIcon
          className="size-5 shrink-0 text-pine"
          aria-hidden="true"
        />
        <span className="sr-only">Search the pantry</span>
        <input
          type="search"
          className="min-w-0 grow border-0 bg-transparent py-1 text-base text-ink-soft outline-none placeholder:text-muted"
          value={search}
          placeholder="Search the pantry"
          onChange={(event) => {
            setSearch(event.target.value);
          }}
        />
      </label>
      {!searching && (
        <Segmented
          label="Where it's kept"
          value={place}
          options={PLACES}
          onChange={(next) => {
            setParams(next === "fridge" ? {} : { place: next }, {
              replace: true,
            });
          }}
        />
      )}
      {!searching && cookableCount > 0 && (
        <Link
          to="/pantry/cook"
          className="flex items-center gap-3 rounded-2xl bg-pine-50 px-4 py-3.5 text-base font-bold text-pine-dark"
        >
          <FireIcon className="size-5 shrink-0" aria-hidden="true" />
          <span className="grow">
            {cookableCount === 1
              ? "1 lunch or dinner you can cook right now"
              : `${cookableCount} lunches and dinners you can cook right now`}
          </span>
          <ChevronRightIcon className="size-5 shrink-0" aria-hidden="true" />
        </Link>
      )}
      {/* A bowl of what's on hand, when nothing whole is. */}
      {!searching && (
        <Link
          to="/bowls"
          className="flex items-center gap-3 rounded-2xl border border-line bg-white px-4 py-3.5 text-base font-bold text-ink-soft"
        >
          <span className="grow">Build a bowl of what&apos;s on hand</span>
          <ChevronRightIcon className="size-5 shrink-0" aria-hidden="true" />
        </Link>
      )}
      <ErrorText error={setAlwaysInStock.error ?? freeze.error} />
      {frozenAsIs !== null && (
        <p className="rounded-xl bg-warn-tint px-4 py-3 text-sm text-ink-soft">
          {frozenAsIs.foodName} is in the freezer, but it still says to use it
          by {formatNearDate(frozenAsIs.useByDate)}, as its food doesn&apos;t
          say how long it keeps frozen.{" "}
          <Link to={`/foods/${frozenAsIs.foodId}`} className={linkClass}>
            Set its freezer shelf life
          </Link>{" "}
          to count from today.
        </p>
      )}
      {content}
      {openLeftover !== null && (
        <LeftoverSheet
          leftover={openLeftover.leftover}
          step={openLeftover.step}
          onClose={() => {
            setOpenLeftover(null);
          }}
        />
      )}
      {planningLot !== null && (
        <RecipesWithFood
          item={planningLot}
          onClose={() => {
            setPlanningLot(null);
          }}
        />
      )}
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
