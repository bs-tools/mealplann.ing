import Loading from "@components/loading";
import { useApplyFix } from "@components/plan/apply-fix";
import { warningTitle } from "@components/today/today-bits";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  ExclamationTriangleIcon,
  SparklesIcon,
} from "@heroicons/react/24/outline";
import type { PlannedPortion, Warning } from "gen/plans/v1/plans_pb";
import { suggestFixes } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import { formatAmount } from "helpers/nutrients";
import React from "react";
import { Link } from "react-router";

interface WarningSheetProps {
  warning: Warning;
  personId: string;
  date: string;
  // The eating time the warning is about, "" for the day's targets.
  eatingTimeId: string;
  // The day's portions, to resize them.
  portions: PlannedPortion[];
  onClose: () => void;
}

/**
 * WarningSheet explains a warning, and offers the best change to the
 * portion sizes that fixes it, to do in one tap or keep as it is.
 */
const WarningSheet: React.FC<WarningSheetProps> = ({
  warning,
  personId,
  date,
  eatingTimeId,
  portions,
  onClose,
}) => {
  const fixes = useQuery(suggestFixes, { date, eatingTimeId, personId });
  const apply = useApplyFix(portions, onClose);
  const [fix] = fixes.data?.fixes ?? [];

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
  } else if (fix === undefined) {
    content = (
      <p className="text-sm text-muted">
        No change to the portion sizes helps.{" "}
        <Link to={`/plan?date=${date}`} className={linkClass}>
          Change the plan
        </Link>{" "}
        instead.
      </p>
    );
  } else {
    content = (
      <>
        <div className="grid grid-cols-[2rem_minmax(0,1fr)] items-center gap-3 rounded-2xl border border-line bg-white p-4">
          <SparklesIcon
            className="size-6 justify-self-center text-pine"
            aria-hidden="true"
          />
          <div className="flex flex-col gap-1">
            <p className="font-bold text-ink-soft">{fix.description}</p>
            {fix.energyKcal !== fix.plannedEnergyKcal && (
              <p className="text-sm text-muted tabular-nums">
                Brings the day to {formatAmount(fix.energyKcal)} kcal.
              </p>
            )}
          </div>
        </div>
        <div className="flex gap-2">
          <button
            type="button"
            className={`${primaryButton} grow`}
            disabled={apply.isPending}
            onClick={() => {
              apply.mutate(fix);
            }}
          >
            {apply.isPending ? "Doing it…" : "Do it"}
          </button>
          <button type="button" className={secondaryButton} onClick={onClose}>
            Keep it
          </button>
        </div>
        <ErrorText error={apply.error} />
      </>
    );
  }

  return (
    <Modal open onClose={onClose} title={warningTitle(warning)}>
      <div className="flex flex-col gap-4">
        <p className="flex items-start gap-2 text-base leading-relaxed text-ink-soft">
          <ExclamationTriangleIcon
            className="mt-1 size-5 shrink-0 text-warn"
            aria-hidden="true"
          />
          {warning.message}
        </p>
        {content}
      </div>
    </Modal>
  );
};

export default WarningSheet;
