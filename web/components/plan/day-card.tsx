import {
  DayTotals,
  EatingTimeBlock,
  WarningList,
} from "@components/plan/plan-bits";
import type { Slot } from "@components/plan/portion-dialog";
import ConfirmButton from "@components/ui/confirm-button";
import { cardClass, iconButton } from "@components/ui/styles";
import { TrashIcon } from "@heroicons/react/24/outline";
import type { Person } from "gen/people/v1/people_pb";
import type {
  PlanDay,
  PlannedCook,
  PlannedPortion,
} from "gen/plans/v1/plans_pb";
import { formatDate } from "helpers/format";
import { formatServings, householdMealLabel } from "helpers/labels";
import React from "react";
import { Link } from "react-router";

interface CookRowProps {
  planned: PlannedCook;
  disabled: boolean;
  onDone: (done: boolean) => void;
  onDelete: () => void;
}

// CookRow shows a cook of the day, to mark done or delete.
const CookRow: React.FC<CookRowProps> = ({
  planned,
  disabled,
  onDone,
  onDelete,
}) => {
  const { cook } = planned;
  if (cook === undefined) {
    return null;
  }
  const meal = householdMealLabel(cook.meal);
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
          </span>
        </span>
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
      <WarningList warnings={planned.warnings} />
    </li>
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
  people: Person[];
  // Whether a mutation is running, which disables the buttons.
  disabled: boolean;
  // Whether it's shown on phones, which show one day at a time.
  shownOnPhones: boolean;
  onAdd: (slot: Slot) => void;
  onEdit: (slot: Slot, portion: PlannedPortion) => void;
  onDelete: (portion: PlannedPortion) => void;
  onSkip: (portion: PlannedPortion, date: string) => void;
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
  people,
  disabled,
  shownOnPhones,
  onAdd,
  onEdit,
  onDelete,
  onSkip,
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
      {cooks.length > 0 && (
        <ul className="flex flex-col gap-1.5 lg:flex-row lg:flex-wrap">
          {cooks.map((planned) => (
            <CookRow
              key={planned.cook?.id}
              planned={planned}
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
                      onEdit={(portion) => {
                        onEdit(slot, portion);
                      }}
                      onDelete={onDelete}
                      onSkip={(portion) => {
                        onSkip(portion, day.date);
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
