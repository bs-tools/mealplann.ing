import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import { primaryButton, secondaryButton } from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import type { ShoppingList } from "gen/shopping/v1/shopping_pb";
import React from "react";

interface CompleteDialogProps {
  list: ShoppingList;
  pending: boolean;
  error: ConnectError | null;
  onConfirm: () => void;
  onClose: () => void;
}

/**
 * CompleteDialog confirms finishing the shopping, saying what goes into
 * the pantry: the checked foods with a weight.
 */
const CompleteDialog: React.FC<CompleteDialogProps> = ({
  list,
  pending,
  error,
  onConfirm,
  onClose,
}) => {
  const stocked = list.items.filter(
    (item) => item.checked && item.foodId !== "" && item.grams > 0
  );
  const unchecked = list.items.filter((item) => !item.checked).length;
  return (
    <Modal open onClose={onClose} title="Done shopping?">
      <div className="flex flex-col gap-4 text-sm text-ink-soft">
        {stocked.length === 0 ? (
          <p>Nothing checked goes into the pantry.</p>
        ) : (
          <div className="flex flex-col gap-2">
            <p>These go into the pantry, expiring after their shelf life:</p>
            <ul className="max-h-64 overflow-y-auto rounded-xl bg-ground px-3 py-2">
              {stocked.map((item) => (
                <li key={item.id} className="flex justify-between gap-3 py-0.5">
                  <span>{item.name}</span>
                  <span className="text-muted tabular-nums">
                    {item.amountText}
                  </span>
                </li>
              ))}
            </ul>
            <p className="text-xs text-muted">
              Foods always in stock, like salt, aren&apos;t tracked.
            </p>
          </div>
        )}
        {unchecked > 0 && (
          <p>
            {unchecked === 1
              ? "1 item isn't checked; it stays on the list."
              : `${unchecked} items aren't checked; they stay on the list.`}
          </p>
        )}
        <ErrorText error={error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Keep shopping
          </button>
          <button
            type="button"
            className={primaryButton}
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? "Saving…" : "Done shopping"}
          </button>
        </div>
      </div>
    </Modal>
  );
};

export default CompleteDialog;
