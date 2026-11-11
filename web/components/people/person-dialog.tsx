import {
  CheckboxField,
  SelectField,
  TextAreaField,
  TextField,
} from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  iconButton,
  inputClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import {
  ArrowDownIcon,
  ArrowUpIcon,
  InformationCircleIcon,
  PlusIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import type { Nutrient } from "gen/foods/v1/foods_pb";
import {
  createPerson,
  updatePerson,
} from "gen/people/v1/people-PersonService_connectquery";
import type { Person } from "gen/people/v1/people_pb";
import {
  EatingTimeKind,
  HouseholdMeal,
  PersonService,
} from "gen/people/v1/people_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import type { HouseholdMember } from "gen/users/v1/users_pb";
import {
  EATING_TIME_KINDS,
  HOUSEHOLD_MEALS,
  optionValue,
} from "helpers/labels";
import { NUTRIENTS, nutrientInfoOf } from "helpers/nutrients";
import { optionalNumberInput, parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

// EatingTimeInput is an eating time being edited.
interface EatingTimeInput {
  // A key for React, as eating times move.
  key: number;
  // The ID of an existing eating time, "" for a new one.
  id: string;
  name: string;
  kind: EatingTimeKind;
  time: string;
  meal: HouseholdMeal;
}

// TargetInput is a daily target being edited.
interface TargetInput {
  key: number;
  nutrient: Nutrient;
  min: string;
  max: string;
}

// The keys of eating times and targets added, unique on the page.
let nextKey = 1;
const newKey = (): number => {
  nextKey += 1;
  return nextKey;
};

const eatingTime = (
  name: string,
  kind: EatingTimeKind,
  time: string,
  meal: HouseholdMeal
): EatingTimeInput => ({ id: "", key: newKey(), kind, meal, name, time });

// PRESETS are common eating patterns, to start a person's from.
const PRESETS: { label: string; times: () => EatingTimeInput[] }[] = [
  {
    label: "Three meals",
    times: () => [
      eatingTime(
        "Breakfast",
        EatingTimeKind.MAIN,
        "07:30",
        HouseholdMeal.BREAKFAST
      ),
      eatingTime("Lunch", EatingTimeKind.MAIN, "12:30", HouseholdMeal.LUNCH),
      eatingTime("Dinner", EatingTimeKind.MAIN, "19:00", HouseholdMeal.DINNER),
    ],
  },
  {
    label: "Six small meals",
    times: () => [
      eatingTime(
        "Breakfast",
        EatingTimeKind.MAIN,
        "07:30",
        HouseholdMeal.BREAKFAST
      ),
      eatingTime(
        "Mid-morning",
        EatingTimeKind.SMALL,
        "10:00",
        HouseholdMeal.UNSPECIFIED
      ),
      eatingTime("Lunch", EatingTimeKind.MAIN, "12:30", HouseholdMeal.LUNCH),
      eatingTime(
        "Mid-afternoon",
        EatingTimeKind.SMALL,
        "15:30",
        HouseholdMeal.UNSPECIFIED
      ),
      eatingTime("Dinner", EatingTimeKind.MAIN, "19:00", HouseholdMeal.DINNER),
      eatingTime(
        "Evening",
        EatingTimeKind.SMALL,
        "21:00",
        HouseholdMeal.UNSPECIFIED
      ),
    ],
  },
];

// RULES are the rules of eating times, with their inputs' labels.
const RULES = [
  {
    hint: "The most an eating time may weigh. Shared meals over it are split, with the rest at the next small meal.",
    key: "maxPortionGrams",
    label: "Maximum portion",
    path: "max_portion_grams",
    unit: "g",
  },
  {
    hint: "The least protein an eating time should have.",
    key: "minProteinGrams",
    label: "Minimum protein",
    path: "min_protein_grams",
    unit: "g",
  },
  {
    hint: "The most carbohydrate an eating time may have.",
    key: "maxCarbohydrateGrams",
    label: "Maximum carbohydrate",
    path: "max_carbohydrate_grams",
    unit: "g",
  },
  {
    hint: "The least protein and fat per gram of carbohydrate, e.g. 0.5 for 1 g of protein or fat per 2 g of carbohydrate.",
    key: "minPairingRatio",
    label: "Minimum pairing ratio",
    path: "min_pairing_ratio",
    unit: "",
  },
  {
    hint: "The most sugars an eating time may have, for dumping.",
    key: "maxSugarsGrams",
    label: "Maximum sugars",
    path: "max_sugars_grams",
    unit: "g",
  },
] as const;

type RuleKey = (typeof RULES)[number]["key"];

const ruleInputs = (person: Person | undefined): Record<RuleKey, string> => ({
  maxCarbohydrateGrams: optionalNumberInput(
    person?.rules?.maxCarbohydrateGrams
  ),
  maxPortionGrams: optionalNumberInput(person?.rules?.maxPortionGrams),
  maxSugarsGrams: optionalNumberInput(person?.rules?.maxSugarsGrams),
  minPairingRatio: optionalNumberInput(person?.rules?.minPairingRatio),
  minProteinGrams: optionalNumberInput(person?.rules?.minProteinGrams),
});

// EatingTimesKey summarizes eating times, to tell whether they changed.
const eatingTimesKey = (times: EatingTimeInput[]): string =>
  JSON.stringify(
    times.map((et) => [et.id, et.name.trim(), et.kind, et.time, et.meal])
  );

const targetsKey = (targets: TargetInput[]): string =>
  JSON.stringify(
    targets.map((target) => [
      target.nutrient,
      target.min.trim(),
      target.max.trim(),
    ])
  );

const NUTRIENT_OPTIONS = NUTRIENTS.map((info) => ({
  label: `${info.label} (${info.unit})`,
  value: info.nutrient,
}));

interface PersonDialogProps {
  // The person to edit, or undefined to create one.
  person?: Person | undefined;
  // The household's users, to link the person to.
  members: HouseholdMember[];
  onClose: () => void;
}

/**
 * PersonDialog creates or edits a person: their name, the user they are,
 * their eating pattern, the rules their eating times are checked against
 * and their daily targets.
 */
const PersonDialog: React.FC<PersonDialogProps> = ({
  person,
  members,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [name, setName] = useState(person?.name ?? "");
  const [userId, setUserId] = useState(person?.userId ?? "");
  const [notes, setNotes] = useState(person?.notes ?? "");
  const initialTimes = (person?.eatingTimes ?? []).map((et) => ({
    id: et.id,
    key: newKey(),
    kind: et.kind,
    meal: et.meal,
    name: et.name,
    time: et.time,
  }));
  const [eatingTimes, setEatingTimes] =
    useState<EatingTimeInput[]>(initialTimes);
  const [rules, setRules] = useState(() => ruleInputs(person));
  const [warnFastCarbs, setWarnFastCarbs] = useState(
    person?.rules?.warnFastCarbs ?? false
  );
  const initialTargets = (person?.dailyTargets ?? []).map((target) => ({
    key: newKey(),
    max: optionalNumberInput(target.maxPerDay),
    min: optionalNumberInput(target.minPerDay),
    nutrient: target.nutrient,
  }));
  const [targets, setTargets] = useState<TargetInput[]>(initialTargets);
  const [invalid, setInvalid] = useState("");

  const onSuccess = async (): Promise<void> => {
    // The plan shows people's eating times and checks their rules.
    await invalidateServices(queryClient, [PersonService, PlanService]);
    onClose();
  };
  const create = useMutation(createPerson, { onSuccess });
  const update = useMutation(updatePerson, { onSuccess });
  const pending = create.isPending || update.isPending;

  // The fields changed, as update mask paths.
  const initialRules = ruleInputs(person);
  const paths: string[] = [];
  if (name !== (person?.name ?? "")) {
    paths.push("name");
  }
  if (userId !== (person?.userId ?? "")) {
    paths.push("user_id");
  }
  if (notes !== (person?.notes ?? "")) {
    paths.push("notes");
  }
  if (eatingTimesKey(eatingTimes) !== eatingTimesKey(initialTimes)) {
    paths.push("eating_times");
  }
  for (const rule of RULES) {
    if (rules[rule.key] !== initialRules[rule.key]) {
      paths.push(`rules.${rule.path}`);
    }
  }
  if (warnFastCarbs !== (person?.rules?.warnFastCarbs ?? false)) {
    paths.push("rules.warn_fast_carbs");
  }
  if (targetsKey(targets) !== targetsKey(initialTargets)) {
    paths.push("daily_targets");
  }
  const dirty = paths.length > 0;

  const setEatingTime = (index: number, change: Partial<EatingTimeInput>) => {
    setEatingTimes(
      eatingTimes.map((et, other) =>
        other === index ? { ...et, ...change } : et
      )
    );
  };
  const moveEatingTime = (index: number, by: number) => {
    const next = [...eatingTimes];
    const [moved] = next.splice(index, 1);
    if (moved !== undefined) {
      next.splice(index + by, 0, moved);
      setEatingTimes(next);
    }
  };
  const setTarget = (index: number, change: Partial<TargetInput>) => {
    setTargets(
      targets.map((target, other) =>
        other === index ? { ...target, ...change } : target
      )
    );
  };

  const save = () => {
    setInvalid("");
    const parsedRules: Partial<Record<RuleKey, number>> = {};
    for (const rule of RULES) {
      const value = parseOptionalNumber(rules[rule.key]);
      if (value === null) {
        setInvalid(
          `${rule.label} must be a number of at least 0, or empty for no rule.`
        );
        return;
      }
      if (value !== undefined) {
        parsedRules[rule.key] = value;
      }
    }
    const dailyTargets = [];
    for (const target of targets) {
      const label = nutrientInfoOf(target.nutrient)?.label ?? "A target";
      const min = parseOptionalNumber(target.min);
      const max = parseOptionalNumber(target.max);
      if (min === null || max === null) {
        setInvalid(`${label}'s target must be numbers of at least 0.`);
        return;
      }
      if (min === undefined && max === undefined) {
        setInvalid(`${label}'s target needs a minimum, a maximum or both.`);
        return;
      }
      dailyTargets.push({
        maxPerDay: max,
        minPerDay: min,
        nutrient: target.nutrient,
      });
    }
    const message = {
      dailyTargets,
      eatingTimes: eatingTimes.map((et) => ({
        id: et.id,
        kind: et.kind,
        meal: et.meal,
        name: et.name,
        time: et.time,
      })),
      id: person?.id ?? "",
      name,
      notes,
      rules: { ...parsedRules, warnFastCarbs },
      userId,
    };
    if (person === undefined) {
      create.mutate({ person: message });
    } else {
      update.mutate({ person: message, updateMask: { paths } });
    }
  };

  const usedNutrients = new Set(targets.map((target) => target.nutrient));
  const freeNutrient = NUTRIENTS.find(
    (info) => !usedNutrients.has(info.nutrient)
  );

  return (
    <Modal
      open
      wide
      dirty={dirty}
      onClose={onClose}
      title={person === undefined ? "New person" : `Edit ${person.name}`}
    >
      <form
        className="flex flex-col gap-5"
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <TextField
            label="Name"
            value={name}
            onChange={setName}
            required
            autoFocus={person === undefined}
          />
          <SelectField
            label="User"
            value={userId}
            options={[
              { label: "Not a user", value: "" },
              ...members.map((member) => ({
                label: member.name,
                value: member.userId,
              })),
            ]}
            onChange={setUserId}
          />
        </div>

        <section className="flex flex-col gap-2">
          <div className="flex flex-wrap items-baseline justify-between gap-2">
            <h3 className="text-sm font-semibold text-gray-900">
              Eating pattern
            </h3>
            {eatingTimes.length === 0 && (
              <div className="flex gap-2">
                {PRESETS.map((preset) => (
                  <button
                    key={preset.label}
                    type="button"
                    className={ghostButton}
                    onClick={() => {
                      setEatingTimes(preset.times());
                    }}
                  >
                    {preset.label}
                  </button>
                ))}
              </div>
            )}
          </div>
          <p className="text-xs text-gray-500">
            The times of day the person eats, in order. A shared meal gives them
            a portion of what the household cooks for it.
          </p>
          <ol className="flex flex-col gap-2">
            {eatingTimes.map((et, index) => (
              <li
                key={et.key}
                className="grid grid-cols-[1fr_auto] items-end gap-2 rounded-md border border-gray-200 p-2 sm:grid-cols-[2fr_1.3fr_1fr_1.3fr_auto]"
              >
                <TextField
                  label="Name"
                  value={et.name}
                  onChange={(value) => {
                    setEatingTime(index, { name: value });
                  }}
                />
                <div className="flex gap-0.5 self-end pb-1 sm:order-last">
                  <button
                    type="button"
                    className={iconButton}
                    aria-label="Move up"
                    title="Move up"
                    disabled={index === 0}
                    onClick={() => {
                      moveEatingTime(index, -1);
                    }}
                  >
                    <ArrowUpIcon className="size-4" />
                  </button>
                  <button
                    type="button"
                    className={iconButton}
                    aria-label="Move down"
                    title="Move down"
                    disabled={index === eatingTimes.length - 1}
                    onClick={() => {
                      moveEatingTime(index, 1);
                    }}
                  >
                    <ArrowDownIcon className="size-4" />
                  </button>
                  <button
                    type="button"
                    className={iconButton}
                    aria-label="Remove"
                    title="Remove"
                    onClick={() => {
                      setEatingTimes(
                        eatingTimes.filter((_et, other) => other !== index)
                      );
                    }}
                  >
                    <XMarkIcon className="size-4" />
                  </button>
                </div>
                <SelectField
                  label="Kind"
                  value={String(et.kind)}
                  options={[
                    {
                      label: "Choose",
                      value: String(EatingTimeKind.UNSPECIFIED),
                    },
                    ...EATING_TIME_KINDS.map((option) => ({
                      label: option.label,
                      value: String(option.value),
                    })),
                  ]}
                  onChange={(value) => {
                    setEatingTime(index, {
                      kind: optionValue(
                        EATING_TIME_KINDS,
                        value,
                        EatingTimeKind.UNSPECIFIED
                      ),
                    });
                  }}
                />
                <label className="flex flex-col gap-1">
                  <span className={labelClass}>Usual time</span>
                  <input
                    type="time"
                    className={inputClass}
                    value={et.time}
                    onChange={(event) => {
                      setEatingTime(index, { time: event.target.value });
                    }}
                  />
                </label>
                <SelectField
                  className="col-span-2 sm:col-span-1"
                  label="Shared meal"
                  value={String(et.meal)}
                  options={[
                    { label: "None", value: String(HouseholdMeal.UNSPECIFIED) },
                    ...HOUSEHOLD_MEALS.map((option) => ({
                      label: option.label,
                      value: String(option.value),
                    })),
                  ]}
                  onChange={(value) => {
                    setEatingTime(index, {
                      meal: optionValue(
                        HOUSEHOLD_MEALS,
                        value,
                        HouseholdMeal.UNSPECIFIED
                      ),
                    });
                  }}
                />
              </li>
            ))}
          </ol>
          {person !== undefined && eatingTimes.length < initialTimes.length && (
            <p className="text-xs text-amber-700">
              Removing an eating time deletes what&apos;s planned at it.
            </p>
          )}
          <div>
            <button
              type="button"
              className={ghostButton}
              disabled={eatingTimes.length >= 12}
              onClick={() => {
                setEatingTimes([
                  ...eatingTimes,
                  eatingTime(
                    "",
                    EatingTimeKind.SMALL,
                    "",
                    HouseholdMeal.UNSPECIFIED
                  ),
                ]);
              }}
            >
              <PlusIcon className="size-4" aria-hidden="true" />
              Add eating time
            </button>
          </div>
        </section>

        <div className="flex gap-2 rounded-md bg-sky-50 p-3 text-sm text-sky-900">
          <InformationCircleIcon
            className="size-5 shrink-0"
            aria-hidden="true"
          />
          <p>
            mealplann.ing checks the plan against these rules and targets and
            warns. Targets recommended from the profile only apply once you use
            them. Leave a field empty for no rule.
          </p>
        </div>

        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold text-gray-900">
            Rules for every eating time
          </h3>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            {RULES.map((rule) => (
              <TextField
                key={rule.key}
                label={
                  rule.unit === "" ? rule.label : `${rule.label} (${rule.unit})`
                }
                value={rules[rule.key]}
                inputMode="decimal"
                hint={rule.hint}
                onChange={(value) => {
                  setRules({ ...rules, [rule.key]: value });
                }}
              />
            ))}
            <div className="flex flex-col justify-center gap-1">
              <CheckboxField
                label="Warn about fast carbs"
                checked={warnFastCarbs}
                onChange={setWarnFastCarbs}
              />
              <span className="text-xs text-gray-500">
                Foods marked as fast carbs, like white bread or juice.
              </span>
            </div>
          </div>
        </section>

        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold text-gray-900">Daily targets</h3>
          {targets.length === 0 && (
            <p className="text-xs text-gray-500">No daily targets.</p>
          )}
          <ul className="flex flex-col gap-2">
            {targets.map((target, index) => (
              <li
                key={target.key}
                className="grid grid-cols-[1fr_1fr_auto] items-end gap-2 sm:grid-cols-[2fr_1fr_1fr_auto]"
              >
                <SelectField
                  className="col-span-3 sm:col-span-1"
                  label="Nutrient"
                  value={String(target.nutrient)}
                  options={NUTRIENT_OPTIONS.filter(
                    (option) =>
                      option.value === target.nutrient ||
                      !usedNutrients.has(option.value)
                  ).map((option) => ({
                    label: option.label,
                    value: String(option.value),
                  }))}
                  onChange={(value) => {
                    setTarget(index, {
                      nutrient: optionValue(
                        NUTRIENT_OPTIONS,
                        value,
                        target.nutrient
                      ),
                    });
                  }}
                />
                <TextField
                  label="Minimum a day"
                  value={target.min}
                  inputMode="decimal"
                  onChange={(value) => {
                    setTarget(index, { min: value });
                  }}
                />
                <TextField
                  label="Maximum a day"
                  value={target.max}
                  inputMode="decimal"
                  onChange={(value) => {
                    setTarget(index, { max: value });
                  }}
                />
                <button
                  type="button"
                  className={`${iconButton} mb-1`}
                  aria-label="Remove target"
                  title="Remove target"
                  onClick={() => {
                    setTargets(
                      targets.filter((_target, other) => other !== index)
                    );
                  }}
                >
                  <XMarkIcon className="size-4" />
                </button>
              </li>
            ))}
          </ul>
          <div>
            <button
              type="button"
              className={ghostButton}
              disabled={freeNutrient === undefined}
              onClick={() => {
                if (freeNutrient !== undefined) {
                  setTargets([
                    ...targets,
                    {
                      key: newKey(),
                      max: "",
                      min: "",
                      nutrient: freeNutrient.nutrient,
                    },
                  ]);
                }
              }}
            >
              <PlusIcon className="size-4" aria-hidden="true" />
              Add target
            </button>
          </div>
        </section>

        <TextAreaField
          label="Notes"
          value={notes}
          onChange={setNotes}
          rows={2}
        />

        {invalid !== "" && <p className="text-sm text-red-700">{invalid}</p>}
        <ErrorText error={create.error ?? update.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={pending || (person !== undefined && !dirty)}
          >
            {pending ? "Saving…" : "Save"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default PersonDialog;
