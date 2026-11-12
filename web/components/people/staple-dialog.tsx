import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import type { PickedRecipe } from "@components/recipes/recipe-picker";
import RecipePicker from "@components/recipes/recipe-picker";
import { CheckboxField, SelectField, TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  choiceClass,
  inputClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import type { Person } from "gen/people/v1/people_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import {
  createStaple,
  updateStaple,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { Staple, Weekday } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { today } from "helpers/dates";
import {
  AMOUNT_UNITS,
  WEEKDAYS,
  householdMealLabel,
  optionValue,
} from "helpers/labels";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

type What = "food" | "recipe";

interface StapleDialogProps {
  // The person whose staple it is.
  person: Person;
  // The staple to edit, or undefined to create one.
  staple?: Staple | undefined;
  onClose: () => void;
}

/**
 * StapleDialog creates or edits a staple: something a person eats
 * regularly at one of their eating times, like soy milk at breakfast, on
 * some days of the week.
 */
const StapleDialog: React.FC<StapleDialogProps> = ({
  person,
  staple,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const initialWhat: What = staple?.what.case === "recipe" ? "recipe" : "food";
  const initialFood =
    staple?.what.case === "food"
      ? { id: staple.what.value.foodId, name: staple.name }
      : null;
  const [eatingTimeId, setEatingTimeId] = useState(
    staple?.eatingTimeId ?? person.eatingTimes[0]?.id ?? ""
  );
  const [what, setWhat] = useState<What>(initialWhat);
  const [food, setFood] = useState<PickedFood | null>(initialFood);
  const [amount, setAmount] = useState(
    staple?.what.case === "food" ? String(staple.what.value.amount) : ""
  );
  const [unit, setUnit] = useState(
    staple?.what.case === "food" ? staple.what.value.unit : AmountUnit.GRAMS
  );
  const [recipe, setRecipe] = useState<PickedRecipe | null>(
    staple?.what.case === "recipe"
      ? { id: staple.what.value.recipeId, name: staple.name }
      : null
  );
  const recipeId = recipe?.id ?? "";
  const [servings, setServings] = useState(
    staple?.what.case === "recipe" ? String(staple.what.value.servings) : "1"
  );
  const [weekdays, setWeekdays] = useState<Weekday[]>(staple?.weekdays ?? []);
  const [startDate, setStartDate] = useState(staple?.startDate ?? today());
  const [endDate, setEndDate] = useState(staple?.endDate ?? "");
  const [notes, setNotes] = useState(staple?.notes ?? "");
  const [replacesMeal, setReplacesMeal] = useState(
    staple?.replacesMeal ?? false
  );
  // The shared meal at the eating time, which the staple can replace.
  const sharedMeal = householdMealLabel(
    person.eatingTimes.find((et) => et.id === eatingTimeId)?.meal ??
      HouseholdMeal.UNSPECIFIED
  );
  const [invalid, setInvalid] = useState("");

  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [PlanService]);
    onClose();
  };
  const create = useMutation(createStaple, { onSuccess });
  const update = useMutation(updateStaple, { onSuccess });
  const pending = create.isPending || update.isPending;

  const toggleWeekday = (day: Weekday) => {
    setWeekdays(
      weekdays.includes(day)
        ? weekdays.filter((other) => other !== day)
        : WEEKDAYS.map((option) => option.value).filter(
            (other) => other === day || weekdays.includes(other)
          )
    );
  };

  const save = () => {
    setInvalid("");
    let whatMessage;
    if (what === "food") {
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
    } else {
      const value = parseOptionalNumber(servings);
      if (recipeId === "") {
        setInvalid("Choose a recipe.");
        return;
      }
      if (value === null || value === undefined || value <= 0) {
        setInvalid("The servings must be a number more than 0, like 0.5.");
        return;
      }
      whatMessage = {
        case: "recipe" as const,
        value: { recipeId, servings: value },
      };
    }
    const message = {
      eatingTimeId,
      endDate,
      id: staple?.id ?? "",
      notes,
      personId: person.id,
      replacesMeal: sharedMeal !== "" && replacesMeal,
      startDate,
      weekdays,
      what: whatMessage,
    };
    if (staple === undefined) {
      create.mutate({ staple: message });
      return;
    }
    update.mutate({
      staple: message,
      updateMask: {
        paths: [
          "eating_time_id",
          whatMessage.case,
          "weekdays",
          "start_date",
          "end_date",
          "notes",
          "replaces_meal",
        ],
      },
    });
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={
        staple === undefined
          ? `New staple for ${person.name}`
          : `Edit ${staple.name}`
      }
    >
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        <p className="text-sm text-muted">
          Something {person.name} eats regularly. It shows in every week&apos;s
          plan, and can be skipped on a day.
        </p>
        <SelectField
          label="Eating time"
          value={eatingTimeId}
          options={person.eatingTimes.map((et) => ({
            label: et.name,
            value: et.id,
          }))}
          onChange={setEatingTimeId}
        />
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
            className={choiceClass(what === "recipe")}
            aria-pressed={what === "recipe"}
            onClick={() => {
              setWhat("recipe");
            }}
          >
            A recipe
          </button>
        </div>
        {what === "food" ? (
          <div className="grid grid-cols-[1fr_6rem_6rem] items-end gap-2">
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
            <div className="flex flex-col gap-1">
              <span className={labelClass}>Recipe</span>
              <RecipePicker
                value={recipe}
                onChange={setRecipe}
                label="Recipe"
              />
            </div>
            <TextField
              label="Servings"
              value={servings}
              inputMode="decimal"
              onChange={setServings}
            />
          </div>
        )}
        <div className="flex flex-col gap-1">
          <span className={labelClass}>Days (none for every day)</span>
          <div className="flex flex-wrap gap-1.5">
            {WEEKDAYS.map((day) => (
              <button
                key={day.value}
                type="button"
                aria-pressed={weekdays.includes(day.value)}
                className={choiceClass(weekdays.includes(day.value))}
                onClick={() => {
                  toggleWeekday(day.value);
                }}
              >
                {day.label}
              </button>
            ))}
          </div>
        </div>
        <div className="grid grid-cols-2 gap-2">
          <label className="flex flex-col gap-1">
            <span className={labelClass}>From</span>
            <input
              type="date"
              className={inputClass}
              value={startDate}
              onChange={(event) => {
                setStartDate(event.target.value);
              }}
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className={labelClass}>Until (optional)</span>
            <input
              type="date"
              className={inputClass}
              value={endDate}
              onChange={(event) => {
                setEndDate(event.target.value);
              }}
            />
          </label>
        </div>
        {sharedMeal !== "" && (
          <CheckboxField
            label={`Eaten instead of the shared ${sharedMeal.toLowerCase()}, so it's planned for the others only`}
            checked={replacesMeal}
            onChange={setReplacesMeal}
          />
        )}
        <TextField label="Notes" value={notes} onChange={setNotes} />
        {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
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

export default StapleDialog;
