import AddFoodDialog from "@components/foods/add-food-dialog";
import { inputClass } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  Combobox,
  ComboboxInput,
  ComboboxOption,
  ComboboxOptions,
} from "@headlessui/react";
import { PlusIcon } from "@heroicons/react/24/outline";
import { keepPreviousData } from "@tanstack/react-query";
import { listFoods } from "gen/foods/v1/foods-FoodService_connectquery";
import { useDebouncedValue } from "helpers/hooks";
import React, { useState } from "react";

// PickedFood is a food chosen in a FoodPicker.
export interface PickedFood {
  id: string;
  name: string;
}

// The option that adds a new food, rather than picking one.
const ADD_FOOD: PickedFood = { id: "", name: "" };

interface FoodPickerProps {
  // The food picked, or null if none yet.
  value: PickedFood | null;
  onChange: (food: PickedFood) => void;
  // The label of the input, for screen readers.
  label: string;
}

/**
 * FoodPicker picks one of the household's foods by searching their names,
 * or adds a new one, from FoodData Central or custom, without leaving the
 * form.
 */
const FoodPicker: React.FC<FoodPickerProps> = ({ value, onChange, label }) => {
  const [query, setQuery] = useState("");
  const [adding, setAdding] = useState<string | null>(null);
  const search = useDebouncedValue(query.trim(), 150);
  const { data, isPending } = useQuery(
    listFoods,
    { pageSize: 20, query: search },
    { placeholderData: keepPreviousData }
  );
  const foods = data?.foods ?? [];
  return (
    <>
      <Combobox
        value={value}
        by="id"
        immediate
        onChange={(food: PickedFood | null) => {
          if (food === null) {
            return;
          }
          if (food.id === "") {
            setAdding(query.trim());
            return;
          }
          onChange(food);
          setQuery("");
        }}
        onClose={() => {
          setQuery("");
        }}
      >
        <ComboboxInput
          aria-label={label}
          className={inputClass}
          placeholder="Choose a food"
          displayValue={(food: PickedFood | null) => food?.name ?? ""}
          onChange={(event) => {
            setQuery(event.target.value);
          }}
        />
        <ComboboxOptions
          anchor="bottom start"
          className="z-[2100] max-h-72 w-[max(var(--input-width),16rem)] overflow-y-auto rounded-xl bg-white p-1 text-sm shadow-lg ring-1 ring-black/5 [--anchor-gap:4px] empty:invisible"
        >
          {foods.map((food) => (
            <ComboboxOption
              key={food.id}
              value={{ id: food.id, name: food.name }}
              className="cursor-pointer rounded px-2 py-1.5 text-ink-soft data-[focus]:bg-pine-50"
            >
              {food.name}
            </ComboboxOption>
          ))}
          {!isPending && foods.length === 0 && (
            <p className="px-2 py-1.5 text-muted">
              No foods match &quot;{search}&quot;.
            </p>
          )}
          <ComboboxOption
            value={ADD_FOOD}
            className="flex cursor-pointer items-center gap-1.5 rounded px-2 py-1.5 font-medium text-pine data-[focus]:bg-pine-50"
          >
            <PlusIcon className="size-4" aria-hidden="true" />
            {query.trim() === "" ? "Add a new food" : `Add “${query.trim()}”`}
          </ComboboxOption>
        </ComboboxOptions>
      </Combobox>
      {adding !== null && (
        <AddFoodDialog
          initialQuery={adding}
          onClose={() => {
            setAdding(null);
          }}
          onCreated={(food) => {
            setAdding(null);
            onChange({ id: food.id, name: food.name });
          }}
        />
      )}
    </>
  );
};

export default FoodPicker;
