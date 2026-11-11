import { fillOf } from "@components/plan/week-strip";
import QueryError from "@components/ui/query-error";
import { ConnectError } from "@connectrpc/connect";
import { createQueryOptions, useTransport } from "@connectrpc/connect-query";
import { FireIcon, ShoppingBagIcon } from "@heroicons/react/24/outline";
import { useQueries } from "@tanstack/react-query";
import { getWeek } from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlanDay } from "gen/plans/v1/plans_pb";
import type { ShoppingList } from "gen/shopping/v1/shopping_pb";
import {
  WEEKDAY_NAMES,
  addDays,
  endOfMonth,
  startOfMonth,
  startOfWeek,
} from "helpers/dates";
import React from "react";

interface MonthViewProps {
  // A date in the month to show.
  month: string;
  today: string;
  lists: ShoppingList[];
  onChoose: (date: string) => void;
}

/**
 * MonthView is a month's calendar of how planned each day is, with the days
 * something's cooked and the household shops, to see when to plan next.
 */
const MonthView: React.FC<MonthViewProps> = ({
  month,
  today,
  lists,
  onChoose,
}) => {
  const first = startOfMonth(month);
  const last = endOfMonth(month);
  const mondays: string[] = [];
  for (
    let monday = startOfWeek(first);
    monday <= last;
    monday = addDays(monday, 7)
  ) {
    mondays.push(monday);
  }
  const transport = useTransport();
  const weeks = useQueries({
    queries: mondays.map((monday) =>
      createQueryOptions(getWeek, { monday, summary: true }, { transport })
    ),
  });
  const failed = weeks.find((week) => week.isError);
  if (failed?.error !== undefined && failed.error !== null) {
    return (
      <QueryError
        error={ConnectError.from(failed.error)}
        onRetry={() => {
          for (const week of weeks) {
            void week.refetch();
          }
        }}
      />
    );
  }
  const days = new Map<string, PlanDay>();
  const cooked = new Set<string>();
  for (const week of weeks) {
    for (const day of week.data?.days ?? []) {
      days.set(day.date, day);
    }
    for (const planned of week.data?.cooks ?? []) {
      cooked.add(planned.cook?.date ?? "");
    }
  }
  const shopping = new Set(
    lists.filter((list) => !list.complete).map((list) => list.startDate)
  );
  const loading = weeks.some((week) => week.isPending);

  return (
    <div className="flex flex-col gap-3">
      <div className="grid grid-cols-7 gap-1 text-center text-xs text-muted">
        {WEEKDAY_NAMES.map((name) => (
          <span key={name}>{name.slice(0, 1)}</span>
        ))}
      </div>
      <div className={`grid grid-cols-7 gap-1 ${loading ? "opacity-60" : ""}`}>
        {mondays
          .flatMap((monday) =>
            Array.from({ length: 7 }, (_unused, index) =>
              addDays(monday, index)
            )
          )
          .map((date) => {
            const inMonth = date >= first && date <= last;
            const fill = fillOf(days.get(date));
            let look = "border-2 border-dashed border-gray-500 text-ink-soft";
            if (date < today) {
              look = "bg-ground-dark text-muted";
            } else if (fill >= 1) {
              look = "bg-pine text-white";
            } else if (fill > 0) {
              look = "text-ink-soft";
            }
            return (
              <button
                key={date}
                type="button"
                className={`flex h-12 flex-col justify-between rounded-xl p-1.5 text-left text-xs tabular-nums ${look} ${inMonth ? "" : "opacity-40"} ${date === today ? "shadow-[0_0_0_2px_var(--color-ground),0_0_0_4px_var(--color-ink-soft)]" : ""}`}
                style={
                  date >= today && fill > 0 && fill < 1
                    ? {
                        background: `linear-gradient(to top, var(--color-pine) ${Math.round(fill * 100)}%, var(--color-pine-100) ${Math.round(fill * 100)}%)`,
                      }
                    : undefined
                }
                onClick={() => {
                  onChoose(date);
                }}
              >
                {Number(date.slice(8))}
                <span className="flex gap-0.5">
                  {cooked.has(date) && (
                    <FireIcon
                      className="size-3.5 stroke-2"
                      aria-label="Cooking"
                    />
                  )}
                  {shopping.has(date) && (
                    <ShoppingBagIcon
                      className="size-3.5 stroke-2"
                      aria-label="Shopping"
                    />
                  )}
                </span>
              </button>
            );
          })}
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-ink-soft">
        <span className="flex items-center gap-1.5">
          <span className="size-3 rounded bg-pine" />
          Planned
        </span>
        <span className="flex items-center gap-1.5">
          <span
            className="size-3 rounded"
            style={{
              background:
                "linear-gradient(to top, var(--color-pine) 60%, var(--color-pine-100) 60%)",
            }}
          />
          Partly
        </span>
        <span className="flex items-center gap-1.5">
          <span className="size-3 rounded border-2 border-dashed border-gray-500" />
          To plan
        </span>
        <span className="flex items-center gap-1.5">
          <FireIcon className="size-3.5" aria-hidden="true" />
          Cooking
        </span>
        <span className="flex items-center gap-1.5">
          <ShoppingBagIcon className="size-3.5" aria-hidden="true" />
          Shopping
        </span>
      </div>
    </div>
  );
};

export default MonthView;
