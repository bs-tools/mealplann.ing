import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import Card from "@components/ui/card";
import { CheckboxField, TextAreaField, TextField } from "@components/ui/fields";
import { PageHeader } from "@components/ui/misc";
import { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  iconButton,
  inputClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import UnsavedChangesGuard, {
  leaveUnsaved,
} from "@components/ui/unsaved-changes-guard";
import { useMutation, useQuery } from "@connectrpc/connect-query";
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
  listRecipes,
  updateRecipe,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { Recipe } from "gen/recipes/v1/recipes_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { AMOUNT_UNITS } from "helpers/labels";
import {
  hoursMinutesInput,
  parseDuration,
  parseOptionalNumber,
} from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useNavigate } from "react-router";

// IngredientInput is an ingredient being edited.
interface IngredientInput {
  // A key for React, as ingredients move.
  key: number;
  // Whether it's a component: another recipe, in servings.
  isRecipe: boolean;
  food: PickedFood | null;
  recipe: PickedFood | null;
  amount: string;
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
   * How many minutes into the steps it follows it starts, empty to start
   * once they're done.
   */
  offsetMinutes: string;
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

const ingredientInputs = (recipe: Recipe | undefined): IngredientInput[] => {
  if (recipe === undefined) {
    return [emptyIngredient()];
  }
  return recipe.ingredients.map((ingredient) => {
    const isRecipe = ingredient.recipeId !== "";
    return {
      amount: String(ingredient.amount),
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
      unit: isRecipe ? AmountUnit.GRAMS : ingredient.unit,
    };
  });
};

const emptyStep = (): StepInput => ({
  amounts: {},
  ingredientKeys: [],
  key: newKey(),
  minutes: "",
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
      offsetMinutes: minutesInput(step.startOffsetSeconds),
      section: step.section,
      start,
      text: step.text,
      waiting: step.waiting,
    };
  });
};

// MinutesInput formats seconds as minutes to edit, empty for none.
const minutesInput = (seconds: number): string =>
  seconds > 0 ? String(Math.round((seconds / 60) * 100) / 100) : "";

// Seconds parses minutes as seconds, 0 for empty or invalid.
const seconds = (minutes: string): number => {
  const value = parseOptionalNumber(minutes) ?? 0;
  return Math.round(value * 60);
};

// StepsToSave are the steps with text, as they're saved.
const stepsToSave = (
  steps: StepInput[],
  ingredients: IngredientInput[]
): {
  text: string;
  section: string;
  ingredientIndexes: number[];
  ingredientAmounts: number[];
  durationSeconds: number;
  startOffsetSeconds: number;
  waiting: boolean;
  fromStart: boolean;
  afterStepIndexes: number[];
}[] => {
  const kept = steps.filter((step) => step.text.trim() !== "");
  const positions = new Map(
    ingredients.map((ingredient, position) => [ingredient.key, position])
  );
  return kept.map((step, index) => {
    const used = step.ingredientKeys.filter((key) => positions.has(key));
    const amounts = used.map(
      (key) => parseOptionalNumber(step.amounts[key] ?? "") ?? 0
    );
    const after =
      typeof step.start === "string"
        ? []
        : step.start
            .map((key) => kept.findIndex((other) => other.key === key))
            .filter((other) => other >= 0 && other < index);
    return {
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

// IngredientLabel names an ingredient being edited, "" if it has none yet.
const ingredientLabel = (ingredient: IngredientInput): string =>
  (ingredient.isRecipe ? ingredient.recipe?.name : ingredient.food?.name) ?? "";

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
      <span className="mt-1.5 w-5 shrink-0 text-right text-sm font-semibold text-gray-500 tabular-nums">
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
            <span className="text-gray-600">min</span>
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
            title="Start this many minutes into the steps it follows, rather than once they're done, like pasta 10 minutes into a simmer"
          >
            <span className="text-gray-600">starting</span>
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
            <span className="text-gray-600">min in</span>
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
                      ? "bg-brand-100 font-medium text-brand-900 ring-brand-300"
                      : "bg-white text-gray-600 ring-gray-200 hover:ring-gray-300"
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
  // The tags of the household's recipes, to reuse.
  allTags: string[];
}

/**
 * RecipeForm creates or edits a recipe: its ingredients, picked from the
 * household's foods, its steps and its tags. It goes to the recipe once
 * saved.
 */
const RecipeForm: React.FC<RecipeFormProps> = ({ recipe, allTags }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState(recipe?.name ?? "");
  const [description, setDescription] = useState(recipe?.description ?? "");
  const [servings, setServings] = useState(
    recipe === undefined ? "4" : String(recipe.servings)
  );
  const [prep, setPrep] = useState(hoursMinutesInput(recipe?.prepSeconds ?? 0));
  const [cook, setCook] = useState(hoursMinutesInput(recipe?.cookSeconds ?? 0));
  /*
   * The ingredients and steps as they were, which keep the keys the
   * steps refer to ingredients and other steps by.
   */
  const [initial] = useState(() => {
    const initialIngredients = ingredientInputs(recipe);
    const initialSteps = stepInputs(recipe, initialIngredients);
    return {
      ingredients: initialIngredients,
      ingredientsKey: ingredientsKey(initialIngredients),
      steps: initialSteps,
      stepsKey: stepsKey(initialSteps, initialIngredients),
    };
  });
  const [ingredients, setIngredients] = useState(initial.ingredients);
  const [steps, setSteps] = useState(initial.steps);
  const [tags, setTags] = useState(recipe?.tags.join(", ") ?? "");
  const [sourceUrl, setSourceUrl] = useState(recipe?.sourceUrl ?? "");
  const [fridgeDays, setFridgeDays] = useState(
    recipe?.fridgeDays === undefined ? "" : String(recipe.fridgeDays)
  );
  const [freezable, setFreezable] = useState(recipe?.freezable ?? false);
  const [freezerDays, setFreezerDays] = useState(
    recipe?.freezerDays === undefined ? "" : String(recipe.freezerDays)
  );
  const [prepAheadDays, setPrepAheadDays] = useState(
    recipe === undefined || recipe.prepAheadDays === 0
      ? ""
      : String(recipe.prepAheadDays)
  );
  const [prepAheadNote, setPrepAheadNote] = useState(
    recipe?.prepAheadNote ?? ""
  );
  const [notes, setNotes] = useState(recipe?.notes ?? "");
  const [invalid, setInvalid] = useState("");
  // The household's recipes, to use as components, but this one.
  const recipeList = useQuery(listRecipes, {});
  const components = (recipeList.data?.recipes ?? []).filter(
    (other) => other.id !== recipe?.id
  );

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
      "prep_ahead_days",
      prepAheadDays !==
        (recipe === undefined || recipe.prepAheadDays === 0
          ? ""
          : String(recipe.prepAheadDays)),
    ],
    ["prep_ahead_note", prepAheadNote !== (recipe?.prepAheadNote ?? "")],
  ];
  const paths = changed
    .filter(([, isChanged]) => isChanged)
    .map(([path]) => path);
  const dirty = paths.length > 0;

  const save = () => {
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
    const kept = ingredients.filter(
      (ingredient) =>
        ingredient.food !== null ||
        ingredient.recipe !== null ||
        ingredient.amount.trim() !== ""
    );
    const parsedIngredients = [];
    for (const [index, ingredient] of kept.entries()) {
      const amount = parseOptionalNumber(ingredient.amount);
      if (ingredient.isRecipe) {
        if (ingredient.recipe === null) {
          setInvalid(`Choose the recipe of ingredient ${index + 1}.`);
          return;
        }
        if (amount === null || amount === undefined || amount <= 0) {
          setInvalid(`Give the servings of ${ingredient.recipe.name}.`);
          return;
        }
        parsedIngredients.push({
          amount,
          note: ingredient.note,
          optional: ingredient.optional,
          originalText: ingredient.originalText,
          recipeId: ingredient.recipe.id,
        });
        continue;
      }
      if (ingredient.food === null) {
        setInvalid(`Choose the food of ingredient ${index + 1}.`);
        return;
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
      servings: servingCount,
      sourceUrl,
      tags: parseTags(tags),
    };
    if (recipe === undefined) {
      create.mutate({ recipe: fields });
    } else {
      update.mutate({
        recipe: { ...fields, id: recipe.id },
        updateMask: { paths },
      });
    }
  };

  const setIngredient = (index: number, change: Partial<IngredientInput>) => {
    setIngredients(
      ingredients.map((ingredient, other) =>
        other === index ? { ...ingredient, ...change } : ingredient
      )
    );
  };
  const currentTags = parseTags(tags);
  const suggestedTags = allTags.filter((tag) => !currentTags.includes(tag));

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        save();
      }}
    >
      <UnsavedChangesGuard
        dirty={dirty && !create.isSuccess && !update.isSuccess}
      />
      <PageHeader
        title={recipe === undefined ? "New recipe" : `Edit ${recipe.name}`}
        actions={
          <>
            <Link
              to={recipe === undefined ? "/recipes" : `/recipes/${recipe.id}`}
              className={secondaryButton}
            >
              Cancel
            </Link>
            <button
              type="submit"
              className={primaryButton}
              disabled={saving || (recipe !== undefined && !dirty)}
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </>
        }
      />
      <ErrorText error={create.error ?? update.error} />
      {invalid !== "" && <p className="text-sm text-red-700">{invalid}</p>}
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
            <p className="-mt-1 text-xs text-gray-500">
              Times are minutes, like 45, or hours and minutes, like 1:30.
            </p>
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
                      className="rounded-full bg-gray-100 px-2 py-0.5 text-xs text-gray-700 hover:bg-gray-200"
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
                  className="flex flex-col gap-2 rounded-md border border-gray-200 p-2 sm:p-3"
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
                              ? "Another recipe, in servings, like cooked chickpeas: planning this recipe plans a cook of it too"
                              : "A food, like tomatoes"
                          }
                          className={`-ml-px px-2 py-1.5 text-xs font-medium ring-1 ring-inset first:ml-0 first:rounded-l-md last:rounded-r-md ${
                            ingredient.isRecipe === isRecipe
                              ? "relative z-10 bg-ink text-white ring-ink"
                              : "bg-white text-gray-700 ring-gray-300 hover:bg-gray-50"
                          }`}
                          onClick={() => {
                            setIngredient(index, { isRecipe });
                          }}
                        >
                          {isRecipe ? "Recipe" : "Food"}
                        </button>
                      ))}
                    </div>
                    <div className="min-w-48 grow basis-60">
                      {ingredient.isRecipe ? (
                        <select
                          aria-label={`Recipe of ingredient ${index + 1}`}
                          className={inputClass}
                          value={ingredient.recipe?.id ?? ""}
                          onChange={(event) => {
                            const picked = components.find(
                              (other) => other.id === event.target.value
                            );
                            setIngredient(index, {
                              recipe:
                                picked === undefined
                                  ? null
                                  : { id: picked.id, name: picked.name },
                            });
                          }}
                        >
                          <option value="">Choose a recipe</option>
                          {ingredient.recipe !== null &&
                            !components.some(
                              (other) => other.id === ingredient.recipe?.id
                            ) && (
                              <option value={ingredient.recipe.id}>
                                {ingredient.recipe.name}
                              </option>
                            )}
                          {components.map((other) => (
                            <option key={other.id} value={other.id}>
                              {other.name}
                            </option>
                          ))}
                        </select>
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
                      {ingredient.isRecipe ? (
                        <span className="w-28 text-sm text-gray-600">
                          servings
                        </span>
                      ) : (
                        <select
                          aria-label={`Unit of ingredient ${index + 1}`}
                          className={`${inputClass} w-28`}
                          value={ingredient.unit}
                          onChange={(event) => {
                            // The option's own value keeps the enum's type.
                            const option = AMOUNT_UNITS.find(
                              ({ value }) =>
                                String(value) === event.target.value
                            );
                            if (option !== undefined) {
                              setIngredient(index, { unit: option.value });
                            }
                          }}
                        >
                          {AMOUNT_UNITS.map((option) => (
                            <option key={option.value} value={option.value}>
                              {option.label}
                            </option>
                          ))}
                        </select>
                      )}
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
            <p className="mb-3 text-sm text-gray-600">
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
    </form>
  );
};

export default RecipeForm;
