import { inputClass } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  Combobox,
  ComboboxInput,
  ComboboxOption,
  ComboboxOptions,
} from "@headlessui/react";
import { keepPreviousData } from "@tanstack/react-query";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { useDebouncedValue } from "helpers/hooks";
import React, { useState } from "react";

// PickedRecipe is a recipe chosen in a RecipePicker.
export interface PickedRecipe {
  id: string;
  name: string;
}

// MAX_RECIPES is how many matching recipes the picker lists.
const MAX_RECIPES = 20;

interface RecipePickerProps {
  // The recipe picked, or null if none yet.
  value: PickedRecipe | null;
  onChange: (recipe: PickedRecipe) => void;
  // The label of the input, for screen readers.
  label: string;
  /*
   * The ID of a recipe to leave out, like the one being edited, which
   * can't be its own ingredient.
   */
  exclude?: string | undefined;
}

/**
 * RecipePicker picks one of the household's recipes by searching their
 * names, like FoodPicker does foods, for a recipe used as an ingredient.
 */
const RecipePicker: React.FC<RecipePickerProps> = ({
  value,
  onChange,
  label,
  exclude,
}) => {
  const [query, setQuery] = useState("");
  const search = useDebouncedValue(query.trim(), 150);
  const { data, isPending } = useQuery(
    listRecipes,
    { query: search, summary: true },
    { placeholderData: keepPreviousData }
  );
  const recipes = (data?.recipes ?? [])
    .filter((recipe) => recipe.id !== exclude)
    .slice(0, MAX_RECIPES);
  return (
    <Combobox
      value={value}
      by="id"
      immediate
      onChange={(recipe: PickedRecipe | null) => {
        if (recipe !== null) {
          onChange(recipe);
          setQuery("");
        }
      }}
      onClose={() => {
        setQuery("");
      }}
    >
      <ComboboxInput
        aria-label={label}
        className={inputClass}
        placeholder="Choose a recipe"
        displayValue={(recipe: PickedRecipe | null) => recipe?.name ?? ""}
        onChange={(event) => {
          setQuery(event.target.value);
        }}
      />
      <ComboboxOptions
        anchor="bottom start"
        className="z-[2100] max-h-72 w-[max(var(--input-width),16rem)] overflow-y-auto rounded-xl bg-white p-1 text-sm shadow-lg ring-1 ring-black/5 [--anchor-gap:4px] empty:invisible"
      >
        {recipes.map((recipe) => (
          <ComboboxOption
            key={recipe.id}
            value={{ id: recipe.id, name: recipe.name }}
            className="cursor-pointer rounded px-2 py-1.5 text-ink-soft data-[focus]:bg-pine-50"
          >
            {recipe.name}
          </ComboboxOption>
        ))}
        {!isPending && recipes.length === 0 && (
          <p className="px-2 py-1.5 text-muted">
            {search === ""
              ? "No other recipes yet."
              : `No recipes match “${search}”.`}
          </p>
        )}
      </ComboboxOptions>
    </Combobox>
  );
};

export default RecipePicker;
