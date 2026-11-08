import { TextField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  inputClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { createPlanTemplateFromPlan } from "gen/plans/v1/templates-PlanTemplateService_connectquery";
import { PlanTemplateService } from "gen/plans/v1/templates_pb";
import { addDays, startOfWeek, today } from "helpers/dates";
import { formatShortDate } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { useNavigate } from "react-router";

interface SaveTemplateDialogProps {
  // The first day to copy, the Monday of the week shown by default.
  start?: string | undefined;
  onClose: () => void;
}

/**
 * SaveTemplateDialog saves days of the plan as a template, to plan them
 * again from another date.
 */
const SaveTemplateDialog: React.FC<SaveTemplateDialogProps> = ({
  start: initialStart,
  onClose,
}) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const [source, setSource] = useState("");
  const [start, setStart] = useState(initialStart ?? startOfWeek(today()));
  const [days, setDays] = useState("7");
  const save = useMutation(createPlanTemplateFromPlan, {
    onSuccess: async (response) => {
      await invalidateServices(queryClient, [PlanTemplateService]);
      void navigate(`/plan/templates/${response.template?.id ?? ""}`);
    },
  });
  const dayCount = Number(days);
  const valid =
    name.trim() !== "" &&
    Number.isInteger(dayCount) &&
    dayCount >= 1 &&
    dayCount <= 28;
  return (
    <Modal
      open
      title="Save as a template"
      dirty={name !== "" || source !== ""}
      onClose={onClose}
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          save.mutate({ days: dayCount, name, source, startDate: start });
        }}
      >
        <p className="text-sm text-gray-600">
          The meals planned with recipes, with everyone&apos;s portions and the
          leftovers, become a template to plan again from any date. Staples
          aren&apos;t copied, as they recur anyway.
        </p>
        <TextField
          label="Name"
          value={name}
          placeholder="A week we liked"
          required
          autoFocus
          onChange={setName}
        />
        <TextField
          label="Source"
          value={source}
          placeholder="Where it comes from, e.g. the dietitian's week 1"
          onChange={setSource}
        />
        <div className="flex flex-wrap items-end gap-4">
          <label className="flex flex-col gap-1">
            <span className={labelClass}>From</span>
            <input
              type="date"
              className={`${inputClass} w-40`}
              value={start}
              required
              onChange={(event) => {
                if (event.target.value !== "") {
                  setStart(event.target.value);
                }
              }}
            />
          </label>
          <TextField
            label="Days"
            value={days}
            inputMode="numeric"
            className="w-20"
            onChange={setDays}
          />
          {valid && (
            <span className="pb-2 text-sm text-gray-600">
              to {formatShortDate(addDays(start, dayCount - 1))}
            </span>
          )}
        </div>
        <ErrorText error={save.error} />
        <div className="flex justify-end gap-2">
          <button type="button" className={secondaryButton} onClick={onClose}>
            Cancel
          </button>
          <button
            type="submit"
            className={primaryButton}
            disabled={!valid || save.isPending}
          >
            {save.isPending ? "Saving…" : "Save template"}
          </button>
        </div>
      </form>
    </Modal>
  );
};

export default SaveTemplateDialog;
