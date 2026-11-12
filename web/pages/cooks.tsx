import Loading from "@components/loading";
import { WarningList } from "@components/plan/plan-bits";
import type { PickedRecipe } from "@components/recipes/recipe-picker";
import RecipePicker from "@components/recipes/recipe-picker";
import { ConfirmDialog } from "@components/ui/confirm-button";
import { EmptyState, PageHeader } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import MoreMenu from "@components/ui/more-menu";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  inputClass,
  labelClass,
  linkClass,
  primaryButton,
  secondaryButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { CheckIcon, ClockIcon, PlusIcon } from "@heroicons/react/24/outline";
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
import { addDays, parseDate, startOfWeek, today } from "helpers/dates";
import { formatWeekday } from "helpers/format";
import { formatServings, householdMealLabel } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

// DAYS is how many days of cooks the page shows, from today.
const DAYS = 14;

// DayLabel names a day of cooks, e.g. "Today", "Tomorrow" or "Sat 14 Nov".
const dayLabel = (date: string, now: string): string => {
  if (date === now) {
    return "Today";
  }
  if (date === addDays(now, 1)) {
    return "Tomorrow";
  }
  return parseDate(date).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    weekday: "long",
  });
};

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

interface CookChange {
  date: string;
  servings: number;
  notes: string;
}

interface ChangeCookDialogProps {
  planned: PlannedCook;
  disabled: boolean;
  onSave: (change: CookChange) => void;
  onClose: () => void;
}

// ChangeCookDialog changes a cook's date, servings or notes.
const ChangeCookDialog: React.FC<ChangeCookDialogProps> = ({
  planned,
  disabled,
  onSave,
  onClose,
}) => {
  const { cook } = planned;
  const [date, setDate] = useState(cook?.date ?? "");
  const [servings, setServings] = useState(String(cook?.servings ?? 1));
  const [notes, setNotes] = useState(cook?.notes ?? "");
  const [invalid, setInvalid] = useState("");
  return (
    <Modal open title={cook?.recipeName ?? "Cook"} onClose={onClose}>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const count = Number(servings);
          if (date === "" || !(count > 0)) {
            setInvalid("Give a date and servings more than 0.");
            return;
          }
          setInvalid("");
          onSave({ date, notes, servings: count });
          onClose();
        }}
      >
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
        {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={primaryButton} disabled={disabled}>
            Save
          </button>
        </div>
      </form>
    </Modal>
  );
};

/**
 * EatersOf says who eats a cook's servings and when, in short, like
 * "Anna Mon lunch", with the days after the cook as leftovers.
 */
const eatersOf = (cookDate: string, eaters: Eater[]): string[] =>
  eaters.map(
    (eater) =>
      `${eater.personName} ${eater.date === cookDate ? "" : `${formatWeekday(eater.date)} `}${eater.eatingTimeName.toLowerCase()}`
  );

interface CookRowProps {
  upcoming: Upcoming;
  disabled: boolean;
  onSave: (id: string, change: CookChange) => void;
  onDone: (id: string, done: boolean) => void;
  onDelete: (id: string) => void;
}

/**
 * CookRow shows a cook to come: a tick to mark it cooked, which takes its
 * ingredients out of the pantry, what it is and who eats it, with what to
 * start ahead, and a ⋯ menu to change or delete it.
 */
const CookRow: React.FC<CookRowProps> = ({
  upcoming,
  disabled,
  onSave,
  onDone,
  onDelete,
}) => {
  const { planned, eaters, prepTasks } = upcoming;
  const { cook } = planned;
  const [changing, setChanging] = useState(false);
  const [deleting, setDeleting] = useState(false);
  if (cook === undefined) {
    return null;
  }
  const meal = householdMealLabel(cook.meal);
  const who = eatersOf(cook.date, eaters);
  return (
    <li className="flex items-start gap-3 py-3.5">
      <button
        type="button"
        className={`mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-full border-2 transition-colors disabled:opacity-50 ${cook.done ? "border-cook bg-cook text-white" : "border-cook/50 text-transparent hover:border-cook"}`}
        aria-pressed={cook.done}
        aria-label={`${cook.recipeName} is cooked`}
        title={
          cook.done
            ? "Cooked: its ingredients were taken out of the pantry. Tap to put them back."
            : "Mark as cooked, taking its ingredients out of the pantry"
        }
        disabled={disabled}
        onClick={() => {
          onDone(cook.id, !cook.done);
        }}
      >
        <CheckIcon className="size-4" strokeWidth={3} aria-hidden="true" />
      </button>
      <div className="flex min-w-0 grow flex-col gap-1">
        <Link
          to={`/recipes/${cook.recipeId}`}
          className={`leading-snug font-bold ${cook.done ? "text-muted line-through decoration-line" : "text-ink-soft"}`}
        >
          {cook.recipeName}
        </Link>
        <p className="text-sm text-muted tabular-nums">
          {[
            formatServings(cook.servings),
            meal === "" ? "" : `for ${meal.toLowerCase()}`,
          ]
            .filter((part) => part !== "")
            .join(" · ")}
          {who.length > 0 && ` · ${who.join(", ")}`}
        </p>
        {cook.notes !== "" && (
          <p className="text-sm text-muted italic">{cook.notes}</p>
        )}
        {(prepTasks.length > 0 || planned.leftServings > 0) && (
          <div className="mt-1 flex flex-wrap gap-1.5">
            {prepTasks.map((task) => (
              <span
                key={`${task.cookId}|${task.date}`}
                className="inline-flex items-center gap-1 rounded-full bg-cook-tint px-2.5 py-1 text-xs font-medium text-cook-dark"
              >
                <ClockIcon className="size-3.5" aria-hidden="true" />
                {formatWeekday(task.date)}: {task.note}
              </span>
            ))}
            {planned.leftServings > 0 && (
              <Link
                to={`/plan?week=${startOfWeek(cook.date)}&day=${cook.date}`}
                className="inline-flex items-center rounded-full bg-ground-dark px-2.5 py-1 text-xs font-medium text-ink-soft"
              >
                {formatServings(planned.leftServings)} left over · Plan them ›
              </Link>
            )}
          </div>
        )}
        {planned.warnings.length > 0 && (
          <div className="mt-1">
            <WarningList warnings={planned.warnings} />
          </div>
        )}
      </div>
      <MoreMenu
        label={`More for ${cook.recipeName}`}
        className="-mt-1.5 -mr-2 flex size-10 items-center justify-center rounded-full text-muted hover:bg-ground-dark/60"
        items={[
          {
            disabled,
            label: "Change date, servings or notes",
            onClick: () => {
              setChanging(true);
            },
          },
          {
            danger: true,
            disabled,
            label: "Delete",
            onClick: () => {
              setDeleting(true);
            },
          },
        ]}
      />
      {changing && (
        <ChangeCookDialog
          planned={planned}
          disabled={disabled}
          onSave={(change) => {
            onSave(cook.id, change);
          }}
          onClose={() => {
            setChanging(false);
          }}
        />
      )}
      <ConfirmDialog
        open={deleting}
        title={`Delete ${cook.recipeName}?`}
        message="The cook is deleted with its portions, from every day it's eaten."
        onClose={() => {
          setDeleting(false);
        }}
        onConfirm={() => {
          onDelete(cook.id);
        }}
      />
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
        <p className="text-sm text-muted">
          Its servings are left over to plan: add them to meals from the plan,
          as leftovers of the cook.
        </p>
        {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
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
        <div className="flex flex-col gap-5">
          {[...byDate.entries()].map(([date, dayCooks]) => (
            <section key={date} className="flex flex-col gap-2">
              <h2 className={sectionLabelClass}>{dayLabel(date, from)}</h2>
              <ul className="divide-y divide-line rounded-2xl border border-line bg-white px-4">
                {dayCooks.map((upcoming) => (
                  <CookRow
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
            </section>
          ))}
        </div>
      );
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        kicker="The next two weeks"
        title="Cooks"
        subtitle="Tick a cook when it's done, which takes its ingredients out of the pantry."
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
