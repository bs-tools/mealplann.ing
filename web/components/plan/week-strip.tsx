import type { PlanDay } from "gen/plans/v1/plans_pb";
import { WEEKDAY_NAMES } from "helpers/dates";
import React from "react";

/**
 * FillOf returns how much of a day is planned, from 0 to 1: the share of
 * everyone's eating times with something planned, or eating out. A day
 * with no eating times counts as planned, as there's nothing to plan.
 */
export const fillOf = (day: PlanDay | undefined): number => {
  let slots = 0;
  let planned = 0;
  for (const person of day?.people ?? []) {
    for (const et of person.eatingTimes) {
      slots += 1;
      if (et.eatingOut || et.portions.length > 0) {
        planned += 1;
      }
    }
  }
  return slots === 0 ? 1 : planned / slots;
};

// OpenSlotsOf returns how many of a day's eating times have nothing planned.
export const openSlotsOf = (day: PlanDay | undefined): number =>
  (day?.people ?? []).reduce(
    (sum, person) =>
      sum +
      person.eatingTimes.filter(
        (et) => !et.eatingOut && et.portions.length === 0
      ).length,
    0
  );

interface FillDotProps {
  fill: number;
  // Whether it's the chosen day, ringed.
  chosen?: boolean;
  size?: string;
}

/**
 * FillDot shows how planned a day is: solid when planned, a pie when
 * partly, and a dashed circle when there's nothing yet.
 */
export const FillDot: React.FC<FillDotProps> = ({
  fill,
  chosen = false,
  size = "size-9",
}) => {
  const ring = chosen
    ? "shadow-[0_0_0_3px_var(--color-ground),0_0_0_5px_var(--color-ink-soft)]"
    : "";
  if (fill <= 0) {
    return (
      <span
        className={`block ${size} rounded-full border-2 border-dashed border-gray-500 ${ring}`}
      />
    );
  }
  const percent = Math.round(fill * 100);
  return (
    <span
      className={`block ${size} rounded-full ${ring}`}
      style={{
        background:
          percent >= 100
            ? "var(--color-pine)"
            : `conic-gradient(var(--color-pine) 0 ${percent}%, var(--color-pine-100) ${percent}% 100%)`,
      }}
    />
  );
};

interface WeekStripProps {
  dates: string[];
  // Today, before which days are dimmed, as there's no planning them.
  today: string;
  days: PlanDay[];
  chosen: string;
  onChoose: (date: string) => void;
}

/**
 * WeekStrip is the week's days as dots, filled as they're planned, to see
 * at a glance which days still need planning, and to choose one.
 */
const WeekStrip: React.FC<WeekStripProps> = ({
  dates,
  today,
  days,
  chosen,
  onChoose,
}) => (
  <nav aria-label="Days" className="grid grid-cols-7 gap-1.5 print:hidden">
    {dates.map((date, index) => {
      const fill = fillOf(days.find((day) => day.date === date));
      let state = "planned";
      if (fill <= 0) {
        state = "nothing planned";
      } else if (fill < 1) {
        state = "partly planned";
      }
      return (
        <button
          key={date}
          type="button"
          aria-current={date === chosen ? "date" : undefined}
          aria-label={`${WEEKDAY_NAMES[index] ?? ""} ${Number(date.slice(8))}, ${state}`}
          className={`flex flex-col items-center gap-1.5 rounded-xl py-1 text-xs ${date === chosen ? "font-bold text-ink-soft" : "text-muted"} ${date < today && date !== chosen ? "opacity-50" : ""}`}
          onClick={() => {
            onChoose(date);
          }}
        >
          {WEEKDAY_NAMES[index]?.slice(0, 1)}
          <FillDot fill={fill} chosen={date === chosen} />
          <span className="tabular-nums">{Number(date.slice(8))}</span>
        </button>
      );
    })}
  </nav>
);

export default WeekStrip;
