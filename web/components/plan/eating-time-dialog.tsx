import EatingTimeSuggestionsDialog from "@components/plan/eating-time-suggestions-dialog";
import PortionDialog from "@components/plan/portion-dialog";
import type { Slot } from "@components/plan/portion-dialog";
import { ErrorText } from "@components/ui/query-error";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import type { Person } from "gen/people/v1/people_pb";
import { setEatingOut } from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

interface EatingTimeDialogProps {
  // The person's eating time on a date to plan.
  slot: Slot;
  people: Person[];
  // The dates of the week shown.
  dates: string[];
  // The cooks whose leftovers can be eaten.
  cooks: PlannedCook[];
  onClose: () => void;
}

const actionClass =
  "text-sm font-bold text-pine hover:underline disabled:opacity-50";

/**
 * EatingTimeDialog plans one person's eating time, like a snack between
 * meals: a recipe to cook, leftovers or a food, chosen directly. Suggestions
 * are only shown when asked for, as choosing is usually quicker.
 */
const EatingTimeDialog: React.FC<EatingTimeDialogProps> = ({
  slot,
  people,
  dates,
  cooks,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [suggesting, setSuggesting] = useState(false);
  const person = people.find((other) => other.id === slot.personId);
  const eatingTime = person?.eatingTimes.find(
    (other) => other.id === slot.eatingTimeId
  );
  const eatOut = useMutation(setEatingOut, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
      onClose();
    },
  });
  if (suggesting) {
    return (
      <EatingTimeSuggestionsDialog
        slot={slot}
        people={people}
        onClose={onClose}
      />
    );
  }
  return (
    <PortionDialog
      slot={slot}
      people={people}
      dates={dates}
      cooks={cooks}
      mealName={eatingTime?.name ?? "Meal"}
      title={`${person?.name ?? "Someone"}'s ${(eatingTime?.name ?? "meal").toLowerCase()}`}
      initialWhat="recipe"
      actions={
        <div className="-mt-1 flex flex-wrap gap-x-4 gap-y-1">
          <button
            type="button"
            className={actionClass}
            onClick={() => {
              setSuggesting(true);
            }}
          >
            Suggest for me
          </button>
          <button
            type="button"
            className={actionClass}
            disabled={eatOut.isPending}
            onClick={() => {
              eatOut.mutate({
                date: slot.date,
                eatingOut: true,
                eatingTimeId: slot.eatingTimeId,
                personId: slot.personId,
              });
            }}
          >
            {eatOut.isPending ? "Marking it…" : "Eating out"}
          </button>
          <ErrorText error={eatOut.error} />
        </div>
      }
      onClose={onClose}
    />
  );
};

export default EatingTimeDialog;
