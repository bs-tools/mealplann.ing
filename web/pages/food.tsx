import { timestampMs } from "@bufbuild/protobuf/wkt";
import Loading from "@components/loading";
import { FoodStock } from "@components/pantry/in-the-kitchen";
import BackLink from "@components/ui/back-link";
import Card from "@components/ui/card";
import { ConfirmDialog } from "@components/ui/confirm-button";
import {
  CheckboxField,
  SelectField,
  TextAreaField,
  TextField,
} from "@components/ui/fields";
import { Badge, PageHeader } from "@components/ui/misc";
import MoreMenu from "@components/ui/more-menu";
import QueryError, { ErrorText } from "@components/ui/query-error";
import { inputClass, labelClass, primaryButton } from "@components/ui/styles";
import UnsavedChangesGuard, {
  leaveUnsaved,
} from "@components/ui/unsaved-changes-guard";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { ChevronDownIcon } from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import {
  deleteFood,
  getFood,
  updateFood,
} from "gen/foods/v1/foods-FoodService_connectquery";
import type { Food } from "gen/foods/v1/foods_pb";
import { FoodService, Measure, StoreSection } from "gen/foods/v1/foods_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { listStores } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import type { Store } from "gen/shopping/v1/shopping_pb";
import { today } from "helpers/dates";
import { formatDollars, formatShortDate } from "helpers/format";
import {
  MEASURES,
  STORE_SECTIONS,
  foodSourceInfo,
  optionValue,
} from "helpers/labels";
import type { NutrientGroup, NutrientKey } from "helpers/nutrients";
import { NUTRIENTS, formatAmount } from "helpers/nutrients";
import { optionalNumberInput, parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { useNavigate, useParams } from "react-router";

/**
 * PriceHint says what a pack costs at a price per kilogram, and when the
 * price was seen, e.g. "$3.49 a 900 g pack · seen Nov 1".
 */
const priceHint = (
  price: string,
  packageGrams: string,
  priceDate: string
): string => {
  const parts: string[] = [];
  const dollars = Number(price);
  const grams = Number(packageGrams);
  if (price !== "" && dollars > 0 && grams > 0) {
    parts.push(`${formatDollars((dollars * grams) / 10)} a ${grams} g pack`);
  }
  if (priceDate !== "") {
    parts.push(`seen ${formatShortDate(priceDate)}`);
  }
  return parts.length > 0
    ? parts.join(" · ")
    : "What it costs where you shop, to estimate meals' costs.";
};

// MACROS are the nutrients a food's summary shows, per 100 g.
const MACROS: { key: NutrientKey; label: string; unit: string }[] = [
  { key: "energyKcal", label: "Energy", unit: "kcal" },
  { key: "proteinGrams", label: "Protein", unit: "g" },
  { key: "carbohydrateGrams", label: "Carbs", unit: "g" },
  { key: "fatGrams", label: "Fat", unit: "g" },
];

/**
 * FoodPage shows one of the household's foods, to edit all of its fields,
 * including its nutrients per 100 g.
 */
const FoodPage: React.FC = () => {
  const { id = "" } = useParams();
  const { isPending, isError, data, error, refetch } = useQuery(getFood, {
    id,
  });
  const stores = useQuery(listStores, {});
  if (isPending) {
    return <Loading />;
  }
  if (isError) {
    return (
      <QueryError
        error={error}
        onRetry={() => {
          void refetch();
        }}
      />
    );
  }
  const { food } = data;
  if (food === undefined) {
    return null;
  }
  /*
   * The form starts over from each saved version, so that it shows what
   * the server stored.
   */
  const version =
    food.updateTime === undefined ? 0 : timestampMs(food.updateTime);
  return (
    <FoodForm
      key={`${food.id}-${version}`}
      food={food}
      stores={stores.data?.stores ?? []}
    />
  );
};

// NutrientInputs are the text of the nutrients' inputs.
type NutrientInputs = Partial<Record<NutrientKey, string>>;

const nutrientInputs = (food: Food): NutrientInputs => {
  const inputs: NutrientInputs = {};
  for (const nutrient of NUTRIENTS) {
    inputs[nutrient.key] = optionalNumberInput(food.nutrients?.[nutrient.key]);
  }
  return inputs;
};

// ZeroAsEmpty shows 0, which the API uses for unknown, as an empty input.
const zeroAsEmpty = (value: number): string =>
  value === 0 ? "" : String(value);

interface FoodFormProps {
  food: Food;
  // The household's stores, to choose the food's own store from.
  stores: Store[];
}

// FoodForm edits a food.
const FoodForm: React.FC<FoodFormProps> = ({ food, stores }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState(food.name);
  const [nutrients, setNutrients] = useState(() => nutrientInputs(food));
  const [gramsPerPiece, setGramsPerPiece] = useState(
    zeroAsEmpty(food.gramsPerPiece)
  );
  const [gramsPerMilliliter, setGramsPerMilliliter] = useState(
    zeroAsEmpty(food.gramsPerMilliliter)
  );
  const [packageGrams, setPackageGrams] = useState(
    zeroAsEmpty(food.packageGrams)
  );
  const [shelfLifeDays, setShelfLifeDays] = useState(
    zeroAsEmpty(food.shelfLifeDays)
  );
  const [openedShelfLifeDays, setOpenedShelfLifeDays] = useState(
    zeroAsEmpty(food.openedShelfLifeDays)
  );
  const [freezerShelfLifeDays, setFreezerShelfLifeDays] = useState(
    zeroAsEmpty(food.freezerShelfLifeDays)
  );
  const [storeSection, setStoreSection] = useState(food.storeSection);
  const [measure, setMeasure] = useState(food.measure);
  const [fastCarb, setFastCarb] = useState(food.fastCarb);
  const [preferredStoreId, setPreferredStoreId] = useState(
    food.preferredStoreId
  );
  const [alwaysInStock, setAlwaysInStock] = useState(food.alwaysInStock);
  // The price in dollars per kilogram, as entered.
  const initialPrice =
    food.priceCentsPerKilogram > 0
      ? (food.priceCentsPerKilogram / 100).toFixed(2)
      : "";
  const [price, setPrice] = useState(initialPrice);
  const [notes, setNotes] = useState(food.notes);
  const [invalid, setInvalid] = useState("");
  const [deleting, setDeleting] = useState(false);

  const update = useMutation(updateFood, {
    onSuccess: async () => {
      // Recipes compute their nutrition from their foods.
      await invalidateServices(queryClient, [FoodService, RecipeService]);
    },
  });
  const remove = useMutation(deleteFood, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [FoodService]);
      void navigate("/foods", leaveUnsaved);
    },
  });

  const initialNutrients = nutrientInputs(food);
  const changedNutrients = NUTRIENTS.filter(
    (nutrient) => nutrients[nutrient.key] !== initialNutrients[nutrient.key]
  );
  const numbers: {
    path: string;
    label: string;
    text: string;
    initial: number;
  }[] = [
    {
      initial: food.gramsPerPiece,
      label: "Weight per piece",
      path: "grams_per_piece",
      text: gramsPerPiece,
    },
    {
      initial: food.gramsPerMilliliter,
      label: "Density",
      path: "grams_per_milliliter",
      text: gramsPerMilliliter,
    },
    {
      initial: food.packageGrams,
      label: "Pack size",
      path: "package_grams",
      text: packageGrams,
    },
    {
      initial: food.shelfLifeDays,
      label: "Keeps for",
      path: "shelf_life_days",
      text: shelfLifeDays,
    },
    {
      initial: food.openedShelfLifeDays,
      label: "Keeps once opened for",
      path: "opened_shelf_life_days",
      text: openedShelfLifeDays,
    },
    {
      initial: food.freezerShelfLifeDays,
      label: "Keeps frozen for",
      path: "freezer_shelf_life_days",
      text: freezerShelfLifeDays,
    },
  ];
  const paths: string[] = [];
  if (name !== food.name) {
    paths.push("name");
  }
  for (const number of numbers) {
    if (number.text !== zeroAsEmpty(number.initial)) {
      paths.push(number.path);
    }
  }
  if (storeSection !== food.storeSection) {
    paths.push("store_section");
  }
  if (measure !== food.measure) {
    paths.push("measure");
  }
  if (fastCarb !== food.fastCarb) {
    paths.push("fast_carb");
  }
  if (preferredStoreId !== food.preferredStoreId) {
    paths.push("preferred_store_id");
  }
  if (alwaysInStock !== food.alwaysInStock) {
    paths.push("always_in_stock");
  }
  if (notes !== food.notes) {
    paths.push("notes");
  }
  if (price !== initialPrice) {
    paths.push("price_cents_per_kilogram", "price_date");
  }
  for (const nutrient of changedNutrients) {
    paths.push(`nutrients.${nutrient.field}`);
  }
  const dirty = paths.length > 0;

  const save = () => {
    setInvalid("");
    const parsed: Record<string, number> = {};
    for (const number of numbers) {
      const value = parseOptionalNumber(number.text);
      if (value === null) {
        setInvalid(`${number.label} must be a number of at least 0.`);
        return;
      }
      parsed[number.path] = value ?? 0;
    }
    const priceDollars = parseOptionalNumber(price);
    if (priceDollars === null) {
      setInvalid(
        "The price must be dollars per kilogram, like 4.99, or empty if unknown."
      );
      return;
    }
    const values: Partial<Record<NutrientKey, number>> = {};
    for (const nutrient of NUTRIENTS) {
      const value = parseOptionalNumber(nutrients[nutrient.key] ?? "");
      if (value === null) {
        setInvalid(
          `${nutrient.label} must be a number of at least 0, or empty if unknown.`
        );
        return;
      }
      if (value !== undefined) {
        values[nutrient.key] = value;
      }
    }
    update.mutate({
      food: {
        alwaysInStock,
        fastCarb,
        freezerShelfLifeDays: Math.round(parsed.freezer_shelf_life_days ?? 0),
        gramsPerMilliliter: parsed.grams_per_milliliter ?? 0,
        gramsPerPiece: parsed.grams_per_piece ?? 0,
        id: food.id,
        measure,
        name,
        notes,
        nutrients: values,
        openedShelfLifeDays: Math.round(parsed.opened_shelf_life_days ?? 0),
        packageGrams: parsed.package_grams ?? 0,
        preferredStoreId,
        priceCentsPerKilogram: Math.round((priceDollars ?? 0) * 100),
        // A price entered here was seen today.
        priceDate: priceDollars === undefined ? "" : today(),
        shelfLifeDays: Math.round(parsed.shelf_life_days ?? 0),
        storeSection,
      },
      updateMask: { paths },
    });
  };

  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        save();
      }}
    >
      <UnsavedChangesGuard dirty={dirty && !remove.isSuccess} />
      <BackLink to="/foods">Foods</BackLink>
      <div className="flex items-start gap-2">
        <div className="min-w-0 grow">
          <PageHeader
            title={food.name}
            subtitle={<FoodSourceNote food={food} />}
          />
        </div>
        <MoreMenu
          label={`More for ${food.name}`}
          items={[
            {
              danger: true,
              disabled: remove.isPending,
              label: "Delete",
              onClick: () => {
                setDeleting(true);
              },
            },
          ]}
        />
        <ConfirmDialog
          open={deleting}
          title="Delete food?"
          message={`"${food.name}" will be removed from your foods. Foods used by recipes can't be deleted.`}
          onClose={() => {
            setDeleting(false);
          }}
          onConfirm={() => {
            remove.mutate({ id: food.id });
          }}
        />
      </div>
      <div className="-mt-4 grid grid-cols-4 gap-2">
        {MACROS.map((macro) => (
          <div
            key={macro.key}
            className="flex flex-col rounded-2xl border border-line bg-white px-2 py-2.5 text-center"
          >
            <span className="text-lg font-bold text-ink-soft tabular-nums">
              {food.nutrients?.[macro.key] === undefined
                ? "–"
                : formatAmount(food.nutrients[macro.key] ?? 0)}
              <span className="text-xs font-medium text-muted">
                {" "}
                {macro.unit}
              </span>
            </span>
            <span className="text-xs text-muted">{macro.label}</span>
          </div>
        ))}
      </div>
      <p className="-mt-2 text-xs text-muted">Per 100 g, as saved.</p>
      <FoodStock foodId={food.id} foodName={food.name} />
      <ErrorText error={remove.error ?? update.error} />
      {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
      <div className="grid grid-cols-1 gap-5 lg:grid-cols-2">
        <div className="flex flex-col gap-5">
          <Card title="Nutrients per 100 g">
            <div className="flex flex-col gap-3">
              <p className="text-sm text-muted">
                Leave a nutrient empty if it&apos;s unknown: 0 means
                there&apos;s none. Enter labels&apos; &quot;per 100 g&quot;
                values.
              </p>
              <NutrientFields
                group="Energy and macronutrients"
                nutrients={nutrients}
                onChange={setNutrients}
              />
            </div>
          </Card>
          {(["Vitamins", "Minerals"] as const).map((group) => {
            const keys = NUTRIENTS.filter(
              (nutrient) => nutrient.group === group
            );
            const known = keys.filter(
              (nutrient) => (nutrients[nutrient.key] ?? "") !== ""
            ).length;
            return (
              <details
                key={group}
                className="group rounded-2xl border border-line bg-white"
              >
                <summary className="flex cursor-pointer list-none items-center justify-between gap-2 p-4 [&::-webkit-details-marker]:hidden">
                  <span className="font-bold text-ink-soft">{group}</span>
                  <span className="flex items-center gap-1 text-sm text-muted">
                    {known} of {keys.length} known
                    <ChevronDownIcon
                      className="size-4 transition-transform group-open:rotate-180"
                      aria-hidden="true"
                    />
                  </span>
                </summary>
                <div className="px-4 pb-4">
                  <NutrientFields
                    group={group}
                    nutrients={nutrients}
                    onChange={setNutrients}
                  />
                </div>
              </details>
            );
          })}
        </div>
        <div className="flex flex-col gap-5">
          <Card title="In the kitchen">
            <div className="flex flex-col gap-3">
              <TextField label="Name" value={name} onChange={setName} />
              <SelectField
                label="Store section"
                value={String(storeSection)}
                options={[
                  { label: "Not set", value: String(StoreSection.UNSPECIFIED) },
                  ...STORE_SECTIONS.map((option) => ({
                    label: option.label,
                    value: String(option.value),
                  })),
                ]}
                onChange={(value) => {
                  // The option's own value keeps the enum's type.
                  setStoreSection(
                    STORE_SECTIONS.find(
                      (option) => String(option.value) === value
                    )?.value ?? StoreSection.UNSPECIFIED
                  );
                }}
              />
              {stores.length > 1 && (
                <SelectField
                  label="Store"
                  value={preferredStoreId}
                  options={[
                    {
                      label: `The main store (${stores[0]?.name ?? ""})`,
                      value: "",
                    },
                    ...stores.slice(1).map((store) => ({
                      label: store.name,
                      value: store.id,
                    })),
                  ]}
                  onChange={setPreferredStoreId}
                />
              )}
              <div>
                <CheckboxField
                  label="Always in stock"
                  checked={alwaysInStock}
                  onChange={setAlwaysInStock}
                />
                <p className="mt-1 text-xs text-muted">
                  For things you always have, like salt and spices: the pantry
                  doesn&apos;t weigh them, and the shopping list leaves them
                  out.
                </p>
              </div>
              <div>
                <CheckboxField
                  label="Fast carb"
                  checked={fastCarb}
                  onChange={setFastCarb}
                />
                <p className="mt-1 text-xs text-muted">
                  Quickly absorbed carbohydrate, like white bread, juice, sugar
                  or dried fruit. It matters for reactive hypoglycemia.
                </p>
              </div>
              <TextAreaField label="Notes" value={notes} onChange={setNotes} />
            </div>
          </Card>
          <Card title="Weighing and keeping">
            <div className="grid grid-cols-2 gap-3">
              <TextField
                label="Weight per piece (g)"
                value={gramsPerPiece}
                inputMode="decimal"
                hint="To weigh amounts in pieces."
                onChange={setGramsPerPiece}
              />
              <TextField
                label="Density (g/ml)"
                value={gramsPerMilliliter}
                inputMode="decimal"
                hint="To weigh amounts in ml, and show weights by volume."
                onChange={setGramsPerMilliliter}
              />
              <SelectField
                className="col-span-2"
                label="Show amounts in recipes"
                value={String(measure)}
                options={MEASURES.map((option) => ({
                  label: option.label,
                  value: String(option.value),
                }))}
                onChange={(value) => {
                  setMeasure(optionValue(MEASURES, value, Measure.UNSPECIFIED));
                }}
              />
              <TextField
                label="Pack size (g)"
                value={packageGrams}
                inputMode="decimal"
                onChange={setPackageGrams}
              />
              <TextField
                label="Price ($/kg)"
                value={price}
                inputMode="decimal"
                placeholder="Unknown"
                hint={priceHint(price, packageGrams, food.priceDate)}
                onChange={setPrice}
              />
              <TextField
                label="Keeps (days)"
                value={shelfLifeDays}
                inputMode="numeric"
                onChange={setShelfLifeDays}
              />
              <TextField
                label="Once opened (days)"
                value={openedShelfLifeDays}
                inputMode="numeric"
                onChange={setOpenedShelfLifeDays}
              />
              <TextField
                label="Frozen (days)"
                value={freezerShelfLifeDays}
                inputMode="numeric"
                onChange={setFreezerShelfLifeDays}
              />
            </div>
          </Card>
        </div>
      </div>
      {(dirty || update.isPending) && (
        <div className="sticky bottom-[3.75rem] z-10 bg-ground/90 py-2 backdrop-blur lg:bottom-4">
          <button
            type="submit"
            className={`${primaryButton} w-full py-3.5 text-base`}
            disabled={update.isPending}
          >
            {update.isPending ? "Saving…" : "Save changes"}
          </button>
        </div>
      )}
    </form>
  );
};

interface NutrientFieldsProps {
  group: NutrientGroup;
  // The nutrients as typed, by key.
  nutrients: Partial<Record<NutrientKey, string>>;
  onChange: (nutrients: Partial<Record<NutrientKey, string>>) => void;
}

// NutrientFields are the inputs of a group of nutrients, per 100 g.
const NutrientFields: React.FC<NutrientFieldsProps> = ({
  group,
  nutrients,
  onChange,
}) => (
  <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
    {NUTRIENTS.filter((nutrient) => nutrient.group === group).map(
      (nutrient) => (
        <label
          key={nutrient.key}
          className="flex flex-col gap-1"
          title={nutrient.hint}
        >
          <span className={labelClass}>
            {nutrient.label} ({nutrient.unit})
          </span>
          <input
            className={`${inputClass} tabular-nums`}
            inputMode="decimal"
            value={nutrients[nutrient.key] ?? ""}
            placeholder="Unknown"
            onChange={(event) => {
              onChange({ ...nutrients, [nutrient.key]: event.target.value });
            }}
          />
        </label>
      )
    )}
  </div>
);

interface FoodSourceNoteProps {
  food: Food;
}

// FoodSourceNote says where a food's nutrients come from.
const FoodSourceNote: React.FC<FoodSourceNoteProps> = ({ food }) => {
  const info = foodSourceInfo(food.source, food.sourceId);
  if (info === undefined) {
    return (
      <span className="inline-flex items-center gap-2">
        <Badge>Custom</Badge>
        Nutrients entered by hand or from a label.
      </span>
    );
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <Badge className={info.badgeClass}>{info.badge}</Badge>
      <span>
        Nutrients from {info.name},{" "}
        <a
          href={info.url}
          target="_blank"
          rel="noreferrer"
          className="font-medium text-pine hover:underline"
        >
          food {food.sourceId}
        </a>
        .
      </span>
    </span>
  );
};

export default FoodPage;
