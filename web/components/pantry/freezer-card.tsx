import Loading from "@components/loading";
import Card from "@components/ui/card";
import ConfirmButton from "@components/ui/confirm-button";
import { TextField } from "@components/ui/fields";
import { Badge } from "@components/ui/misc";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  ghostButton,
  inputClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import {
  CalendarDaysIcon,
  MinusCircleIcon,
  TrashIcon,
} from "@heroicons/react/24/outline";
import { useQueryClient } from "@tanstack/react-query";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import {
  createPortion,
  listFrozenLeftovers,
  takeFromFreezer,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { FrozenLeftover } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { addDays, maxDate, today } from "helpers/dates";
import { formatShortDate } from "helpers/format";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

// FormatAmount formats servings without a unit, e.g. "2.5".
const formatAmount = (servings: number): string =>
  servings.toLocaleString(undefined, { maximumFractionDigits: 2 });

interface PlanFrozenDialogProps {
  leftover: FrozenLeftover;
  onClose: () => void;
}

/**
 * PlanFrozenDialog plans a portion of a frozen leftover for someone, at an
 * eating time on a later date.
 */
const PlanFrozenDialog: React.FC<PlanFrozenDialogProps> = ({
  leftover,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const people = useQuery(listPeople, {});
  const { cook } = leftover;
  const [date, setDate] = useState(
    maxDate(
      addDays(today(), 1),
      leftover.frozenFrom === "" ? today() : leftover.frozenFrom
    )
  );
  const [personId, setPersonId] = useState("");
  const [eatingTimeId, setEatingTimeId] = useState("");
  const [servings, setServings] = useState(
    formatAmount(Math.min(1, leftover.remainingServings))
  );
  const [invalid, setInvalid] = useState("");
  const create = useMutation(createPortion, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
      onClose();
    },
  });
  const everyone = people.data?.people ?? [];
  const person = everyone.find((other) => other.id === personId) ?? everyone[0];
  const eatingTime =
    person?.eatingTimes.find((et) => et.id === eatingTimeId) ??
    person?.eatingTimes.find((et) => et.name === "Dinner") ??
    person?.eatingTimes[0];
  if (cook === undefined) {
    return null;
  }
  return (
    <Modal open title={`Plan ${cook.recipeName}`} onClose={onClose}>
      {people.isPending ? (
        <Loading />
      ) : (
        <form
          className="flex flex-col gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            setInvalid("");
            const value = parseOptionalNumber(servings);
            if (value === null || value === undefined || value <= 0) {
              setInvalid("Give the servings, more than 0.");
              return;
            }
            if (person === undefined || eatingTime === undefined) {
              setInvalid("Choose who eats it, and when.");
              return;
            }
            create.mutate({
              portion: {
                date,
                eatingTimeId: eatingTime.id,
                personId: person.id,
                what: {
                  case: "cook",
                  value: { cookId: cook.id, servings: value },
                },
              },
            });
          }}
        >
          <p className="text-sm text-gray-600">
            {formatAmount(leftover.remainingServings)} of the servings cooked on{" "}
            {formatShortDate(cook.date)} are in the freezer
            {leftover.keepsUntil === ""
              ? "."
              : `, keeping until ${formatShortDate(leftover.keepsUntil)}.`}
          </p>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="flex flex-col gap-1">
              <span className={labelClass}>Date</span>
              <input
                type="date"
                className={inputClass}
                value={date}
                required
                onChange={(event) => {
                  if (event.target.value !== "") {
                    setDate(event.target.value);
                  }
                }}
              />
            </label>
            <TextField
              label="Servings"
              value={servings}
              inputMode="decimal"
              onChange={setServings}
            />
            <label className="flex flex-col gap-1">
              <span className={labelClass}>Who</span>
              <select
                className={inputClass}
                value={person?.id ?? ""}
                onChange={(event) => {
                  setPersonId(event.target.value);
                  setEatingTimeId("");
                }}
              >
                {everyone.map((other) => (
                  <option key={other.id} value={other.id}>
                    {other.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1">
              <span className={labelClass}>When</span>
              <select
                className={inputClass}
                value={eatingTime?.id ?? ""}
                onChange={(event) => {
                  setEatingTimeId(event.target.value);
                }}
              >
                {person?.eatingTimes.map((et) => (
                  <option key={et.id} value={et.id}>
                    {et.name}
                  </option>
                ))}
              </select>
            </label>
          </div>
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
              {create.isPending ? "Planning…" : "Plan it"}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
};

/**
 * FreezerCard shows the frozen leftovers not planned yet, with how long
 * they keep, to plan them for a later meal or throw them out.
 */
const FreezerCard: React.FC = () => {
  const queryClient = useQueryClient();
  const leftovers = useQuery(listFrozenLeftovers, {});
  const [planning, setPlanning] = useState<FrozenLeftover | null>(null);
  // Servings are taken off plan: thrown out, or eaten without a plan.
  const discard = useMutation(takeFromFreezer, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
    },
  });

  let body: React.ReactNode = null;
  if (leftovers.isPending) {
    body = <Loading />;
  } else if (leftovers.isError) {
    body = (
      <QueryError
        error={leftovers.error}
        onRetry={() => {
          void leftovers.refetch();
        }}
      />
    );
  } else if (leftovers.data.leftovers.length === 0) {
    body = (
      <p className="text-sm text-gray-600">
        Nothing frozen. Freeze a cook&apos;s leftovers from the{" "}
        <Link to="/plan" className="font-medium text-brand-700 hover:underline">
          plan
        </Link>
        , to keep them longer than in the fridge.
      </p>
    );
  } else {
    body = (
      <ul className="flex flex-col divide-y divide-gray-100">
        {leftovers.data.leftovers.map((leftover) => {
          const { cook } = leftover;
          if (cook === undefined) {
            return null;
          }
          return (
            <li
              key={cook.id}
              className="flex flex-col gap-1 py-2 first:pt-0 last:pb-0"
            >
              <div className="flex items-baseline justify-between gap-2">
                <Link
                  to={`/recipes/${cook.recipeId}`}
                  className="text-sm font-medium text-gray-900 hover:underline"
                >
                  {cook.recipeName}
                </Link>
                <span className="shrink-0 text-sm text-gray-700 tabular-nums">
                  {formatAmount(leftover.remainingServings)}{" "}
                  {leftover.remainingServings === 1 ? "serving" : "servings"}
                </span>
              </div>
              <p className="flex flex-wrap items-center gap-x-1.5 text-xs text-gray-600">
                <span>Cooked {formatShortDate(cook.date)}</span>
                {leftover.keepsUntil !== "" && (
                  <span>
                    · keeps until {formatShortDate(leftover.keepsUntil)}
                  </span>
                )}
                {leftover.expired && (
                  <Badge className="bg-amber-50 text-amber-900 ring-amber-300">
                    Past its freezer days
                  </Badge>
                )}
              </p>
              <div className="flex gap-1">
                <button
                  type="button"
                  className={ghostButton}
                  onClick={() => {
                    setPlanning(leftover);
                  }}
                >
                  <CalendarDaysIcon className="size-4" aria-hidden="true" />
                  Plan
                </button>
                <button
                  type="button"
                  className={ghostButton}
                  title="Take a serving out of the freezer, eaten without a plan"
                  disabled={discard.isPending}
                  onClick={() => {
                    discard.mutate({
                      cookId: cook.id,
                      servings: Math.min(1, leftover.remainingServings),
                    });
                  }}
                >
                  <MinusCircleIcon className="size-4" aria-hidden="true" />
                  Used 1
                </button>
                <ConfirmButton
                  className={ghostButton}
                  title={`Throw out ${cook.recipeName}?`}
                  message="The frozen servings not planned yet are taken out of the freezer. Those already planned stay."
                  disabled={discard.isPending}
                  onConfirm={() => {
                    discard.mutate({
                      cookId: cook.id,
                      servings: leftover.remainingServings,
                    });
                  }}
                >
                  <TrashIcon className="size-4" aria-hidden="true" />
                  Throw out
                </ConfirmButton>
              </div>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <Card title="Freezer">
      <div className="flex flex-col gap-2">
        <ErrorText error={discard.error} />
        {body}
      </div>
      {planning !== null && (
        <PlanFrozenDialog
          leftover={planning}
          onClose={() => {
            setPlanning(null);
          }}
        />
      )}
    </Card>
  );
};

export default FreezerCard;
