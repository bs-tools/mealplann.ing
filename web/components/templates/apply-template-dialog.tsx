import Loading from "@components/loading";
import { WarningList } from "@components/plan/plan-bits";
import { CheckboxField } from "@components/ui/fields";
import Modal from "@components/ui/modal";
import QueryError, { ErrorText } from "@components/ui/query-error";
import {
  inputClass,
  labelClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { keepPreviousData, useQueryClient } from "@tanstack/react-query";
import type { Person } from "gen/people/v1/people_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import {
  applyPlanTemplate,
  previewPlanTemplate,
} from "gen/plans/v1/templates-PlanTemplateService_connectquery";
import type { AppliedTemplate } from "gen/plans/v1/templates_pb";
import { createShoppingList } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { addDays, startOfWeek, today } from "helpers/dates";
import { formatShortDate, formatWeekday } from "helpers/format";
import { formatShare } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { useNavigate } from "react-router";

interface PreviewProps {
  result: AppliedTemplate;
  people: Person[];
}

/**
 * Preview shows what applying a template plans: each day's cooks with who
 * eats them, the warnings, who's left out, and what to buy.
 */
const Preview: React.FC<PreviewProps> = ({ result, people }) => {
  const name = (id: string): string =>
    people.find((person) => person.id === id)?.name ?? "Someone";
  const etName = (personId: string, etId: string): string =>
    people
      .find((person) => person.id === personId)
      ?.eatingTimes.find((et) => et.id === etId)?.name ?? "";
  const dates = [...new Set(result.cooks.map((cook) => cook.date))];
  return (
    <div className="flex flex-col gap-4">
      <ol className="flex flex-col gap-3">
        {dates.map((date) => (
          <li key={date} className="flex flex-col gap-1">
            <h4 className="text-sm font-semibold text-gray-900">
              {formatWeekday(date)}{" "}
              <span className="font-normal text-gray-500">
                {formatShortDate(date)}
              </span>
            </h4>
            <ul className="flex flex-col gap-1">
              {result.cooks
                .filter((cook) => cook.date === date)
                .map((cook) => {
                  const portions = result.portions.filter(
                    (portion) =>
                      portion.what.case === "cook" &&
                      portion.what.value.cookId === cook.id
                  );
                  return (
                    <li key={cook.id} className="text-sm text-gray-800">
                      <span className="font-medium">{cook.recipeName}</span>
                      <span className="text-gray-500">
                        {" "}
                        · cook{" "}
                        {cook.servings.toLocaleString(undefined, {
                          maximumFractionDigits: 2,
                        })}
                      </span>
                      <span className="block text-xs text-gray-600">
                        {portions
                          .map((portion) => {
                            const later =
                              portion.date === cook.date
                                ? ""
                                : ` ${formatShortDate(portion.date)}`;
                            return `${name(portion.personId)} ${formatShare(portion.what.case === "cook" ? portion.what.value.servings : 0)} at ${etName(portion.personId, portion.eatingTimeId)}${later}`;
                          })
                          .join(" · ")}
                      </span>
                    </li>
                  );
                })}
            </ul>
          </li>
        ))}
      </ol>
      {result.warnings.length > 0 && (
        <div className="flex flex-col gap-1">
          <h4 className="text-sm font-semibold text-gray-900">Warnings</h4>
          <WarningList warnings={result.warnings} />
        </div>
      )}
      {result.skipped.length > 0 && (
        <div className="flex flex-col gap-1">
          <h4 className="text-sm font-semibold text-gray-900">Left out</h4>
          <ul className="flex flex-col gap-0.5 text-xs text-gray-600">
            {result.skipped.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        </div>
      )}
      <div className="flex flex-col gap-1">
        <h4 className="text-sm font-semibold text-gray-900">To buy</h4>
        {result.shopping.length === 0 ? (
          <p className="text-xs text-gray-600">
            Nothing: the pantry has it all.
          </p>
        ) : (
          <ul className="grid grid-cols-1 gap-x-4 gap-y-0.5 text-xs text-gray-700 sm:grid-cols-2">
            {result.shopping.map((item) => (
              <li key={`${item.foodId}-${item.topUp}`}>
                {item.foodName}{" "}
                <span className="text-gray-500 tabular-nums">
                  {item.amountText}
                </span>
                {item.topUp && (
                  <span className="text-amber-700"> · later trip</span>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
};

// TemplateRef is what applying a template needs to know of it.
export interface TemplateRef {
  id: string;
  name: string;
  days: number;
}

interface ApplyTemplateDialogProps {
  template: TemplateRef;
  people: Person[];
  onClose: () => void;
}

/**
 * ApplyTemplateDialog plans a template from a start date for some people:
 * it previews what it plans, with the warnings and what to buy, then plans
 * it, and optionally makes the shopping list for its dates.
 */
const ApplyTemplateDialog: React.FC<ApplyTemplateDialogProps> = ({
  template,
  people,
  onClose,
}) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const nextMonday = addDays(startOfWeek(today()), 7);
  const [start, setStart] = useState(nextMonday);
  const [chosen, setChosen] = useState<string[]>(
    people.map((person) => person.id)
  );
  const [makeList, setMakeList] = useState(true);
  const request = {
    personIds: chosen.length === people.length ? [] : chosen,
    startDate: start,
    templateId: template.id,
  };
  const preview = useQuery(previewPlanTemplate, request, {
    placeholderData: keepPreviousData,
  });
  const list = useMutation(createShoppingList);
  const apply = useMutation(applyPlanTemplate, {
    onSuccess: async (response) => {
      await invalidateServices(queryClient, [PlanService, ShoppingService]);
      const end = response.result?.endDate ?? start;
      if (makeList) {
        const created = await list.mutateAsync({
          endDate: end,
          startDate: start,
        });
        void navigate(`/shopping?list=${created.list?.id ?? ""}`);
        return;
      }
      void navigate(`/plan?week=${startOfWeek(start)}&day=${start}`);
    },
  });
  const pending = apply.isPending || list.isPending;

  let body: React.ReactNode = null;
  if (preview.isPending) {
    body = <Loading />;
  } else if (preview.isError) {
    body = (
      <QueryError
        error={preview.error}
        onRetry={() => {
          void preview.refetch();
        }}
      />
    );
  } else if (preview.data.result !== undefined) {
    body = <Preview result={preview.data.result} people={people} />;
  }

  return (
    <Modal open title={`Plan "${template.name}"`} wide onClose={onClose}>
      <div className="flex flex-col gap-4">
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
          <span className="pb-2 text-sm text-gray-600">
            to {formatShortDate(addDays(start, template.days - 1))} ·{" "}
            {template.days} {template.days === 1 ? "day" : "days"}
          </span>
        </div>
        <fieldset className="flex flex-col gap-1">
          <legend className={labelClass}>For</legend>
          <div className="flex flex-wrap gap-x-4 gap-y-1">
            {people.map((person) => (
              <CheckboxField
                key={person.id}
                label={person.name}
                checked={chosen.includes(person.id)}
                onChange={(checked) => {
                  const next = checked
                    ? [...chosen, person.id]
                    : chosen.filter((id) => id !== person.id);
                  // At least one person stays, or there's nobody to plan for.
                  if (next.length > 0) {
                    setChosen(next);
                  }
                }}
              />
            ))}
          </div>
        </fieldset>
        <div
          className={`max-h-[50vh] overflow-y-auto rounded-md border border-gray-200 p-3 transition-opacity ${preview.isFetching ? "opacity-60" : ""}`}
        >
          {body}
        </div>
        <ErrorText error={apply.error ?? list.error} />
        <div className="flex flex-wrap items-center justify-between gap-3">
          <CheckboxField
            label="Make a shopping list for these days"
            checked={makeList}
            onChange={setMakeList}
          />
          <div className="flex gap-2">
            <button type="button" className={secondaryButton} onClick={onClose}>
              Cancel
            </button>
            <button
              type="button"
              className={primaryButton}
              disabled={pending || preview.data === undefined}
              onClick={() => {
                apply.mutate(request);
              }}
            >
              {pending ? "Planning…" : "Add to the plan"}
            </button>
          </div>
        </div>
      </div>
    </Modal>
  );
};

export default ApplyTemplateDialog;
