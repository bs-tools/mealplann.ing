import { TextField } from "@components/ui/fields";
import { ErrorText } from "@components/ui/query-error";
import { primaryButton, secondaryButton } from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { createShoppingList } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { addDays, isDateString, startOfWeek, today } from "helpers/dates";
import { formatDay } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

interface NewListFormProps {
  // Called with the ID of the created list.
  onCreated: (id: string) => void;
  onCancel?: (() => void) | undefined;
}

/**
 * NewListForm makes a shopping list for a week of the plan, from the day of
 * the shop: today, or next Monday.
 */
const NewListForm: React.FC<NewListFormProps> = ({ onCreated, onCancel }) => {
  const queryClient = useQueryClient();
  const now = today();
  const [start, setStart] = useState(now);
  const [end, setEnd] = useState(addDays(now, 6));
  const create = useMutation(createShoppingList, {
    onSuccess: async (response) => {
      await invalidateServices(queryClient, [ShoppingService]);
      onCreated(response.list?.id ?? "");
    },
  });
  const nextMonday = addDays(startOfWeek(now), 7);
  const valid = isDateString(start) && isDateString(end) && end >= start;
  return (
    <form
      className="flex flex-col gap-3"
      onSubmit={(event) => {
        event.preventDefault();
        create.mutate({ endDate: end, startDate: start });
      }}
    >
      <p className="text-sm text-gray-600">
        The list has what the plan needs for the meals of these days, less
        what&apos;s in the pantry, by store and aisle. Shop on the first day.
      </p>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className={secondaryButton}
          onClick={() => {
            setStart(now);
            setEnd(addDays(now, 6));
          }}
        >
          From today
        </button>
        <button
          type="button"
          className={secondaryButton}
          onClick={() => {
            setStart(nextMonday);
            setEnd(addDays(nextMonday, 6));
          }}
        >
          Next week, from {formatDay(nextMonday)}
        </button>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <TextField
          label="Shopping day"
          type="date"
          value={start}
          onChange={(value) => {
            setStart(value);
            if (isDateString(value)) {
              setEnd(addDays(value, 6));
            }
          }}
        />
        <TextField
          label="Meals until"
          type="date"
          value={end}
          onChange={setEnd}
        />
      </div>
      <ErrorText error={create.error} />
      <div className="flex justify-end gap-2">
        {onCancel !== undefined && (
          <button type="button" className={secondaryButton} onClick={onCancel}>
            Cancel
          </button>
        )}
        <button
          type="submit"
          className={primaryButton}
          disabled={!valid || create.isPending}
        >
          {create.isPending ? "Making the list…" : "Make the list"}
        </button>
      </div>
    </form>
  );
};

export default NewListForm;
