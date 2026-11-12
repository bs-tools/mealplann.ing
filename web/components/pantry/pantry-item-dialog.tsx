import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import ConfirmButton from "@components/ui/confirm-button";
import { CheckboxField, SelectField, TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  inputClass,
  labelClass,
  ghostDangerButton,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import {
  addToPantry,
  removeFromPantry,
  updatePantryItem,
} from "gen/pantry/v1/pantry-PantryService_connectquery";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { PantryService, Storage } from "gen/pantry/v1/pantry_pb";
import { formatGrams } from "helpers/format";
import { AMOUNT_UNITS, STORAGES, optionValue } from "helpers/labels";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

// StockUnit is a unit a lot's amount is edited in.
interface StockUnit {
  key: "packages" | "pieces" | "grams";
  label: string;
  // Its weight, to convert to and from grams.
  grams: number;
  // How much the stepper changes it by.
  step: number;
}

/**
 * StockUnitsOf returns the units a lot can be edited in, the one it's
 * bought in first: packages if its food has a package size, pieces if it
 * has a weight per piece, and always grams.
 */
const stockUnitsOf = (item: PantryItem): StockUnit[] => {
  const units: StockUnit[] = [];
  if (item.packageGrams > 0) {
    units.push({
      grams: item.packageGrams,
      key: "packages",
      label: "Packages",
      step: 1,
    });
  }
  if (item.gramsPerPiece > 0) {
    units.push({
      grams: item.gramsPerPiece,
      key: "pieces",
      label: "Pieces",
      step: 1,
    });
  }
  units.push({ grams: 1, key: "grams", label: "Grams", step: 50 });
  return units;
};

// AmountIn says grams in a unit, to two decimals, e.g. "2.5".
const amountIn = (grams: number, unit: StockUnit): string =>
  String(Math.round((grams / unit.grams) * 100) / 100);

interface PantryItemDialogProps {
  // The lot to edit, or undefined to add one.
  item?: PantryItem | undefined;
  onClose: () => void;
}

/**
 * PantryItemDialog adds an amount of a food to the pantry, in grams,
 * milliliters or pieces, or edits a lot: how much is left, in the packages
 * or pieces it's bought in where its food has them, its dates, where it's
 * kept, its label and notes.
 */
const PantryItemDialog: React.FC<PantryItemDialogProps> = ({
  item,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [food, setFood] = useState<PickedFood | null>(
    item === undefined ? null : { id: item.foodId, name: item.foodName }
  );
  const stockUnits = item === undefined ? [] : stockUnitsOf(item);
  // The unit a lot's amount is edited in: what it's bought in, at first.
  const [stockUnit, setStockUnit] = useState<StockUnit | undefined>(
    stockUnits[0]
  );
  const [amount, setAmount] = useState(
    item === undefined || stockUnit === undefined
      ? ""
      : amountIn(item.grams, stockUnit)
  );
  // The grams the amount comes to, in the unit it's edited in.
  const amountGrams = (): number | undefined => {
    const value = parseOptionalNumber(amount);
    return value === null || value === undefined
      ? undefined
      : value * (stockUnit?.grams ?? 1);
  };
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
  const remove = useMutation(removeFromPantry, { onSuccess });
  const dirty =
    item === undefined
      ? food !== null || amount !== ""
      : Math.abs((amountGrams() ?? -1) - item.grams) > 0.05 ||
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
          grams: value * (stockUnit?.grams ?? 1),
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
        {item !== undefined && stockUnit !== undefined && (
          <div className="flex flex-col gap-2">
            <span className={labelClass}>In stock</span>
            {stockUnits.length > 1 && (
              <Segmented
                label="Unit"
                value={stockUnit.key}
                options={stockUnits.map((option) => ({
                  label: option.label,
                  value: option.key,
                }))}
                onChange={(key) => {
                  const next = stockUnits.find((option) => option.key === key);
                  const grams = amountGrams();
                  if (next !== undefined) {
                    setStockUnit(next);
                    if (grams !== undefined) {
                      setAmount(amountIn(grams, next));
                    }
                  }
                }}
              />
            )}
            <div className="flex items-center gap-2">
              <button
                type="button"
                className={`${secondaryButton} size-11 px-0 text-lg`}
                aria-label={`One ${stockUnit.key === "grams" ? "step" : stockUnit.label.toLowerCase().replace(/s$/u, "")} less`}
                title="Less"
                onClick={() => {
                  const value = parseOptionalNumber(amount) ?? 0;
                  setAmount(String(Math.max(value - stockUnit.step, 0)));
                }}
              >
                −
              </button>
              <input
                aria-label={`${stockUnit.label} in stock`}
                inputMode="decimal"
                className={`${inputClass} text-center text-base`}
                value={amount}
                onChange={(event) => {
                  setAmount(event.target.value);
                }}
              />
              <button
                type="button"
                className={`${secondaryButton} size-11 px-0 text-lg`}
                aria-label="More"
                title="More"
                onClick={() => {
                  const value = parseOptionalNumber(amount) ?? 0;
                  setAmount(String(value + stockUnit.step));
                }}
              >
                +
              </button>
            </div>
            {stockUnit.key !== "grams" && (
              <span className="text-xs text-muted">
                1 {stockUnit.key === "packages" ? "package" : "piece"} is{" "}
                {formatGrams(stockUnit.grams)}
                {amountGrams() === undefined
                  ? ""
                  : `, so ${formatGrams(amountGrams() ?? 0)} in all`}
                .
              </span>
            )}
          </div>
        )}
        <div className="grid grid-cols-2 gap-3">
          {item === undefined && (
            <TextField
              label="Amount"
              value={amount}
              inputMode="decimal"
              onChange={setAmount}
            />
          )}
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
        {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
        <ErrorText error={add.error ?? update.error ?? remove.error} />
        <div className="flex justify-end gap-2">
          {item !== undefined && (
            <ConfirmButton
              className={`${ghostDangerButton} mr-auto`}
              title={`Remove ${item.foodName}?`}
              message="Remove it from the pantry when it's used up or thrown away."
              confirmLabel="Remove"
              disabled={remove.isPending}
              onConfirm={() => {
                remove.mutate({ id: item.id });
              }}
            >
              Remove
            </ConfirmButton>
          )}
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
