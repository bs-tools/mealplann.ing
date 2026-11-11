import Loading from "@components/loading";
import AddItemDialog from "@components/shopping/add-item-dialog";
import CompleteDialog from "@components/shopping/complete-dialog";
import NewListForm from "@components/shopping/new-list-form";
import ShoppingListView from "@components/shopping/shopping-list";
import StoresDialog from "@components/shopping/stores-dialog";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { EmptyState, PageHeader } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  ghostDangerButton,
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
import {
  ArrowPathIcon,
  BuildingStorefrontIcon,
  CheckCircleIcon,
  ReceiptPercentIcon,
  ExclamationTriangleIcon,
  PlusIcon,
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
import type { ShoppingItem } from "gen/shopping/v1/shopping_pb";
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
  const storesButton = (
    <button
      type="button"
      className={secondaryButton}
      onClick={() => {
        setManagingStores(true);
      }}
    >
      <BuildingStorefrontIcon className="size-4" aria-hidden="true" />
      Stores
    </button>
  );

  let content: React.ReactNode = null;
  let subtitle: React.ReactNode = "One shop a week, by store and aisle.";
  let actions: React.ReactNode = storesButton;
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
    subtitle = (
      <>
        For the meals of {formatDay(current.startDate)} to{" "}
        {formatDay(current.endDate)}
        {current.items.length > 0 && (
          <span className="tabular-nums">
            {" "}
            · {checkedCount} of {current.items.length} checked
          </span>
        )}
      </>
    );
    actions = (
      <>
        {!current.complete && (
          <>
            <button
              type="button"
              className={secondaryButton}
              disabled={regenerate.isPending}
              title="Make the list again from the plan and the pantry, keeping what's checked"
              onClick={() => {
                regenerate.mutate({ id: current.id });
              }}
            >
              <ArrowPathIcon
                className={`size-4 ${regenerate.isPending ? "animate-spin" : ""}`}
                aria-hidden="true"
              />
              Update from plan
            </button>
            <button
              type="button"
              className={secondaryButton}
              onClick={() => {
                setAdding(true);
              }}
            >
              <PlusIcon className="size-4" aria-hidden="true" />
              Add item
            </button>
          </>
        )}
        {storesButton}
      </>
    );
    content = (
      <div className="flex flex-col gap-4">
        {done !== "" && <DoneBanner message={done} />}
        {current.complete && done === "" && (
          <p className="rounded-md bg-gray-100 px-3 py-2 text-sm text-gray-700">
            This shopping is done, and what was checked is in the{" "}
            <Link to="/pantry" className={linkClass}>
              pantry
            </Link>
            .
          </p>
        )}
        {!current.complete && current.topUpCount > 0 && (
          <div className="flex items-start gap-2 rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-900">
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
          <button
            type="button"
            className={`${primaryButton} w-full py-3 text-base sm:w-auto sm:self-end`}
            onClick={() => {
              setCompleting(true);
            }}
          >
            <CheckCircleIcon className="size-5" aria-hidden="true" />
            Done shopping
          </button>
        )}
        <div className="flex flex-wrap items-center justify-between gap-2 border-t border-gray-200 pt-3 text-sm print:hidden">
          <div className="flex flex-wrap items-center gap-2">
            <button
              type="button"
              className={secondaryButton}
              onClick={() => {
                setCreating(true);
              }}
            >
              New list
            </button>
            {otherLists.length > 0 && (
              <label className="flex items-center gap-2 text-gray-600">
                <span>Other lists</span>
                <select
                  className="rounded-md border border-gray-300 bg-white px-2 py-1 text-sm"
                  value=""
                  onChange={(event) => {
                    showList(event.target.value);
                  }}
                >
                  <option value="" disabled>
                    Choose…
                  </option>
                  {otherLists.map((other) => (
                    <option key={other.id} value={other.id}>
                      {formatShortDate(other.startDate)} to{" "}
                      {formatShortDate(other.endDate)}
                      {other.complete ? " (done)" : ""}
                    </option>
                  ))}
                </select>
              </label>
            )}
          </div>
          <ConfirmButton
            className={ghostDangerButton}
            title="Delete this list?"
            message="The list and its ticks are deleted. The pantry doesn't change."
            disabled={removeList.isPending}
            onConfirm={() => {
              removeList.mutate({ id: current.id });
            }}
          >
            Delete list
          </ConfirmButton>
        </div>
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
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-2">
      <PageHeader title="Shopping" subtitle={subtitle} actions={actions} />
      {content}
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
      <div className="flex flex-col gap-3 rounded-2xl border border-amber-200 bg-amber-50 p-4">
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
