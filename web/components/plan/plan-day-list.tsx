import type { MealRow } from "@components/plan/plan-rows";
import {
  rowEffort,
  rowItems,
  rowPlanned,
  rowsOf,
} from "@components/plan/plan-rows";
import type { Effort } from "@components/today/today-bits";
import { sectionLabelClass } from "@components/ui/styles";
import type { PlanDay } from "gen/plans/v1/plans_pb";
import { parseDate } from "helpers/dates";
import { formatList } from "helpers/format";
import React from "react";
import { Link } from "react-router";

// EFFORT_WORD says in a word how much work a meal is.
const EFFORT_WORD: Record<Effort, string> = {
  cook: "cook",
  heat: "heat",
  none: "",
  out: "eating out",
  ready: "ready",
};

// EFFORT_STYLE colors each effort's dot and word.
const EFFORT_STYLE: Record<Effort, { dot: string; word: string }> = {
  cook: { dot: "bg-cook", word: "text-cook-dark" },
  heat: { dot: "bg-heat", word: "text-heat" },
  none: { dot: "bg-line", word: "text-muted" },
  out: { dot: "bg-muted", word: "text-muted" },
  ready: { dot: "bg-ready", word: "text-ready" },
};

const emptyRowClass =
  "flex w-full items-center rounded-2xl border-2 border-dashed border-line px-4 py-3.5 text-left text-base text-ink-soft hover:border-muted";

interface PlanDayListProps {
  days: PlanDay[];
  today: string;
  onOpen: (day: PlanDay, row: MealRow) => void;
  onPlan: (day: PlanDay, row: MealRow) => void;
}

/**
 * PlanDayList lists days of the plan as the household thinks of them: a
 * row for each meal, saying what's planned and how much work it is, or a
 * dashed row to plan it, and a day with nothing planned as one row.
 */
const PlanDayList: React.FC<PlanDayListProps> = ({
  days,
  today,
  onOpen,
  onPlan,
}) => (
  <div className="flex flex-col gap-5">
    {days.map((day) => {
      const rows = rowsOf(day);
      const empty = rows.every((row) => !rowPlanned(row));
      const label =
        day.date === today
          ? "Today"
          : parseDate(day.date).toLocaleDateString(undefined, {
              weekday: "long",
            });
      return (
        <section
          key={day.date}
          id={`day-${day.date}`}
          className="flex scroll-mt-4 flex-col gap-2"
        >
          <h2 className={sectionLabelClass}>{label}</h2>
          {empty ? (
            <Link
              to={`/plan/auto?start=${day.date}&days=1`}
              className={emptyRowClass}
            >
              +{" "}
              {formatList(rows.map((row) => row.label.toLowerCase())).replace(
                /^./u,
                (first) => first.toUpperCase()
              )}
            </Link>
          ) : (
            rows.map((row) => {
              if (!rowPlanned(row)) {
                return (
                  <button
                    key={row.key}
                    type="button"
                    className={emptyRowClass}
                    onClick={() => {
                      onPlan(day, row);
                    }}
                  >
                    + {row.label}
                  </button>
                );
              }
              const effort = rowEffort(row, day.date);
              const style = EFFORT_STYLE[effort];
              return (
                <button
                  key={row.key}
                  type="button"
                  className="flex w-full items-start gap-3 rounded-2xl border border-line bg-white px-4 py-3.5 text-left hover:border-muted"
                  onClick={() => {
                    onOpen(day, row);
                  }}
                >
                  <span
                    className={`mt-2 size-2.5 shrink-0 rounded-full ${style.dot}`}
                    aria-hidden="true"
                  />
                  <span className="flex min-w-0 grow flex-col gap-1">
                    <span className="flex items-baseline gap-3">
                      <span className="grow text-base font-bold text-ink-soft">
                        {row.label}
                      </span>
                      <span className={`shrink-0 text-sm ${style.word}`}>
                        {EFFORT_WORD[effort]}
                      </span>
                    </span>
                    {rowItems(row).map((item) => (
                      <span key={item.name} className="text-sm leading-snug">
                        <span className="text-ink-soft">{item.name}</span>
                        {item.leftover && (
                          <span className="text-muted"> (leftovers)</span>
                        )}
                        <span className="text-muted">
                          {item.shares.every((share) => share.amount === "")
                            ? ""
                            : " · "}
                          {row.slots.length === 1
                            ? item.shares
                                .map((share) => share.amount)
                                .join(" + ")
                            : item.shares
                                .map((share) =>
                                  `${share.personName} ${share.amount}`.trim()
                                )
                                .join(" · ")}
                        </span>
                      </span>
                    ))}
                    {row.slots
                      .filter((slot) => slot.planned.eatingOut)
                      .map((slot) => (
                        <span
                          key={slot.person.personId}
                          className="text-sm text-muted"
                        >
                          {slot.person.personName} is eating out
                        </span>
                      ))}
                  </span>
                </button>
              );
            })
          )}
        </section>
      );
    })}
  </div>
);

export default PlanDayList;
