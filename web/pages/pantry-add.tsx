import type { PickedFood } from "@components/foods/food-picker";
import FoodPicker from "@components/foods/food-picker";
import Loading from "@components/loading";
import BackLink from "@components/ui/back-link";
import { PageHeader } from "@components/ui/misc";
import { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import { inputClass, labelClass, primaryButton } from "@components/ui/styles";
import { skipToken, useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { getFood } from "gen/foods/v1/foods-FoodService_connectquery";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import { addToPantry } from "gen/pantry/v1/pantry-PantryService_connectquery";
import { PantryService, Storage } from "gen/pantry/v1/pantry_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { addDays, today } from "helpers/dates";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { useNavigate } from "react-router";

type Place = "fridge" | "freezer" | "cupboard";

const STORAGE_OF: Record<Place, Storage> = {
  cupboard: Storage.PANTRY,
  freezer: Storage.FREEZER,
  fridge: Storage.FRIDGE,
};

// UseBy is how long it keeps: as usual for the food, or a set time.
type UseBy = "usual" | "3" | "7" | "date";

// GRAMS_STEP is how much the stepper changes an amount weighed in grams.
const GRAMS_STEP = 50;

/**
 * PantryAddPage adds a food to the pantry by hand, like something bought
 * at a market: how many of its packages or pieces, or how many grams if it
 * has neither, where it's kept, and how long it keeps.
 */
const PantryAddPage: React.FC = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [food, setFood] = useState<PickedFood | null>(null);
  const details = useQuery(
    getFood,
    food === null ? skipToken : { id: food.id }
  );
  const [count, setCount] = useState(1);
  const [place, setPlace] = useState<Place>("fridge");
  const [useBy, setUseBy] = useState<UseBy>("usual");
  const [date, setDate] = useState(addDays(today(), 7));
  const add = useMutation(addToPantry, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PantryService, SuggestionService]);
      void navigate("/pantry");
    },
  });

  const picked = details.data?.food;
  let unit = "g";
  let step = GRAMS_STEP;
  if ((picked?.packageGrams ?? 0) > 0) {
    unit = count === 1 ? "package" : "packages";
    step = 1;
  } else if ((picked?.gramsPerPiece ?? 0) > 0) {
    unit = count === 1 ? "piece" : "pieces";
    step = 1;
  }
  const amountText = `${count.toLocaleString()} ${unit}`;

  let buttonText = "Add";
  if (add.isPending) {
    buttonText = "Adding…";
  } else if (picked !== undefined) {
    buttonText = `Add ${amountText} of ${picked.name.toLowerCase()}`;
  }

  const save = () => {
    if (food === null || picked === undefined) {
      return;
    }
    let expiryDate = "";
    if (useBy === "date") {
      expiryDate = date;
    } else if (useBy !== "usual") {
      expiryDate = addDays(today(), Number(useBy));
    }
    const amount =
      picked.packageGrams > 0
        ? { packages: count }
        : {
            amount: count,
            unit:
              picked.gramsPerPiece > 0 ? AmountUnit.PIECES : AmountUnit.GRAMS,
          };
    add.mutate({
      ...amount,
      expiryDate,
      foodId: food.id,
      storage: STORAGE_OF[place],
    });
  };

  return (
    <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
      <BackLink to="/pantry">Pantry</BackLink>
      <PageHeader title="Add" />
      <FoodPicker
        value={food}
        label="Food to add"
        onChange={(next) => {
          setFood(next);
          setCount(1);
        }}
      />
      {food !== null && details.isPending && <Loading />}
      {picked !== undefined && (
        <div className="flex flex-col gap-5 rounded-2xl border border-line bg-white p-4">
          <p className="text-lg font-bold text-ink-soft">{picked.name}</p>
          <div className="flex items-center justify-between gap-3">
            <span className="text-base text-ink-soft">How many</span>
            <span className="flex items-center gap-2">
              <button
                type="button"
                className="size-10 rounded-full border border-gray-300 bg-white text-xl text-ink-soft disabled:opacity-40"
                aria-label="Fewer"
                disabled={count <= step}
                onClick={() => {
                  setCount(count - step);
                }}
              >
                −
              </button>
              <span className="min-w-24 text-center text-base font-bold tabular-nums">
                {amountText}
              </span>
              <button
                type="button"
                className="size-10 rounded-full border border-gray-300 bg-white text-xl text-ink-soft"
                aria-label="More"
                onClick={() => {
                  setCount(count + step);
                }}
              >
                +
              </button>
            </span>
          </div>
          <div className="flex flex-col gap-2">
            <span className="text-base text-ink-soft">Where</span>
            <Segmented
              label="Where it's kept"
              value={place}
              options={[
                { label: "Fridge", value: "fridge" },
                { label: "Freezer", value: "freezer" },
                { label: "Cupboard", value: "cupboard" },
              ]}
              onChange={setPlace}
            />
          </div>
          <div className="flex flex-col gap-2">
            <span className="text-base text-ink-soft">Use by</span>
            <Segmented
              label="Use by"
              value={useBy}
              options={[
                { label: "As usual", value: "usual" },
                { label: "3 days", value: "3" },
                { label: "1 week", value: "7" },
                { label: "Date", value: "date" },
              ]}
              onChange={setUseBy}
            />
            {useBy === "date" && (
              <label className="flex flex-col gap-1">
                <span className={labelClass}>Use by</span>
                <input
                  type="date"
                  className={inputClass}
                  value={date}
                  min={today()}
                  onChange={(event) => {
                    setDate(event.target.value);
                  }}
                />
              </label>
            )}
          </div>
        </div>
      )}
      <ErrorText error={add.error} />
      <button
        type="button"
        className={`${primaryButton} py-3.5 text-base`}
        disabled={picked === undefined || add.isPending}
        onClick={save}
      >
        {buttonText}
      </button>
    </div>
  );
};

export default PantryAddPage;
