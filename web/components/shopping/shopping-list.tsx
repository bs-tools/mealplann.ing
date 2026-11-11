import { iconButton } from "@components/ui/styles";
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
  <li className="flex items-stretch">
    <div className="flex min-w-0 grow flex-col">
      <button
        type="button"
        role="checkbox"
        aria-checked={checked}
        disabled={disabled}
        className={`flex min-h-14 grow items-center gap-3 rounded-md px-2 py-2 text-left hover:bg-gray-50 active:bg-gray-100 disabled:cursor-not-allowed ${checked ? "text-gray-400" : "text-gray-900"}`}
        onClick={onToggle}
      >
        <span
          className={`flex size-7 shrink-0 items-center justify-center rounded-full ring-2 ring-inset ${checked ? "bg-brand-600 text-white ring-brand-600" : "bg-white ring-gray-300"}`}
          aria-hidden="true"
        >
          {checked && <CheckIcon className="size-4 stroke-[3]" />}
        </span>
        <span className="flex min-w-0 grow flex-col">
          <span className="flex flex-wrap items-baseline gap-x-2">
            <span
              className={`text-base font-medium ${checked ? "line-through decoration-gray-400" : ""}`}
            >
              {item.name}
            </span>
            {item.amountText !== "" && (
              <span
                className={`text-sm tabular-nums ${checked ? "" : "text-gray-600"}`}
              >
                {item.amountText}
              </span>
            )}
          </span>
          {item.note !== "" && (
            <span className="text-sm text-gray-500 italic">{item.note}</span>
          )}
          {!checked && item.warning !== "" && (
            <span className="mt-0.5 flex items-start gap-1 text-xs text-amber-800">
              <ExclamationTriangleIcon
                className="mt-px size-3.5 shrink-0"
                aria-hidden="true"
              />
              {item.warning}
            </span>
          )}
        </span>
        <span className="flex shrink-0 flex-col items-end gap-0.5 text-xs">
          {item.topUp && !checked && (
            <span className="rounded-full bg-amber-50 px-2 py-0.5 font-medium text-amber-800 ring-1 ring-amber-200 ring-inset">
              Top-up
            </span>
          )}
          {checked && item.checkedBy !== "" && (
            <span className="text-gray-400">{item.checkedBy}</span>
          )}
          {!checked && item.neededByDate !== "" && (
            <span className="text-gray-500">
              {formatWeekday(item.neededByDate)}
            </span>
          )}
        </span>
      </button>
      {/* The recipes are links, so they sit below the row's button, not in it. */}
      {!checked && item.recipes.length > 0 && (
        <p className="-mt-1 pb-2 pl-12 text-xs text-gray-500">
          For{" "}
          {item.recipes.map((recipe, index) => (
            <React.Fragment key={recipe.recipeId}>
              {index > 0 && ", "}
              <Link
                to={`/recipes/${recipe.recipeId}`}
                className="underline decoration-gray-300 underline-offset-2 hover:text-gray-900"
              >
                {recipe.recipeName}
              </Link>
            </React.Fragment>
          ))}
        </p>
      )}
    </div>
    {!disabled && (
      <button
        type="button"
        className={`${iconButton} shrink-0 self-center print:hidden`}
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
 * ShoppingListView shows a list's items by store and section, in the order
 * of the shop, to tick off.
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
  const stores = groupItems(list.items, isChecked);
  return (
    <div className="flex flex-col gap-4">
      {stores.map((store) => (
        <section
          key={store.storeId}
          className="rounded-lg border border-gray-200 bg-white shadow-sm print:shadow-none"
        >
          <div className="flex items-baseline justify-between gap-2 border-b border-gray-100 px-3 py-2.5">
            <h2 className="text-base font-semibold text-gray-900">
              {store.storeName === "" ? "Shopping" : store.storeName}
            </h2>
            <span className="text-sm text-gray-500 tabular-nums">
              {store.left === 0 ? "All done" : `${store.left} to buy`}
            </span>
          </div>
          <div className="flex flex-col gap-1 px-1 py-2">
            {store.sections.map((section) => (
              <div key={section.section}>
                <h3 className="px-2 pt-1 text-xs font-semibold tracking-wide text-gray-500 uppercase">
                  {section.section === StoreSection.UNSPECIFIED
                    ? "Elsewhere"
                    : storeSectionLabel(section.section)}
                </h3>
                <ul className="divide-y divide-gray-100">
                  {section.items.map((item) => (
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
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </section>
      ))}
    </div>
  );
};

export default ShoppingListView;
