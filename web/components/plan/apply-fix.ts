import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import type { UseMutationResult } from "@tanstack/react-query";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type { PlannedPortion } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import type { PlanFix } from "gen/suggestions/v1/suggestions_pb";
import {
  PlanFixKind,
  SuggestionService,
} from "gen/suggestions/v1/suggestions_pb";
import { invalidateServices } from "helpers/query";

/**
 * UseApplyFix applies a fix SuggestFixes suggested, given the day's
 * portions to resize, and calls onDone once it's applied.
 */
export const useApplyFix = (
  portions: PlannedPortion[],
  onDone: () => void
): UseMutationResult<void, ConnectError, PlanFix> => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  return useMutation<void, ConnectError, PlanFix>({
    mutationFn: async (fix: PlanFix) => {
      const client = createClient(PlanService, transport);
      if (fix.kind === PlanFixKind.REMOVE) {
        await client.deletePortion({ id: fix.portionId });
        if (fix.deleteCookId !== "") {
          await client.deleteCook({ id: fix.deleteCookId });
        }
        return;
      }
      // Every portion is found before any is resized.
      const resizes = fix.resizes.map((resize) => {
        const portion = portions.find(
          (other) => other.portionId === resize.portionId
        );
        if (portion === undefined) {
          throw new ConnectError(
            "The portion was changed: reload the plan.",
            Code.FailedPrecondition
          );
        }
        return { portion, resize };
      });
      await Promise.all(
        resizes.map(async ({ portion, resize }) =>
          portion.cookId === ""
            ? client.updatePortion({
                portion: {
                  id: resize.portionId,
                  what: {
                    case: "food",
                    value: {
                      amount: resize.amount,
                      foodId: portion.foodId,
                      unit: resize.unit,
                    },
                  },
                },
                updateMask: { paths: ["food"] },
              })
            : client.updatePortion({
                portion: {
                  id: resize.portionId,
                  what: {
                    case: "cook",
                    value: {
                      cookId: portion.cookId,
                      servings: resize.servings,
                    },
                  },
                },
                updateMask: { paths: ["cook"] },
              })
        )
      );
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        SuggestionService,
      ]);
      onDone();
    },
  });
};
