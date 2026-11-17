import type { MessageInitShape } from "@bufbuild/protobuf";
import { TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  primaryButton,
  secondaryButton,
  stepButtonClass,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import { updateCook } from "gen/plans/v1/plans-PlanService_connectquery";
import type { Cook, CookSchema } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { formatServings } from "helpers/labels";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

/*
 * STEP is how much the stepper changes the servings by: a quarter, as
 * portions of leftovers are.
 */
const STEP = 0.25;

// Round rounds servings to hundredths, as floats add up to crumbs.
const round = (value: number): number => Math.round(value * 100) / 100;

/**
 * CookCorrection is the change to a cook that makes the servings of its
 * leftovers in the fridge, or in the freezer, the number given, with the
 * mask of the fields it sets.
 */
interface CookCorrection {
  cook: MessageInitShape<typeof CookSchema>;
  paths: string[];
}

/**
 * MostServings returns the most servings the leftovers can be corrected
 * to, without changing what was cooked: in the fridge, as many as were
 * taken out before; in the freezer, those taken out too, and the rest of
 * the cook, counted as frozen.
 */
export const mostServings = (
  cook: Cook,
  servings: number,
  frozen: boolean
): number =>
  frozen
    ? round(
        servings +
          cook.offPlanServings +
          Math.max(cook.servings - cook.frozenServings, 0)
      )
    : round(servings + cook.removedServings);

/**
 * CorrectionOf returns the change to a cook that makes its leftovers the
 * target servings rather than the servings there are now. In the fridge,
 * those taken out of it (removed_servings) make up the difference. In the
 * freezer, fewer are taken out as eaten without a plan (off_plan_servings),
 * and more give back those taken, then count more of the cook as frozen.
 */
export const correctionOf = (
  cook: Cook,
  servings: number,
  target: number,
  frozen: boolean
): CookCorrection => {
  const delta = target - servings;
  if (!frozen) {
    return {
      cook: {
        id: cook.id,
        removedServings: round(Math.max(cook.removedServings - delta, 0)),
      },
      paths: ["removed_servings"],
    };
  }
  if (delta <= 0) {
    return {
      cook: {
        id: cook.id,
        offPlanServings: round(cook.offPlanServings - delta),
      },
      paths: ["off_plan_servings"],
    };
  }
  const givenBack = Math.min(cook.offPlanServings, delta);
  return {
    cook: {
      frozenServings: round(
        Math.min(cook.frozenServings + delta - givenBack, cook.servings)
      ),
      id: cook.id,
      offPlanServings: round(cook.offPlanServings - givenBack),
    },
    paths: ["off_plan_servings", "frozen_servings"],
  };
};

interface CorrectServingsDialogProps {
  // The cook of the leftovers.
  cook: Cook;
  recipeName: string;
  // The servings of leftovers there are now, by the plan.
  servings: number;
  // Whether they're in the freezer, rather than the fridge.
  frozen: boolean;
  onClose: () => void;
}

/**
 * CorrectServingsDialog sets how many servings of leftovers there really
 * are, in the fridge or the freezer, when the plan has it wrong: some were
 * eaten without a plan, or more were put away than planned.
 */
const CorrectServingsDialog: React.FC<CorrectServingsDialogProps> = ({
  cook,
  recipeName,
  servings,
  frozen,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [text, setText] = useState(String(round(servings)));
  const [invalid, setInvalid] = useState("");
  const most = mostServings(cook, servings, frozen);
  const value = parseOptionalNumber(text) ?? 0;
  const update = useMutation(updateCook, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
      onClose();
    },
  });
  const save = () => {
    const target = parseOptionalNumber(text);
    if (target === null || target === undefined || target < 0) {
      setInvalid("Enter the servings there are, like 1.5, or 0 for none.");
      return;
    }
    if (target > most) {
      setInvalid(
        `There can be at most ${formatServings(most)}, as that's what was cooked. Change the cook's servings on its page to have more.`
      );
      return;
    }
    setInvalid("");
    const { cook: changed, paths } = correctionOf(
      cook,
      servings,
      target,
      frozen
    );
    update.mutate({ cook: changed, updateMask: { paths } });
  };
  return (
    <Modal open title={`Correct ${recipeName}`} onClose={onClose}>
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          save();
        }}
      >
        <p className="-mt-2 text-sm text-muted">
          How many servings are in the {frozen ? "freezer" : "fridge"}, not
          planned yet? The plan counts {formatServings(servings)}.
        </p>
        <div className="flex items-end gap-2">
          <button
            type="button"
            className={stepButtonClass}
            aria-label="A quarter serving fewer"
            disabled={value <= 0}
            onClick={() => {
              setText(String(round(Math.max(value - STEP, 0))));
            }}
          >
            −
          </button>
          <div className="w-28">
            <TextField
              label="Servings"
              value={text}
              inputMode="decimal"
              onChange={setText}
            />
          </div>
          <button
            type="button"
            className={stepButtonClass}
            aria-label="A quarter serving more"
            disabled={value + STEP > most}
            onClick={() => {
              setText(String(round(Math.min(value + STEP, most))));
            }}
          >
            +
          </button>
        </div>
        {invalid !== "" && <p className="text-sm text-error">{invalid}</p>}
        <ErrorText error={update.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={update.isPending}
          >
            Save
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default CorrectServingsDialog;
