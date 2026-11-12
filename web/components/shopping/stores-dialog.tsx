import ConfirmButton from "@components/ui/confirm-button";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  iconButton,
  inputClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  TrashIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { FoodService } from "gen/foods/v1/foods_pb";
import {
  createStore,
  deleteStore,
  updateStore,
} from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import type { Store } from "gen/shopping/v1/shopping_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

interface StoresDialogProps {
  stores: Store[];
  onClose: () => void;
}

/**
 * StoresDialog manages the stores the household shops at: their names and
 * order. The first is the main store, where everything is bought unless a
 * food says otherwise.
 */
const StoresDialog: React.FC<StoresDialogProps> = ({ stores, onClose }) => {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  // The names being edited, by store ID.
  const [names, setNames] = useState<Record<string, string>>({});
  const onSuccess = async () => {
    await invalidateServices(queryClient, [ShoppingService, FoodService]);
  };
  const create = useMutation(createStore, {
    onSuccess: async () => {
      setName("");
      await onSuccess();
    },
  });
  const update = useMutation(updateStore, { onSuccess });
  const remove = useMutation(deleteStore, { onSuccess });
  const pending = create.isPending || update.isPending || remove.isPending;

  /*
   * Moving a store renumbers them all, as positions can tie. Households
   * have two or three stores, so it's a request or two.
   */
  const move = async (index: number, offset: number): Promise<void> => {
    const order = [...stores];
    const [store] = order.splice(index, 1);
    if (store === undefined) {
      return;
    }
    order.splice(index + offset, 0, store);
    for (const [position, other] of order.entries()) {
      if (other.position !== position) {
        // oxlint-disable-next-line no-await-in-loop -- in order, to keep positions consistent
        await update.mutateAsync({
          store: { id: other.id, position },
          updateMask: { paths: ["position"] },
        });
      }
    }
  };

  return (
    <Modal open onClose={onClose} title="Stores">
      <div className="flex flex-col gap-4">
        <p className="text-sm text-muted">
          The first store is your main store, where everything is bought unless
          a food has its own store, set on the food&apos;s page.
        </p>
        {stores.length > 0 && (
          <ul className="flex flex-col gap-2">
            {stores.map((store, index) => {
              const edited = names[store.id] ?? store.name;
              return (
                <li key={store.id} className="flex items-center gap-1.5">
                  <input
                    className={`${inputClass} grow`}
                    aria-label="Store name"
                    value={edited}
                    onChange={(event) => {
                      setNames({ ...names, [store.id]: event.target.value });
                    }}
                  />
                  {edited !== store.name && (
                    <button
                      type="button"
                      className={secondaryButton}
                      disabled={pending}
                      onClick={() => {
                        update.mutate({
                          store: { id: store.id, name: edited },
                          updateMask: { paths: ["name"] },
                        });
                      }}
                    >
                      Rename
                    </button>
                  )}
                  {index === 0 && (
                    <span className="text-xs whitespace-nowrap text-muted">
                      Main
                    </span>
                  )}
                  <button
                    type="button"
                    className={iconButton}
                    disabled={pending || index === 0}
                    aria-label={`Move ${store.name} up`}
                    title="Move up"
                    onClick={() => {
                      void move(index, -1);
                    }}
                  >
                    <ArrowUpIcon className="size-4" />
                  </button>
                  <button
                    type="button"
                    className={iconButton}
                    disabled={pending || index === stores.length - 1}
                    aria-label={`Move ${store.name} down`}
                    title="Move down"
                    onClick={() => {
                      void move(index, 1);
                    }}
                  >
                    <ArrowDownIcon className="size-4" />
                  </button>
                  <ConfirmButton
                    className={iconButton}
                    title={`Delete ${store.name}?`}
                    message="Foods bought there, and the items of shopping lists, move to the main store."
                    disabled={pending}
                    onConfirm={() => {
                      remove.mutate({ id: store.id });
                    }}
                  >
                    <TrashIcon
                      className="size-4"
                      aria-label={`Delete ${store.name}`}
                    />
                  </ConfirmButton>
                </li>
              );
            })}
          </ul>
        )}
        <form
          className="flex gap-2"
          onSubmit={(event) => {
            event.preventDefault();
            create.mutate({ store: { name } });
          }}
        >
          <input
            className={`${inputClass} grow`}
            aria-label="New store"
            placeholder={stores.length === 0 ? "Supermarket" : "Bulk store"}
            value={name}
            onChange={(event) => {
              setName(event.target.value);
            }}
          />
          <button
            type="submit"
            className={`${primaryButton} whitespace-nowrap`}
            disabled={pending || name.trim() === ""}
          >
            Add store
          </button>
        </form>
        <ErrorText error={create.error ?? update.error ?? remove.error} />
        <div className="flex justify-end">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </Modal>
  );
};

export default StoresDialog;
