import Loading from "@components/loading";
import { WarningList } from "@components/plan/plan-bits";
import type { PickedRecipe } from "@components/recipes/recipe-picker";
import RecipePicker from "@components/recipes/recipe-picker";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { EmptyState, PageHeader } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  iconButton,
  inputClass,
  labelClass,
  linkClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  ClockIcon,
  PencilSquareIcon,
  PlusIcon,
  TrashIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  createCook,
  deleteCook,
  getWeek,
  updateCook,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type {
  GetWeekResponse,
  PlannedCook,
  PrepTask,
} from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { addDays, startOfWeek, today } from "helpers/dates";
import { formatDate, formatShortDate, formatWeekday } from "helpers/format";
import { formatServings, householdMealLabel } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

// DAYS is how many days of cooks the page shows, from today.
const DAYS = 14;

// Eater is a portion of a cook: who eats it, when.
interface Eater {
  personName: string;
  date: string;
  eatingTimeName: string;
  servings: number;
}

// Upcoming is a cook to come, with who eats it and what to start ahead.
interface Upcoming {
  planned: PlannedCook;
  eaters: Eater[];
  prepTasks: (PrepTask & { date: string })[];
}

/**
 * UpcomingCooks returns the cooks of the weeks read from one date to
 * another, by date, each with its portions and prep tasks.
 */
const upcomingCooks = (
  weeks: (GetWeekResponse | undefined)[],
  from: string,
  to: string
): Upcoming[] => {
  const eaters = new Map<string, Eater[]>();
  const prep = new Map<string, (PrepTask & { date: string })[]>();
  const cooks: PlannedCook[] = [];
  for (const week of weeks) {
    if (week === undefined) {
      continue;
    }
    cooks.push(...week.cooks);
    for (const day of week.days) {
      for (const task of day.prepTasks) {
        prep.set(task.cookId, [
          ...(prep.get(task.cookId) ?? []),
          { ...task, date: day.date },
        ]);
      }
      const portions = day.people.flatMap((person) =>
        person.eatingTimes.flatMap((planned) =>
          planned.portions
            .filter((portion) => portion.cookId !== "")
            .map((portion) => ({
              cookId: portion.cookId,
              eater: {
                date: day.date,
                eatingTimeName: planned.eatingTime?.name ?? "",
                personName: person.personName,
                servings: portion.servings,
              },
            }))
        )
      );
      for (const { cookId, eater } of portions) {
        eaters.set(cookId, [...(eaters.get(cookId) ?? []), eater]);
      }
    }
  }
  // The weeks are in order, and their cooks by date.
  return cooks
    .filter((planned) => {
      const date = planned.cook?.date ?? "";
      return date >= from && date <= to;
    })
    .map((planned) => ({
      eaters: eaters.get(planned.cook?.id ?? "") ?? [],
      planned,
      prepTasks: prep.get(planned.cook?.id ?? "") ?? [],
    }));
};

interface CookCardProps {
  upcoming: Upcoming;
  disabled: boolean;
  onSave: (
    id: string,
    change: { date: string; servings: number; notes: string }
  ) => void;
  onDone: (id: string, done: boolean) => void;
  onDelete: (id: string) => void;
}

// CookCard shows a cook to come, to mark done, change or delete.
const CookCard: React.FC<CookCardProps> = ({
  upcoming,
  disabled,
  onSave,
  onDone,
  onDelete,
}) => {
  const { planned, eaters, prepTasks } = upcoming;
  const { cook } = planned;
  const [editing, setEditing] = useState(false);
  const [date, setDate] = useState(cook?.date ?? "");
  const [servings, setServings] = useState(String(cook?.servings ?? 1));
  const [notes, setNotes] = useState(cook?.notes ?? "");
  const [invalid, setInvalid] = useState("");
  if (cook === undefined) {
    return null;
  }
  const meal = householdMealLabel(cook.meal);
  return (
    <li className="flex flex-col gap-2 py-3">
      <div className="flex items-start gap-2">
        <input
          type="checkbox"
          className="mt-1 size-4 shrink-0 rounded border-gray-300 accent-brand-700"
          checked={cook.done}
          disabled={disabled}
          aria-label={`${cook.recipeName} is cooked`}
          title={
            cook.done
              ? "Cooked: its ingredients were taken out of the pantry. Uncheck to put them back."
              : "Mark as cooked, taking its ingredients out of the pantry"
          }
          onChange={(event) => {
            onDone(cook.id, event.target.checked);
          }}
        />
        <div className="flex min-w-0 grow flex-col gap-0.5">
          <span className="text-sm">
            <Link
              to={`/recipes/${cook.recipeId}`}
              className={`font-medium text-gray-900 hover:underline ${cook.done ? "line-through decoration-gray-400" : ""}`}
            >
              {cook.recipeName}
            </Link>
            <span className="text-gray-600 tabular-nums">
              {" "}
              · {formatServings(cook.servings)}
              {meal === "" ? "" : ` · for ${meal.toLowerCase()}`}
            </span>
          </span>
          {cook.notes !== "" && (
            <span className="text-xs text-gray-500 italic">{cook.notes}</span>
          )}
          {eaters.length > 0 && (
            <ul className="flex flex-col text-xs text-gray-600">
              {eaters.map((eater) => (
                <li
                  key={`${eater.personName}|${eater.date}|${eater.eatingTimeName}`}
                >
                  {eater.personName} · {formatWeekday(eater.date)}{" "}
                  {formatShortDate(eater.date)} {eater.eatingTimeName} ·{" "}
                  {formatServings(eater.servings)}
                  {eater.date > cook.date ? " (leftover)" : ""}
                </li>
              ))}
            </ul>
          )}
          {planned.leftServings > 0 && (
            <span className="text-xs text-gray-600">
              {formatServings(planned.leftServings)} left over, not planned yet:{" "}
              <Link
                to={`/plan?week=${startOfWeek(cook.date)}&day=${cook.date}`}
                className={linkClass}
              >
                plan them
              </Link>
            </span>
          )}
          {prepTasks.map((task) => (
            <span
              key={`${task.cookId}|${task.date}`}
              className="flex items-center gap-1 text-xs text-sky-900"
            >
              <ClockIcon className="size-3.5" aria-hidden="true" />
              {formatWeekday(task.date)}: {task.note}
            </span>
          ))}
        </div>
        <button
          type="button"
          className={iconButton}
          aria-label={`Change ${cook.recipeName}`}
          title="Change its date, servings or notes"
          disabled={disabled}
          onClick={() => {
            setEditing(!editing);
          }}
        >
          <PencilSquareIcon className="size-5" />
        </button>
        <ConfirmButton
          className={iconButton}
          title={`Delete ${cook.recipeName}?`}
          message="The cook is deleted with its portions, from every day it's eaten."
          confirmLabel="Delete"
          disabled={disabled}
          onConfirm={() => {
            onDelete(cook.id);
          }}
        >
          <TrashIcon
            className="size-5"
            aria-label={`Delete ${cook.recipeName}`}
          />
        </ConfirmButton>
      </div>
      {editing && (
        <form
          className="ml-6 flex flex-wrap items-end gap-2 rounded-md bg-gray-50 p-2"
          onSubmit={(event) => {
            event.preventDefault();
            const count = Number(servings);
            if (date === "" || !(count > 0)) {
              setInvalid("Give a date and servings more than 0.");
              return;
            }
            setInvalid("");
            onSave(cook.id, { date, notes, servings: count });
            setEditing(false);
          }}
        >
          <label className="flex flex-col gap-1">
            <span className={labelClass}>Cook on</span>
            <input
              type="date"
              className={inputClass}
              value={date}
              onChange={(event) => {
                setDate(event.target.value);
              }}
            />
          </label>
          <label className="flex w-24 flex-col gap-1">
            <span className={labelClass}>Servings</span>
            <input
              className={inputClass}
              inputMode="decimal"
              value={servings}
              onChange={(event) => {
                setServings(event.target.value);
              }}
            />
          </label>
          <label className="flex min-w-48 grow flex-col gap-1">
            <span className={labelClass}>Notes</span>
            <input
              className={inputClass}
              value={notes}
              onChange={(event) => {
                setNotes(event.target.value);
              }}
            />
          </label>
          <button type="submit" className={primaryButton} disabled={disabled}>
            Save
          </button>
          {invalid !== "" && (
            <p className="w-full text-sm text-red-700">{invalid}</p>
          )}
        </form>
      )}
      {planned.warnings.length > 0 && (
        <div className="ml-6">
          <WarningList warnings={planned.warnings} />
        </div>
      )}
    </li>
  );
};

interface PlanCookDialogProps {
  onClose: () => void;
}

/**
 * PlanCookDialog plans a cook of a recipe on a date, without portions
 * yet: like a batch cooked ahead, to eat as leftovers later.
 */
const PlanCookDialog: React.FC<PlanCookDialogProps> = ({ onClose }) => {
  const queryClient = useQueryClient();
  const [recipe, setRecipe] = useState<PickedRecipe | null>(null);
  const [date, setDate] = useState(today());
  const [servings, setServings] = useState("4");
  const [notes, setNotes] = useState("");
  const [invalid, setInvalid] = useState("");
  const create = useMutation(createCook, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService, SuggestionService]);
      onClose();
    },
  });
  return (
    <Modal open title="Plan a cook" onClose={onClose}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const count = Number(servings);
          if (recipe === null || date === "" || !(count > 0)) {
            setInvalid("Choose a recipe, a date and servings more than 0.");
            return;
          }
          setInvalid("");
          create.mutate({
            cook: { date, notes, recipeId: recipe.id, servings: count },
          });
        }}
      >
        <div className="flex flex-col gap-1">
          <span className={labelClass}>Recipe</span>
          <RecipePicker value={recipe} label="Recipe" onChange={setRecipe} />
        </div>
        <div className="flex gap-2">
          <label className="flex grow flex-col gap-1">
            <span className={labelClass}>Cook on</span>
            <input
              type="date"
              className={inputClass}
              value={date}
              onChange={(event) => {
                setDate(event.target.value);
              }}
            />
          </label>
          <label className="flex w-28 flex-col gap-1">
            <span className={labelClass}>Servings</span>
            <input
              className={inputClass}
              inputMode="decimal"
              value={servings}
              onChange={(event) => {
                setServings(event.target.value);
              }}
            />
          </label>
        </div>
        <label className="flex flex-col gap-1">
          <span className={labelClass}>Notes</span>
          <input
            className={inputClass}
            value={notes}
            onChange={(event) => {
              setNotes(event.target.value);
            }}
          />
        </label>
        <p className="text-sm text-gray-600">
          Its servings are left over to plan: add them to meals from the plan,
          as leftovers of the cook.
        </p>
        {invalid !== "" && <p className="text-sm text-red-700">{invalid}</p>}
        <ErrorText error={create.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={create.isPending}
          >
            Plan it
          </button>
        </div>
      </form>
    </Modal>
  );
};

/**
 * CooksPage lists the cooks of the next two weeks, by day: who eats each,
 * what's left over and what to start ahead, to mark them cooked, move,
 * resize or delete them, or plan a new one.
 */
const CooksPage: React.FC = () => {
  const queryClient = useQueryClient();
  const from = today();
  const to = addDays(from, DAYS - 1);
  const monday = startOfWeek(from);
  // The weeks the days span: at most three.
  const first = useQuery(getWeek, { monday });
  const second = useQuery(getWeek, { monday: addDays(monday, 7) });
  const third = useQuery(getWeek, { monday: addDays(monday, 14) });
  const [planning, setPlanning] = useState(false);
  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [
      PlanService,
      PantryService,
      SuggestionService,
    ]);
  };
  const update = useMutation(updateCook, { onSuccess });
  const remove = useMutation(deleteCook, { onSuccess });
  const disabled = update.isPending || remove.isPending;

  const queries = [first, second, third];
  let content: React.ReactNode = null;
  const failed = queries.find((query) => query.isError);
  if (failed !== undefined) {
    content = (
      <QueryError
        error={failed.error}
        onRetry={() => {
          for (const query of queries) {
            void query.refetch();
          }
        }}
      />
    );
  } else if (queries.some((query) => query.isPending)) {
    content = <Loading />;
  } else {
    const cooks = upcomingCooks(
      queries.map((query) => query.data),
      from,
      to
    );
    const byDate = new Map<string, Upcoming[]>();
    for (const upcoming of cooks) {
      const date = upcoming.planned.cook?.date ?? "";
      byDate.set(date, [...(byDate.get(date) ?? []), upcoming]);
    }
    content =
      cooks.length === 0 ? (
        <EmptyState title="Nothing to cook in the next two weeks">
          Plan meals on the{" "}
          <Link to="/plan" className={linkClass}>
            plan
          </Link>
          , or plan a cook here, like a batch to eat as leftovers.
        </EmptyState>
      ) : (
        <div className="flex flex-col gap-4">
          {[...byDate.entries()].map(([date, dayCooks]) => (
            <Card key={date} title={formatDate(date)}>
              <ul className="-my-3 divide-y divide-gray-100">
                {dayCooks.map((upcoming) => (
                  <CookCard
                    // A changed cook shows its new values.
                    key={`${upcoming.planned.cook?.id ?? ""}|${upcoming.planned.cook?.date ?? ""}|${upcoming.planned.cook?.servings ?? 0}|${upcoming.planned.cook?.notes ?? ""}`}
                    upcoming={upcoming}
                    disabled={disabled}
                    onSave={(id, change) => {
                      update.mutate({
                        cook: { id, ...change },
                        updateMask: { paths: ["date", "servings", "notes"] },
                      });
                    }}
                    onDone={(id, done) => {
                      update.mutate({
                        cook: { done, id },
                        updateMask: { paths: ["done"] },
                      });
                    }}
                    onDelete={(id) => {
                      remove.mutate({ id });
                    }}
                  />
                ))}
              </ul>
            </Card>
          ))}
        </div>
      );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Cooks"
        subtitle="What to cook in the next two weeks, who eats it, and what to start ahead."
        actions={
          <button
            type="button"
            className={primaryButton}
            onClick={() => {
              setPlanning(true);
            }}
          >
            <PlusIcon className="size-4" aria-hidden="true" />
            Plan a cook
          </button>
        }
      />
      <ErrorText error={update.error ?? remove.error} />
      {content}
      {planning && (
        <PlanCookDialog
          onClose={() => {
            setPlanning(false);
          }}
        />
      )}
    </div>
  );
};

export default CooksPage;
