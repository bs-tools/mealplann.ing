import Loading from "@components/loading";
import BackLink from "@components/ui/back-link";
import Card from "@components/ui/card";
import { PageHeader, Stat } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import { inputClass } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  getRecipe,
  getRecipeTimeline,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { TimedStep } from "gen/recipes/v1/recipes_pb";
import { formatHoursMinutes } from "helpers/format";
import { formatStepDuration } from "helpers/steps";
import React, { useState } from "react";
import { Link, useParams } from "react-router";

// TickMinutes returns the minutes between the axis's ticks for a total.
const tickMinutes = (totalMinutes: number): number => {
  for (const step of [5, 10, 15, 30, 60]) {
    if (totalMinutes / step <= 8) {
      return step;
    }
  }
  return 120;
};

/**
 * ClockAt formats the time of day a number of seconds after a start, given
 * as "HH:MM", e.g. "17:45".
 */
const clockAt = (start: string, seconds: number): string => {
  const [hours = 0, minutes = 0] = start.split(":").map(Number);
  const total = (hours * 60 + minutes + Math.round(seconds / 60)) % (24 * 60);
  const wrapped = total < 0 ? total + 24 * 60 : total;
  return `${String(Math.floor(wrapped / 60)).padStart(2, "0")}:${String(wrapped % 60).padStart(2, "0")}`;
};

interface BarProps {
  step: TimedStep;
  total: number;
}

// Bar is a step's bar on the timeline, placed by its start and end.
const Bar: React.FC<BarProps> = ({ step, total }) => {
  const left = (step.startSeconds / total) * 100;
  const width = Math.max(
    ((step.endSeconds - step.startSeconds) / total) * 100,
    0.8
  );
  let style = "bg-pine text-white";
  if (step.waiting) {
    style =
      "bg-[repeating-linear-gradient(135deg,var(--color-amber-200)_0_6px,var(--color-amber-100)_6px_12px)] text-warn-dark ring-1 ring-warn-line";
  }
  if (step.estimated) {
    style +=
      " opacity-60 outline-1 outline-offset-1 outline-dashed outline-gray-400";
  }
  const duration = formatStepDuration(step.endSeconds - step.startSeconds);
  return (
    <div className="relative h-7">
      <div
        className={`absolute inset-y-0 flex items-center overflow-hidden rounded px-1.5 text-xs font-medium whitespace-nowrap ${style}`}
        style={{ left: `${left}%`, width: `${width}%` }}
        title={`${duration}${step.estimated ? " (estimated)" : ""}${step.waiting ? ", waiting" : ""}`}
      >
        {step.estimated ? `~${duration}` : duration}
      </div>
    </div>
  );
};

/**
 * RecipeTimelinePage shows when a recipe's steps happen, for one cook: the
 * hands-on steps in a row, and the waiting ones, like simmering, under
 * them, with what can be done in the meantime. Given when to serve, it
 * says when to start each step.
 */
const RecipeTimelinePage: React.FC = () => {
  const { id = "" } = useParams();
  const recipe = useQuery(getRecipe, { id });
  const timeline = useQuery(getRecipeTimeline, { id });
  const [serveAt, setServeAt] = useState("");

  if (recipe.isPending || timeline.isPending) {
    return <Loading />;
  }
  const error = recipe.error ?? timeline.error;
  if (error !== null) {
    return (
      <QueryError
        error={error}
        onRetry={() => {
          void recipe.refetch();
          void timeline.refetch();
        }}
      />
    );
  }
  const data = recipe.data?.recipe;
  if (data === undefined || timeline.data === undefined) {
    return null;
  }
  const { steps, totalSeconds, handsOnSeconds } = timeline.data;
  const total = Math.max(totalSeconds, 60);
  const tick = tickMinutes(total / 60);
  const ticks = Array.from(
    { length: Math.floor(total / 60 / tick) + 1 },
    (_unused, index) => index * tick
  );
  const start = serveAt === "" ? "" : clockAt(serveAt, -totalSeconds);
  const estimated = steps.filter((step) => step.estimated).length;

  return (
    <div className="flex flex-col gap-4">
      <BackLink to={`/recipes/${id}`}>{data.name}</BackLink>
      <PageHeader
        kicker="Timeline"
        title={data.name}
        subtitle="When each step happens, for one cook: hands-on work fits into the waiting, like chopping while rice simmers."
      />
      <div className="-mt-2 grid grid-cols-3 gap-2">
        <Stat label="In all" value={formatHoursMinutes(totalSeconds)} />
        <Stat label="Hands-on" value={formatHoursMinutes(handsOnSeconds)} />
        <Stat
          label="Waiting"
          value={formatHoursMinutes(Math.max(totalSeconds - handsOnSeconds, 0))}
        />
      </div>
      <label className="flex items-center justify-between gap-3 rounded-2xl border border-line bg-white px-4 py-3">
        <span className="flex flex-col">
          <span className="font-bold text-ink-soft">Serve at</span>
          <span className="text-sm text-muted">
            {start === "" ? (
              "To know when to start."
            ) : (
              <>
                Start at{" "}
                <span className="font-bold text-ink-soft tabular-nums">
                  {start}
                </span>
              </>
            )}
          </span>
        </span>
        <input
          type="time"
          className={`${inputClass} w-32`}
          value={serveAt}
          onChange={(event) => {
            setServeAt(event.target.value);
          }}
        />
      </label>
      {data.instructions.length === 0 && steps.length > 0 && (
        <p className="rounded-2xl border border-warn-line bg-warn-tint p-4 text-sm text-ink-soft">
          This recipe&apos;s steps don&apos;t say how long they take or what can
          happen at the same time yet, so they&apos;re shown one after the
          other, 5 minutes each. Edit the recipe to add their durations.
        </p>
      )}
      {data.instructions.length > 0 && estimated > 0 && (
        <p className="text-sm text-muted">
          {estimated} {estimated === 1 ? "step doesn't" : "steps don't"} say how
          long {estimated === 1 ? "it takes" : "they take"}, so{" "}
          {estimated === 1 ? "it's" : "they're"} counted as 5 minutes, marked ~.
        </p>
      )}
      <Card>
        {steps.length === 0 ? (
          <p className="text-sm text-muted">This recipe has no steps yet.</p>
        ) : (
          <div className="flex flex-col gap-1">
            <div className="grid grid-cols-1 gap-x-4 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
              <span className="hidden sm:block" />
              <div className="relative h-5 text-[11px] text-muted tabular-nums">
                {ticks.map((minute) => (
                  <span
                    key={minute}
                    className="absolute -translate-x-1/2 first:translate-x-0"
                    style={{ left: `${((minute * 60) / total) * 100}%` }}
                  >
                    {start === "" ? `${minute}m` : clockAt(start, minute * 60)}
                  </span>
                ))}
              </div>
            </div>
            {steps.map((step, position) => {
              const heading =
                step.section !== "" &&
                step.section !== steps[position - 1]?.section
                  ? step.section
                  : "";
              return (
                <React.Fragment key={step.stepIndex}>
                  {heading !== "" && (
                    <p className="mt-2 text-xs font-semibold tracking-wide text-muted uppercase">
                      {heading}
                    </p>
                  )}
                  <div className="grid grid-cols-1 items-center gap-x-4 gap-y-1 border-t border-line py-1.5 sm:grid-cols-[minmax(0,2fr)_minmax(0,3fr)]">
                    <p className="text-sm text-ink-soft">
                      <span className="mr-1.5 font-semibold text-muted tabular-nums">
                        {step.stepIndex + 1}.
                      </span>
                      {start !== "" && (
                        <span className="mr-1.5 text-xs font-medium text-pine tabular-nums">
                          {clockAt(start, step.startSeconds)}
                        </span>
                      )}
                      {step.text}
                    </p>
                    <div className="relative">
                      {ticks.map((minute) => (
                        <span
                          key={minute}
                          className="absolute inset-y-0 border-l border-line"
                          style={{ left: `${((minute * 60) / total) * 100}%` }}
                        />
                      ))}
                      <Bar step={step} total={total} />
                    </div>
                  </div>
                </React.Fragment>
              );
            })}
            <div className="mt-3 flex flex-wrap gap-4 text-xs text-muted">
              <span className="flex items-center gap-1.5">
                <span className="size-3 rounded-sm bg-pine" /> Hands-on
              </span>
              <span className="flex items-center gap-1.5">
                <span className="size-3 rounded-sm bg-warn-line ring-1 ring-warn-line" />{" "}
                Waiting: simmering, baking, resting
              </span>
            </div>
          </div>
        )}
      </Card>
      <div className="sticky bottom-[3.75rem] z-10 bg-ground/90 py-2 backdrop-blur lg:bottom-4">
        <Link
          to={`/recipes/${id}/cook`}
          className="flex w-full items-center justify-center rounded-xl bg-cook py-3.5 text-base font-bold text-white hover:bg-cook-dark"
        >
          Cook it now
        </Link>
      </div>
    </div>
  );
};

export default RecipeTimelinePage;
