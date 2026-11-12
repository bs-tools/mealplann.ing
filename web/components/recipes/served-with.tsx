import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import type { PickedRecipe } from "@components/recipes/recipe-picker";
import RecipePicker from "@components/recipes/recipe-picker";
import {
  choiceClass,
  iconButton,
  inputClass,
  labelClass,
} from "@components/ui/styles";
import { XMarkIcon } from "@heroicons/react/24/outline";
import type { RecipeSide } from "gen/recipes/v1/recipes_pb";
import React, { useState } from "react";

/*
 * SideInput is a side of a recipe as edited: a food, with grams per
 * serving, or a recipe, with servings per serving, as typed.
 */
export interface SideInput {
  kind: "food" | "recipe";
  id: string;
  name: string;
  amount: string;
}

/*
 * DEFAULT_SIDE_GRAMS is the grams per serving a new food side starts
 * with, about a cooked portion of rice or pasta, and DEFAULT_SIDE_SERVINGS
 * the servings of a recipe side: one with each serving.
 */
const DEFAULT_SIDE_GRAMS = "150";
const DEFAULT_SIDE_SERVINGS = "1";

// MAX_SIDES is how many sides a recipe can have, as the API allows.
const MAX_SIDES = 6;

// SidesKey identifies sides as edited, to tell whether they changed.
export const sidesKey = (sides: SideInput[]): string =>
  sides.map((side) => `${side.kind}:${side.id}:${side.amount}`).join("|");

// SidesFromRecipe reads a recipe's sides to edit them.
export const sidesFromRecipe = (sides: RecipeSide[]): SideInput[] =>
  sides.map((side) =>
    side.recipeId === ""
      ? {
          amount: String(side.gramsPerServing),
          id: side.foodId,
          kind: "food",
          name: side.foodName,
        }
      : {
          amount: String(side.servingsPerServing),
          id: side.recipeId,
          kind: "recipe",
          name: side.recipeName,
        }
  );

// SidesToSave converts sides as edited to save them.
export const sidesToSave = (
  sides: SideInput[]
): {
  foodId: string;
  gramsPerServing: number;
  recipeId: string;
  servingsPerServing: number;
}[] =>
  sides.map((side) => {
    const amount = Number.parseFloat(side.amount.replace(",", "."));
    return side.kind === "food"
      ? {
          foodId: side.id,
          gramsPerServing: amount,
          recipeId: "",
          servingsPerServing: 0,
        }
      : {
          foodId: "",
          gramsPerServing: 0,
          recipeId: side.id,
          servingsPerServing: amount,
        };
  });

interface ServedWithProps {
  sides: SideInput[];
  // The recipe being edited, which can't be its own side.
  recipeId?: string | undefined;
  onChange: (sides: SideInput[]) => void;
}

/**
 * ServedWith edits the sides a recipe is served with: foods, like bread,
 * with how many grams go with a serving, or recipes, like a pot of rice
 * or a salad, with how many of their servings do. A meal adds the one
 * that fits the day best, and a recipe side is cooked for it.
 */
const ServedWith: React.FC<ServedWithProps> = ({
  sides,
  recipeId,
  onChange,
}) => {
  const [adding, setAdding] = useState<"food" | "recipe">("recipe");
  const add = (kind: "food" | "recipe", id: string, name: string) => {
    if (!sides.some((side) => side.kind === kind && side.id === id)) {
      onChange([
        ...sides,
        {
          amount: kind === "food" ? DEFAULT_SIDE_GRAMS : DEFAULT_SIDE_SERVINGS,
          id,
          kind,
          name,
        },
      ]);
    }
  };
  return (
    <div className="flex flex-col gap-2">
      <span className={labelClass}>Served with</span>
      {sides.map((side, index) => (
        <div
          key={`${side.kind}:${side.id}`}
          className="flex items-center gap-2"
        >
          <span className="min-w-0 flex-1 truncate text-sm text-ink-soft">
            {side.name}
          </span>
          <label className="flex shrink-0 items-center gap-1.5 text-sm whitespace-nowrap text-muted">
            <span className="w-20">
              <input
                inputMode="decimal"
                aria-label={
                  side.kind === "food"
                    ? `Grams of ${side.name} per serving`
                    : `Servings of ${side.name} per serving`
                }
                className={`${inputClass} text-right`}
                value={side.amount}
                onChange={(event) => {
                  onChange(
                    sides.map((other, at) =>
                      at === index
                        ? { ...other, amount: event.target.value }
                        : other
                    )
                  );
                }}
              />
            </span>
            {side.kind === "food" ? "g a serving" : "servings each"}
          </label>
          <button
            type="button"
            className={iconButton}
            aria-label={`Remove ${side.name}`}
            onClick={() => {
              onChange(sides.filter((_other, at) => at !== index));
            }}
          >
            <XMarkIcon className="size-4" aria-hidden="true" />
          </button>
        </div>
      ))}
      {sides.length < MAX_SIDES && (
        <div className="flex flex-col gap-2">
          <div className="flex gap-2" role="group" aria-label="Add a side">
            <button
              type="button"
              className={choiceClass(adding === "recipe")}
              aria-pressed={adding === "recipe"}
              onClick={() => {
                setAdding("recipe");
              }}
            >
              A recipe
            </button>
            <button
              type="button"
              className={choiceClass(adding === "food")}
              aria-pressed={adding === "food"}
              onClick={() => {
                setAdding("food");
              }}
            >
              A food
            </button>
          </div>
          {adding === "recipe" ? (
            <RecipePicker
              label="Add a recipe side"
              value={null}
              exclude={recipeId}
              onChange={(picked: PickedRecipe) => {
                add("recipe", picked.id, picked.name);
              }}
            />
          ) : (
            <FoodPicker
              label="Add a food side"
              value={null}
              onChange={(food: PickedFood) => {
                add("food", food.id, food.name);
              }}
            />
          )}
        </div>
      )}
      <span className="text-xs text-muted">
        Like a pot of rice, a salad or bread: a meal adds the one that fits the
        day best, and a recipe side is cooked with it.
      </span>
    </div>
  );
};

export default ServedWith;
