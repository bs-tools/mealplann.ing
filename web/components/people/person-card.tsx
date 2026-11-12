import StapleDialog from "@components/people/staple-dialog";
import ConfirmButton, { ConfirmDialog } from "@components/ui/confirm-button";
import MoreMenu from "@components/ui/more-menu";
import { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  iconButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import {
  PencilIcon,
  PlusIcon,
  TrashIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { deletePerson } from "gen/people/v1/people-PersonService_connectquery";
import type { Person } from "gen/people/v1/people_pb";
import { EatingTimeKind, PersonService } from "gen/people/v1/people_pb";
import {
  deleteStaple,
  unskipStaple,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { Staple } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import type { HouseholdMember } from "gen/users/v1/users_pb";
import type { UnitSystem } from "gen/users/v1/users_pb";
import { today } from "helpers/dates";
import { formatShortDate } from "helpers/format";
import {
  formatIngredientAmount,
  formatServings,
  formatWeekdays,
  householdMealLabel,
} from "helpers/labels";
import { formatAmount, nutrientInfoOf } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import { useUnitSystem } from "helpers/user";
import React, { useState } from "react";
import { Link } from "react-router";

// RuleFacts describes a person's rules, e.g. "Portions up to 200 g".
const ruleFacts = (person: Person): string[] => {
  const { rules } = person;
  if (rules === undefined) {
    return [];
  }
  const facts: string[] = [];
  if (rules.maxPortionGrams !== undefined) {
    facts.push(`Portions up to ${formatAmount(rules.maxPortionGrams)} g`);
  }
  if (rules.minProteinGrams !== undefined) {
    facts.push(`At least ${formatAmount(rules.minProteinGrams)} g protein`);
  }
  if (rules.maxCarbohydrateGrams !== undefined) {
    facts.push(
      `Up to ${formatAmount(rules.maxCarbohydrateGrams)} g carbohydrate`
    );
  }
  if (rules.minPairingRatio !== undefined) {
    facts.push(
      `Protein and fat at least ${formatAmount(rules.minPairingRatio)}× carbohydrate`
    );
  }
  if (rules.maxSugarsGrams !== undefined) {
    facts.push(`Up to ${formatAmount(rules.maxSugarsGrams)} g sugars`);
  }
  if (rules.warnFastCarbs) {
    facts.push("Warn about fast carbs");
  }
  return facts;
};

// TargetFacts describes a person's daily targets, e.g. "Protein 60–120 g".
const targetFacts = (person: Person): string[] =>
  person.dailyTargets.map((target) => {
    const info = nutrientInfoOf(target.nutrient);
    const label = info?.label ?? "Nutrient";
    const unit = info?.unit ?? "";
    const { minPerDay: min, maxPerDay: max } = target;
    if (min !== undefined && max !== undefined) {
      return `${label} ${formatAmount(min)}–${formatAmount(max)} ${unit}`;
    }
    if (min !== undefined) {
      return `${label} at least ${formatAmount(min)} ${unit}`;
    }
    return `${label} up to ${formatAmount(max ?? 0)} ${unit}`;
  });

// StapleAmount describes how much of a staple is eaten.
const stapleAmount = (staple: Staple, system: UnitSystem): string => {
  if (staple.what.case === "food") {
    const { amount, unit } = staple.what.value;
    return formatIngredientAmount(amount, unit, system);
  }
  if (staple.what.case === "recipe") {
    return formatServings(staple.what.value.servings);
  }
  return "";
};

interface PersonCardProps {
  person: Person;
  // The person's staples.
  staples: Staple[];
  members: HouseholdMember[];
  onEdit: () => void;
}

/**
 * PersonCard shows a person: who they are, when they eat, their rules and
 * targets as chips, and their staples, managed here, with their profile
 * and their eating times and rules a tap away.
 */
const PersonCard: React.FC<PersonCardProps> = ({
  person,
  staples,
  members,
  onEdit,
}) => {
  const queryClient = useQueryClient();
  const system = useUnitSystem();
  // The staple being edited, null for a new one, undefined if none.
  const [editing, setEditing] = useState<Staple | null | undefined>(undefined);
  const [deleting, setDeleting] = useState(false);
  const invalidate = async (): Promise<void> => {
    await invalidateServices(queryClient, [PersonService, PlanService]);
  };
  const remove = useMutation(deletePerson, { onSuccess: invalidate });
  const removeStaple = useMutation(deleteStaple, { onSuccess: invalidate });
  const unskip = useMutation(unskipStaple, { onSuccess: invalidate });
  const member = members.find((other) => other.userId === person.userId);
  const rules = ruleFacts(person);
  const targets = targetFacts(person);
  const now = today();
  const initials = person.name
    .split(/\s+/u)
    .filter((part) => part !== "")
    .slice(0, 2)
    .map((part) => part.charAt(0).toUpperCase())
    .join("");
  const chip = "rounded-full px-3 py-1 text-sm";
  return (
    <section className="flex flex-col gap-4 rounded-2xl border border-line bg-white p-4">
      <div className="flex items-center gap-3">
        <span
          className="flex size-11 shrink-0 items-center justify-center rounded-full bg-pine-50 text-sm font-bold text-pine"
          aria-hidden="true"
        >
          {initials}
        </span>
        <div className="flex min-w-0 grow flex-col">
          <h2 className="truncate text-lg font-bold text-ink-soft">
            {person.name}
          </h2>
          <p className="truncate text-sm text-muted">
            {member === undefined
              ? "Not signed in themselves"
              : `Signs in as ${member.name}`}
          </p>
        </div>
        <MoreMenu
          label={`More for ${person.name}`}
          className="-mr-2 flex size-10 items-center justify-center rounded-full text-muted hover:bg-ground-dark/60"
          items={[
            { label: "Profile and goals", to: `/people/${person.id}/profile` },
            { label: "Eating times and rules", onClick: onEdit },
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
          title={`Delete ${person.name}?`}
          message="Everything planned for them, their portions and staples, is deleted too. The cooks stay."
          onClose={() => {
            setDeleting(false);
          }}
          onConfirm={() => {
            remove.mutate({ id: person.id });
          }}
        />
      </div>
      <div className="-mx-2 -my-1 flex flex-wrap">
        <Link
          to={`/people/${person.id}/profile`}
          className="rounded-lg px-2 py-1 text-sm font-bold text-pine hover:bg-pine-50"
        >
          Profile and goals ›
        </Link>
        <button
          type="button"
          className="rounded-lg px-2 py-1 text-sm font-bold text-pine hover:bg-pine-50"
          onClick={onEdit}
        >
          Eating times and rules ›
        </button>
      </div>
      <div className="flex flex-col gap-2">
        <h3 className={sectionLabelClass}>Eats</h3>
        {person.eatingTimes.length === 0 ? (
          <p className="text-sm text-muted">
            No eating times yet: add them to plan for {person.name}.
          </p>
        ) : (
          <ul className="flex flex-wrap gap-1.5">
            {person.eatingTimes.map((et) => (
              <li
                key={et.id}
                className={`${chip} ${et.kind === EatingTimeKind.MAIN ? "bg-ground-dark text-ink-soft" : "bg-ground-dark/50 text-muted"}`}
                title={
                  householdMealLabel(et.meal) === ""
                    ? undefined
                    : `Shared ${householdMealLabel(et.meal).toLowerCase()}`
                }
              >
                {et.time !== "" && (
                  <span className="text-muted tabular-nums">{et.time} </span>
                )}
                {et.name}
              </li>
            ))}
          </ul>
        )}
      </div>
      <div className="flex flex-col gap-2">
        <h3 className={sectionLabelClass}>Rules and targets</h3>
        {rules.length === 0 && targets.length === 0 ? (
          <p className="text-sm text-muted">None yet.</p>
        ) : (
          <ul className="flex flex-wrap gap-1.5">
            {targets.map((target) => (
              <li key={target} className={`${chip} bg-pine-50 text-pine-dark`}>
                {target}
              </li>
            ))}
            {rules.map((rule) => (
              <li key={rule} className={`${chip} bg-heat-tint text-heat`}>
                {rule}
              </li>
            ))}
          </ul>
        )}
        {person.notes !== "" && (
          <p className="text-sm whitespace-pre-line text-muted">
            {person.notes}
          </p>
        )}
      </div>
      <section className="flex flex-col">
        <div className="flex items-center justify-between gap-2">
          <h3 className={sectionLabelClass}>Staples</h3>
          <button
            type="button"
            className={ghostButton}
            disabled={person.eatingTimes.length === 0}
            onClick={() => {
              setEditing(null);
            }}
          >
            <PlusIcon className="size-4" aria-hidden="true" />
            Add staple
          </button>
        </div>
        {staples.length === 0 ? (
          <p className="text-sm text-muted">
            Nothing eaten regularly yet, like a usual breakfast or a daily glass
            of soy milk.
          </p>
        ) : (
          <ul className="divide-y divide-line">
            {staples.map((staple) => {
              const et = person.eatingTimes.find(
                (other) => other.id === staple.eatingTimeId
              );
              const skipped = staple.skippedDates.filter((date) => date >= now);
              const dates: string[] = [];
              if (staple.startDate !== "" && staple.startDate > now) {
                dates.push(`from ${formatShortDate(staple.startDate)}`);
              }
              if (staple.endDate !== "") {
                dates.push(`until ${formatShortDate(staple.endDate)}`);
              }
              return (
                <li
                  key={staple.id}
                  className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm"
                >
                  <div className="min-w-0 grow">
                    <p className="font-medium text-ink-soft">
                      {staple.name}{" "}
                      <span className="font-normal text-muted tabular-nums">
                        {stapleAmount(staple, system)}
                      </span>
                    </p>
                    <p className="text-xs text-muted">
                      {[
                        et?.name ?? "",
                        staple.replacesMeal && et !== undefined
                          ? `instead of the shared ${householdMealLabel(et.meal).toLowerCase()}`
                          : "",
                        formatWeekdays(staple.weekdays),
                        ...dates,
                      ]
                        .filter((part) => part !== "")
                        .join(" · ")}
                    </p>
                    {skipped.length > 0 && (
                      <p className="mt-1 flex flex-wrap items-center gap-1 text-xs text-muted">
                        Skipped on
                        {skipped.map((date) => (
                          <span
                            key={date}
                            className="inline-flex items-center gap-0.5 rounded bg-ground-dark px-1.5 py-0.5"
                          >
                            {formatShortDate(date)}
                            <button
                              type="button"
                              className="text-muted hover:text-ink-soft"
                              aria-label={`Don't skip on ${formatShortDate(date)}`}
                              title="Don't skip"
                              disabled={unskip.isPending}
                              onClick={() => {
                                unskip.mutate({ date, id: staple.id });
                              }}
                            >
                              <XMarkIcon className="size-3" />
                            </button>
                          </span>
                        ))}
                      </p>
                    )}
                  </div>
                  <div className="flex gap-1">
                    <button
                      type="button"
                      className={iconButton}
                      aria-label={`Edit ${staple.name}`}
                      title="Edit"
                      onClick={() => {
                        setEditing(staple);
                      }}
                    >
                      <PencilIcon className="size-4" />
                    </button>
                    <ConfirmButton
                      className={iconButton}
                      title={`Delete ${staple.name}?`}
                      message={`${person.name} stops having it in every week, past weeks too.`}
                      disabled={removeStaple.isPending}
                      onConfirm={() => {
                        removeStaple.mutate({ id: staple.id });
                      }}
                    >
                      <TrashIcon
                        className="size-4"
                        aria-label={`Delete ${staple.name}`}
                      />
                    </ConfirmButton>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        <ErrorText error={remove.error ?? removeStaple.error ?? unskip.error} />
      </section>
      {editing !== undefined && (
        <StapleDialog
          person={person}
          staple={editing ?? undefined}
          onClose={() => {
            setEditing(undefined);
          }}
        />
      )}
    </section>
  );
};

export default PersonCard;
