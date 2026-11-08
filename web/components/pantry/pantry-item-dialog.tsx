import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import { CheckboxField, SelectField, TextField } from "@components/ui/fields";
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
import { PantryService, Storage } from "gen/pantry/v1/pantry_pb";
import { AMOUNT_UNITS, STORAGES, optionValue } from "helpers/labels";
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
 * milliliters or pieces, or edits a lot: its grams, dates, where it's kept,
 * its label and notes.
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
  const [storage, setStorage] = useState(item?.storage ?? Storage.UNSPECIFIED);
  const [location, setLocation] = useState(item?.location ?? "");
  const [frozenDate, setFrozenDate] = useState(item?.frozenDate ?? "");
  const [label, setLabel] = useState(item?.label ?? "");
  const [opened, setOpened] = useState(item?.opened ?? false);
  const [estimated, setEstimated] = useState(item?.estimated ?? false);
  const frozen = storage === Storage.FREEZER;
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
        notes !== item.notes ||
        storage !== item.storage ||
        location !== item.location ||
        frozenDate !== item.frozenDate ||
        label !== item.label ||
        opened !== item.opened ||
        estimated !== item.estimated;

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
          estimated,
          expiryDate,
          frozenDate: frozen ? frozenDate : "",
          grams: value,
          id: item.id,
          label,
          location,
          notes,
          opened: opened || openedDate !== "",
          openedDate,
          storage,
        },
        updateMask: {
          paths: [
            "grams",
            "expiry_date",
            "opened_date",
            "notes",
            "storage",
            "location",
            "frozen_date",
            "label",
            "opened",
            "estimated",
          ],
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
      estimated,
      expiryDate,
      foodId: food.id,
      frozenDate: frozen ? frozenDate : "",
      label,
      location,
      notes,
      opened,
      openedDate,
      storage,
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
          <SelectField
            label="Kept in"
            value={String(storage)}
            options={STORAGES.map((option) => ({
              label: option.label,
              value: String(option.value),
            }))}
            onChange={(value) => {
              setStorage(optionValue(STORAGES, value, Storage.UNSPECIFIED));
            }}
          />
          {frozen ? (
            <TextField
              label="Frozen on"
              type="date"
              value={frozenDate}
              hint="Empty for today."
              onChange={setFrozenDate}
            />
          ) : (
            <span />
          )}
          <TextField
            label="Where"
            value={location}
            placeholder="Chest freezer"
            onChange={setLocation}
          />
          <TextField
            label="Brand or label"
            value={label}
            placeholder="Sriracha"
            onChange={setLabel}
          />
        </div>
        <div className="flex flex-col gap-2">
          {openedDate === "" && (
            <CheckboxField
              label="Opened, on a day I don't know"
              checked={opened}
              onChange={setOpened}
            />
          )}
          <CheckboxField
            label="The amount is a guess"
            checked={estimated}
            onChange={setEstimated}
          />
        </div>
        <TextField
          label="Notes"
          value={notes}
          placeholder="Half used"
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
