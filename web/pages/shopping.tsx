import Loading from "@components/loading";
import AddItemDialog from "@components/shopping/add-item-dialog";
import CompleteDialog from "@components/shopping/complete-dialog";
import NewListForm from "@components/shopping/new-list-form";
import ShoppingListView from "@components/shopping/shopping-list";
import StoresDialog from "@components/shopping/stores-dialog";
import Card from "@components/ui/card";
import { EmptyState } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import { menuItemClass } from "@components/ui/more-menu";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  dangerButton,
  iconButton,
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import {
  createConnectQueryKey,
  useMutation,
  useQuery,
  useTransport,
} from "@connectrpc/connect-query";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import {
  EllipsisHorizontalIcon,
  CheckCircleIcon,
  ReceiptPercentIcon,
  ExclamationTriangleIcon,
} from "@heroicons/react/24/outline";
import { skipToken, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  checkItem,
  completeShopping,
  deleteItem,
  deleteShoppingList,
  getShoppingList,
  listShoppingLists,
  listStores,
  regenerateShoppingList,
  uncheckItem,
} from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import type { ShoppingItem, ShoppingList } from "gen/shopping/v1/shopping_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { today } from "helpers/dates";
import { formatDay, formatShortDate } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

/**
 * POLL_MS is how often an open list is read again, so that ticks on the
 * other phone in the store show within a few seconds. Lists are small, and
 * only polled while the page is visible and the shopping isn't complete.
 */
const POLL_MS = 4000;

/**
 * ShoppingPage shows the household's shopping list, by store and aisle, to
 * tick off on a phone in the store, or makes a new one from the plan.
 */
const ShoppingPage: React.FC = () => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const [params, setParams] = useSearchParams();
  const lists = useQuery(listShoppingLists, { pageSize: 10 });
  const stores = useQuery(listStores, {});
  const now = today();
  /*
   * The list shown: the one in the URL, or the latest whose shopping isn't
   * complete and whose meals aren't all past.
   */
  const listParam = params.get("list") ?? "";
  const currentId =
    listParam === ""
      ? (lists.data?.lists.find(
          (other) => !other.complete && other.endDate >= now
        )?.id ?? "")
      : listParam;
  const list = useQuery(
    getShoppingList,
    currentId === "" ? skipToken : { id: currentId },
    {
      refetchInterval: (query) =>
        query.state.data?.list?.complete === true ? false : POLL_MS,
    }
  );
  const [creating, setCreating] = useState(false);
  const [adding, setAdding] = useState(false);
  const [managingStores, setManagingStores] = useState(false);
  const [completing, setCompleting] = useState(false);
  const [deletingList, setDeletingList] = useState(false);
  const [done, setDone] = useState("");
  // Checks being saved, by item ID, shown before the server has them.
  const [pending, setPending] = useState<Record<string, boolean>>({});

  const listKey = createConnectQueryKey({
    cardinality: "finite",
    input: { id: currentId },
    schema: getShoppingList,
    transport,
  });
  const showList = (id: string) => {
    const next = new URLSearchParams();
    if (id !== "") {
      next.set("list", id);
    }
    setParams(next, { replace: true });
  };

  const check = useMutation(checkItem);
  const uncheck = useMutation(uncheckItem);
  const toggle = (item: ShoppingItem, checked: boolean) => {
    setPending((current) => ({ ...current, [item.id]: checked }));
    const settle = () => {
      setPending((current) =>
        Object.fromEntries(
          Object.entries(current).filter(([id]) => id !== item.id)
        )
      );
    };
    (checked ? check : uncheck).mutate(
      { id: item.id },
      {
        onError: () => {
          settle();
        },
        onSuccess: (response) => {
          // The response is the item as saved: put it into the list.
          queryClient.setQueryData(listKey, (old) => {
            if (old?.list === undefined || response.item === undefined) {
              return old;
            }
            const saved = response.item;
            const items = old.list.items.map((other) =>
              other.id === saved.id ? saved : other
            );
            return {
              ...old,
              list: {
                ...old.list,
                checkedCount: items.filter((other) => other.checked).length,
                items,
              },
            };
          });
          settle();
        },
      }
    );
  };
  const onChanged = async () => {
    await invalidateServices(queryClient, [ShoppingService]);
  };
  const remove = useMutation(deleteItem, { onSuccess: onChanged });
  const regenerate = useMutation(regenerateShoppingList, {
    onSuccess: (response) => {
      queryClient.setQueryData(listKey, (old) =>
        old === undefined ? old : { ...old, list: response.list }
      );
      void onChanged();
    },
  });
  const complete = useMutation(completeShopping, {
    onSuccess: async (response) => {
      setCompleting(false);
      const count = response.pantryItems.length;
      setDone(
        count === 0
          ? "Shopping done."
          : `Shopping done: ${count === 1 ? "1 item" : `${count} items`} went into the pantry.`
      );
      await invalidateServices(queryClient, [ShoppingService, PantryService]);
    },
  });
  const removeList = useMutation(deleteShoppingList, {
    onSuccess: async () => {
      showList("");
      await onChanged();
    },
  });

  const otherLists = (lists.data?.lists ?? []).filter(
    (other) => other.id !== currentId
  );
  const storeList = stores.data?.stores ?? [];
  let content: React.ReactNode = null;
  let kicker = "One shop a week, by store and aisle";
  let progress: { checked: number; total: number } | undefined;
  // The list shown, for the menu's actions on it.
  let shown: ShoppingList | undefined;
  if (lists.isPending || (currentId !== "" && list.isPending)) {
    content = <Loading />;
  } else if (lists.isError) {
    content = (
      <QueryError
        error={lists.error}
        onRetry={() => {
          void lists.refetch();
        }}
      />
    );
  } else if (currentId === "") {
    content = (
      <div className="flex flex-col gap-4">
        {done !== "" && <DoneBanner message={done} />}
        <Card title="New shopping list">
          <NewListForm onCreated={showList} />
        </Card>
        {storeList.length === 0 && (
          <EmptyState title="No stores yet">
            Add the stores you shop at, so the list is split by store.{" "}
            <button
              type="button"
              className={linkClass}
              onClick={() => {
                setManagingStores(true);
              }}
            >
              Add stores
            </button>
          </EmptyState>
        )}
      </div>
    );
  } else if (list.isError) {
    content = (
      <QueryError
        error={list.error}
        onRetry={() => {
          void list.refetch();
        }}
      />
    );
  } else if (list.data?.list !== undefined) {
    const current = list.data.list;
    const checkedCount = current.items.filter(
      (item) => pending[item.id] ?? item.checked
    ).length;
    kicker = `${formatDay(current.startDate)} – ${formatDay(current.endDate)}`;
    progress = { checked: checkedCount, total: current.items.length };
    shown = current;
    content = (
      <div className="flex flex-col gap-4">
        {done !== "" && <DoneBanner message={done} />}
        {current.complete && done === "" && (
          <p className="rounded-xl bg-ground-dark px-3 py-2 text-sm text-ink-soft">
            This shopping is done, and what was checked is in the{" "}
            <Link to="/pantry" className={linkClass}>
              pantry
            </Link>
            .
          </p>
        )}
        {!current.complete && current.topUpCount > 0 && (
          <div className="flex items-start gap-2 rounded-xl border border-warn-line bg-warn-tint px-3 py-2 text-sm text-warn-dark">
            <ExclamationTriangleIcon
              className="mt-0.5 size-5 shrink-0"
              aria-hidden="true"
            />
            <p>
              {current.topUpCount === 1
                ? "1 item won't keep until it's needed"
                : `${current.topUpCount} items won't keep until they're needed`}
              : buy {current.topUpCount === 1 ? "it" : "them"} on a top-up trip
              closer to the day, or move those meals earlier in the{" "}
              <Link to="/plan" className={linkClass}>
                plan
              </Link>{" "}
              and update the list.
            </p>
          </div>
        )}
        <ErrorText
          error={
            check.error ??
            uncheck.error ??
            remove.error ??
            regenerate.error ??
            removeList.error
          }
        />
        {current.items.length === 0 ? (
          <EmptyState title="Nothing to buy">
            The pantry has everything the plan needs for these days. Plan more
            meals, then update the list, or add items by hand.
          </EmptyState>
        ) : (
          <ShoppingListView
            list={current}
            pending={pending}
            disabled={current.complete}
            onToggle={toggle}
            onDelete={(item) => {
              remove.mutate({ id: item.id });
            }}
          />
        )}
        {!current.complete && current.items.length > 0 && (
          <div className="sticky bottom-[3.75rem] z-10 bg-ground/90 py-2 backdrop-blur lg:bottom-4 print:hidden">
            <button
              type="button"
              className={`${primaryButton} w-full py-3.5 text-base`}
              onClick={() => {
                setCompleting(true);
              }}
            >
              Done: put it in the pantry
            </button>
          </div>
        )}
        {completing && (
          <CompleteDialog
            list={{
              ...current,
              items: current.items.map((item) => ({
                ...item,
                checked: pending[item.id] ?? item.checked,
              })),
            }}
            pending={complete.isPending}
            error={complete.error}
            onClose={() => {
              setCompleting(false);
            }}
            onConfirm={() => {
              complete.mutate({ date: today(), id: current.id });
            }}
          />
        )}
        {adding && (
          <AddItemDialog
            listId={current.id}
            stores={storeList}
            onClose={() => {
              setAdding(false);
            }}
          />
        )}
      </div>
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium text-muted">{kicker}</p>
        <div className="flex items-center justify-between gap-3">
          <h1 className="text-[28px] leading-tight font-bold tracking-tight text-ink-soft">
            Shop
          </h1>
          <div className="flex items-center gap-2">
            {progress !== undefined && progress.total > 0 && (
              <span className="text-sm text-muted tabular-nums">
                {progress.checked} of {progress.total}
              </span>
            )}
            <Menu as="div" className="relative print:hidden">
              <MenuButton className={iconButton} aria-label="More" title="More">
                <EllipsisHorizontalIcon className="size-6" />
              </MenuButton>
              <MenuItems
                transition
                anchor="bottom end"
                className="z-[1100] mt-1 w-64 rounded-2xl border border-line bg-white py-1 font-ui shadow-lg transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
              >
                {shown !== undefined && !shown.complete && (
                  <>
                    <MenuItem>
                      <button
                        type="button"
                        className={menuItemClass}
                        disabled={regenerate.isPending}
                        onClick={() => {
                          regenerate.mutate({ id: shown?.id ?? "" });
                        }}
                      >
                        Update from the plan
                      </button>
                    </MenuItem>
                    <MenuItem>
                      <button
                        type="button"
                        className={menuItemClass}
                        onClick={() => {
                          setAdding(true);
                        }}
                      >
                        Add an item
                      </button>
                    </MenuItem>
                  </>
                )}
                <MenuItem>
                  <button
                    type="button"
                    className={menuItemClass}
                    onClick={() => {
                      setManagingStores(true);
                    }}
                  >
                    Stores
                  </button>
                </MenuItem>
                {shown !== undefined && (
                  <MenuItem>
                    <button
                      type="button"
                      className={menuItemClass}
                      onClick={() => {
                        setCreating(true);
                      }}
                    >
                      New list
                    </button>
                  </MenuItem>
                )}
                {otherLists.slice(0, 5).map((other) => (
                  <MenuItem key={other.id}>
                    <button
                      type="button"
                      className={menuItemClass}
                      onClick={() => {
                        showList(other.id);
                      }}
                    >
                      {formatShortDate(other.startDate)} –{" "}
                      {formatShortDate(other.endDate)}
                      {other.complete ? " (done)" : ""}
                    </button>
                  </MenuItem>
                ))}
                {shown !== undefined && (
                  <MenuItem>
                    <button
                      type="button"
                      className={`${menuItemClass} border-t border-line text-error`}
                      onClick={() => {
                        setDeletingList(true);
                      }}
                    >
                      Delete this list
                    </button>
                  </MenuItem>
                )}
              </MenuItems>
            </Menu>
          </div>
        </div>
        {progress !== undefined && progress.total > 0 && (
          <div
            className="mt-2 h-2 rounded-full bg-ground-dark"
            role="progressbar"
            aria-label="Ticked"
            aria-valuemin={0}
            aria-valuemax={progress.total}
            aria-valuenow={progress.checked}
          >
            <div
              className="h-2 rounded-full bg-pine transition-[width]"
              style={{ width: `${(progress.checked / progress.total) * 100}%` }}
            />
          </div>
        )}
      </div>
      {content}
      {deletingList && shown !== undefined && (
        <Modal
          open
          title="Delete this list?"
          onClose={() => {
            setDeletingList(false);
          }}
        >
          <div className="flex flex-col gap-4">
            <p className="text-sm text-ink-soft">
              The list and its ticks are deleted. The pantry doesn&apos;t
              change.
            </p>
            <div className="flex gap-2">
              <button
                type="button"
                className={dangerButton}
                disabled={removeList.isPending}
                onClick={() => {
                  removeList.mutate({ id: shown?.id ?? "" });
                  setDeletingList(false);
                }}
              >
                Delete
              </button>
              <button
                type="button"
                className={secondaryButton}
                onClick={() => {
                  setDeletingList(false);
                }}
              >
                Keep it
              </button>
            </div>
          </div>
        </Modal>
      )}
      {creating && (
        <Modal
          open
          title="New shopping list"
          onClose={() => {
            setCreating(false);
          }}
        >
          <NewListForm
            onCreated={(id) => {
              setCreating(false);
              setDone("");
              showList(id);
            }}
            onCancel={() => {
              setCreating(false);
            }}
          />
        </Modal>
      )}
      {managingStores && (
        <StoresDialog
          stores={storeList}
          onClose={() => {
            setManagingStores(false);
          }}
        />
      )}
    </div>
  );
};

interface DoneBannerProps {
  message: string;
}

/*
 * RECEIPT_REQUEST is what to ask an agent with a photo of the receipt: it
 * reads it through the MCP server, to record prices and correct the lots.
 */
const RECEIPT_REQUEST =
  "Here's the receipt from today's shop. Record what each food cost, and correct what went into the pantry to what we actually bought: the brands, sizes and amounts.";

/**
 * DoneBanner says the shopping is done, with a link to the pantry, and a
 * reminder to have an agent read the receipt, which records the prices and
 * corrects the amounts and details of what was bought.
 */
const DoneBanner: React.FC<DoneBannerProps> = ({ message }) => {
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col gap-3">
      <p className="flex items-center gap-3 rounded-2xl bg-pine-50 px-4 py-3 text-base font-bold text-pine-dark">
        <CheckCircleIcon className="size-6 shrink-0" aria-hidden="true" />
        <span>
          {message}{" "}
          <Link to="/pantry" className={`${linkClass} font-medium`}>
            See the pantry
          </Link>
        </span>
      </p>
      <div className="flex flex-col gap-3 rounded-2xl border border-warn-line bg-warn-tint p-4">
        <p className="flex items-center gap-2 text-base font-bold text-ink-soft">
          <ReceiptPercentIcon className="size-6 shrink-0" aria-hidden="true" />
          Scan the receipt with your agent
        </p>
        <button
          type="button"
          className={`${primaryButton} self-start`}
          onClick={() => {
            void navigator.clipboard.writeText(RECEIPT_REQUEST).then(() => {
              setCopied(true);
            });
          }}
        >
          {copied ? "Copied: send it with a photo" : "Copy the request"}
        </button>
      </div>
    </div>
  );
};

export default ShoppingPage;
