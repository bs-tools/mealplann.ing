import { useApplyFix } from "@components/plan/apply-fix";
import { warningTitle } from "@components/today/today-bits";
import { ErrorText } from "@components/ui/query-error";
import { useQuery } from "@connectrpc/connect-query";
import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import type { PlannedPortion, Warning } from "gen/plans/v1/plans_pb";
import { suggestFixes } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import React from "react";

interface WarningFixProps {
  warning: Warning;
  // Whose it is, to say in its title, e.g. "Charlotte".
  personName: string;
  personId: string;
  date: string;
  // The eating time it's about, "" for the day's targets.
  eatingTimeId: string;
  // The day's portions, to resize them.
  portions: PlannedPortion[];
}

/**
 * WarningFix is a warning as a card: what's wrong, and the best change to
 * the portion sizes that fixes it, done in one tap, like "Have ¾ of a
 * bowl".
 */
const WarningFix: React.FC<WarningFixProps> = ({
  warning,
  personName,
  personId,
  date,
  eatingTimeId,
  portions,
}) => {
  const fixes = useQuery(suggestFixes, { date, eatingTimeId, personId });
  const apply = useApplyFix(portions, () => {
    // The plan's queries refresh, so there's nothing more to do.
  });
  const [fix] = fixes.data?.fixes ?? [];
  return (
    <div className="grid grid-cols-[1.5rem_minmax(0,1fr)] gap-x-3 gap-y-2 rounded-2xl border border-cook-tint bg-warn-tint/70 p-4">
      <ExclamationTriangleIcon
        className="size-6 self-center justify-self-center text-warn-dark"
        aria-hidden="true"
      />
      <p className="self-center font-bold text-warn-dark">
        {warningTitle(warning)}
        {personName === "" ? "" : ` · ${personName}`}
      </p>
      <p className="col-start-2 text-sm text-ink-soft">{warning.message}</p>
      {fix !== undefined && (
        <button
          type="button"
          className="col-start-2 justify-self-start rounded-xl bg-ink-soft px-4 py-2.5 text-sm font-bold text-white disabled:opacity-50"
          disabled={apply.isPending}
          onClick={() => {
            apply.mutate(fix);
          }}
        >
          {apply.isPending ? "Doing it…" : fix.description}
        </button>
      )}
      {apply.error !== null && (
        <div className="col-start-2">
          <ErrorText error={apply.error} />
        </div>
      )}
    </div>
  );
};

export default WarningFix;
