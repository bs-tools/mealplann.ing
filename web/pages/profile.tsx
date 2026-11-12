import Loading from "@components/loading";
import BackLink from "@components/ui/back-link";
import QueryError, { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import { inputClass, secondaryButton } from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { XMarkIcon } from "@heroicons/react/24/outline";
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
import { Navigate, useParams } from "react-router";

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
  likes: string[];
  dislikes: string[];
  onChange: (likes: string[], dislikes: string[]) => void;
}

/**
 * TasteChips shows what a person likes and doesn't as chips, "Loves
 * noodles" and "Not mushrooms", tapping one to remove it, and adds one.
 */
const TasteChips: React.FC<TasteChipsProps> = ({
  likes,
  dislikes,
  onChange,
}) => {
  const [adding, setAdding] = useState(false);
  const [text, setText] = useState("");
  const [liked, setLiked] = useState("dislike");
  const add = (): void => {
    const item = text.trim();
    const all = [...likes, ...dislikes];
    if (
      item !== "" &&
      !all.some((other) => other.toLowerCase() === item.toLowerCase())
    ) {
      if (liked === "like") {
        onChange([...likes, item], dislikes);
      } else {
        onChange(likes, [...dislikes, item]);
      }
    }
    setText("");
    setAdding(false);
  };
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2">
        {dislikes.map((item) => (
          <button
            key={`not-${item}`}
            type="button"
            className="inline-flex items-center gap-1 rounded-full bg-[#fde3e0] px-3 py-1.5 text-sm text-[#9a2b1e]"
            title={`Remove "not ${item}"`}
            onClick={() => {
              onChange(
                likes,
                dislikes.filter((other) => other !== item)
              );
            }}
          >
            Not {item}
            <XMarkIcon className="size-3.5" aria-hidden="true" />
          </button>
        ))}
        {likes.map((item) => (
          <button
            key={`loves-${item}`}
            type="button"
            className="inline-flex items-center gap-1 rounded-full bg-ready-tint px-3 py-1.5 text-sm text-pine-dark"
            title={`Remove "loves ${item}"`}
            onClick={() => {
              onChange(
                likes.filter((other) => other !== item),
                dislikes
              );
            }}
          >
            Loves {item}
            <XMarkIcon className="size-3.5" aria-hidden="true" />
          </button>
        ))}
        {!adding && (
          <button
            type="button"
            className="rounded-full border border-dashed border-muted/50 px-3 py-1.5 text-sm text-pine"
            onClick={() => {
              setAdding(true);
            }}
          >
            + Add
          </button>
        )}
      </div>
      {adding && (
        <div className="flex flex-col gap-2 rounded-2xl border border-line bg-white p-3">
          <Segmented
            label="Likes or not"
            value={liked}
            options={[
              { label: "Doesn't like", value: "dislike" },
              { label: "Loves", value: "like" },
            ]}
            onChange={setLiked}
          />
          <div className="flex gap-2">
            <input
              type="text"
              value={text}
              placeholder={liked === "like" ? "e.g. noodles" : "e.g. mushrooms"}
              maxLength={50}
              autoFocus
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
            <button type="button" className={secondaryButton} onClick={add}>
              Add
            </button>
          </div>
        </div>
      )}
    </div>
  );
};

// TargetChange is a daily target the recommendation would set.
interface TargetChange {
  nutrient: Nutrient;
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

// FormState is the profile as edited.
interface FormState {
  height: string;
  weight: string;
  birthYear: string;
  sex: string;
  activity: string;
  goal: string;
  smallMeals: string;
  spice: string;
  likes: string[];
  dislikes: string[];
}

interface ProfileFormProps {
  person: Person;
}

/**
 * ProfileForm edits a person's body, activity, goal and tastes, saving
 * each change as it's made, and shows what the profile recommends they
 * eat a day, which they can use as their daily targets.
 */
const ProfileForm: React.FC<ProfileFormProps> = ({ person }) => {
  const queryClient = useQueryClient();
  const { profile, tastes } = person;
  const [form, setForm] = useState<FormState>({
    activity: String(profile?.activity ?? Activity.UNSPECIFIED),
    birthYear: numberText(profile?.birthYear),
    dislikes: tastes?.dislikes ?? [],
    goal: String(profile?.goal ?? Goal.UNSPECIFIED),
    height: numberText(profile?.heightCm),
    likes: tastes?.likes ?? [],
    sex: String(profile?.sex ?? Sex.UNSPECIFIED),
    smallMeals: String(tastes?.smallMeals ?? SmallMealTaste.UNSPECIFIED),
    spice: String(tastes?.spice ?? SpiceLevel.UNSPECIFIED),
    weight: numberText(profile?.weightKg),
  });
  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [
      PersonService,
      PlanService,
      SuggestionService,
    ]);
  };
  const save = useMutation(updatePerson, { onSuccess });
  const adopt = useMutation(updatePerson, { onSuccess });

  // Commit saves the form with a change, as each change is made.
  const commit = (change: Partial<FormState>): void => {
    const next = { ...form, ...change };
    setForm(next);
    save.mutate({
      person: {
        id: person.id,
        profile: {
          activity: kindOf(ACTIVITIES, next.activity, Activity.UNSPECIFIED),
          birthYear: Math.round(parseNumber(next.birthYear) ?? 0),
          goal: kindOf(GOALS, next.goal, Goal.UNSPECIFIED),
          heightCm: parseNumber(next.height),
          sex: kindOf(SEXES, next.sex, Sex.UNSPECIFIED),
          weightKg: parseNumber(next.weight),
        },
        tastes: {
          dislikes: next.dislikes,
          likes: next.likes,
          smallMeals: kindOf(
            SMALL_MEALS,
            next.smallMeals,
            SmallMealTaste.UNSPECIFIED
          ),
          spice: kindOf(SPICES, next.spice, SpiceLevel.UNSPECIFIED),
        },
      },
      updateMask: { paths: ["profile", "tastes"] },
    });
  };

  const rec = person.recommended;
  const changes: TargetChange[] =
    rec === undefined
      ? []
      : [
          { max: rec.energyKcal, nutrient: Nutrient.ENERGY_KCAL },
          { min: rec.proteinGrams, nutrient: Nutrient.PROTEIN_GRAMS },
          { max: rec.carbohydrateGrams, nutrient: Nutrient.CARBOHYDRATE_GRAMS },
          { max: rec.fatGrams, nutrient: Nutrient.FAT_GRAMS },
        ];
  const adopted = changes.every((change) =>
    person.dailyTargets.some(
      (target) =>
        target.nutrient === change.nutrient &&
        target.minPerDay === change.min &&
        target.maxPerDay === change.max
    )
  );
  const goal = kindOf(GOALS, form.goal, Goal.UNSPECIFIED);
  const activity = kindOf(ACTIVITIES, form.activity, Activity.UNSPECIFIED);
  const field = (
    label: string,
    key: "height" | "weight" | "birthYear",
    unit: string
  ): React.ReactNode => (
    <label className="flex flex-col gap-1">
      <span className="text-sm text-muted">{label}</span>
      <span className="relative">
        <input
          inputMode="decimal"
          className={`${inputClass} py-3 text-base ${unit === "" ? "" : "pr-10"}`}
          value={form[key]}
          onChange={(event) => {
            setForm({ ...form, [key]: event.target.value });
          }}
          onBlur={() => {
            commit({});
          }}
        />
        {unit !== "" && (
          <span className="pointer-events-none absolute inset-y-0 right-3 flex items-center text-sm text-muted">
            {unit}
          </span>
        )}
      </span>
    </label>
  );

  return (
    <div className="flex flex-col gap-5">
      <div className="grid grid-cols-2 gap-3">
        {field("Height", "height", "cm")}
        {field("Weight", "weight", "kg")}
        {field("Born in", "birthYear", "")}
        <div className="flex flex-col gap-1">
          <span className="text-sm text-muted">Sex</span>
          <Segmented
            label="Sex"
            value={form.sex}
            options={SEXES}
            className="py-[5px]"
            onChange={(value) => {
              commit({ sex: value });
            }}
          />
        </div>
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-sm text-muted">Activity</span>
        <Segmented
          label="Activity"
          value={form.activity}
          options={ACTIVITIES}
          onChange={(value) => {
            commit({ activity: value });
          }}
        />
        {activity !== Activity.UNSPECIFIED && (
          <span className="text-xs text-muted">{ACTIVITY_HINTS[activity]}</span>
        )}
      </div>
      <div className="flex flex-col gap-2">
        <span className="text-sm text-muted">Goal</span>
        <Segmented
          label="Goal"
          value={form.goal}
          options={GOALS}
          onChange={(value) => {
            commit({ goal: value });
          }}
        />
      </div>
      {rec === undefined ? (
        <p className="rounded-2xl bg-ground-dark/60 p-4 text-sm text-muted">
          With height, weight, birth year, sex, activity and goal, you&apos;ll
          see how much to eat a day for the goal.
        </p>
      ) : (
        <div className="flex flex-col gap-3 rounded-2xl bg-ready-tint p-4 text-base text-pine-dark">
          <p>
            About {rec.energyKcal.toLocaleString()} kcal a day, with{" "}
            {rec.proteinGrams} g protein, {rec.carbohydrateGrams} g carbohydrate
            and {rec.fatGrams} g fat.{" "}
            {goal === Goal.UNSPECIFIED ? "" : GOAL_HINTS[goal]}
          </p>
          <button
            type="button"
            className={`self-start rounded-xl px-4 py-2 text-sm font-bold ${adopted ? "bg-white/70 text-pine-dark" : "bg-pine text-white"}`}
            disabled={adopt.isPending || adopted}
            onClick={() => {
              adopt.mutate({
                person: {
                  dailyTargets: withTargets(person.dailyTargets, changes),
                  id: person.id,
                },
                updateMask: { paths: ["daily_targets"] },
              });
            }}
          >
            {adopted ? "These are the daily targets" : "Use as daily targets"}
          </button>
          <ErrorText error={adopt.error} />
        </div>
      )}
      <h2 className="mt-1 text-lg font-bold text-ink-soft">Tastes</h2>
      <div className="grid grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-3">
        <span className="text-base text-ink-soft">Small meals</span>
        <Segmented
          label="Small meals"
          value={form.smallMeals}
          options={SMALL_MEALS}
          onChange={(value) => {
            commit({ smallMeals: value });
          }}
        />
        <span className="text-base text-ink-soft">Spice</span>
        <Segmented
          label="Spice"
          value={form.spice}
          options={SPICES}
          onChange={(value) => {
            commit({ spice: value });
          }}
        />
      </div>
      <TasteChips
        likes={form.likes}
        dislikes={form.dislikes}
        onChange={(likes, dislikes) => {
          commit({ dislikes, likes });
        }}
      />
      <p className="text-sm text-muted" role="status">
        {save.isPending ? "Saving…" : ""}
        {save.isSuccess && !save.isPending ? "Saved" : ""}
      </p>
      <ErrorText error={save.error} />
    </div>
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
    <div className="mx-auto flex w-full max-w-xl flex-col gap-5">
      <div className="flex flex-col gap-1">
        <BackLink to="/people">Household</BackLink>
        <h1 className="mt-2 text-[28px] leading-tight font-bold tracking-tight text-ink-soft">
          {name === "" ? "Profile" : name}
        </h1>
      </div>
      {content}
    </div>
  );
};

export default ProfilePage;
