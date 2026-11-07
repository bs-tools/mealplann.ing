import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import { SelectField, TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import {
  addToPantry,
  updatePantryItem,
} from "gen/pantry/v1/pantry-PantryService_connectquery";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { AMOUNT_UNITS, optionValue } from "helpers/labels";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

interface PantryItemDialogProps {
  // The lot to edit, or undefined to add one.
  item?: PantryItem | undefined;
  onClose: () => void;
}

/**
 * PantryItemDialog adds an amount of a food to the pantry, in grams,
 * milliliters or pieces, or edits a lot: its grams, dates and notes.
 */
const PantryItemDialog: React.FC<PantryItemDialogProps> = ({
  item,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [food, setFood] = useState<PickedFood | null>(
    item === undefined ? null : { id: item.foodId, name: item.foodName }
  );
  const [amount, setAmount] = useState(
    item === undefined ? "" : String(Math.round(item.grams * 10) / 10)
  );
  const [unit, setUnit] = useState(AmountUnit.GRAMS);
  const [expiryDate, setExpiryDate] = useState(item?.expiryDate ?? "");
  const [openedDate, setOpenedDate] = useState(item?.openedDate ?? "");
  const [notes, setNotes] = useState(item?.notes ?? "");
  const [invalid, setInvalid] = useState("");
  const onSuccess = async () => {
    await invalidateServices(queryClient, [PantryService]);
    onClose();
  };
  const add = useMutation(addToPantry, { onSuccess });
  const update = useMutation(updatePantryItem, { onSuccess });
  const dirty =
    item === undefined
      ? food !== null || amount !== ""
      : amount !== String(Math.round(item.grams * 10) / 10) ||
        expiryDate !== item.expiryDate ||
        openedDate !== item.openedDate ||
        notes !== item.notes;

  const save = () => {
    setInvalid("");
    const value = parseOptionalNumber(amount);
    if (value === null || value === undefined || value <= 0) {
      setInvalid("The amount must be a number more than 0.");
      return;
    }
    if (item !== undefined) {
      update.mutate({
        item: {
          expiryDate,
          grams: value,
          id: item.id,
          notes,
          openedDate,
        },
        updateMask: {
          paths: ["grams", "expiry_date", "opened_date", "notes"],
        },
      });
      return;
    }
    if (food === null) {
      setInvalid("Choose a food.");
      return;
    }
    add.mutate({
      amount: value,
      expiryDate,
      foodId: food.id,
      notes,
      openedDate,
      unit,
    });
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={item === undefined ? "Add to the pantry" : item.foodName}
      dirty={dirty}
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        {item === undefined && (
          <div className="flex flex-col gap-1">
            <span className={labelClass}>Food</span>
            <FoodPicker value={food} label="Food" onChange={setFood} />
          </div>
        )}
        <div className="grid grid-cols-2 gap-3">
          <TextField
            label={item === undefined ? "Amount" : "Grams in stock"}
            value={amount}
            inputMode="decimal"
            autoFocus={item !== undefined}
            onChange={setAmount}
          />
          {item === undefined && (
            <SelectField
              label="Unit"
              value={String(unit)}
              options={AMOUNT_UNITS.map((option) => ({
                label: option.label,
                value: String(option.value),
              }))}
              onChange={(value) => {
                setUnit(optionValue(AMOUNT_UNITS, value, AmountUnit.GRAMS));
              }}
            />
          )}
          <TextField
            label="Expires"
            type="date"
            value={expiryDate}
            {...(item === undefined
              ? { hint: "Empty for today plus how long the food keeps." }
              : {})}
            onChange={setExpiryDate}
          />
          <TextField
            label="Opened"
            type="date"
            value={openedDate}
            onChange={setOpenedDate}
          />
        </div>
        <TextField
          label="Notes"
          value={notes}
          placeholder="In the freezer"
          onChange={setNotes}
        />
        {invalid !== "" && <p className="text-sm text-red-700">{invalid}</p>}
        <ErrorText error={add.error ?? update.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={add.isPending || update.isPending}
          >
            {add.isPending || update.isPending ? "Saving…" : "Save"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default PantryItemDialog;
