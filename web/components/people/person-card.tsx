import StapleDialog from "@components/people/staple-dialog";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { Badge } from "@components/ui/misc";
import { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  ghostDangerButton,
  iconButton,
  secondaryButton,
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
import type { RecipeSummary } from "gen/recipes/v1/recipes_pb";
import type { HouseholdMember } from "gen/users/v1/users_pb";
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
import React, { useState } from "react";

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
const stapleAmount = (staple: Staple): string => {
  if (staple.what.case === "food") {
    const { amount, unit } = staple.what.value;
    return formatIngredientAmount(amount, unit);
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
  recipes: RecipeSummary[];
  onEdit: () => void;
}

/**
 * PersonCard shows a person: their eating pattern, rules, targets and
 * staples, with the staples managed here.
 */
const PersonCard: React.FC<PersonCardProps> = ({
  person,
  staples,
  members,
  recipes,
  onEdit,
}) => {
  const queryClient = useQueryClient();
  // The staple being edited, null for a new one, undefined if none.
  const [editing, setEditing] = useState<Staple | null | undefined>(undefined);
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
  return (
    <Card
      title={
        <span className="flex items-center gap-2">
          {person.name}
          {member !== undefined && <Badge>{member.name}</Badge>}
        </span>
      }
      actions={
        <>
          <ConfirmButton
            className={ghostDangerButton}
            title={`Delete ${person.name}?`}
            message="Everything planned for them, their portions and staples, is deleted too. The cooks stay."
            disabled={remove.isPending}
            onConfirm={() => {
              remove.mutate({ id: person.id });
            }}
          >
            <TrashIcon className="size-4" aria-hidden="true" />
            Delete
          </ConfirmButton>
          <button type="button" className={secondaryButton} onClick={onEdit}>
            <PencilIcon className="size-4" aria-hidden="true" />
            Edit
          </button>
        </>
      }
    >
      <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
        <section>
          <h3 className="mb-1.5 text-xs font-semibold tracking-wide text-gray-500 uppercase">
            Eating pattern
          </h3>
          {person.eatingTimes.length === 0 ? (
            <p className="text-sm text-gray-500">
              No eating times yet: add them to plan for {person.name}.
            </p>
          ) : (
            <ol className="flex flex-col gap-1 text-sm">
              {person.eatingTimes.map((et) => (
                <li key={et.id} className="flex items-baseline gap-2">
                  <span className="w-11 shrink-0 text-gray-500 tabular-nums">
                    {et.time}
                  </span>
                  <span
                    className={
                      et.kind === EatingTimeKind.MAIN
                        ? "font-medium text-gray-900"
                        : "text-gray-800"
                    }
                  >
                    {et.name}
                  </span>
                  <span className="text-xs text-gray-500">
                    {et.kind === EatingTimeKind.MAIN ? "main" : "small"}
                    {householdMealLabel(et.meal) !== "" &&
                      ` · shared ${householdMealLabel(et.meal).toLowerCase()}`}
                  </span>
                </li>
              ))}
            </ol>
          )}
        </section>
        <section className="flex flex-col gap-3">
          <div>
            <h3 className="mb-1.5 text-xs font-semibold tracking-wide text-gray-500 uppercase">
              Rules for every eating time
            </h3>
            {rules.length === 0 ? (
              <p className="text-sm text-gray-500">None.</p>
            ) : (
              <ul className="flex flex-wrap gap-1.5">
                {rules.map((rule) => (
                  <li key={rule}>
                    <Badge className="bg-sky-50 text-sky-800 ring-sky-200">
                      {rule}
                    </Badge>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div>
            <h3 className="mb-1.5 text-xs font-semibold tracking-wide text-gray-500 uppercase">
              Daily targets
            </h3>
            {targets.length === 0 ? (
              <p className="text-sm text-gray-500">None.</p>
            ) : (
              <ul className="flex flex-wrap gap-1.5">
                {targets.map((target) => (
                  <li key={target}>
                    <Badge>{target}</Badge>
                  </li>
                ))}
              </ul>
            )}
          </div>
          {person.notes !== "" && (
            <p className="text-sm whitespace-pre-line text-gray-600">
              {person.notes}
            </p>
          )}
        </section>
      </div>
      <section className="mt-5 border-t border-gray-100 pt-4">
        <div className="mb-2 flex items-center justify-between gap-2">
          <h3 className="text-xs font-semibold tracking-wide text-gray-500 uppercase">
            Staples
          </h3>
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
          <p className="text-sm text-gray-500">
            Nothing eaten regularly yet, like a usual breakfast or a daily glass
            of soy milk.
          </p>
        ) : (
          <ul className="divide-y divide-gray-100">
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
                    <p className="font-medium text-gray-900">
                      {staple.name}{" "}
                      <span className="font-normal text-gray-600 tabular-nums">
                        {stapleAmount(staple)}
                      </span>
                    </p>
                    <p className="text-xs text-gray-500">
                      {[
                        et?.name ?? "",
                        formatWeekdays(staple.weekdays),
                        ...dates,
                      ]
                        .filter((part) => part !== "")
                        .join(" · ")}
                    </p>
                    {skipped.length > 0 && (
                      <p className="mt-1 flex flex-wrap items-center gap-1 text-xs text-gray-500">
                        Skipped on
                        {skipped.map((date) => (
                          <span
                            key={date}
                            className="inline-flex items-center gap-0.5 rounded bg-gray-100 px-1.5 py-0.5"
                          >
                            {formatShortDate(date)}
                            <button
                              type="button"
                              className="text-gray-500 hover:text-gray-900"
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
          recipes={recipes}
          onClose={() => {
            setEditing(undefined);
          }}
        />
      )}
    </Card>
  );
};

export default PersonCard;
