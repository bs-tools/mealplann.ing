import { TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import { primaryButton, secondaryButton } from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { updateCook } from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { formatShortDate } from "helpers/format";
import { formatServings } from "helpers/labels";
import { parseOptionalNumber } from "helpers/parse";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";

// FormatAmount formats servings without a unit, e.g. "2.5".
const formatAmount = (servings: number): string =>
  servings.toLocaleString(undefined, { maximumFractionDigits: 2 });

interface FreezeDialogProps {
  planned: PlannedCook;
  onClose: () => void;
}

/**
 * FreezeDialog sets how many of a cook's servings go in the freezer, to keep
 * longer than in the fridge: by default, those nobody is planned to eat.
 */
const FreezeDialog: React.FC<FreezeDialogProps> = ({ planned, onClose }) => {
  const queryClient = useQueryClient();
  const { cook } = planned;
  const leftover = Math.max(
    (cook?.servings ?? 0) - planned.assignedServings,
    0
  );
  const initial =
    cook !== undefined && cook.frozenServings > 0
      ? cook.frozenServings
      : Math.round(leftover * 100) / 100;
  const [servings, setServings] = useState(formatAmount(initial));
  const [invalid, setInvalid] = useState("");
  const update = useMutation(updateCook, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
      onClose();
    },
  });
  if (cook === undefined) {
    return null;
  }
  return (
    <Modal open title={`Freeze ${cook.recipeName}`} onClose={onClose}>
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          setInvalid("");
          const parsed = parseOptionalNumber(servings);
          const value = parsed ?? 0;
          if (parsed === null || value > cook.servings) {
            setInvalid(
              `Freeze from 0 to the ${formatAmount(cook.servings)} servings cooked.`
            );
            return;
          }
          update.mutate({
            cook: { frozenServings: value, id: cook.id },
            updateMask: { paths: ["frozen_servings"] },
          });
        }}
      >
        <p className="text-sm text-muted">
          Of the {formatServings(cook.servings)} cooked on{" "}
          {formatShortDate(cook.date)}, {formatAmount(planned.assignedServings)}{" "}
          are planned for people. Frozen servings keep for the recipe&apos;s
          days in the freezer: plan them for later from the freezer on the
          Pantry page.
        </p>
        <TextField
          label="Servings to freeze"
          value={servings}
          inputMode="decimal"
          autoFocus
          hint="0 to take them out of the freezer."
          onChange={setServings}
        />
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
            {update.isPending ? "Saving…" : "Freeze"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default FreezeDialog;
