import Loading from "@components/loading";
import { PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  cardClass,
  choiceClass,
  inputClass,
  labelClass,
  primaryButton,
  secondaryButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { ArrowLeftIcon, XMarkIcon } from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { Nutrient } from "gen/foods/v1/foods_pb";
import {
  getPerson,
  listPeople,
  updatePerson,
} from "gen/people/v1/people-PersonService_connectquery";
import type { NutrientTarget, Person } from "gen/people/v1/people_pb";
import {
  Activity,
  Goal,
  PersonService,
  Sex,
  SmallMealTaste,
  SpiceLevel,
} from "gen/people/v1/people_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import type { User } from "gen/users/v1/users_pb";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, Navigate, useParams } from "react-router";

const SEXES = [
  { kind: Sex.FEMALE, label: "Female", value: String(Sex.FEMALE) },
  { kind: Sex.MALE, label: "Male", value: String(Sex.MALE) },
];

const ACTIVITIES = [
  { kind: Activity.LOW, label: "Low", value: String(Activity.LOW) },
  { kind: Activity.LIGHT, label: "Light", value: String(Activity.LIGHT) },
  { kind: Activity.ACTIVE, label: "Active", value: String(Activity.ACTIVE) },
  { kind: Activity.VERY, label: "Very", value: String(Activity.VERY) },
];

const ACTIVITY_HINTS: Record<Activity, string> = {
  [Activity.UNSPECIFIED]: "",
  [Activity.LOW]: "Mostly sitting, little exercise",
  [Activity.LIGHT]: "Light exercise 1–3 days a week",
  [Activity.ACTIVE]: "Moderate exercise 3–5 days a week",
  [Activity.VERY]: "Hard exercise most days",
};

const GOALS = [
  { kind: Goal.MAINTAIN, label: "Maintain", value: String(Goal.MAINTAIN) },
  {
    kind: Goal.BUILD_STRENGTH,
    label: "Build strength",
    value: String(Goal.BUILD_STRENGTH),
  },
  {
    kind: Goal.LOSE_BODY_FAT,
    label: "Lose body fat",
    value: String(Goal.LOSE_BODY_FAT),
  },
];

const GOAL_HINTS: Record<Goal, string> = {
  [Goal.UNSPECIFIED]: "",
  [Goal.MAINTAIN]: "Keep your weight, eating what you use.",
  [Goal.BUILD_STRENGTH]: "A little more energy, and more protein.",
  [Goal.LOSE_BODY_FAT]:
    "Less energy, more protein, and a little less carbohydrate and fat.",
};

const SMALL_MEALS = [
  {
    kind: SmallMealTaste.SAVORY,
    label: "Savory",
    value: String(SmallMealTaste.SAVORY),
  },
  {
    kind: SmallMealTaste.SWEET,
    label: "Sweet",
    value: String(SmallMealTaste.SWEET),
  },
  {
    kind: SmallMealTaste.UNSPECIFIED,
    label: "Either",
    value: String(SmallMealTaste.UNSPECIFIED),
  },
];

const SPICES = [
  { kind: SpiceLevel.MILD, label: "Mild", value: String(SpiceLevel.MILD) },
  {
    kind: SpiceLevel.MEDIUM,
    label: "Medium",
    value: String(SpiceLevel.MEDIUM),
  },
  { kind: SpiceLevel.HOT, label: "Hot", value: String(SpiceLevel.HOT) },
  {
    kind: SpiceLevel.UNSPECIFIED,
    label: "Any",
    value: String(SpiceLevel.UNSPECIFIED),
  },
];

// KindOf returns the enum value of the chosen option.
const kindOf = <Kind,>(
  options: { kind: Kind; value: string }[],
  value: string,
  fallback: Kind
): Kind => options.find((option) => option.value === value)?.kind ?? fallback;

// NumberText formats an optional number for an input, "" if unset.
const numberText = (value: number | undefined): string =>
  value === undefined || value === 0 ? "" : String(value);

// ParseNumber parses an input's number, undefined if empty or invalid.
const parseNumber = (text: string): number | undefined => {
  const value = Number.parseFloat(text.replace(",", "."));
  return Number.isFinite(value) && value > 0 ? value : undefined;
};

interface TasteChipsProps {
  label: string;
  items: string[];
  placeholder: string;
  onChange: (items: string[]) => void;
}

/**
 * TasteChips edits a list of likes or dislikes as chips, adding one on
 * Enter or with the button.
 */
const TasteChips: React.FC<TasteChipsProps> = ({
  label,
  items,
  placeholder,
  onChange,
}) => {
  const [text, setText] = useState("");
  const add = (): void => {
    const item = text.trim();
    if (
      item !== "" &&
      !items.some((other) => other.toLowerCase() === item.toLowerCase())
    ) {
      onChange([...items, item]);
    }
    setText("");
  };
  return (
    <div className="flex flex-col gap-2">
      <span className={labelClass}>{label}</span>
      {items.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {items.map((item) => (
            <span
              key={item}
              className="inline-flex items-center gap-1 rounded-full bg-ground-dark py-1 pr-1.5 pl-3 text-sm text-ink-soft"
            >
              {item}
              <button
                type="button"
                className="rounded-full p-0.5 text-muted hover:bg-white hover:text-ink-soft"
                aria-label={`Remove ${item}`}
                onClick={() => {
                  onChange(items.filter((other) => other !== item));
                }}
              >
                <XMarkIcon className="size-4" aria-hidden="true" />
              </button>
            </span>
          ))}
        </div>
      )}
      <div className="flex gap-2">
        <input
          type="text"
          value={text}
          placeholder={placeholder}
          maxLength={50}
          className={inputClass}
          onChange={(event) => {
            setText(event.target.value);
          }}
          onKeyDown={(event) => {
            if (event.key === "Enter") {
              event.preventDefault();
              add();
            }
          }}
        />
        <button
          type="button"
          className={secondaryButton}
          disabled={text.trim() === ""}
          onClick={add}
        >
          Add
        </button>
      </div>
    </div>
  );
};

// TargetChange is a daily target the recommendation would set.
interface TargetChange {
  nutrient: Nutrient;
  label: string;
  min?: number;
  max?: number;
}

/**
 * WithTargets returns a person's daily targets with the changes made,
 * keeping their other targets.
 */
const withTargets = (
  targets: NutrientTarget[],
  changes: TargetChange[]
): {
  nutrient: Nutrient;
  minPerDay?: number | undefined;
  maxPerDay?: number | undefined;
}[] => [
  ...targets
    .filter(
      (target) => !changes.some((change) => change.nutrient === target.nutrient)
    )
    .map((target) => ({
      maxPerDay: target.maxPerDay,
      minPerDay: target.minPerDay,
      nutrient: target.nutrient,
    })),
  ...changes.map((change) => ({
    maxPerDay: change.max,
    minPerDay: change.min,
    nutrient: change.nutrient,
  })),
];

interface ProfileFormProps {
  person: Person;
}

/**
 * ProfileForm edits a person's body, activity, goal and tastes, showing
 * what the profile recommends they eat a day, which they can use as their
 * daily targets.
 */
const ProfileForm: React.FC<ProfileFormProps> = ({ person }) => {
  const queryClient = useQueryClient();
  const { profile, tastes } = person;
  const [height, setHeight] = useState(numberText(profile?.heightCm));
  const [weight, setWeight] = useState(numberText(profile?.weightKg));
  const [birthYear, setBirthYear] = useState(numberText(profile?.birthYear));
  const [sex, setSex] = useState(String(profile?.sex ?? Sex.UNSPECIFIED));
  const [activity, setActivity] = useState(
    String(profile?.activity ?? Activity.UNSPECIFIED)
  );
  const [goal, setGoal] = useState(String(profile?.goal ?? Goal.UNSPECIFIED));
  const [smallMeals, setSmallMeals] = useState(
    String(tastes?.smallMeals ?? SmallMealTaste.UNSPECIFIED)
  );
  const [spice, setSpice] = useState(
    String(tastes?.spice ?? SpiceLevel.UNSPECIFIED)
  );
  const [likes, setLikes] = useState(tastes?.likes ?? []);
  const [dislikes, setDislikes] = useState(tastes?.dislikes ?? []);
  const [saved, setSaved] = useState(false);

  const onSuccess = async (): Promise<void> => {
    setSaved(true);
    await invalidateServices(queryClient, [
      PersonService,
      PlanService,
      SuggestionService,
    ]);
  };
  const save = useMutation(updatePerson, { onSuccess });
  const adopt = useMutation(updatePerson, { onSuccess });

  const rec = person.recommended;
  const changes: TargetChange[] =
    rec === undefined
      ? []
      : [
          {
            label: "Energy, at most",
            max: rec.energyKcal,
            nutrient: Nutrient.ENERGY_KCAL,
          },
          {
            label: "Protein, at least",
            min: rec.proteinGrams,
            nutrient: Nutrient.PROTEIN_GRAMS,
          },
          {
            label: "Carbohydrate, at most",
            max: rec.carbohydrateGrams,
            nutrient: Nutrient.CARBOHYDRATE_GRAMS,
          },
          {
            label: "Fat, at most",
            max: rec.fatGrams,
            nutrient: Nutrient.FAT_GRAMS,
          },
        ];
  const adopted = changes.every((change) =>
    person.dailyTargets.some(
      (target) =>
        target.nutrient === change.nutrient &&
        target.minPerDay === change.min &&
        target.maxPerDay === change.max
    )
  );
  const goalNumber = kindOf(GOALS, goal, Goal.UNSPECIFIED);
  const activityNumber = kindOf(ACTIVITIES, activity, Activity.UNSPECIFIED);

  return (
    <form
      className="flex flex-col gap-5"
      onSubmit={(event) => {
        event.preventDefault();
        setSaved(false);
        save.mutate({
          person: {
            id: person.id,
            profile: {
              activity: activityNumber,
              birthYear: Math.round(parseNumber(birthYear) ?? 0),
              goal: goalNumber,
              heightCm: parseNumber(height),
              sex: kindOf(SEXES, sex, Sex.UNSPECIFIED),
              weightKg: parseNumber(weight),
            },
            tastes: {
              dislikes,
              likes,
              smallMeals: kindOf(
                SMALL_MEALS,
                smallMeals,
                SmallMealTaste.UNSPECIFIED
              ),
              spice: kindOf(SPICES, spice, SpiceLevel.UNSPECIFIED),
            },
          },
          updateMask: { paths: ["profile", "tastes"] },
        });
      }}
    >
      <section className="flex flex-col gap-3">
        <h2 className={sectionLabelClass}>Goal</h2>
        <Segmented
          label="Goal"
          value={goal}
          options={GOALS}
          onChange={(value) => {
            setGoal(value);
            setSaved(false);
          }}
        />
        {goalNumber !== Goal.UNSPECIFIED && (
          <p className="text-sm text-muted">{GOAL_HINTS[goalNumber]}</p>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className={sectionLabelClass}>Body</h2>
        <div className="grid grid-cols-3 gap-3">
          <label className="flex flex-col gap-1">
            <span className={labelClass}>Height, cm</span>
            <input
              inputMode="decimal"
              className={inputClass}
              value={height}
              onChange={(event) => {
                setHeight(event.target.value);
              }}
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className={labelClass}>Weight, kg</span>
            <input
              inputMode="decimal"
              className={inputClass}
              value={weight}
              onChange={(event) => {
                setWeight(event.target.value);
              }}
            />
          </label>
          <label className="flex flex-col gap-1">
            <span className={labelClass}>Born in</span>
            <input
              inputMode="numeric"
              className={inputClass}
              value={birthYear}
              placeholder="1990"
              onChange={(event) => {
                setBirthYear(event.target.value);
              }}
            />
          </label>
        </div>
        <Segmented label="Sex" value={sex} options={SEXES} onChange={setSex} />
        <span className={labelClass}>Activity</span>
        <Segmented
          label="Activity"
          value={activity}
          options={ACTIVITIES}
          onChange={setActivity}
        />
        {activityNumber !== Activity.UNSPECIFIED && (
          <p className="-mt-1 text-sm text-muted">
            {ACTIVITY_HINTS[activityNumber]}
          </p>
        )}
      </section>

      <section className="flex flex-col gap-3">
        <h2 className={sectionLabelClass}>Tastes</h2>
        <span className={labelClass}>Small meals</span>
        <Segmented
          label="Small meals"
          value={smallMeals}
          options={SMALL_MEALS}
          onChange={setSmallMeals}
        />
        <span className={labelClass}>Spice</span>
        <Segmented
          label="Spice"
          value={spice}
          options={SPICES}
          onChange={setSpice}
        />
        <TasteChips
          label="Likes"
          items={likes}
          placeholder="e.g. noodles"
          onChange={setLikes}
        />
        <TasteChips
          label="Doesn't like"
          items={dislikes}
          placeholder="e.g. mushrooms"
          onChange={setDislikes}
        />
      </section>

      <div className="flex items-center gap-3">
        <button
          type="submit"
          className={primaryButton}
          disabled={save.isPending}
        >
          {save.isPending ? "Saving…" : "Save"}
        </button>
        {saved && !save.isPending && (
          <span className="text-sm text-pine">Saved</span>
        )}
      </div>
      <ErrorText error={save.error} />

      {rec === undefined ? (
        <p className="text-sm text-muted">
          With height, weight, birth year, sex, activity and goal saved,
          you&apos;ll see how much to eat a day for the goal.
        </p>
      ) : (
        <section className={`${cardClass} flex flex-col gap-3 p-4`}>
          <h2 className={sectionLabelClass}>A day for this goal</h2>
          <div className="grid grid-cols-4 gap-2 text-center">
            {[
              { label: "kcal", value: rec.energyKcal },
              { label: "protein", value: `${rec.proteinGrams} g` },
              { label: "carbs", value: `${rec.carbohydrateGrams} g` },
              { label: "fat", value: `${rec.fatGrams} g` },
            ].map((item) => (
              <div
                key={item.label}
                className="flex flex-col rounded-xl bg-ground-dark/60 py-2"
              >
                <span className="text-lg font-bold text-ink-soft">
                  {item.value}
                </span>
                <span className="text-xs text-muted">{item.label}</span>
              </div>
            ))}
          </div>
          <p className="text-sm text-muted">
            A general estimate from the profile. Using it sets the daily targets
            for energy, protein, carbohydrate and fat; the other targets and
            rules stay.
          </p>
          <button
            type="button"
            className={`${adopted ? secondaryButton : primaryButton} self-start`}
            disabled={adopt.isPending || adopted}
            onClick={() => {
              setSaved(false);
              adopt.mutate({
                person: {
                  dailyTargets: withTargets(person.dailyTargets, changes),
                  id: person.id,
                },
                updateMask: { paths: ["daily_targets"] },
              });
            }}
          >
            {adopted ? "These are the targets" : "Use as targets"}
          </button>
          <ErrorText error={adopt.error} />
        </section>
      )}
    </form>
  );
};

interface ProfilePageProps {
  user: User;
}

/**
 * ProfilePage shows a person's profile: theirs at /people/:id/profile, or
 * the signed in user's at /profile.
 */
const ProfilePage: React.FC<ProfilePageProps> = ({ user }) => {
  const { id = "" } = useParams();
  const people = useQuery(listPeople, {}, { enabled: id === "" });
  const own = people.data?.people.find((other) => other.userId === user.id);
  const personId = id === "" ? (own?.id ?? "") : id;
  const person = useQuery(
    getPerson,
    { id: personId },
    { enabled: personId !== "" }
  );

  if (id === "" && people.isSuccess && own === undefined) {
    return <Navigate to="/people" replace />;
  }
  let content: React.ReactNode = null;
  const error = people.error ?? person.error;
  if (error !== null) {
    content = (
      <QueryError
        error={error}
        onRetry={() => {
          void people.refetch();
          void person.refetch();
        }}
      />
    );
  } else if (person.data?.person === undefined) {
    content = <Loading />;
  } else {
    content = (
      <ProfileForm key={person.data.person.id} person={person.data.person} />
    );
  }
  const name = person.data?.person?.name ?? "";
  return (
    <div className="mx-auto flex max-w-xl flex-col">
      <Link
        to="/people"
        className={`${choiceClass(false)} mb-3 inline-flex items-center gap-1 self-start`}
      >
        <ArrowLeftIcon className="size-4" aria-hidden="true" />
        People
      </Link>
      <PageHeader
        title={name === "" ? "Profile" : name}
        subtitle="What they're aiming for, and what they like to eat"
      />
      {content}
    </div>
  );
};

export default ProfilePage;
