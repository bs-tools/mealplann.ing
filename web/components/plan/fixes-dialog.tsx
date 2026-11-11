import Loading from "@components/loading";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import { secondaryButton } from "@components/ui/styles";
import { Code, ConnectError, createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { CheckCircleIcon } from "@heroicons/react/24/outline";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type { PlannedPortion } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { suggestFixes } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { PlanFix } from "gen/suggestions/v1/suggestions_pb";
import {
  PlanFixKind,
  SuggestionService,
} from "gen/suggestions/v1/suggestions_pb";
import { formatAmount } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import React from "react";

interface FixesDialogProps {
  personId: string;
  date: string;
  // The eating time to fix, "" for any of the day's.
  eatingTimeId: string;
  // What the dialog is about, e.g. "Lunch" or "Ben's day".
  title: string;
  // The day's portions, to resize them.
  portions: PlannedPortion[];
  onClose: () => void;
}

/**
 * FixesDialog suggests portion sizes for a person's day, or one of its
 * eating times, that fix its warnings or bring it closer to its rules and
 * targets, and applies the one picked.
 */
const FixesDialog: React.FC<FixesDialogProps> = ({
  personId,
  date,
  eatingTimeId,
  title,
  portions,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const fixes = useQuery(suggestFixes, { date, eatingTimeId, personId });
  const apply = useMutation<void, ConnectError, PlanFix>({
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
      onClose();
    },
  });

  let content: React.ReactNode = null;
  if (fixes.isPending) {
    content = <Loading />;
  } else if (fixes.isError) {
    content = (
      <QueryError
        error={fixes.error}
        onRetry={() => {
          void fixes.refetch();
        }}
      />
    );
  } else if (fixes.data.fixes.length === 0) {
    content = (
      <p className="text-sm text-gray-600">
        {fixes.data.warnings.length === 0
          ? "Nothing to fix: it meets the rules and targets."
          : "No change to the portion sizes helps. Try planning something else, or check the rules and targets on the People page."}
      </p>
    );
  } else {
    content = (
      <ul className="flex flex-col gap-2">
        {fixes.data.fixes.map((fix) => (
          <li
            key={`${fix.kind}|${fix.description}`}
            className="flex items-start gap-3 rounded-md border border-gray-200 p-3"
          >
            <div className="flex min-w-0 grow flex-col gap-1 text-sm">
              <span className="font-medium text-gray-900">
                {fix.description}
              </span>
              {fix.fixedWarnings.map((warning) => (
                <span
                  key={warning}
                  className="flex items-start gap-1 text-xs text-brand-800"
                >
                  <CheckCircleIcon
                    className="mt-px size-3.5 shrink-0"
                    aria-hidden="true"
                  />
                  <span>
                    Fixes: <span className="text-gray-600">{warning}</span>
                  </span>
                </span>
              ))}
              {fix.warnings.length > 0 && (
                <span className="text-xs text-amber-800">
                  {fix.warnings.length === 1
                    ? "1 warning left"
                    : `${fix.warnings.length} warnings left`}
                  , closer to the limits
                </span>
              )}
              {fix.energyKcal !== fix.plannedEnergyKcal && (
                <span className="text-xs text-gray-500 tabular-nums">
                  Day: {formatAmount(fix.plannedEnergyKcal)} →{" "}
                  {formatAmount(fix.energyKcal)} kcal
                </span>
              )}
            </div>
            <button
              type="button"
              className={secondaryButton}
              disabled={apply.isPending}
              onClick={() => {
                apply.mutate(fix);
              }}
            >
              Apply
            </button>
          </li>
        ))}
      </ul>
    );
  }

  return (
    <Modal open onClose={onClose} title={`Fix ${title}`}>
      <div className="flex flex-col gap-3">
        <p className="text-sm text-gray-600">
          Portion sizes that fix the warnings, or bring them closer to the rules
          and targets, keeping what&apos;s planned.
        </p>
        {content}
        <ErrorText error={apply.error} />
      </div>
    </Modal>
  );
};

export default FixesDialog;
