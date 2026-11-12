import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import { SelectField, TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  choiceClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { addManualItem } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import type { Store } from "gen/shopping/v1/shopping_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

interface AddItemDialogProps {
  listId: string;
  stores: Store[];
  onClose: () => void;
}

/**
 * AddItemDialog adds an item to a shopping list by hand: one of the
 * household's foods, or anything else, like dish soap.
 */
const AddItemDialog: React.FC<AddItemDialogProps> = ({
  listId,
  stores,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [isFood, setIsFood] = useState(true);
  const [food, setFood] = useState<PickedFood | null>(null);
  const [text, setText] = useState("");
  const [amount, setAmount] = useState("");
  const [grams, setGrams] = useState("");
  const [storeId, setStoreId] = useState("");
  const [note, setNote] = useState("");
  const [invalid, setInvalid] = useState("");
  const add = useMutation(addManualItem, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [ShoppingService]);
      onClose();
    },
  });
  const dirty =
    food !== null ||
    text !== "" ||
    amount !== "" ||
    grams !== "" ||
    note !== "";

  const save = () => {
    setInvalid("");
    const weight = parseOptionalNumber(grams);
    if (weight === null) {
      setInvalid("The weight must be a number of grams, or empty.");
      return;
    }
    if (isFood && food === null) {
      setInvalid("Choose a food.");
      return;
    }
    if (!isFood && text.trim() === "") {
      setInvalid("Say what to buy.");
      return;
    }
    add.mutate({
      amountText: amount,
      grams: isFood ? (weight ?? 0) : 0,
      listId,
      note,
      storeId,
      what:
        isFood && food !== null
          ? { case: "foodId", value: food.id }
          : { case: "text", value: text },
    });
  };

  return (
    <Modal open onClose={onClose} title="Add to the list" dirty={dirty}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        <div className="flex gap-2">
          <button
            type="button"
            className={choiceClass(isFood)}
            onClick={() => {
              setIsFood(true);
            }}
          >
            A food
          </button>
          <button
            type="button"
            className={choiceClass(!isFood)}
            onClick={() => {
              setIsFood(false);
            }}
          >
            Something else
          </button>
        </div>
        {isFood ? (
          <div className="flex flex-col gap-1">
            <span className={labelClass}>Food</span>
            <FoodPicker value={food} label="Food" onChange={setFood} />
          </div>
        ) : (
          <TextField
            label="What"
            value={text}
            placeholder="Dish soap"
            autoFocus
            onChange={setText}
          />
        )}
        <div className="grid grid-cols-2 gap-3">
          <TextField
            label="Amount"
            value={amount}
            placeholder="2 rolls"
            onChange={setAmount}
          />
          {isFood && (
            <TextField
              label="Weight (g)"
              value={grams}
              inputMode="decimal"
              hint="Goes into the pantry when you're done."
              onChange={setGrams}
            />
          )}
        </div>
        {stores.length > 1 && (
          <SelectField
            label="Store"
            value={storeId}
            options={[
              {
                label: isFood ? "The food's usual store" : "The main store",
                value: "",
              },
              ...stores.map((store) => ({
                label: store.name,
                value: store.id,
              })),
            ]}
            onChange={setStoreId}
          />
        )}
        <TextField label="Note" value={note} onChange={setNote} />
        {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
        <ErrorText error={add.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={add.isPending}
          >
            {add.isPending ? "Adding…" : "Add"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default AddItemDialog;
