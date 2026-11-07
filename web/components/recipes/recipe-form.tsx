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
  food: PickedFood | null;
  amount: string;
  unit: AmountUnit;
  note: string;
  originalText: string;
  optional: boolean;
}

// StepInput is a step being edited.
interface StepInput {
  key: number;
  text: string;
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
  key: newKey(),
  note: "",
  optional: false,
  originalText: "",
  unit: AmountUnit.GRAMS,
});

const ingredientInputs = (recipe: Recipe | undefined): IngredientInput[] => {
  if (recipe === undefined) {
    return [emptyIngredient()];
  }
  return recipe.ingredients.map((ingredient) => ({
    amount: String(ingredient.amount),
    food: { id: ingredient.foodId, name: ingredient.foodName },
    key: newKey(),
    note: ingredient.note,
    optional: ingredient.optional,
    originalText: ingredient.originalText,
    unit: ingredient.unit,
  }));
};

const stepInputs = (recipe: Recipe | undefined): StepInput[] => {
  if (recipe === undefined || recipe.steps.length === 0) {
    return [{ key: newKey(), text: "" }];
  }
  return recipe.steps.map((text) => ({ key: newKey(), text }));
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
      ingredient.food?.id ?? "",
      ingredient.amount.trim(),
      ingredient.unit,
      ingredient.note.trim(),
      ingredient.originalText.trim(),
      ingredient.optional,
    ])
  );

const stepsKey = (steps: StepInput[]): string =>
  JSON.stringify(
    steps.map((step) => step.text.trim()).filter((text) => text !== "")
  );

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
  const [ingredients, setIngredients] = useState(() =>
    ingredientInputs(recipe)
  );
  const [steps, setSteps] = useState(() => stepInputs(recipe));
  const [tags, setTags] = useState(recipe?.tags.join(", ") ?? "");
  const [sourceUrl, setSourceUrl] = useState(recipe?.sourceUrl ?? "");
  const [fridgeDays, setFridgeDays] = useState(
    recipe?.fridgeDays === undefined ? "" : String(recipe.fridgeDays)
  );
  const [freezable, setFreezable] = useState(recipe?.freezable ?? false);
  const [notes, setNotes] = useState(recipe?.notes ?? "");
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
  const initialIngredients = ingredientsKey(ingredientInputs(recipe));
  const initialSteps = stepsKey(stepInputs(recipe));
  const changed: [string, boolean][] = [
    ["name", name !== (recipe?.name ?? "")],
    ["description", description !== (recipe?.description ?? "")],
    [
      "servings",
      servings !== (recipe === undefined ? "4" : String(recipe.servings)),
    ],
    ["prep_seconds", prep !== hoursMinutesInput(recipe?.prepSeconds ?? 0)],
    ["cook_seconds", cook !== hoursMinutesInput(recipe?.cookSeconds ?? 0)],
    ["ingredients", ingredientsKey(ingredients) !== initialIngredients],
    ["steps", stepsKey(steps) !== initialSteps],
    ["tags", tags !== (recipe?.tags.join(", ") ?? "")],
    ["source_url", sourceUrl !== (recipe?.sourceUrl ?? "")],
    [
      "fridge_days",
      fridgeDays !==
        (recipe?.fridgeDays === undefined ? "" : String(recipe.fridgeDays)),
    ],
    ["freezable", freezable !== (recipe?.freezable ?? false)],
    ["notes", notes !== (recipe?.notes ?? "")],
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
    const kept = ingredients.filter(
      (ingredient) =>
        ingredient.food !== null || ingredient.amount.trim() !== ""
    );
    const parsedIngredients = [];
    for (const [index, ingredient] of kept.entries()) {
      const amount = parseOptionalNumber(ingredient.amount);
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
      fridgeDays: days,
      ingredients: parsedIngredients,
      name,
      notes,
      prepSeconds,
      servings: servingCount,
      sourceUrl,
      steps: steps
        .map((step) => step.text.trim())
        .filter((text) => text !== ""),
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
                    <div className="min-w-48 grow basis-60">
                      <FoodPicker
                        label={`Food of ingredient ${index + 1}`}
                        value={ingredient.food}
                        onChange={(food) => {
                          setIngredient(index, { food });
                        }}
                      />
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
                          const option = AMOUNT_UNITS.find(
                            ({ value }) => String(value) === event.target.value
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
            <ol className="flex flex-col gap-2">
              {steps.map((step, index) => (
                <li key={step.key} className="flex items-start gap-2">
                  <span className="mt-1.5 w-5 shrink-0 text-right text-sm font-semibold text-gray-500 tabular-nums">
                    {index + 1}.
                  </span>
                  <textarea
                    aria-label={`Step ${index + 1}`}
                    className={`${inputClass} grow`}
                    rows={2}
                    value={step.text}
                    onChange={(event) => {
                      setSteps(
                        steps.map((other, otherIndex) =>
                          otherIndex === index
                            ? { ...other, text: event.target.value }
                            : other
                        )
                      );
                    }}
                  />
                  <div className="flex flex-col">
                    <button
                      type="button"
                      className={iconButton}
                      aria-label="Move step up"
                      title="Move step up"
                      disabled={index === 0}
                      onClick={() => {
                        setSteps(move(steps, index, -1));
                      }}
                    >
                      <ArrowUpIcon className="size-4" />
                    </button>
                    <button
                      type="button"
                      className={iconButton}
                      aria-label="Remove step"
                      title="Remove step"
                      onClick={() => {
                        setSteps(
                          steps.filter((_item, other) => other !== index)
                        );
                      }}
                    >
                      <XMarkIcon className="size-4" />
                    </button>
                  </div>
                </li>
              ))}
            </ol>
            <button
              type="button"
              className={`${ghostButton} mt-3`}
              onClick={() => {
                setSteps([...steps, { key: newKey(), text: "" }]);
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
