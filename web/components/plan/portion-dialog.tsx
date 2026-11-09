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
import { AmountUnit } from "gen/foods/v1/foods_pb";
import type { Person } from "gen/people/v1/people_pb";
import {
  createPortion,
  updatePortion,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedCook, PlannedPortion } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { formatDay, formatShortDate } from "helpers/format";
import { AMOUNT_UNITS, formatServings, optionValue } from "helpers/labels";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

type What = "cook" | "food";

// Slot is a person's eating time on a date.
export interface Slot {
  personId: string;
  eatingTimeId: string;
  date: string;
}

interface PortionDialogProps {
  // Where the portion is, or goes.
  slot: Slot;
  // The portion to change, or undefined to add one.
  portion?: PlannedPortion | undefined;
  people: Person[];
  // The dates to choose from, those of the week shown.
  dates: string[];
  /*
   * The cooks to eat leftovers of: the week's, and earlier ones whose
   * leftovers still keep. Those with no servings left aren't offered.
   */
  cooks: PlannedCook[];
  onClose: () => void;
}

/**
 * PortionDialog adds something a person eats at an eating time, leftovers
 * of a cook or an amount of a food like a handful of nuts, or changes or
 * moves one.
 */
const PortionDialog: React.FC<PortionDialogProps> = ({
  slot,
  portion,
  people,
  dates,
  cooks,
  onClose,
}) => {
  const queryClient = useQueryClient();
  // A portion being edited keeps its cook, even with nothing left.
  const offered = cooks.filter(
    (planned) =>
      planned.leftServings > 0 || planned.cook?.id === portion?.cookId
  );
  const [personId, setPersonId] = useState(slot.personId);
  const [eatingTimeId, setEatingTimeId] = useState(slot.eatingTimeId);
  const [date, setDate] = useState(slot.date);
  const initialWhat: What =
    portion === undefined || portion.foodId !== "" ? "food" : "cook";
  const [what, setWhat] = useState<What>(
    offered.length === 0 && portion === undefined ? "food" : initialWhat
  );
  const [cookId, setCookId] = useState(
    portion?.cookId ?? offered[0]?.cook?.id ?? ""
  );
  const [servings, setServings] = useState(
    portion !== undefined && portion.cookId !== ""
      ? String(portion.servings)
      : "1"
  );
  const [food, setFood] = useState<PickedFood | null>(
    portion !== undefined && portion.foodId !== ""
      ? { id: portion.foodId, name: portion.name }
      : null
  );
  const [amount, setAmount] = useState(
    portion !== undefined && portion.foodId !== "" ? String(portion.amount) : ""
  );
  const [unit, setUnit] = useState(
    portion !== undefined && portion.foodId !== ""
      ? portion.unit
      : AmountUnit.GRAMS
  );
  const [notes, setNotes] = useState(portion?.notes ?? "");
  const [invalid, setInvalid] = useState("");

  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [PlanService]);
    onClose();
  };
  const create = useMutation(createPortion, { onSuccess });
  const update = useMutation(updatePortion, { onSuccess });
  const pending = create.isPending || update.isPending;

  const person = people.find((other) => other.id === personId);
  const cookOptions = offered.map((planned) => {
    const left = planned.leftServings;
    return {
      label: `${planned.cook?.recipeName ?? ""} · ${formatShortDate(planned.cook?.date ?? "")}${
        left > 0 ? ` · ${formatServings(left)} left` : ""
      }`,
      value: planned.cook?.id ?? "",
    };
  });
  // A portion of a cook of another week keeps it as an option.
  if (
    portion !== undefined &&
    portion.cookId !== "" &&
    !cookOptions.some((option) => option.value === portion.cookId)
  ) {
    cookOptions.push({
      label: `${portion.name} · ${formatShortDate(portion.cookDate)}`,
      value: portion.cookId,
    });
  }
  const dateOptions = dates.includes(slot.date) ? dates : [slot.date, ...dates];

  const save = () => {
    setInvalid("");
    if (
      person === undefined ||
      !person.eatingTimes.some((et) => et.id === eatingTimeId)
    ) {
      setInvalid("Choose one of the person's eating times.");
      return;
    }
    let whatMessage;
    if (what === "cook") {
      const value = parseOptionalNumber(servings);
      if (cookId === "") {
        setInvalid("Choose a cook.");
        return;
      }
      if (value === null || value === undefined || value <= 0) {
        setInvalid("The servings must be a number more than 0, like 0.5.");
        return;
      }
      whatMessage = {
        case: "cook" as const,
        value: { cookId, servings: value },
      };
    } else {
      const value = parseOptionalNumber(amount);
      if (food === null) {
        setInvalid("Choose a food.");
        return;
      }
      if (value === null || value === undefined || value <= 0) {
        setInvalid("The amount must be a number more than 0.");
        return;
      }
      whatMessage = {
        case: "food" as const,
        value: { amount: value, foodId: food.id, unit },
      };
    }
    const message = {
      date,
      eatingTimeId,
      id: portion?.portionId ?? "",
      notes,
      personId,
      what: whatMessage,
    };
    if (portion === undefined) {
      create.mutate({ portion: message });
      return;
    }
    update.mutate({
      portion: message,
      updateMask: {
        paths: [
          "person_id",
          "eating_time_id",
          "date",
          whatMessage.case,
          "notes",
        ],
      },
    });
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={
        portion === undefined ? "Add to the plan" : `Change ${portion.name}`
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          <SelectField
            label="Person"
            value={personId}
            options={people.map((other) => ({
              label: other.name,
              value: other.id,
            }))}
            onChange={(value) => {
              setPersonId(value);
              const next = people.find((other) => other.id === value);
              const sameName = next?.eatingTimes.find(
                (et) =>
                  et.name ===
                  person?.eatingTimes.find((other) => other.id === eatingTimeId)
                    ?.name
              );
              setEatingTimeId(sameName?.id ?? next?.eatingTimes[0]?.id ?? "");
            }}
          />
          <SelectField
            label="Eating time"
            value={eatingTimeId}
            options={(person?.eatingTimes ?? []).map((et) => ({
              label: et.name,
              value: et.id,
            }))}
            onChange={setEatingTimeId}
          />
          <SelectField
            label="Day"
            value={date}
            options={dateOptions.map((value) => ({
              label: formatDay(value),
              value,
            }))}
            onChange={setDate}
          />
        </div>
        <div className="flex gap-2" role="group" aria-label="What's eaten">
          <button
            type="button"
            className={choiceClass(what === "food")}
            aria-pressed={what === "food"}
            onClick={() => {
              setWhat("food");
            }}
          >
            A food
          </button>
          <button
            type="button"
            className={choiceClass(what === "cook")}
            aria-pressed={what === "cook"}
            disabled={cookOptions.length === 0}
            onClick={() => {
              setWhat("cook");
            }}
          >
            A cook or leftovers
          </button>
        </div>
        {what === "food" ? (
          <div className="grid grid-cols-[1fr_5.5rem_5.5rem] items-end gap-2">
            <div className="flex flex-col gap-1">
              <span className={labelClass}>Food</span>
              <FoodPicker value={food} onChange={setFood} label="Food" />
            </div>
            <TextField
              label="Amount"
              value={amount}
              inputMode="decimal"
              onChange={setAmount}
            />
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
          </div>
        ) : (
          <div className="grid grid-cols-[1fr_6rem] items-end gap-2">
            <SelectField
              label="Cook"
              value={cookId}
              options={cookOptions}
              onChange={setCookId}
            />
            <TextField
              label="Servings"
              value={servings}
              inputMode="decimal"
              onChange={setServings}
            />
          </div>
        )}
        <TextField label="Notes" value={notes} onChange={setNotes} />
        {invalid !== "" && <p className="text-sm text-red-700">{invalid}</p>}
        <ErrorText error={create.error ?? update.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={primaryButton} disabled={pending}>
            {pending ? "Saving…" : "Save"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default PortionDialog;
