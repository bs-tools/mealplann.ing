import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import { iconButton, inputClass, labelClass } from "@components/ui/styles";
import { XMarkIcon } from "@heroicons/react/24/outline";
import React from "react";

/*
 * SideInput is a side of a recipe as edited: its food, and grams per
 * serving as typed.
 */
export interface SideInput {
  foodId: string;
  foodName: string;
  grams: string;
}

/*
 * DEFAULT_SIDE_GRAMS is the grams per serving a new side starts with,
 * about a cooked portion of rice or pasta.
 */
const DEFAULT_SIDE_GRAMS = "150";

// MAX_SIDES is how many sides a recipe can have, as the API allows.
const MAX_SIDES = 6;

// SidesKey identifies sides as edited, to tell whether they changed.
export const sidesKey = (sides: SideInput[]): string =>
  sides.map((side) => `${side.foodId}:${side.grams}`).join("|");

interface ServedWithProps {
  sides: SideInput[];
  onChange: (sides: SideInput[]) => void;
}

/**
 * ServedWith edits the sides a recipe is served with, like rice or
 * quinoa: foods a meal of it adds one of, with how much goes with a
 * serving.
 */
const ServedWith: React.FC<ServedWithProps> = ({ sides, onChange }) => (
  <div className="flex flex-col gap-2">
    <span className={labelClass}>Served with</span>
    {sides.map((side, index) => (
      <div key={side.foodId} className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-sm text-ink-soft">
          {side.foodName}
        </span>
        <label className="flex shrink-0 items-center gap-1.5 text-sm whitespace-nowrap text-muted">
          <span className="w-20">
            <input
              inputMode="decimal"
              aria-label={`Grams of ${side.foodName} per serving`}
              className={`${inputClass} text-right`}
              value={side.grams}
              onChange={(event) => {
                onChange(
                  sides.map((other, at) =>
                    at === index
                      ? { ...other, grams: event.target.value }
                      : other
                  )
                );
              }}
            />
          </span>
          g a serving
        </label>
        <button
          type="button"
          className={iconButton}
          aria-label={`Remove ${side.foodName}`}
          onClick={() => {
            onChange(sides.filter((other) => other.foodId !== side.foodId));
          }}
        >
          <XMarkIcon className="size-4" aria-hidden="true" />
        </button>
      </div>
    ))}
    {sides.length < MAX_SIDES && (
      <FoodPicker
        label="Add a side"
        value={null}
        onChange={(food: PickedFood) => {
          if (!sides.some((side) => side.foodId === food.id)) {
            onChange([
              ...sides,
              {
                foodId: food.id,
                foodName: food.name,
                grams: DEFAULT_SIDE_GRAMS,
              },
            ]);
          }
        }}
      />
    )}
    <span className="text-xs text-muted">
      Like rice or quinoa: a meal adds the one that fits the day best.
    </span>
  </div>
);

export default ServedWith;
