import { useApplyFix } from "@components/plan/apply-fix";
import { splitFix } from "@components/today/plate";
import { warningTitle } from "@components/today/today-bits";
import { ErrorText } from "@components/ui/query-error";
import { useQuery } from "@connectrpc/connect-query";
import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import type { PlannedPortion, Warning } from "gen/plans/v1/plans_pb";
import { suggestFixes } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { PlanFix } from "gen/suggestions/v1/suggestions_pb";
import { formatList } from "helpers/format";
import React from "react";

/**
 * FixFor returns the best of a day's fixes that fixes a warning: one that
 * leaves no warning of its kind and nutrient. The best fix overall can
 * fix another warning and leave this one, or make it worse, like a
 * smaller portion for a day over its fat that's also under its protein,
 * so it would contradict the warning it's shown with. Returns undefined
 * if none fixes it.
 */
export const fixFor = (
  fixes: PlanFix[],
  warning: Warning
): PlanFix | undefined =>
  fixes.find(
    (fix) =>
      !fix.warnings.some(
        (left) =>
          left.kind === warning.kind && left.nutrient === warning.nutrient
      )
  );

/**
 * FixLeaves says which warnings a fix leaves, e.g. "Still under daily
 * protein", or "" if none, so it's clear a fix for one isn't for all.
 */
export const fixLeaves = (fix: PlanFix): string => {
  const titles = [
    ...new Set(fix.warnings.map((left) => warningTitle(left).toLowerCase())),
  ];
  return titles.length === 0 ? "" : `Still ${formatList(titles)}.`;
};

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
 * the portion sizes that fixes that warning, if any, done in one tap,
 * like "Have ¾ of a bowl".
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
  const fix = fixFor(fixes.data?.fixes ?? [], warning);
  return (
    <div className="grid grid-cols-[1.5rem_minmax(0,1fr)] gap-x-3 gap-y-2 rounded-2xl border border-warn-line bg-warn-tint p-4">
      <ExclamationTriangleIcon
        className="size-6 self-center justify-self-center text-warn-dark"
        aria-hidden="true"
      />
      <p className="self-center font-bold text-warn-dark">
        {warningTitle(warning)}
        {personName === "" ? "" : ` · ${personName}`}
      </p>
      <p className="col-start-2 text-sm text-ink-soft">{warning.message}</p>
      {fix !== undefined && splitFix(fix.description).detail !== "" && (
        <p className="col-start-2 text-sm text-ink-soft/80">
          {splitFix(fix.description).detail}.
        </p>
      )}
      {fix !== undefined && fixLeaves(fix) !== "" && (
        <p className="col-start-2 text-sm text-ink-soft/80">{fixLeaves(fix)}</p>
      )}
      {fix !== undefined && (
        <button
          type="button"
          className="col-start-2 justify-self-start rounded-xl bg-ink-soft px-4 py-2.5 text-sm font-bold text-white disabled:opacity-50"
          disabled={apply.isPending}
          onClick={() => {
            apply.mutate(fix);
          }}
        >
          {apply.isPending ? "Doing it…" : splitFix(fix.description).action}
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
