import FreezeDialog from "@components/plan/freeze-dialog";
import {
  DayTotals,
  EatingTimeBlock,
  WarningList,
} from "@components/plan/plan-bits";
import type { Slot } from "@components/plan/portion-dialog";
import ConfirmButton from "@components/ui/confirm-button";
import { ErrorText } from "@components/ui/query-error";
import { cardClass, iconButton } from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import {
  ArchiveBoxArrowDownIcon,
  ClockIcon,
  TrashIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import type { EatingTime, Person } from "gen/people/v1/people_pb";
import { setCookSupplier } from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService } from "gen/plans/v1/plans_pb";
import type {
  CookSupply,
  PlanDay,
  PlannedCook,
  PlannedPortion,
  Staple,
} from "gen/plans/v1/plans_pb";
import { formatDate, formatDollars, formatShortDate } from "helpers/format";
import { formatServings, householdMealLabel } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

interface CookRowProps {
  planned: PlannedCook;
  // The week's cooks, which can supply its components.
  weekCooks: PlannedCook[];
  // What its ingredients cost, in cents, undefined if unknown.
  costCents?: number | undefined;
  disabled: boolean;
  onDone: (done: boolean) => void;
  onDelete: () => void;
}

// CookRow shows a cook of the day, to mark done or delete.
const CookRow: React.FC<CookRowProps> = ({
  planned,
  weekCooks,
  costCents,
  disabled,
  onDone,
  onDelete,
}) => {
  const [freezing, setFreezing] = useState(false);
  const { cook } = planned;
  if (cook === undefined) {
    return null;
  }
  const meal = householdMealLabel(cook.meal);
  const frozen = cook.frozenServings.toLocaleString(undefined, {
    maximumFractionDigits: 2,
  });
  return (
    <li className="flex flex-col gap-1 rounded-md bg-brand-50 px-2.5 py-1.5">
      <div className="flex items-center gap-2 text-sm">
        <input
          type="checkbox"
          className="size-4 shrink-0 rounded border-gray-300 accent-brand-700 print:hidden"
          checked={cook.done}
          disabled={disabled}
          aria-label={`${cook.recipeName} is cooked`}
          title={
            cook.done
              ? "Cooked: its ingredients were taken out of the pantry. Uncheck to put them back."
              : "Mark as cooked, taking its ingredients out of the pantry"
          }
          onChange={(event) => {
            onDone(event.target.checked);
          }}
        />
        <span className="min-w-0 grow">
          <Link
            to={`/recipes/${cook.recipeId}`}
            className={`font-medium text-gray-900 hover:underline ${cook.done ? "line-through decoration-gray-400" : ""}`}
          >
            {cook.recipeName}
          </Link>
          <span className="text-xs text-gray-600 tabular-nums">
            {" "}
            · {meal === "" ? "" : `${meal} · `}
            {formatServings(cook.servings)}
            {cook.frozenServings > 0 && ` · ${frozen} frozen`}
            {costCents !== undefined &&
              costCents > 0 &&
              ` · ≈ ${formatDollars(costCents)}`}
          </span>
        </span>
        <button
          type="button"
          className={`${iconButton} print:hidden`}
          aria-label={`Freeze ${cook.recipeName}`}
          title="Freeze leftovers, to keep them longer"
          disabled={disabled}
          onClick={() => {
            setFreezing(true);
          }}
        >
          <ArchiveBoxArrowDownIcon className="size-4" />
        </button>
        <ConfirmButton
          className={`${iconButton} print:hidden`}
          title={`Delete ${cook.recipeName}?`}
          message="The cook is deleted with all its portions, leftovers on other days too."
          disabled={disabled}
          onConfirm={onDelete}
        >
          <TrashIcon
            className="size-4"
            aria-label={`Delete ${cook.recipeName}`}
          />
        </ConfirmButton>
      </div>
      {cook.supplying.length > 0 && (
        <p className="text-xs text-gray-600">
          For{" "}
          {cook.supplying
            .map(
              (supply) =>
                `${supply.consumerRecipeName} on ${formatShortDate(supply.consumerDate)}`
            )
            .join(", ")}
        </p>
      )}
      {cook.supplies.map((supply) => (
        <SupplyPicker
          key={supply.componentRecipeId}
          cookId={cook.id}
          supply={supply}
          weekCooks={weekCooks}
          disabled={disabled}
        />
      ))}
      <WarningList warnings={planned.warnings} />
      {freezing && (
        <FreezeDialog
          planned={planned}
          onClose={() => {
            setFreezing(false);
          }}
        />
      )}
    </li>
  );
};

interface SupplyPickerProps {
  cookId: string;
  supply: CookSupply;
  weekCooks: PlannedCook[];
  disabled: boolean;
}

/**
 * SupplyPicker says which cook makes a component of a cook, e.g. "Cooked
 * chickpeas from Fri 6 Nov", to choose another of the week's cooks of it,
 * like one pot for two meals.
 */
const SupplyPicker: React.FC<SupplyPickerProps> = ({
  cookId,
  supply,
  weekCooks,
  disabled,
}) => {
  const queryClient = useQueryClient();
  const set = useMutation(setCookSupplier, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
    },
  });
  const options = weekCooks.filter(
    (other) =>
      other.cook !== undefined &&
      other.cook.recipeId === supply.componentRecipeId
  );
  return (
    <div className="flex flex-col gap-0.5">
      <label className="flex flex-wrap items-center gap-1.5 text-xs text-gray-600">
        <span>{supply.supplierRecipeName} from</span>
        <select
          className="rounded border border-gray-300 bg-white px-1 py-0.5 text-xs text-gray-800"
          value={supply.supplierCookId}
          disabled={disabled || set.isPending || options.length < 2}
          title="The cook that makes it: choose another to share one pot between meals"
          onChange={(event) => {
            set.mutate({
              componentRecipeId: supply.componentRecipeId,
              cookId,
              supplierCookId: event.target.value,
            });
          }}
        >
          {!options.some(
            (other) => other.cook?.id === supply.supplierCookId
          ) && (
            <option value={supply.supplierCookId}>
              {formatShortDate(supply.supplierDate)}
            </option>
          )}
          {options.map((other) => (
            <option key={other.cook?.id} value={other.cook?.id}>
              {formatShortDate(other.cook?.date ?? "")}
              {other.cook?.id === supply.supplierCookId ? "" : " (share)"}
            </option>
          ))}
        </select>
      </label>
      <ErrorText error={set.error} />
    </div>
  );
};

/**
 * PEOPLE_COLUMNS are the classes of the people's columns on large screens,
 * by how many people there are: side by side, up to four in a row.
 */
const PEOPLE_COLUMNS = [
  "",
  "lg:grid-cols-1",
  "lg:grid-cols-2",
  "lg:grid-cols-3",
  "lg:grid-cols-4",
];

interface DayCardProps {
  day: PlanDay;
  // The cooks cooked on the day.
  cooks: PlannedCook[];
  // The week's cooks, which can supply the day's cooks' components.
  weekCooks: PlannedCook[];
  people: Person[];
  // Whether a mutation is running, which disables the buttons.
  disabled: boolean;
  // Whether it's shown on phones, which show one day at a time.
  shownOnPhones: boolean;
  onAdd: (slot: Slot) => void;
  // Suggests recipes for an empty eating time.
  onSuggest: (slot: Slot, eatingTime: EatingTime | undefined) => void;
  onEdit: (slot: Slot, portion: PlannedPortion) => void;
  onDelete: (portion: PlannedPortion) => void;
  onSkip: (portion: PlannedPortion, date: string) => void;
  // What each cook costs, in cents, by cook ID.
  costs: Map<string, number>;
  // The household's staples, to show those skipped on the day.
  staples: Staple[];
  onUnskip: (staple: Staple, date: string) => void;
  onCookDone: (planned: PlannedCook, done: boolean) => void;
  onCookDelete: (planned: PlannedCook) => void;
}

/**
 * DayCard shows a day of the plan: its cooks, then each person's eating
 * times side by side, with what they eat, their totals against their
 * targets, and the warnings.
 */
const DayCard: React.FC<DayCardProps> = ({
  day,
  cooks,
  weekCooks,
  people,
  disabled,
  shownOnPhones,
  onAdd,
  onSuggest,
  onEdit,
  onDelete,
  onSkip,
  staples,
  onUnskip,
  costs,
  onCookDone,
  onCookDelete,
}) => (
  <section
    className={`${cardClass} ${shownOnPhones ? "" : "hidden lg:block"} break-inside-avoid`}
    aria-label={formatDate(day.date)}
  >
    <div className="flex flex-col gap-2 border-b border-gray-100 px-3 py-2.5 sm:px-4">
      <h2 className="text-base font-semibold text-gray-900">
        {formatDate(day.date)}
      </h2>
      {day.prepTasks.length > 0 && (
        <ul className="flex flex-col gap-1">
          {day.prepTasks.map((task) => (
            <li
              key={task.cookId}
              className="flex items-start gap-1.5 rounded-md border border-sky-200 bg-sky-50 px-2.5 py-1.5 text-sm text-sky-950"
            >
              <ClockIcon
                className="mt-0.5 size-4 shrink-0 text-sky-700"
                aria-label="Start ahead"
              />
              <span>
                <span className="font-medium">{task.note}</span>
                <span className="text-sky-800">
                  {" "}
                  · for{" "}
                  {task.forRecipeNames.length > 0
                    ? task.forRecipeNames.join(", ")
                    : task.recipeName}{" "}
                  on {formatShortDate(task.cookDate)}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      {cooks.length > 0 && (
        <ul className="flex flex-col gap-1.5 lg:flex-row lg:flex-wrap">
          {cooks.map((planned) => (
            <CookRow
              key={planned.cook?.id}
              planned={planned}
              weekCooks={weekCooks}
              costCents={costs.get(planned.cook?.id ?? "")}
              disabled={disabled}
              onDone={(done) => {
                onCookDone(planned, done);
              }}
              onDelete={() => {
                onCookDelete(planned);
              }}
            />
          ))}
        </ul>
      )}
      {cooks.some((planned) => planned.cook?.done === false) && (
        <p className="text-xs text-gray-500 print:hidden">
          Tick a cook once it&apos;s cooked: its ingredients come out of the
          pantry, and go back if you untick it.
        </p>
      )}
    </div>
    <div
      className={`grid grid-cols-1 gap-4 p-3 sm:p-4 ${PEOPLE_COLUMNS[Math.min(day.people.length, PEOPLE_COLUMNS.length - 1)] ?? ""}`}
    >
      {day.people.map((personDay) => {
        const person = people.find((other) => other.id === personDay.personId);
        return (
          <div key={personDay.personId} className="flex min-w-0 flex-col gap-2">
            <h3 className="text-sm font-semibold text-gray-900">
              {personDay.personName}
            </h3>
            {personDay.eatingTimes.length === 0 ? (
              <p className="text-sm text-gray-500">
                No eating times yet:{" "}
                <Link
                  to="/people"
                  className="font-medium text-brand-600 hover:underline"
                >
                  add them
                </Link>
                .
              </p>
            ) : (
              <ol className="flex flex-col gap-1.5">
                {personDay.eatingTimes.map((planned) => {
                  const slot = {
                    date: day.date,
                    eatingTimeId: planned.eatingTime?.id ?? "",
                    personId: personDay.personId,
                  };
                  return (
                    <EatingTimeBlock
                      key={slot.eatingTimeId}
                      planned={planned}
                      disabled={disabled}
                      onAdd={() => {
                        onAdd(slot);
                      }}
                      onSuggest={() => {
                        onSuggest(slot, planned.eatingTime);
                      }}
                      onEdit={(portion) => {
                        onEdit(slot, portion);
                      }}
                      onDelete={onDelete}
                      onSkip={(portion) => {
                        onSkip(portion, day.date);
                      }}
                      skipped={staples.filter(
                        (staple) =>
                          staple.personId === slot.personId &&
                          staple.eatingTimeId === slot.eatingTimeId &&
                          staple.skippedDates.includes(day.date)
                      )}
                      onUnskip={(staple) => {
                        onUnskip(staple, day.date);
                      }}
                    />
                  );
                })}
              </ol>
            )}
            <DayTotals day={personDay} person={person} />
          </div>
        );
      })}
    </div>
  </section>
);

export default DayCard;
