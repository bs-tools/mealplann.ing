import type { Slot } from "@components/plan/portion-dialog";
import SuggestionList from "@components/plan/suggestion-list";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import type { Person } from "gen/people/v1/people_pb";
import {
  createCook,
  createPortion,
  planMeals,
} from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService } from "gen/plans/v1/plans_pb";
import type { Suggestion } from "gen/suggestions/v1/suggestions_pb";
import { formatDay } from "helpers/format";
import { invalidateServices } from "helpers/query";
import { suggestionKey } from "helpers/suggestions";
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
 * cook of the suggested servings, and the person's portion of it, or for
 * the rest of an earlier meal, a portion of that meal's cook, which grows
 * by it.
 */
const EatingTimeSuggestionsDialog: React.FC<
  EatingTimeSuggestionsDialogProps
> = ({ slot, people, onClose }) => {
  const queryClient = useQueryClient();
  const person = people.find((other) => other.id === slot.personId);
  const eatingTime = person?.eatingTimes.find(
    (other) => other.id === slot.eatingTimeId
  );
  // What's being planned, as its suggestionKey, empty if nothing.
  const [planningId, setPlanningId] = useState("");
  const cook = useMutation(createCook);
  const portion = useMutation(createPortion);
  const leftover = useMutation(planMeals);

  const plan = async (suggestion: Suggestion): Promise<void> => {
    setPlanningId(suggestionKey(suggestion));
    try {
      if (suggestion.leftoverOf !== undefined) {
        await leftover.mutateAsync({
          meals: [
            {
              date: slot.date,
              eatingTimeId: slot.eatingTimeId,
              leftoverOf: suggestion.leftoverOf,
              personId: slot.personId,
              recipeId: suggestion.recipeId,
            },
          ],
        });
        await invalidateServices(queryClient, [PlanService]);
        onClose();
        return;
      }
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
      title={`${person?.name ?? "Someone"}'s ${(eatingTime?.name ?? "meal").toLowerCase()}`}
      wide
    >
      <div className="flex flex-col gap-4">
        <p className="-mt-2 text-sm text-muted">
          {formatDay(slot.date)} · Planning one cooks it for this portion, or
          eats the rest of an earlier meal.
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
        <ErrorText error={cook.error ?? portion.error ?? leftover.error} />
      </div>
    </Modal>
  );
};

export default EatingTimeSuggestionsDialog;
