import Avatar from "@components/header/avatar";
import Loading from "@components/loading";
import Card from "@components/ui/card";
import QueryError, { ErrorText } from "@components/ui/query-error";
import { ghostButton, inputClass, primaryButton } from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { PencilIcon } from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import {
  getHousehold,
  updateHousehold,
} from "gen/users/v1/users-UserService_connectquery";
import { UserService } from "gen/users/v1/users_pb";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

/**
 * HouseholdCard shows the user's household, whose members share recipes,
 * plans, the pantry and the shopping list, and renames it.
 */
const HouseholdCard: React.FC = () => {
  const queryClient = useQueryClient();
  const { isPending, isError, data, error, refetch } = useQuery(getHousehold);
  // The name being edited, or null when not editing.
  const [name, setName] = useState<string | null>(null);
  const rename = useMutation(updateHousehold, {
    onSuccess: async () => {
      setName(null);
      await invalidateServices(queryClient, [UserService]);
    },
  });

  if (isPending) {
    return (
      <Card title="Household">
        <Loading />
      </Card>
    );
  }
  if (isError) {
    return (
      <Card title="Household">
        <QueryError
          error={error}
          onRetry={() => {
            void refetch();
          }}
        />
      </Card>
    );
  }
  const { household } = data;
  return (
    <Card
      title="Household"
      actions={
        name === null && (
          <button
            type="button"
            className={ghostButton}
            onClick={() => {
              setName(household?.name ?? "");
            }}
          >
            <PencilIcon className="size-4" aria-hidden="true" />
            Rename
          </button>
        )
      }
    >
      <div className="flex flex-col gap-4 text-sm text-gray-700">
        {name === null ? (
          <p className="text-lg font-semibold text-gray-900">
            {household?.name}
          </p>
        ) : (
          <form
            className="flex flex-wrap items-center gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              rename.mutate({ name });
            }}
          >
            <input
              aria-label="Household name"
              className={`${inputClass} min-w-0 grow`}
              value={name}
              maxLength={100}
              autoFocus
              onChange={(event) => {
                setName(event.target.value);
              }}
            />
            <button
              type="submit"
              className={primaryButton}
              disabled={rename.isPending}
            >
              Save
            </button>
            <button
              type="button"
              className={ghostButton}
              onClick={() => {
                setName(null);
                rename.reset();
              }}
            >
              Cancel
            </button>
          </form>
        )}
        {rename.error !== null && <ErrorText error={rename.error} />}
        <p>
          Everyone in the household shares the recipes, meal plans, pantry and
          shopping list. Invitations add people to this household.
        </p>
        <ul className="flex flex-col gap-2">
          {household?.members.map((member) => (
            <li key={member.userId} className="flex items-center gap-3">
              <Avatar
                avatarUrl={member.avatarUrl}
                name={member.name}
                className="size-8 shrink-0 text-sm"
              />
              <span className="font-medium text-gray-900">{member.name}</span>
            </li>
          ))}
        </ul>
      </div>
    </Card>
  );
};

export default HouseholdCard;
