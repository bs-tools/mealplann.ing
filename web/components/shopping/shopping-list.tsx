import { iconButton, sectionLabelClass } from "@components/ui/styles";
import {
  CheckIcon,
  ExclamationTriangleIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { StoreSection } from "gen/foods/v1/foods_pb";
import type { ShoppingItem, ShoppingList } from "gen/shopping/v1/shopping_pb";
import { formatWeekday } from "helpers/format";
import { storeSectionLabel } from "helpers/labels";
import React from "react";
import { Link } from "react-router";

// SectionGroup is the items of a store section.
// MAX_RECIPES is how many of the recipes an item is for are named.
const MAX_RECIPES = 2;

interface SectionGroup {
  section: StoreSection;
  items: ShoppingItem[];
}

// StoreGroup is the items of a store, by section.
interface StoreGroup {
  storeId: string;
  storeName: string;
  sections: SectionGroup[];
  left: number;
}

/**
 * GroupItems groups a list's items by store and then section, keeping the
 * server's order, which is the order of the shop. Within a section, checked
 * items move to the bottom, so what's left to buy stays at the top.
 */
export const groupItems = (
  items: ShoppingItem[],
  checked: (item: ShoppingItem) => boolean
): StoreGroup[] => {
  const stores: StoreGroup[] = [];
  for (const item of items) {
    let store = stores.find((group) => group.storeId === item.storeId);
    if (store === undefined) {
      store = {
        left: 0,
        sections: [],
        storeId: item.storeId,
        storeName: item.storeName,
      };
      stores.push(store);
    }
    let section = store.sections.find(
      (group) => group.section === item.storeSection
    );
    if (section === undefined) {
      section = { items: [], section: item.storeSection };
      store.sections.push(section);
    }
    section.items.push(item);
    if (!checked(item)) {
      store.left += 1;
    }
  }
  for (const store of stores) {
    for (const section of store.sections) {
      section.items = [
        ...section.items.filter((item) => !checked(item)),
        ...section.items.filter((item) => checked(item)),
      ];
    }
  }
  return stores;
};

interface ItemRowProps {
  item: ShoppingItem;
  checked: boolean;
  disabled: boolean;
  onToggle: () => void;
  onDelete: () => void;
}

/**
 * ItemRow is an item to tick off: the whole row is the button, big enough
 * to hit with a thumb while pushing a trolley.
 */
const ItemRow: React.FC<ItemRowProps> = ({
  item,
  checked,
  disabled,
  onToggle,
  onDelete,
}) => (
  <li className="flex items-stretch border-b border-line last:border-b-0">
    <div className="flex min-w-0 grow flex-col">
      <button
        type="button"
        role="checkbox"
        aria-checked={checked}
        disabled={disabled}
        className="flex min-h-14 grow items-center gap-4 py-2.5 text-left disabled:cursor-not-allowed"
        onClick={onToggle}
      >
        <span
          className={`flex size-7 shrink-0 items-center justify-center rounded-full ${checked ? "bg-pine text-white" : "bg-white ring-2 ring-muted/60 ring-inset"}`}
          aria-hidden="true"
        >
          {checked && <CheckIcon className="size-4 stroke-[3]" />}
        </span>
        <span className="flex min-w-0 grow flex-col">
          <span
            className={`text-base ${checked ? "text-muted line-through decoration-muted/60" : "text-ink-soft"}`}
          >
            {item.name}
          </span>
          {item.note !== "" && (
            <span className="text-sm text-muted italic">{item.note}</span>
          )}
          {!checked && item.warning !== "" && (
            <span className="mt-0.5 flex items-start gap-1 text-xs text-warn-dark">
              <ExclamationTriangleIcon
                className="mt-px size-3.5 shrink-0"
                aria-hidden="true"
              />
              {item.warning}
            </span>
          )}
        </span>
        <span className="flex shrink-0 flex-col items-end gap-0.5">
          {item.amountText !== "" && (
            <span
              className={`text-sm tabular-nums ${checked ? "text-muted" : "text-ink-soft"}`}
            >
              {item.amountText}
            </span>
          )}
          {item.topUp && !checked && (
            <span className="rounded-full bg-warn-tint px-2 py-0.5 text-xs font-medium text-warn-dark">
              Top-up {formatWeekday(item.neededByDate)}
            </span>
          )}
          {checked && item.checkedBy !== "" && (
            <span className="text-xs text-muted">{item.checkedBy}</span>
          )}
        </span>
      </button>
      {/*
       * The recipes are links, so they sit below the row's button, not in
       * it. A food most meals use, like garlic, names two of them.
       */}
      {!checked && item.recipes.length > 0 && (
        <p className="-mt-2 pb-2.5 pl-11 text-xs text-muted">
          for{" "}
          {item.recipes.slice(0, MAX_RECIPES).map((recipe, index) => (
            <React.Fragment key={recipe.recipeId}>
              {index > 0 && ", "}
              <Link
                to={`/recipes/${recipe.recipeId}`}
                className="hover:text-ink-soft hover:underline"
              >
                {recipe.recipeName}
              </Link>
            </React.Fragment>
          ))}
          {item.recipes.length > MAX_RECIPES && (
            <span
              title={item.recipes
                .slice(MAX_RECIPES)
                .map((recipe) => recipe.recipeName)
                .join(", ")}
            >
              {" "}
              and {item.recipes.length - MAX_RECIPES} more
            </span>
          )}
        </p>
      )}
    </div>
    {!disabled && (
      <button
        type="button"
        className={`${iconButton} shrink-0 self-center text-muted/60 print:hidden`}
        aria-label={`Remove ${item.name}`}
        title={`Remove ${item.name}`}
        onClick={onDelete}
      >
        <XMarkIcon className="size-4" />
      </button>
    )}
  </li>
);

interface ShoppingListViewProps {
  list: ShoppingList;
  // Checks being saved, by item ID, shown before the server has them.
  pending: Record<string, boolean>;
  disabled: boolean;
  onToggle: (item: ShoppingItem, checked: boolean) => void;
  onDelete: (item: ShoppingItem) => void;
}

/**
 * ShoppingListView shows a list's items to buy by store and section, in
 * the order of the shop, to tick off, and those ticked in the cart below.
 */
const ShoppingListView: React.FC<ShoppingListViewProps> = ({
  list,
  pending,
  disabled,
  onToggle,
  onDelete,
}) => {
  const isChecked = (item: ShoppingItem): boolean =>
    pending[item.id] ?? item.checked;
  // What's left to buy is by store and section, and what's ticked in the cart.
  const stores = groupItems(
    list.items.filter((item) => !isChecked(item)),
    isChecked
  );
  const cart = list.items.filter(isChecked);
  const row = (item: ShoppingItem): React.ReactNode => (
    <ItemRow
      key={item.id}
      item={item}
      checked={isChecked(item)}
      disabled={disabled}
      onToggle={() => {
        onToggle(item, !isChecked(item));
      }}
      onDelete={() => {
        onDelete(item);
      }}
    />
  );
  return (
    <div className="flex flex-col gap-5">
      {stores.map((store) => (
        <section key={store.storeId} className="flex flex-col gap-4">
          {stores.length > 1 && (
            <h2 className="text-lg font-bold text-ink-soft">
              {store.storeName === "" ? "Elsewhere" : store.storeName}
            </h2>
          )}
          {store.sections.map((section) => (
            <div key={section.section} className="flex flex-col">
              <h3 className={sectionLabelClass}>
                {section.section === StoreSection.UNSPECIFIED
                  ? "Other"
                  : storeSectionLabel(section.section)}
              </h3>
              <ul>{section.items.map(row)}</ul>
            </div>
          ))}
        </section>
      ))}
      {cart.length > 0 && (
        <section className="flex flex-col">
          <h3 className={sectionLabelClass}>In the cart</h3>
          <ul>{cart.map(row)}</ul>
        </section>
      )}
    </div>
  );
};

export default ShoppingListView;
