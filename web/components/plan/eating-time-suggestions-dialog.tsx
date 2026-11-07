import type { Slot } from "@components/plan/portion-dialog";
import SuggestionList from "@components/plan/suggestion-list";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import { secondaryButton } from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import type { Person } from "gen/people/v1/people_pb";
import {
  createCook,
  createPortion,
} from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService } from "gen/plans/v1/plans_pb";
import type { Suggestion } from "gen/suggestions/v1/suggestions_pb";
import { formatDay } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

interface EatingTimeSuggestionsDialogProps {
  // The person's eating time on a date to suggest for.
  slot: Slot;
  people: Person[];
  onClose: () => void;
}

/**
 * EatingTimeSuggestionsDialog suggests recipes for one person's eating
 * time, like a small meal between main meals, and plans the chosen one: a
 * cook of the suggested servings, and the person's portion of it.
 */
const EatingTimeSuggestionsDialog: React.FC<
  EatingTimeSuggestionsDialogProps
> = ({ slot, people, onClose }) => {
  const queryClient = useQueryClient();
  const person = people.find((other) => other.id === slot.personId);
  const eatingTime = person?.eatingTimes.find(
    (other) => other.id === slot.eatingTimeId
  );
  // The recipe being planned, empty if none.
  const [planningId, setPlanningId] = useState("");
  const cook = useMutation(createCook);
  const portion = useMutation(createPortion);

  const plan = async (suggestion: Suggestion): Promise<void> => {
    setPlanningId(suggestion.recipeId);
    try {
      const created = await cook.mutateAsync({
        cook: {
          date: slot.date,
          recipeId: suggestion.recipeId,
          servings: suggestion.cookServings,
        },
      });
      // A suggestion for an eating time has one portion, at it.
      const [suggested] = suggestion.portions;
      await portion.mutateAsync({
        portion: {
          date: slot.date,
          eatingTimeId: slot.eatingTimeId,
          personId: slot.personId,
          what: {
            case: "cook",
            value: {
              cookId: created.cook?.id ?? "",
              servings: suggested?.servings ?? suggestion.cookServings,
            },
          },
        },
      });
      await invalidateServices(queryClient, [PlanService]);
      onClose();
    } catch {
      // The mutations show their errors.
    } finally {
      setPlanningId("");
    }
  };

  return (
    <Modal
      open
      onClose={onClose}
      title={`Suggestions for ${person?.name ?? "someone"}'s ${eatingTime?.name ?? "eating time"}`}
      wide
    >
      <div className="flex flex-col gap-3">
        <p className="text-sm text-gray-600">
          {formatDay(slot.date)}. Planning one cooks the recipe that day for
          this portion, within {person?.name ?? "their"}&apos;s maximum portion;
          the rest is left over to plan later.
        </p>
        <SuggestionList
          request={{
            date: slot.date,
            slot: {
              case: "eatingTime",
              value: {
                eatingTimeId: slot.eatingTimeId,
                personId: slot.personId,
              },
            },
          }}
          planningId={planningId}
          disabled={planningId !== ""}
          onPlan={(suggestion) => {
            void plan(suggestion);
          }}
        />
        <ErrorText error={cook.error ?? portion.error} />
        <div className="flex justify-end">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Close
          </button>
        </div>
      </div>
    </Modal>
  );
};

export default EatingTimeSuggestionsDialog;
