import { ClockIcon } from "@heroicons/react/24/outline";
import type { PrepTask } from "gen/plans/v1/plans_pb";
import { addDays, today } from "helpers/dates";
import { formatList, formatWeekday } from "helpers/format";
import React from "react";

interface PrepTasksProps {
  // What to start on the date for cooks of later days.
  tasks: PrepTask[];
}

/**
 * PrepTasks lists what to start on a day for a cook of a later day, like
 * beans to soak the night before, on the day it's done rather than only on
 * the cook's, where it would be read too late. Nothing shows without any.
 */
const PrepTasks: React.FC<PrepTasksProps> = ({ tasks }) => {
  if (tasks.length === 0) {
    return null;
  }
  return (
    <ul className="flex flex-col gap-2 rounded-2xl bg-cook-tint px-4 py-3">
      {tasks.map((task) => (
        <li key={task.cookId} className="flex items-start gap-2.5">
          <ClockIcon
            className="mt-0.5 size-5 shrink-0 text-cook-dark"
            aria-hidden="true"
          />
          <span className="flex min-w-0 flex-col">
            <span className="text-sm font-medium text-ink-soft">
              {task.note === "" ? `Start ${task.recipeName}` : task.note}
            </span>
            <span className="text-sm text-cook-dark">
              For{" "}
              {task.forRecipeNames.length > 0
                ? formatList(task.forRecipeNames)
                : task.recipeName}
              ,{" "}
              {task.cookDate === addDays(today(), 1)
                ? "tomorrow"
                : `on ${formatWeekday(task.cookDate)}`}
            </span>
          </span>
        </li>
      ))}
    </ul>
  );
};

export default PrepTasks;
