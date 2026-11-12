import type { Leftover } from "@components/pantry/leftovers";
import {
  LeftoverRow,
  LeftoverSheet,
  useLeftovers,
} from "@components/pantry/leftovers";
import { sectionLabelClass } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { ChevronRightIcon } from "@heroicons/react/24/outline";
import { listPantry } from "gen/pantry/v1/pantry-PantryService_connectquery";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { Storage } from "gen/pantry/v1/pantry_pb";
import { formatShortDate, formatStock } from "helpers/format";
import React, { useState } from "react";
import { Link } from "react-router";

// PLACE_LABELS name where a lot is kept.
const PLACE_LABELS: Record<Storage, string> = {
  [Storage.UNSPECIFIED]: "Cupboard",
  [Storage.PANTRY]: "Cupboard",
  [Storage.FRIDGE]: "Fridge",
  [Storage.FREEZER]: "Freezer",
};

// LotLine says where a lot is and until when, e.g. "Fridge · use by Nov 20".
const lotLine = (item: PantryItem): string =>
  [
    PLACE_LABELS[item.storage],
    item.useByDate === "" ? "" : `use by ${formatShortDate(item.useByDate)}`,
    item.label,
  ]
    .filter((part) => part !== "")
    .join(" · ");

interface FoodStockProps {
  foodId: string;
  foodName: string;
}

/**
 * FoodStock shows how much of a food the kitchen has: each lot, where it's
 * kept and until when, with a link to the pantry to change them.
 */
export const FoodStock: React.FC<FoodStockProps> = ({ foodId, foodName }) => {
  const pantry = useQuery(listPantry, { foodIds: [foodId] });
  if (pantry.data === undefined) {
    return null;
  }
  const always = pantry.data.alwaysInStockFoods.some(
    (food) => food.foodId === foodId
  );
  const lots = pantry.data.items;
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-2">
        <h2 className={sectionLabelClass}>In the kitchen</h2>
        <Link
          to={`/pantry?q=${encodeURIComponent(foodName)}`}
          className="text-sm font-bold text-pine"
        >
          The pantry ›
        </Link>
      </div>
      {lots.length === 0 ? (
        <p className="rounded-2xl border border-line bg-white px-4 py-3 text-sm text-muted">
          {always ? "Always in stock." : "None in the pantry."}
        </p>
      ) : (
        <ul className="divide-y divide-line rounded-2xl border border-line bg-white px-4">
          {lots.map((item) => (
            <li key={item.id}>
              <Link
                to={`/pantry?q=${encodeURIComponent(foodName)}`}
                className="flex items-center gap-3 py-3"
              >
                <span className="min-w-0 grow text-sm text-muted">
                  {lotLine(item)}
                </span>
                <span className="shrink-0 font-bold text-ink-soft tabular-nums">
                  {item.estimated ? "≈ " : ""}
                  {formatStock(
                    item.grams,
                    item.gramsPerPiece,
                    item.packageGrams
                  )}
                </span>
                <ChevronRightIcon
                  className="size-4 shrink-0 text-muted/70"
                  aria-hidden="true"
                />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
};

interface RecipeLeftoversProps {
  recipeId: string;
}

/**
 * RecipeLeftovers shows a recipe's leftovers in the fridge and freezer not
 * planned yet, to plan, freeze or use them, and nothing if there are none.
 */
export const RecipeLeftovers: React.FC<RecipeLeftoversProps> = ({
  recipeId,
}) => {
  const { fridge, frozen } = useLeftovers();
  const [open, setOpen] = useState<Leftover | null>(null);
  const mine = [...fridge, ...frozen].filter(
    (leftover) => leftover.recipeId === recipeId
  );
  if (mine.length === 0) {
    return null;
  }
  return (
    <section className="flex flex-col gap-1">
      <h2 className={sectionLabelClass}>In the kitchen</h2>
      <ul>
        {mine.map((leftover) => (
          <LeftoverRow
            key={leftover.key}
            leftover={{
              ...leftover,
              detail: `${leftover.frozen === undefined ? "Fridge" : "Freezer"} · ${leftover.detail}`,
            }}
            onOpen={() => {
              setOpen(leftover);
            }}
          />
        ))}
      </ul>
      {open !== null && (
        <LeftoverSheet
          leftover={open}
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
    </section>
  );
};
