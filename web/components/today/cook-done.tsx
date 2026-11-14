import { markEaten } from "@components/today/day-change";
import { ErrorText } from "@components/ui/query-error";
import { pillButton } from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type { GetWeekResponse, PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { invalidateServices } from "helpers/query";
import React from "react";

interface CookDoneProps {
  planned: PlannedCook;
  week: GetWeekResponse;
  date: string;
}

/**
 * CookDone marks a cook cooked without going through cook mode, which takes
 * its ingredients out of the pantry, and the portions of it eaten that day,
 * everyone's: a meal cooked and eaten in one tap.
 */
const CookDone: React.FC<CookDoneProps> = ({ planned, week, date }) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const cookId = planned.cook?.id ?? "";
  // Everyone's portions of it that day not eaten yet.
  const portions = (week.days.find((day) => day.date === date)?.people ?? [])
    .flatMap((person) => person.eatingTimes)
    .flatMap((eating) => eating.portions)
    .filter((portion) => portion.cookId === cookId && !portion.eaten);
  const done = useMutation<void, ConnectError>({
    mutationFn: async () => {
      const client = createClient(PlanService, transport);
      await client.updateCook({
        cook: { done: true, id: cookId },
        updateMask: { paths: ["done"] },
      });
      await markEaten(client, portions, date, true);
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
    },
  });
  return (
    <>
      <button
        type="button"
        className={`${pillButton} bg-white`}
        disabled={done.isPending}
        onClick={() => {
          done.mutate();
        }}
      >
        {portions.length > 0 ? "Cooked and eaten" : "Mark as cooked"}
      </button>
      <ErrorText error={done.error} />
    </>
  );
};

export default CookDone;
