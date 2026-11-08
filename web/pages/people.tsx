import Loading from "@components/loading";
import PersonCard from "@components/people/person-card";
import PersonDialog from "@components/people/person-dialog";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import { primaryButton } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { InformationCircleIcon, PlusIcon } from "@heroicons/react/24/outline";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import type { Person } from "gen/people/v1/people_pb";
import { listStaples } from "gen/plans/v1/plans-PlanService_connectquery";
import { listRecipes } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { getHousehold } from "gen/users/v1/users-UserService_connectquery";
import React, { useState } from "react";

/**
 * PeoplePage lists the people the household plans for, with their eating
 * patterns, rules, targets and staples, to edit.
 */
const PeoplePage: React.FC = () => {
  const people = useQuery(listPeople, {});
  const staples = useQuery(listStaples, {});
  const household = useQuery(getHousehold, {});
  const recipes = useQuery(listRecipes, {});
  // The person being edited, null for a new one, undefined if none.
  const [editing, setEditing] = useState<Person | null | undefined>(undefined);
  const members = household.data?.household?.members ?? [];

  let content: React.ReactNode = null;
  if (people.isPending) {
    content = <Loading />;
  } else if (people.isError) {
    content = (
      <QueryError
        error={people.error}
        onRetry={() => {
          void people.refetch();
        }}
      />
    );
  } else if (people.data.people.length === 0) {
    content = (
      <EmptyState title="No one to plan for yet">
        Add the people you cook for, with the times they eat, to plan their
        meals.
      </EmptyState>
    );
  } else {
    content = (
      <div className="flex flex-col gap-4">
        {people.data.people.map((person) => (
          <PersonCard
            key={person.id}
            person={person}
            staples={(staples.data?.staples ?? []).filter(
              (staple) => staple.personId === person.id
            )}
            members={members}
            recipes={recipes.data?.recipes ?? []}
            onEdit={() => {
              setEditing(person);
            }}
          />
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="People"
        subtitle="The people you plan meals for: when they eat, and the rules and targets their plan is checked against."
        actions={
          <button
            type="button"
            className={primaryButton}
            onClick={() => {
              setEditing(null);
            }}
          >
            <PlusIcon className="size-4" aria-hidden="true" />
            New person
          </button>
        }
      />
      <p className="-mt-2 flex items-start gap-1.5 text-sm text-gray-600">
        <InformationCircleIcon
          className="mt-0.5 size-4 shrink-0 text-sky-700"
          aria-hidden="true"
        />
        Rules and targets come from each person&apos;s care team. mealplann.ing
        checks the plan against them and warns; it never suggests them.
      </p>
      {content}
      {editing !== undefined && (
        <PersonDialog
          person={editing ?? undefined}
          members={members}
          onClose={() => {
            setEditing(undefined);
          }}
        />
      )}
    </div>
  );
};

export default PeoplePage;
