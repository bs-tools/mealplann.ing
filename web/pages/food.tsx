import { timestampMs } from "@bufbuild/protobuf/wkt";
import Loading from "@components/loading";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import {
  CheckboxField,
  SelectField,
  TextAreaField,
  TextField,
} from "@components/ui/fields";
import { Badge, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import { inputClass, labelClass, primaryButton } from "@components/ui/styles";
import UnsavedChangesGuard, {
  leaveUnsaved,
} from "@components/ui/unsaved-changes-guard";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { ArrowLeftIcon, TrashIcon } from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import {
  deleteFood,
  getFood,
  updateFood,
} from "gen/foods/v1/foods-FoodService_connectquery";
import type { Food } from "gen/foods/v1/foods_pb";
import { FoodService, FoodSource, StoreSection } from "gen/foods/v1/foods_pb";
import { RecipeService } from "gen/recipes/v1/recipes_pb";
import { STORE_SECTIONS } from "helpers/labels";
import type { NutrientKey } from "helpers/nutrients";
import { NUTRIENT_GROUPS, NUTRIENTS } from "helpers/nutrients";
import { optionalNumberInput, parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useNavigate, useParams } from "react-router";

/**
 * FoodPage shows one of the household's foods, to edit all of its fields,
 * including its nutrients per 100 g.
 */
const FoodPage: React.FC = () => {
  const { id = "" } = useParams();
  const { isPending, isError, data, error, refetch } = useQuery(getFood, {
    id,
  });
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
  return <FoodForm key={`${food.id}-${version}`} food={food} />;
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
}

// FoodForm edits a food.
const FoodForm: React.FC<FoodFormProps> = ({ food }) => {
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
  const [storeSection, setStoreSection] = useState(food.storeSection);
  const [fastCarb, setFastCarb] = useState(food.fastCarb);
  const [notes, setNotes] = useState(food.notes);
  const [invalid, setInvalid] = useState("");

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
  if (fastCarb !== food.fastCarb) {
    paths.push("fast_carb");
  }
  if (notes !== food.notes) {
    paths.push("notes");
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
        fastCarb,
        gramsPerMilliliter: parsed.grams_per_milliliter ?? 0,
        gramsPerPiece: parsed.grams_per_piece ?? 0,
        id: food.id,
        name,
        notes,
        nutrients: values,
        openedShelfLifeDays: Math.round(parsed.opened_shelf_life_days ?? 0),
        packageGrams: parsed.package_grams ?? 0,
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
      <div>
        <Link
          to="/foods"
          className="inline-flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900 print:hidden"
        >
          <ArrowLeftIcon className="size-4" aria-hidden="true" />
          Foods
        </Link>
      </div>
      <PageHeader
        title={food.name}
        subtitle={<FoodSourceNote food={food} />}
        actions={
          <>
            <ConfirmButton
              title="Delete food?"
              message={`"${food.name}" will be removed from your foods. Foods used by recipes can't be deleted.`}
              disabled={remove.isPending}
              onConfirm={() => {
                remove.mutate({ id: food.id });
              }}
            >
              <TrashIcon className="size-4" aria-hidden="true" />
              Delete
            </ConfirmButton>
            <button
              type="submit"
              className={primaryButton}
              disabled={!dirty || update.isPending}
            >
              {update.isPending ? "Saving…" : "Save"}
            </button>
          </>
        }
      />
      <ErrorText error={remove.error ?? update.error} />
      {invalid !== "" && <p className="text-sm text-red-700">{invalid}</p>}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <div className="flex flex-col gap-4 lg:col-span-1">
          <Card title="Food">
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
              <div>
                <CheckboxField
                  label="Fast carb"
                  checked={fastCarb}
                  onChange={setFastCarb}
                />
                <p className="mt-1 text-xs text-gray-500">
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
                hint="To weigh amounts in ml."
                onChange={setGramsPerMilliliter}
              />
              <TextField
                label="Pack size (g)"
                value={packageGrams}
                inputMode="decimal"
                onChange={setPackageGrams}
              />
              <span />
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
            </div>
          </Card>
        </div>
        <Card title="Nutrients per 100 g" className="lg:col-span-2">
          <div className="flex flex-col gap-4">
            <p className="text-sm text-gray-600">
              Leave a nutrient empty if it&apos;s unknown: 0 means there&apos;s
              none. Enter labels&apos; &quot;per 100 g&quot; values.
            </p>
            {NUTRIENT_GROUPS.map((group) => (
              <fieldset key={group}>
                <legend className="mb-2 text-sm font-semibold text-gray-800">
                  {group}
                </legend>
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
                            setNutrients({
                              ...nutrients,
                              [nutrient.key]: event.target.value,
                            });
                          }}
                        />
                      </label>
                    )
                  )}
                </div>
              </fieldset>
            ))}
          </div>
        </Card>
      </div>
    </form>
  );
};

interface FoodSourceNoteProps {
  food: Food;
}

// FoodSourceNote says where a food's nutrients come from.
const FoodSourceNote: React.FC<FoodSourceNoteProps> = ({ food }) => {
  if (food.source !== FoodSource.FDC) {
    return (
      <span className="inline-flex items-center gap-2">
        <Badge>Custom</Badge>
        Nutrients entered by hand or from a label.
      </span>
    );
  }
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <Badge className="bg-sky-50 text-sky-800 ring-sky-200">FDC</Badge>
      <span>
        Nutrients from USDA FoodData Central,{" "}
        <a
          href={`https://fdc.nal.usda.gov/food-details/${food.fdcId}/nutrients`}
          target="_blank"
          rel="noreferrer"
          className="font-medium text-brand-600 hover:underline"
        >
          food {food.fdcId}
        </a>
        .
      </span>
    </span>
  );
};

export default FoodPage;
