import type { MealRow } from "@components/plan/plan-rows";
import {
  rowEffort,
  rowItems,
  rowPlanned,
  rowsOf,
} from "@components/plan/plan-rows";
import TargetBars, { targetStates } from "@components/plan/target-bars";
import type { Effort } from "@components/today/today-bits";
import { sectionLabelClass } from "@components/ui/styles";
import type { NutrientTarget, Person } from "gen/people/v1/people_pb";
import type { PlanDay } from "gen/plans/v1/plans_pb";
import { parseDate, startOfWeek } from "helpers/dates";
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

// TargetsOf returns a person's daily targets, none if they aren't known.
const targetsOf = (people: Person[], personId: string): NutrientTarget[] =>
  people.find((person) => person.id === personId)?.dailyTargets ?? [];

interface PlanDayListProps {
  days: PlanDay[];
  // The household, for each person's daily targets.
  people: Person[];
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
  people,
  today,
  onOpen,
  onPlan,
}) => (
  // A wide screen shows the days side by side, as a week reads.
  <div className="flex flex-col gap-5 lg:grid lg:grid-cols-3 lg:items-start">
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
          <div className="flex items-baseline justify-between gap-3">
            <h2 className={sectionLabelClass}>{label}</h2>
            {/* A day with meals still open can be planned on its own. */}
            {!empty &&
              day.date >= today &&
              rows.some((row) => !rowPlanned(row)) && (
                <Link
                  to={`/plan/auto?start=${day.date}&days=1`}
                  className="text-sm font-medium text-pine hover:underline"
                >
                  Plan this day
                </Link>
              )}
          </div>
          {/* Each person's day against their targets, once it has meals. */}
          {!empty &&
            day.people.some(
              (person) =>
                targetStates(targetsOf(people, person.personId), undefined)
                  .length > 0
            ) && (
              <Link
                to={`/plan/warnings?${new URLSearchParams(
                  startOfWeek(day.date) === startOfWeek(today)
                    ? {}
                    : { week: startOfWeek(day.date) }
                ).toString()}`}
                aria-label={`${label}'s targets`}
                className="flex flex-col gap-2.5 rounded-2xl border border-line bg-white px-4 py-3 hover:border-muted"
              >
                {day.people
                  .filter(
                    (person) => targetsOf(people, person.personId).length > 0
                  )
                  .map((person) => (
                    <span
                      key={person.personId}
                      className="flex items-start gap-3"
                    >
                      <span className="w-20 shrink-0 truncate text-sm font-medium text-ink-soft">
                        {person.personName}
                      </span>
                      <TargetBars
                        compact
                        targets={targetsOf(people, person.personId)}
                        nutrients={person.nutrients}
                      />
                    </span>
                  ))}
              </Link>
            )}
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
              const items = rowItems(row);
              const stapleOnly = items.every((item) => item.staple);
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
                        {stapleOnly && items.length > 0
                          ? "staples"
                          : EFFORT_WORD[effort]}
                      </span>
                    </span>
                    {items
                      .filter((item) => !item.staple)
                      .map((item) => (
                        <span key={item.name} className="text-sm leading-snug">
                          <span className="text-ink-soft">{item.name}</span>
                          {item.leftover && (
                            <span className="text-muted"> (leftovers)</span>
                          )}
                          <span className="text-muted">
                            {item.shares.every((share) => share.amount === "")
                              ? ""
                              : " · "}
                            {day.people.length === 1
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
                    {/*
                     * Staples are the same every day, so they take one
                     * line; the meal opens them, to skip one for the day.
                     */}
                    {items.some((item) => item.staple) && (
                      <span className="truncate text-sm text-muted">
                        {items.every((item) => item.staple) ? "" : "+ "}
                        {items
                          .filter((item) => item.staple)
                          .map((item) => item.name)
                          .join(", ")}
                      </span>
                    )}
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
