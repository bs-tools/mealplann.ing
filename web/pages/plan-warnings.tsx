import Loading from "@components/loading";
import type { WeekWarning } from "@components/plan/week-warnings";
import { weekWarnings } from "@components/plan/week-warnings";
import { warningTitle } from "@components/today/today-bits";
import WarningSheet from "@components/today/warning-sheet";
import BackLink from "@components/ui/back-link";
import ConfirmButton from "@components/ui/confirm-button";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  iconButton,
  pillDangerButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ExclamationTriangleIcon,
} from "@heroicons/react/24/outline";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  deleteCook,
  getWeek,
} from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService, WarningKind } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { addDays, isDateString, startOfWeek, today } from "helpers/dates";
import { formatDay } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link, useSearchParams } from "react-router";

/*
 * WarningRowTitle names a warning and whose it is, e.g. "Charlotte ·
 * Under daily protein", or the cook's recipe for a cook's.
 */
const warningRowTitle = (item: WeekWarning): string => {
  const who =
    item.cook === undefined
      ? (item.person?.personName ?? "")
      : (item.cook.cook?.recipeName ?? "");
  const at = item.eatingTimeName === "" ? "" : ` at ${item.eatingTimeName}`;
  return `${who} · ${warningTitle(item.warning)}${at}`;
};

interface OrphanedCookProps {
  cookId: string;
  recipeName: string;
  // The warning's icon and text.
  children: React.ReactNode;
}

/**
 * OrphanedCook is the warning of a cook nobody's planned to eat any of,
 * with a button to delete it: it's usually left behind by accident, and
 * not on the plan to open, as only meals are.
 */
const OrphanedCook: React.FC<OrphanedCookProps> = ({
  cookId,
  recipeName,
  children,
}) => {
  const queryClient = useQueryClient();
  const remove = useMutation(deleteCook, {
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
    <div className="flex flex-col gap-2 py-3.5">
      <div className="flex items-start gap-3">{children}</div>
      <div className="flex flex-wrap gap-2 pl-8">
        <ConfirmButton
          className={pillDangerButton}
          title={`Delete the cook of ${recipeName}?`}
          message="It isn't cooked, bought for or planned anymore. Plan a meal of it instead if it's still wanted."
          confirmLabel="Delete"
          disabled={remove.isPending}
          onConfirm={() => {
            remove.mutate({ id: cookId });
          }}
        >
          {remove.isPending ? "Deleting…" : "Delete the cook"}
        </ConfirmButton>
      </div>
      <ErrorText error={remove.error} />
    </div>
  );
};

/**
 * PlanWarningsPage lists the week's warnings in one place, from today on
 * for this week: where the plan misses someone's targets or rules, and
 * cooks kept too long. The plan and meals only link here, as warnings
 * beside every meal crowded them out. A person's warning opens the change
 * to the portion sizes that fixes it, if one does.
 */
const PlanWarningsPage: React.FC = () => {
  const [params, setParams] = useSearchParams();
  const now = today();
  const weekParam = params.get("week") ?? "";
  const monday = startOfWeek(isDateString(weekParam) ? weekParam : now);
  const current = monday === startOfWeek(now);
  const week = useQuery(
    getWeek,
    { monday },
    { placeholderData: keepPreviousData }
  );
  const [open, setOpen] = useState<WeekWarning | null>(null);
  const goTo = (next: string) => {
    setParams(next === startOfWeek(now) ? {} : { week: next }, {
      replace: true,
    });
  };

  let content: React.ReactNode = null;
  if (week.isPending) {
    content = <Loading />;
  } else if (week.isError) {
    content = (
      <QueryError
        error={week.error}
        onRetry={() => {
          void week.refetch();
        }}
      />
    );
  } else {
    // This week's past days can't be changed for the better, so they're left out.
    const warnings = weekWarnings(week.data, current ? now : "");
    const dates = [...new Set(warnings.map((item) => item.date))];
    content =
      warnings.length === 0 ? (
        <EmptyState title="No warnings">
          The plan meets everyone&apos;s targets and rules
          {current ? " from today on" : ""}.
        </EmptyState>
      ) : (
        <div className="flex flex-col gap-5">
          {dates.map((date) => (
            <section key={date} className="flex flex-col gap-2">
              <h2 className={sectionLabelClass}>
                {date === now ? "Today" : formatDay(date)}
              </h2>
              <ul className="divide-y divide-line rounded-2xl border border-line bg-white px-4">
                {warnings
                  .filter((item) => item.date === date)
                  .map((item) => {
                    const body = (
                      <>
                        <ExclamationTriangleIcon
                          className="mt-0.5 size-5 shrink-0 text-warn"
                          aria-hidden="true"
                        />
                        <span className="flex min-w-0 grow flex-col gap-0.5">
                          <span className="font-bold text-ink-soft">
                            {warningRowTitle(item)}
                          </span>
                          <span className="text-sm text-muted">
                            {item.warning.message}
                          </span>
                        </span>
                      </>
                    );
                    /*
                     * A cook nobody eats offers to delete it; another
                     * cook's warning opens its day, and a person's, the
                     * fix for it.
                     */
                    let row: React.ReactNode = null;
                    if (
                      item.cook !== undefined &&
                      item.warning.kind === WarningKind.SERVINGS_UNASSIGNED
                    ) {
                      row = (
                        <OrphanedCook
                          cookId={item.cook.cook?.id ?? ""}
                          recipeName={item.cook.cook?.recipeName ?? ""}
                        >
                          {body}
                        </OrphanedCook>
                      );
                    } else if (item.person === undefined) {
                      row = (
                        <Link
                          to={`/plan?${new URLSearchParams(current ? { day: date } : { day: date, week: monday }).toString()}`}
                          className="flex items-start gap-3 py-3.5 text-left"
                        >
                          {body}
                        </Link>
                      );
                    } else {
                      row = (
                        <button
                          type="button"
                          className="flex w-full items-start gap-3 py-3.5 text-left"
                          onClick={() => {
                            setOpen(item);
                          }}
                        >
                          {body}
                        </button>
                      );
                    }
                    return <li key={item.key}>{row}</li>;
                  })}
              </ul>
            </section>
          ))}
        </div>
      );
  }

  return (
    <div className="flex flex-col gap-4">
      <BackLink to={current ? "/plan" : `/plan?week=${monday}`}>Plan</BackLink>
      <PageHeader
        kicker={`Week of ${formatDay(monday)}`}
        title="Warnings"
        subtitle="Where the plan misses someone's targets or rules. Open one for a change to the portions that fixes it."
        actions={
          <div className="flex">
            <button
              type="button"
              className={iconButton}
              aria-label="Previous week"
              title="Previous week"
              onClick={() => {
                goTo(addDays(monday, -7));
              }}
            >
              <ChevronLeftIcon className="size-5" />
            </button>
            <button
              type="button"
              className={iconButton}
              aria-label="Next week"
              title="Next week"
              onClick={() => {
                goTo(addDays(monday, 7));
              }}
            >
              <ChevronRightIcon className="size-5" />
            </button>
          </div>
        }
      />
      {content}
      {open?.person !== undefined && (
        <WarningSheet
          warning={open.warning}
          personId={open.person.personId}
          date={open.date}
          eatingTimeId={open.eatingTimeId}
          portions={open.person.eatingTimes.flatMap(
            (planned) => planned.portions
          )}
          onClose={() => {
            setOpen(null);
          }}
        />
      )}
    </div>
  );
};

export default PlanWarningsPage;
