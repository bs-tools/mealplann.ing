import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import RecipePicker from "@components/recipes/recipe-picker";
import type { SideInput } from "@components/recipes/served-with";
import ServedWith, {
  sidesFromRecipe,
  sidesKey,
  sidesToSave,
} from "@components/recipes/served-with";
import BackLink from "@components/ui/back-link";
import Card from "@components/ui/card";
import { CheckboxField, TextAreaField, TextField } from "@components/ui/fields";
import { PageHeader } from "@components/ui/misc";
import { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  iconButton,
  inputClass,
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import UnsavedChangesGuard, {
  leaveUnsaved,
} from "@components/ui/unsaved-changes-guard";
import { useMutation } from "@connectrpc/connect-query";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  PlusIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import {
  createRecipe,
  updateRecipe,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { Ingredient, Recipe, Step } from "gen/recipes/v1/recipes_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { AMOUNT_UNITS } from "helpers/labels";
import {
  hoursMinutesInput,
  parseDuration,
  parseOptionalNumber,
  parseQuantity,
} from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useNavigate } from "react-router";

// IngredientInput is an ingredient being edited.
interface IngredientInput {
  // A key for React, as ingredients move.
  key: number;
  // Whether it's a component: another recipe.
  isRecipe: boolean;
  food: PickedFood | null;
  recipe: PickedFood | null;
  amount: string;
  // The unit of the amount, unspecified for servings of a component.
  unit: AmountUnit;
  note: string;
  originalText: string;
  optional: boolean;
}

/**
 * StepStart is when a step starts: after the step before it, right away,
 * or after the steps with these keys.
 */
type StepStart = "previous" | "start" | number[];

// StepInput is a step being edited, with its structure.
interface StepInput {
  key: number;
  text: string;
  section: string;
  // The duration in minutes, empty if unknown.
  minutes: string;
  waiting: boolean;
  // The keys of the ingredients it uses.
  ingredientKeys: number[];
  /*
   * How much of an ingredient it uses, by the ingredient's key, for one
   * split between steps; empty or missing for all of it, or the rest.
   */
  amounts: Record<number, string>;
  start: StepStart;
  /*
   * How many minutes into the steps it follows it starts, or with
   * offsetEnd, before they end it ends; empty to start once they're done.
   */
  offsetMinutes: string;
  offsetEnd: boolean;
}

// The keys of ingredients and steps added, unique on the page.
let nextKey = 0;
const newKey = (): number => {
  nextKey += 1;
  return nextKey;
};

const emptyIngredient = (): IngredientInput => ({
  amount: "",
  food: null,
  isRecipe: false,
  key: newKey(),
  note: "",
  optional: false,
  originalText: "",
  recipe: null,
  unit: AmountUnit.GRAMS,
});

/**
 * IngredientUnit returns the unit to edit an ingredient's amount in: its
 * own, or grams for an unmatched draft ingredient without one.
 */
const ingredientUnit = (
  ingredient: Ingredient,
  isRecipe: boolean
): AmountUnit => {
  if (isRecipe || ingredient.unit !== AmountUnit.UNSPECIFIED) {
    return ingredient.unit;
  }
  return AmountUnit.GRAMS;
};

/*
 * YIELD_UNITS are the units of what a recipe makes. A recipe has no package
 * size, so it can't make packages.
 */
const YIELD_UNITS = AMOUNT_UNITS.filter(
  (option) => option.value !== AmountUnit.PACKAGES
);

/*
 * COMPONENT_UNITS are the units of a component's amount: servings of its
 * recipe, or what it makes, by weight, volume or pieces.
 */
const COMPONENT_UNITS: { value: AmountUnit; label: string }[] = [
  { label: "servings", value: AmountUnit.UNSPECIFIED },
  ...YIELD_UNITS,
];

const ingredientInputs = (recipe: Recipe | undefined): IngredientInput[] => {
  if (recipe === undefined) {
    return [emptyIngredient()];
  }
  return recipe.ingredients.map((ingredient) => {
    const isRecipe = ingredient.recipeId !== "";
    return {
      // An unmatched draft ingredient's unknown amount is empty.
      amount: ingredient.amount === 0 ? "" : String(ingredient.amount),
      food: isRecipe
        ? null
        : { id: ingredient.foodId, name: ingredient.foodName },
      isRecipe,
      key: newKey(),
      note: ingredient.note,
      optional: ingredient.optional,
      originalText: ingredient.originalText,
      recipe: isRecipe
        ? { id: ingredient.recipeId, name: ingredient.recipeName }
        : null,
      unit: ingredientUnit(ingredient, isRecipe),
    };
  });
};

const emptyStep = (): StepInput => ({
  amounts: {},
  ingredientKeys: [],
  key: newKey(),
  minutes: "",
  offsetEnd: false,
  offsetMinutes: "",
  section: "",
  start: "previous",
  text: "",
  waiting: false,
});

/**
 * StepInputs returns a recipe's steps to edit, with the keys of the
 * ingredients being edited.
 */
const stepInputs = (
  recipe: Recipe | undefined,
  ingredients: IngredientInput[]
): StepInput[] => {
  if (recipe === undefined || recipe.instructions.length === 0) {
    return [emptyStep()];
  }
  const keys = recipe.instructions.map(() => newKey());
  return recipe.instructions.map((step, index) => {
    let start: StepStart = "previous";
    if (step.fromStart) {
      start = "start";
    } else if (step.afterStepIndexes.length > 0) {
      start = step.afterStepIndexes.flatMap((other) => keys[other] ?? []);
    }
    const amounts: Record<number, string> = {};
    for (const [at, position] of step.ingredientIndexes.entries()) {
      const key = ingredients[position]?.key;
      const amount = step.ingredientAmounts[at] ?? 0;
      if (key !== undefined && amount > 0) {
        amounts[key] = String(amount);
      }
    }
    return {
      amounts,
      ingredientKeys: step.ingredientIndexes.flatMap(
        (position) => ingredients[position]?.key ?? []
      ),
      key: keys[index] ?? newKey(),
      minutes: minutesInput(step.durationSeconds),
      offsetEnd: step.endBeforeSeconds !== undefined,
      offsetMinutes: offsetInput(step),
      section: step.section,
      start,
      text: step.text,
      waiting: step.waiting,
    };
  });
};

// OffsetInput formats a step's offset to edit, "0" to end as others do.
const offsetInput = (step: Step): string => {
  if (step.endBeforeSeconds === undefined) {
    return minutesInput(step.startOffsetSeconds);
  }
  return step.endBeforeSeconds > 0 ? minutesInput(step.endBeforeSeconds) : "0";
};

// MinutesInput formats seconds as minutes to edit, empty for none.
const minutesInput = (seconds: number): string =>
  seconds > 0 ? String(Math.round((seconds / 60) * 100) / 100) : "";

// Seconds parses minutes as seconds, 0 for empty or invalid.
const seconds = (minutes: string): number => {
  const value = parseOptionalNumber(minutes) ?? 0;
  return Math.round(value * 60);
};

// SavedStep is a step as it's saved.
interface SavedStep {
  text: string;
  section: string;
  ingredientIndexes: number[];
  ingredientAmounts: number[];
  durationSeconds: number;
  startOffsetSeconds: number;
  endBeforeSeconds?: number;
  waiting: boolean;
  fromStart: boolean;
  afterStepIndexes: number[];
}

// StepsToSave are the steps with text, as they're saved.
const stepsToSave = (
  steps: StepInput[],
  ingredients: IngredientInput[]
): SavedStep[] => {
  const kept = steps.filter((step) => step.text.trim() !== "");
  const positions = new Map(
    ingredients.map((ingredient, position) => [ingredient.key, position])
  );
  return kept.map((step, index) => {
    const used = step.ingredientKeys.filter((key) => positions.has(key));
    const amounts = used.map(
      (key) => parseQuantity(step.amounts[key] ?? "") ?? 0
    );
    const after =
      typeof step.start === "string"
        ? []
        : step.start
            .map((key) => kept.findIndex((other) => other.key === key))
            .filter((other) => other >= 0 && other < index);
    const saved: SavedStep = {
      afterStepIndexes: after,
      durationSeconds: seconds(step.minutes),
      fromStart: step.start === "start",
      ingredientAmounts: amounts.some((amount) => amount > 0) ? amounts : [],
      ingredientIndexes: used.flatMap((key) => positions.get(key) ?? []),
      section: step.section.trim(),
      startOffsetSeconds: seconds(step.offsetMinutes),
      text: step.text.trim(),
      waiting: step.waiting,
    };
    // A step ending before others needs some to follow.
    if (step.offsetEnd && step.start !== "start" && index > 0) {
      saved.endBeforeSeconds = seconds(step.offsetMinutes);
      saved.startOffsetSeconds = 0;
    }
    return saved;
  });
};

// ParseTags splits comma separated tags.
const parseTags = (text: string): string[] => {
  const tags: string[] = [];
  for (const part of text.split(",")) {
    const tag = part.trim().toLowerCase();
    if (tag !== "" && !tags.includes(tag)) {
      tags.push(tag);
    }
  }
  return tags;
};

// The ingredients and steps as they're saved, to compare them.
const ingredientsKey = (ingredients: IngredientInput[]): string =>
  JSON.stringify(
    ingredients.map((ingredient) => [
      ingredient.isRecipe,
      ingredient.food?.id ?? "",
      ingredient.recipe?.id ?? "",
      ingredient.amount.trim(),
      ingredient.unit,
      ingredient.note.trim(),
      ingredient.originalText.trim(),
      ingredient.optional,
    ])
  );

const stepsKey = (steps: StepInput[], ingredients: IngredientInput[]): string =>
  JSON.stringify(stepsToSave(steps, ingredients));

// Move returns list with the item at index moved by offset.
const move = <Item,>(list: Item[], index: number, offset: number): Item[] => {
  const target = index + offset;
  if (target < 0 || target >= list.length) {
    return list;
  }
  const result = [...list];
  const [item] = result.splice(index, 1);
  if (item !== undefined) {
    result.splice(target, 0, item);
  }
  return result;
};

/**
 * IngredientLabel names an ingredient being edited: its food or recipe, or
 * the original wording of one without a food yet, "" if it has none.
 */
const ingredientLabel = (ingredient: IngredientInput): string =>
  (ingredient.isRecipe ? ingredient.recipe?.name : ingredient.food?.name) ??
  ingredient.originalText.trim();

interface StepEditorProps {
  step: StepInput;
  index: number;
  // The steps before it, which it can start after.
  earlier: StepInput[];
  ingredients: IngredientInput[];
  onChange: (change: Partial<StepInput>) => void;
  onMoveUp: () => void;
  onRemove: () => void;
}

/**
 * StepEditor edits a step: its text, and its structure, folded under it:
 * its section, minutes, whether it's waiting time, when it starts and the
 * ingredients it uses.
 */
const StepEditor: React.FC<StepEditorProps> = ({
  step,
  index,
  earlier,
  ingredients,
  onChange,
  onMoveUp,
  onRemove,
}) => {
  const named = ingredients.filter(
    (ingredient) => ingredientLabel(ingredient) !== ""
  );
  let startValue = "previous";
  if (step.start === "start") {
    startValue = "start";
  } else if (Array.isArray(step.start)) {
    startValue = step.start.join(",");
  }
  const stepNumber = (key: number) =>
    earlier.findIndex((other) => other.key === key) + 1;
  return (
    <li className="flex items-start gap-2">
      <span className="mt-1.5 w-5 shrink-0 text-right text-sm font-semibold text-muted tabular-nums">
        {index + 1}.
      </span>
      <div className="flex grow flex-col gap-2">
        <textarea
          aria-label={`Step ${index + 1}`}
          className={inputClass}
          rows={2}
          value={step.text}
          onChange={(event) => {
            onChange({ text: event.target.value });
          }}
        />
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2 text-sm">
          <label className="flex items-center gap-1.5">
            <input
              aria-label={`Minutes of step ${index + 1}`}
              className={`${inputClass} w-16! py-1`}
              inputMode="decimal"
              placeholder="–"
              value={step.minutes}
              onChange={(event) => {
                onChange({ minutes: event.target.value });
              }}
            />
            <span className="text-muted">min</span>
          </label>
          <CheckboxField
            label="Waiting time"
            checked={step.waiting}
            onChange={(waiting) => {
              onChange({ waiting });
            }}
          />
          {index > 0 && (
            <select
              aria-label={`When step ${index + 1} starts`}
              className={`${inputClass} w-auto! py-1`}
              value={startValue}
              onChange={(event) => {
                const { value } = event.target;
                if (value === "previous" || value === "start") {
                  onChange({ start: value });
                } else {
                  onChange({ start: value.split(",").map(Number) });
                }
              }}
            >
              <option value="previous">After the step before</option>
              <option value="start">Right away</option>
              {earlier.slice(0, -1).map((other) => (
                <option key={other.key} value={String(other.key)}>
                  After step {stepNumber(other.key)}
                </option>
              ))}
              {Array.isArray(step.start) && step.start.length > 1 && (
                <option value={startValue}>
                  After steps{" "}
                  {step.start.map(stepNumber).filter(Boolean).join(" and ")}
                </option>
              )}
            </select>
          )}
          <label
            className="flex items-center gap-1.5"
            title="Start this many minutes into the steps it follows, rather than once they're done, like pasta 10 minutes into a simmer; or end this many minutes before they end, like the oven preheated 3 minutes before the marinating is done"
          >
            <select
              aria-label={`How step ${index + 1} is timed`}
              className={`${inputClass} w-auto! py-1`}
              value={step.offsetEnd ? "end" : "start"}
              onChange={(event) => {
                onChange({ offsetEnd: event.target.value === "end" });
              }}
            >
              <option value="start">starting</option>
              {index > 0 && step.start !== "start" && (
                <option value="end">ending</option>
              )}
            </select>
            <input
              aria-label={`Minutes in that step ${index + 1} starts`}
              className={`${inputClass} w-14! py-1`}
              inputMode="decimal"
              placeholder="–"
              value={step.offsetMinutes}
              onChange={(event) => {
                onChange({ offsetMinutes: event.target.value });
              }}
            />
            <span className="text-muted">
              {step.offsetEnd ? "min before it ends" : "min in"}
            </span>
          </label>
          <input
            aria-label={`Section of step ${index + 1}`}
            className={`${inputClass} w-44! py-1`}
            placeholder="Section, e.g. Sauce"
            value={step.section}
            onChange={(event) => {
              onChange({ section: event.target.value });
            }}
          />
        </div>
        {named.length > 0 && (
          <div
            className="flex flex-wrap gap-1.5"
            role="group"
            aria-label={`Ingredients of step ${index + 1}`}
          >
            {named.map((ingredient) => {
              const used = step.ingredientKeys.includes(ingredient.key);
              const chip = (
                <button
                  key={ingredient.key}
                  type="button"
                  aria-pressed={used}
                  className={`rounded-full px-2 py-0.5 text-xs ring-1 ${
                    used
                      ? "bg-pine-100 font-medium text-pine-dark ring-pine-200"
                      : "bg-white text-muted ring-line hover:ring-line"
                  }`}
                  onClick={() => {
                    onChange({
                      ingredientKeys: used
                        ? step.ingredientKeys.filter(
                            (key) => key !== ingredient.key
                          )
                        : [...step.ingredientKeys, ingredient.key],
                    });
                  }}
                >
                  {ingredientLabel(ingredient)}
                </button>
              );
              if (!used) {
                return chip;
              }
              // An ingredient split between steps takes this step's part.
              return (
                <span
                  key={ingredient.key}
                  className="inline-flex items-center gap-1"
                >
                  {chip}
                  <input
                    aria-label={`Amount of ${ingredientLabel(ingredient)} in step ${index + 1}`}
                    title="How much this step uses, if the ingredient is split between steps; empty for all of it, or the rest"
                    className={`${inputClass} w-14! px-1.5! py-0.5 text-xs`}
                    inputMode="decimal"
                    placeholder="all"
                    value={step.amounts[ingredient.key] ?? ""}
                    onChange={(event) => {
                      onChange({
                        amounts: {
                          ...step.amounts,
                          [ingredient.key]: event.target.value,
                        },
                      });
                    }}
                  />
                </span>
              );
            })}
          </div>
        )}
      </div>
      <div className="flex flex-col">
        <button
          type="button"
          className={iconButton}
          aria-label="Move step up"
          title="Move step up"
          disabled={index === 0}
          onClick={onMoveUp}
        >
          <ArrowUpIcon className="size-4" />
        </button>
        <button
          type="button"
          className={iconButton}
          aria-label="Remove step"
          title="Remove step"
          onClick={onRemove}
        >
          <XMarkIcon className="size-4" />
        </button>
      </div>
    </li>
  );
};

interface RecipeFormProps {
  // The recipe to edit, or undefined for a new one.
  recipe?: Recipe | undefined;
  /*
   * A recipe to start a new one from, as a copy to vary, which stays as it
   * is. Only for a new recipe.
   */
  copyOf?: Recipe | undefined;
  // The tags of the household's recipes, to reuse.
  allTags: string[];
}

/**
 * RecipeForm creates or edits a recipe: its ingredients, picked from the
 * household's foods, its steps and its tags. It goes to the recipe once
 * saved.
 */
const RecipeForm: React.FC<RecipeFormProps> = ({ recipe, copyOf, allTags }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  // The recipe the fields start from: the one edited, or the one copied.
  const source = recipe ?? copyOf;
  const [name, setName] = useState(
    recipe?.name ?? (copyOf === undefined ? "" : `${copyOf.name} (copy)`)
  );
  const [description, setDescription] = useState(source?.description ?? "");
  const [servings, setServings] = useState(
    source === undefined ? "4" : String(source.servings)
  );
  const [prep, setPrep] = useState(hoursMinutesInput(source?.prepSeconds ?? 0));
  const [cook, setCook] = useState(hoursMinutesInput(source?.cookSeconds ?? 0));
  /*
   * The ingredients and steps as they were, which keep the keys the
   * steps refer to ingredients and other steps by.
   */
  const [initial] = useState(() => {
    const initialIngredients = ingredientInputs(source);
    const initialSteps = stepInputs(source, initialIngredients);
    return {
      ingredients: initialIngredients,
      ingredientsKey: ingredientsKey(initialIngredients),
      steps: initialSteps,
      stepsKey: stepsKey(initialSteps, initialIngredients),
    };
  });
  const [ingredients, setIngredients] = useState(initial.ingredients);
  const [steps, setSteps] = useState(initial.steps);
  const [tags, setTags] = useState(source?.tags.join(", ") ?? "");
  const [sourceUrl, setSourceUrl] = useState(source?.sourceUrl ?? "");
  const [fridgeDays, setFridgeDays] = useState(
    source?.fridgeDays === undefined ? "" : String(source.fridgeDays)
  );
  const [freezable, setFreezable] = useState(source?.freezable ?? false);
  const [freezerDays, setFreezerDays] = useState(
    source?.freezerDays === undefined ? "" : String(source.freezerDays)
  );
  const [prepAheadDays, setPrepAheadDays] = useState(
    source === undefined || source.prepAheadDays === 0
      ? ""
      : String(source.prepAheadDays)
  );
  const [prepAheadNote, setPrepAheadNote] = useState(
    source?.prepAheadNote ?? ""
  );
  const [notes, setNotes] = useState(source?.notes ?? "");
  const [initialSides] = useState<SideInput[]>(() =>
    sidesFromRecipe(source?.servedWith ?? [])
  );
  const [sides, setSides] = useState(initialSides);
  // What the source makes, for recipes using it by weight or volume.
  const initialYield =
    source === undefined || source.yieldAmount === 0
      ? ""
      : String(source.yieldAmount);
  const [yieldAmount, setYieldAmount] = useState(initialYield);
  const [yieldUnit, setYieldUnit] = useState<AmountUnit>(
    source === undefined || source.yieldUnit === AmountUnit.UNSPECIFIED
      ? AmountUnit.GRAMS
      : source.yieldUnit
  );
  const [invalid, setInvalid] = useState("");

  const onSaved = async (id: string) => {
    await invalidateServices(queryClient, [RecipeService]);
    void navigate(`/recipes/${id}`, leaveUnsaved);
  };
  const create = useMutation(createRecipe, {
    onSuccess: async (response) => {
      await onSaved(response.recipe?.id ?? "");
    },
  });
  const update = useMutation(updateRecipe, {
    onSuccess: async (response) => {
      await onSaved(response.recipe?.id ?? "");
    },
  });
  const saving = create.isPending || update.isPending;

  // The fields changed, by their API names.

  const changed: [string, boolean][] = [
    ["name", name !== (recipe?.name ?? "")],
    ["description", description !== (recipe?.description ?? "")],
    [
      "servings",
      servings !== (recipe === undefined ? "4" : String(recipe.servings)),
    ],
    ["prep_seconds", prep !== hoursMinutesInput(recipe?.prepSeconds ?? 0)],
    ["cook_seconds", cook !== hoursMinutesInput(recipe?.cookSeconds ?? 0)],
    ["ingredients", ingredientsKey(ingredients) !== initial.ingredientsKey],
    ["instructions", stepsKey(steps, ingredients) !== initial.stepsKey],
    ["tags", tags !== (recipe?.tags.join(", ") ?? "")],
    ["source_url", sourceUrl !== (recipe?.sourceUrl ?? "")],
    [
      "fridge_days",
      fridgeDays !==
        (recipe?.fridgeDays === undefined ? "" : String(recipe.fridgeDays)),
    ],
    ["freezable", freezable !== (recipe?.freezable ?? false)],
    [
      "freezer_days",
      freezerDays !==
        (recipe?.freezerDays === undefined ? "" : String(recipe.freezerDays)),
    ],
    ["notes", notes !== (recipe?.notes ?? "")],
    [
      "served_with",
      sidesKey(sides) !== sidesKey(recipe === undefined ? [] : initialSides),
    ],
    [
      "prep_ahead_days",
      prepAheadDays !==
        (recipe === undefined || recipe.prepAheadDays === 0
          ? ""
          : String(recipe.prepAheadDays)),
    ],
    ["prep_ahead_note", prepAheadNote !== (recipe?.prepAheadNote ?? "")],
    [
      "yield_amount",
      yieldAmount.trim() !== initialYield ||
        (yieldAmount.trim() !== "" && yieldUnit !== recipe?.yieldUnit),
    ],
  ];
  const paths = changed
    .filter(([, isChanged]) => isChanged)
    .map(([path]) => path);
  const dirty = paths.length > 0;

  /*
   * Save saves the recipe, as a draft if draft: a draft can leave
   * ingredients unmatched, with only their original wording, and isn't
   * planned until it's saved as a recipe.
   */
  const save = (draft: boolean) => {
    setInvalid("");
    const servingCount = Number(servings);
    if (!Number.isInteger(servingCount) || servingCount < 1) {
      setInvalid("Servings must be a whole number of at least 1.");
      return;
    }
    const prepSeconds = parseDuration(prep);
    const cookSeconds = parseDuration(cook);
    if (prepSeconds === null || cookSeconds === null) {
      setInvalid(
        "Times must be minutes, like 45, or hours and minutes, like 1:30."
      );
      return;
    }
    const days = parseOptionalNumber(fridgeDays);
    if (days === null || (days !== undefined && !Number.isInteger(days))) {
      setInvalid(
        "Days in the fridge must be a whole number, or empty if unknown."
      );
      return;
    }
    const frozenDays = parseOptionalNumber(freezerDays);
    if (
      frozenDays === null ||
      (frozenDays !== undefined &&
        (!Number.isInteger(frozenDays) || frozenDays < 1))
    ) {
      setInvalid(
        "Days in the freezer must be a whole number of at least 1, or empty if unknown."
      );
      return;
    }
    const aheadDays = parseOptionalNumber(prepAheadDays) ?? 0;
    if (aheadDays === null || !Number.isInteger(aheadDays) || aheadDays > 7) {
      setInvalid(
        "Days to start ahead must be a whole number up to 7, or empty if it starts the day it's cooked."
      );
      return;
    }
    let yieldValue = parseQuantity(yieldAmount);
    if (yieldValue === null || yieldValue === 0) {
      setInvalid(
        "What it makes must be an amount, like 1200, or empty if unknown."
      );
      return;
    }
    yieldValue ??= 0;
    const kept = ingredients.filter(
      (ingredient) =>
        ingredient.food !== null ||
        ingredient.recipe !== null ||
        ingredient.amount.trim() !== "" ||
        ingredient.originalText.trim() !== ""
    );
    const parsedIngredients = [];
    for (const [index, ingredient] of kept.entries()) {
      const amount = parseQuantity(ingredient.amount);
      if (ingredient.isRecipe) {
        if (ingredient.recipe === null) {
          setInvalid(`Choose the recipe of ingredient ${index + 1}.`);
          return;
        }
        if (amount === null || amount === undefined || amount <= 0) {
          setInvalid(`Give the amount of ${ingredient.recipe.name}.`);
          return;
        }
        parsedIngredients.push({
          amount,
          note: ingredient.note,
          optional: ingredient.optional,
          originalText: ingredient.originalText,
          recipeId: ingredient.recipe.id,
          unit: ingredient.unit,
        });
        continue;
      }
      if (ingredient.food === null) {
        const text = ingredient.originalText.trim();
        if (!draft || text === "") {
          setInvalid(
            text === ""
              ? `Choose the food of ingredient ${index + 1}.`
              : `Choose the food of “${text}”, or save a draft to finish it later.`
          );
          return;
        }
        if (amount === null) {
          setInvalid(`The amount of “${text}” must be a number, or empty.`);
          return;
        }
        // An unmatched ingredient of a draft, with its amount if known.
        parsedIngredients.push({
          amount: amount ?? 0,
          note: ingredient.note,
          optional: ingredient.optional,
          originalText: text,
          unit: amount === undefined ? AmountUnit.UNSPECIFIED : ingredient.unit,
        });
        continue;
      }
      if (amount === null || amount === undefined || amount <= 0) {
        setInvalid(`Give an amount for ${ingredient.food.name}.`);
        return;
      }
      parsedIngredients.push({
        amount,
        foodId: ingredient.food.id,
        note: ingredient.note,
        optional: ingredient.optional,
        originalText: ingredient.originalText,
        unit: ingredient.unit,
      });
    }
    const fields = {
      cookSeconds,
      description,
      draft,
      freezable,
      freezerDays: frozenDays,
      fridgeDays: days,
      ingredients: parsedIngredients,
      instructions: stepsToSave(steps, kept),
      name,
      notes,
      prepAheadDays: aheadDays,
      prepAheadNote,
      prepSeconds,
      servedWith: sidesToSave(sides),
      servings: servingCount,
      sourceUrl,
      tags: parseTags(tags),
      yieldAmount: yieldValue,
      yieldUnit: yieldValue === 0 ? AmountUnit.UNSPECIFIED : yieldUnit,
    };
    if (recipe === undefined) {
      create.mutate({ recipe: fields });
    } else {
      // Saving as a recipe or a draft is a change of its own.
      const savePaths = paths.flatMap((path) =>
        path === "yield_amount" ? [path, "yield_unit"] : [path]
      );
      if (draft !== recipe.draft) {
        savePaths.push("draft");
      }
      update.mutate({
        recipe: { ...fields, id: recipe.id },
        updateMask: { paths: savePaths },
      });
    }
  };

  let kicker = recipe === undefined ? "New recipe" : "Edit recipe";
  let title = recipe === undefined ? "New recipe" : recipe.name;
  if (copyOf !== undefined) {
    kicker = "Variation";
    title = `Copy of ${copyOf.name}`;
  }
  if (recipe === undefined && copyOf === undefined) {
    kicker = "Recipes";
  }
  const back = source === undefined ? "/recipes" : `/recipes/${source.id}`;
  const unchanged = recipe !== undefined && !dirty;

  const setIngredient = (index: number, change: Partial<IngredientInput>) => {
    setIngredients(
      ingredients.map((ingredient, other) =>
        other === index ? { ...ingredient, ...change } : ingredient
      )
    );
  };
  /*
   * A recipe whose steps say how long they take gets its times from them,
   * on the server, so they aren't asked for.
   */
  const timed = steps.some(
    (step) => Number.parseFloat(step.minutes.replace(",", ".")) > 0
  );
  const currentTags = parseTags(tags);
  const suggestedTags = allTags.filter((tag) => !currentTags.includes(tag));

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        save(false);
      }}
    >
      <UnsavedChangesGuard
        dirty={dirty && !create.isSuccess && !update.isSuccess}
      />
      <BackLink to={back}>
        {source === undefined ? "Recipes" : source.name}
      </BackLink>
      <PageHeader kicker={kicker} title={title} />
      {recipe?.draft === true && (
        <p className="rounded-2xl border border-warn-line bg-warn-tint p-4 text-sm text-ink-soft">
          This is a draft: it isn&apos;t planned or suggested. Choose a food for
          each ingredient and save it to finish it.
        </p>
      )}
      <ErrorText error={create.error ?? update.error} />
      {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card title="Recipe" className="lg:col-span-1">
          <div className="flex flex-col gap-3">
            <TextField label="Name" value={name} required onChange={setName} />
            <TextAreaField
              label="Description"
              value={description}
              rows={2}
              onChange={setDescription}
            />
            {timed ? (
              <div className="flex flex-col gap-1">
                <TextField
                  label="Servings"
                  value={servings}
                  inputMode="numeric"
                  onChange={setServings}
                />
                <p className="text-xs text-muted">
                  How long it takes comes from the steps&apos; durations, so the
                  two can&apos;t disagree.
                  {recipe !== undefined && (
                    <>
                      {" "}
                      <Link
                        to={`/recipes/${recipe.id}/timeline`}
                        className={linkClass}
                      >
                        See the timeline
                      </Link>
                      .
                    </>
                  )}
                </p>
              </div>
            ) : (
              <>
                <div className="grid grid-cols-3 gap-3">
                  <TextField
                    label="Servings"
                    value={servings}
                    inputMode="numeric"
                    onChange={setServings}
                  />
                  <TextField
                    label="Prep"
                    value={prep}
                    placeholder="min"
                    onChange={setPrep}
                  />
                  <TextField
                    label="Cook"
                    value={cook}
                    placeholder="min"
                    onChange={setCook}
                  />
                </div>
                <p className="-mt-1 text-xs text-muted">
                  Times are minutes, like 45, or hours and minutes, like 1:30.
                  Give the steps durations instead, and they&apos;re worked out
                  from them.
                </p>
              </>
            )}
            <div className="flex flex-col gap-1">
              <div className="flex items-end gap-2">
                <TextField
                  label="Makes"
                  value={yieldAmount}
                  inputMode="decimal"
                  placeholder="Unknown"
                  className="grow"
                  onChange={setYieldAmount}
                />
                <select
                  aria-label="Unit of what it makes"
                  className={`${inputClass} w-28`}
                  value={yieldUnit}
                  onChange={(event) => {
                    const option = YIELD_UNITS.find(
                      ({ value }) => String(value) === event.target.value
                    );
                    if (option !== undefined) {
                      setYieldUnit(option.value);
                    }
                  }}
                >
                  {YIELD_UNITS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </div>
              <p className="text-xs text-muted">
                What the whole recipe makes, like 1200 g of cooked beans, so
                other recipes can use it by weight.
              </p>
            </div>
            <div className="flex flex-col gap-1">
              <TextField
                label="Tags"
                value={tags}
                placeholder="e.g. dinner, batch-friendly"
                hint="Separate tags with commas."
                onChange={setTags}
              />
              {suggestedTags.length > 0 && (
                <div className="flex flex-wrap gap-1">
                  {suggestedTags.map((tag) => (
                    <button
                      key={tag}
                      type="button"
                      className="rounded-full bg-ground-dark px-2 py-0.5 text-xs text-ink-soft hover:bg-ground-dark"
                      onClick={() => {
                        setTags([...currentTags, tag].join(", "));
                      }}
                    >
                      + {tag}
                    </button>
                  ))}
                </div>
              )}
            </div>
            <div className="grid grid-cols-2 items-end gap-3">
              <TextField
                label="Keeps in the fridge (days)"
                value={fridgeDays}
                inputMode="numeric"
                placeholder="Unknown"
                onChange={setFridgeDays}
              />
              <CheckboxField
                label="Freezes well"
                checked={freezable}
                className="pb-2"
                onChange={setFreezable}
              />
              {freezable && (
                <TextField
                  label="Keeps in the freezer (days)"
                  value={freezerDays}
                  inputMode="numeric"
                  placeholder="Unknown"
                  hint="Frozen leftovers are checked against it."
                  onChange={setFreezerDays}
                />
              )}
            </div>
            <div className="grid grid-cols-2 items-start gap-3">
              <TextField
                label="Starts ahead (days)"
                value={prepAheadDays}
                inputMode="numeric"
                placeholder="0"
                hint="E.g. 1 for beans soaked the night before."
                onChange={setPrepAheadDays}
              />
              <TextField
                label="What to do ahead"
                value={prepAheadNote}
                placeholder="Soak the chickpeas"
                onChange={setPrepAheadNote}
              />
            </div>
            <ServedWith
              sides={sides}
              recipeId={recipe?.id}
              onChange={setSides}
            />
            <TextField
              label="Source"
              value={sourceUrl}
              type="url"
              placeholder="https://"
              onChange={setSourceUrl}
            />
            <TextAreaField label="Notes" value={notes} onChange={setNotes} />
          </div>
        </Card>
        <div className="flex flex-col gap-4 lg:col-span-2">
          <Card title="Ingredients">
            <ol className="flex flex-col gap-3">
              {ingredients.map((ingredient, index) => (
                <li
                  key={ingredient.key}
                  className="flex flex-col gap-2 rounded-xl border border-line p-2 sm:p-3"
                >
                  <div className="flex flex-wrap items-center gap-2">
                    <div
                      className="flex"
                      role="group"
                      aria-label={`Kind of ingredient ${index + 1}`}
                    >
                      {[false, true].map((isRecipe) => (
                        <button
                          key={String(isRecipe)}
                          type="button"
                          aria-pressed={ingredient.isRecipe === isRecipe}
                          title={
                            isRecipe
                              ? "Another recipe, like cooked chickpeas, in servings or by weight: planning this recipe plans a cook of it too"
                              : "A food, like tomatoes"
                          }
                          className={`-ml-px px-2 py-1.5 text-xs font-medium ring-1 ring-inset first:ml-0 first:rounded-l-lg last:rounded-r-lg ${
                            ingredient.isRecipe === isRecipe
                              ? "relative z-10 bg-ink-soft text-white ring-ink-soft"
                              : "bg-white text-ink-soft ring-line hover:bg-ground"
                          }`}
                          onClick={() => {
                            if (isRecipe !== ingredient.isRecipe) {
                              setIngredient(index, {
                                isRecipe,
                                unit: isRecipe
                                  ? AmountUnit.UNSPECIFIED
                                  : AmountUnit.GRAMS,
                              });
                            }
                          }}
                        >
                          {isRecipe ? "Recipe" : "Food"}
                        </button>
                      ))}
                    </div>
                    <div className="min-w-48 grow basis-60">
                      {ingredient.isRecipe ? (
                        <RecipePicker
                          label={`Recipe of ingredient ${index + 1}`}
                          value={ingredient.recipe}
                          exclude={recipe?.id}
                          onChange={(picked) => {
                            setIngredient(index, { recipe: picked });
                          }}
                        />
                      ) : (
                        <FoodPicker
                          label={`Food of ingredient ${index + 1}`}
                          value={ingredient.food}
                          onChange={(food) => {
                            setIngredient(index, { food });
                          }}
                        />
                      )}
                    </div>
                    <div className="flex grow items-center gap-2 sm:grow-0">
                      <input
                        aria-label={`Amount of ingredient ${index + 1}`}
                        className={`${inputClass} w-24 tabular-nums`}
                        inputMode="decimal"
                        placeholder="Amount"
                        value={ingredient.amount}
                        onChange={(event) => {
                          setIngredient(index, { amount: event.target.value });
                        }}
                      />
                      <select
                        aria-label={`Unit of ingredient ${index + 1}`}
                        className={`${inputClass} w-28`}
                        value={ingredient.unit}
                        onChange={(event) => {
                          // The option's own value keeps the enum's type.
                          const option = (
                            ingredient.isRecipe ? COMPONENT_UNITS : AMOUNT_UNITS
                          ).find(
                            ({ value }) => String(value) === event.target.value
                          );
                          if (option !== undefined) {
                            setIngredient(index, { unit: option.value });
                          }
                        }}
                      >
                        {(ingredient.isRecipe
                          ? COMPONENT_UNITS
                          : AMOUNT_UNITS
                        ).map((option) => (
                          <option key={option.value} value={option.value}>
                            {option.label}
                          </option>
                        ))}
                      </select>
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <input
                      aria-label={`Note of ingredient ${index + 1}`}
                      className={`${inputClass} min-w-40 grow basis-40`}
                      placeholder="Note, e.g. finely chopped"
                      value={ingredient.note}
                      onChange={(event) => {
                        setIngredient(index, { note: event.target.value });
                      }}
                    />
                    <input
                      aria-label={`Original wording of ingredient ${index + 1}`}
                      className={`${inputClass} min-w-40 grow basis-40`}
                      placeholder="As the source wrote it"
                      value={ingredient.originalText}
                      onChange={(event) => {
                        setIngredient(index, {
                          originalText: event.target.value,
                        });
                      }}
                    />
                  </div>
                  <div className="flex items-center justify-between gap-2">
                    <CheckboxField
                      label="Optional"
                      checked={ingredient.optional}
                      onChange={(optional) => {
                        setIngredient(index, { optional });
                      }}
                    />
                    <div className="flex items-center gap-1">
                      <button
                        type="button"
                        className={iconButton}
                        aria-label="Move up"
                        title="Move up"
                        disabled={index === 0}
                        onClick={() => {
                          setIngredients(move(ingredients, index, -1));
                        }}
                      >
                        <ArrowUpIcon className="size-4" />
                      </button>
                      <button
                        type="button"
                        className={iconButton}
                        aria-label="Move down"
                        title="Move down"
                        disabled={index === ingredients.length - 1}
                        onClick={() => {
                          setIngredients(move(ingredients, index, 1));
                        }}
                      >
                        <ArrowDownIcon className="size-4" />
                      </button>
                      <button
                        type="button"
                        className={iconButton}
                        aria-label="Remove ingredient"
                        title="Remove ingredient"
                        onClick={() => {
                          setIngredients(
                            ingredients.filter(
                              (_item, other) => other !== index
                            )
                          );
                        }}
                      >
                        <XMarkIcon className="size-4" />
                      </button>
                    </div>
                  </div>
                </li>
              ))}
            </ol>
            <button
              type="button"
              className={`${ghostButton} mt-3`}
              onClick={() => {
                setIngredients([...ingredients, emptyIngredient()]);
              }}
            >
              <PlusIcon className="size-4" aria-hidden="true" />
              Add ingredient
            </button>
          </Card>
          <Card title="Steps">
            <p className="mb-3 text-sm text-muted">
              One action per step, without the amounts: cook mode shows each
              step&apos;s ingredients, scaled to the servings cooked. Give steps
              their minutes to get timers and a timeline.
            </p>
            <ol className="flex flex-col gap-4">
              {steps.map((step, index) => (
                <StepEditor
                  key={step.key}
                  step={step}
                  index={index}
                  earlier={steps.slice(0, index)}
                  ingredients={ingredients}
                  onChange={(change) => {
                    setSteps(
                      steps.map((other) =>
                        other.key === step.key ? { ...other, ...change } : other
                      )
                    );
                  }}
                  onMoveUp={() => {
                    setSteps(move(steps, index, -1));
                  }}
                  onRemove={() => {
                    setSteps(steps.filter((other) => other.key !== step.key));
                  }}
                />
              ))}
            </ol>
            <button
              type="button"
              className={`${ghostButton} mt-3`}
              onClick={() => {
                setSteps([
                  ...steps,
                  { ...emptyStep(), section: steps.at(-1)?.section ?? "" },
                ]);
              }}
            >
              <PlusIcon className="size-4" aria-hidden="true" />
              Add step
            </button>
          </Card>
        </div>
      </div>
      <ErrorText error={create.error ?? update.error} />
      {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
      <div className="sticky bottom-[3.75rem] z-10 flex gap-2 bg-ground/90 py-2 backdrop-blur lg:bottom-4">
        <button
          type="button"
          className={`${secondaryButton} py-3.5 text-base`}
          disabled={saving || (unchanged && recipe.draft)}
          title="Save it unfinished: ingredients can be left without a food, and it isn't planned until it's saved as a recipe."
          onClick={() => {
            save(true);
          }}
        >
          {recipe?.draft === false ? "Save as draft" : "Save draft"}
        </button>
        <button
          type="submit"
          className={`${primaryButton} grow py-3.5 text-base`}
          disabled={saving || (unchanged && !recipe.draft)}
        >
          {saving ? "Saving…" : "Save"}
        </button>
      </div>
    </form>
  );
};

export default RecipeForm;
