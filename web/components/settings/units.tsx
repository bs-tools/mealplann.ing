import Card from "@components/ui/card";
import { ErrorText } from "@components/ui/query-error";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { updateSettings } from "gen/users/v1/users-UserService_connectquery";
import { UnitSystem, UserService } from "gen/users/v1/users_pb";
import { invalidateServices } from "helpers/query";
import { useUnitSystem } from "helpers/user";
import React from "react";

// SYSTEMS are the unit systems to choose from, with an example of each.
const SYSTEMS: { value: UnitSystem; label: string; example: string }[] = [
  {
    example: "400 g tofu, 2 tbsp oil, 250 ml soy milk",
    label: "Metric",
    value: UnitSystem.METRIC,
  },
  {
    example: "14 oz tofu, 2 tbsp oil, 1 cup soy milk",
    label: "Imperial (US)",
    value: UnitSystem.IMPERIAL,
  },
];

/**
 * UnitsCard chooses the units the user reads amounts in. It's the user's
 * own: others in the household keep theirs.
 */
const UnitsCard: React.FC = () => {
  const queryClient = useQueryClient();
  const system = useUnitSystem();
  const update = useMutation(updateSettings, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [UserService]);
    },
  });
  return (
    <Card title="Units">
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-2 text-sm text-muted">
          How you read amounts in recipes. Each food shows by weight, by volume
          or in pieces, as set on its page.
        </legend>
        {SYSTEMS.map((option) => (
          <label
            key={option.value}
            className="flex cursor-pointer items-start gap-2 text-sm"
          >
            <input
              type="radio"
              name="unit-system"
              className="mt-0.5 size-4 accent-pine"
              checked={system === option.value}
              disabled={update.isPending}
              onChange={() => {
                update.mutate({ unitSystem: option.value });
              }}
            />
            <span>
              <span className="font-medium text-ink-soft">{option.label}</span>
              <span className="block text-xs text-muted">{option.example}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <ErrorText error={update.error} />
    </Card>
  );
};

export default UnitsCard;
