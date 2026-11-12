import Loading from "@components/loading";
import { TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  inputClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { listPeople } from "gen/people/v1/people-PersonService_connectquery";
import { createPortion } from "gen/plans/v1/plans-PlanService_connectquery";
import type { FrozenLeftover } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { maxDate, today } from "helpers/dates";
import { formatShortDate } from "helpers/format";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

// FormatAmount formats servings without a unit, e.g. "2.5".
const formatAmount = (servings: number): string =>
  servings.toLocaleString(undefined, { maximumFractionDigits: 2 });

interface PlanFrozenDialogProps {
  leftover: FrozenLeftover;
  onClose: () => void;
}

/**
 * PlanFrozenDialog plans a portion of a frozen leftover for someone, at an
 * eating time on a date, thawed from the freezer.
 */
export const PlanFrozenDialog: React.FC<PlanFrozenDialogProps> = ({
  leftover,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const people = useQuery(listPeople, {});
  const { cook } = leftover;
  // It can be eaten from today, or from when it was cooked.
  const [date, setDate] = useState(maxDate(today(), cook?.date ?? today()));
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
          <p className="text-sm text-muted">
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
              {create.isPending ? "Planning…" : "Plan it"}
            </button>
          </div>
        </form>
      )}
    </Modal>
  );
};
