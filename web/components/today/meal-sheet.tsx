import { markEaten, scalePortions } from "@components/today/day-change";
import DisplacedChoice from "@components/today/displaced-choice";
import { MealWarnings, Plate, SidesCard } from "@components/today/plate";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import { choiceClass } from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useTransport } from "@connectrpc/connect-query";
import {
  ArrowsRightLeftIcon,
  BuildingStorefrontIcon,
  CheckIcon,
  ScaleIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import type {
  GetWeekResponse,
  PersonDay,
  PlannedEatingTime,
} from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { formatList } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

// AMOUNTS are how much more or less of a meal can be said to be eaten.
const AMOUNTS: { factor: number; label: string }[] = [
  { factor: 0.5, label: "Half" },
  { factor: 0.75, label: "¾" },
  { factor: 1.25, label: "1¼" },
  { factor: 1.5, label: "1½" },
  { factor: 2, label: "Double" },
];

type Step = "actions" | "amount" | "out" | "skip";

interface MealSheetProps {
  week: GetWeekResponse;
  day: PersonDay;
  planned: PlannedEatingTime;
  date: string;
  today: string;
  onClose: () => void;
}

/**
 * MealSheet changes a meal of the day as it happens: it was eaten, or
 * something else was, more or less of it, eaten out, or skipped. A meal
 * not eaten then asks what happens to it.
 */
const MealSheet: React.FC<MealSheetProps> = ({
  week,
  day,
  planned,
  date,
  today,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const transport = useTransport();
  const [step, setStep] = useState<Step>("actions");
  const et = planned.eatingTime;
  const { portions } = planned;
  const names = formatList(portions.map((portion) => portion.name));
  const eaten =
    portions.length > 0 && portions.every((portion) => portion.eaten);

  const change = useMutation<void, ConnectError, () => Promise<void>>({
    mutationFn: async (run) => {
      await run();
    },
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
  const client = createClient(PlanService, transport);
  const eatingOut = async (): Promise<void> => {
    await client.setEatingOut({
      date,
      eatingOut: true,
      eatingTimeId: et?.id ?? "",
      personId: day.personId,
    });
  };

  const actionClass =
    "flex w-full items-center gap-4 border-b border-line py-3.5 text-left text-base text-ink-soft last:border-b-0";
  const iconClass =
    "flex size-9 shrink-0 items-center justify-center rounded-full";

  let content: React.ReactNode = null;
  switch (step) {
    case "actions": {
      content = (
        <div className="flex flex-col">
          {portions.length > 0 && (
            <div className="flex flex-col gap-3 pb-3">
              <Plate portions={portions} />
              <MealWarnings warnings={planned.warnings} />
              {et !== undefined &&
                portions.some((portion) => portion.cookId !== "") && (
                  <SidesCard
                    personId={day.personId}
                    eatingTimeId={et.id}
                    date={date}
                  />
                )}
            </div>
          )}
          <button
            type="button"
            className={actionClass}
            disabled={change.isPending || portions.length === 0}
            onClick={() => {
              change.mutate(async () => {
                await markEaten(client, portions, date, !eaten);
              });
            }}
          >
            <span className={`${iconClass} bg-ready-tint text-ready`}>
              <CheckIcon className="size-5 stroke-2" aria-hidden="true" />
            </span>
            {eaten ? "Not eaten yet" : "Ate it"}
          </button>
          <Link
            to={`/today/change?person=${day.personId}&et=${et?.id ?? ""}&date=${date}`}
            className={actionClass}
          >
            <span className={`${iconClass} bg-heat-tint text-heat`}>
              <ArrowsRightLeftIcon className="size-5" aria-hidden="true" />
            </span>
            <span className="flex flex-col">
              Had something else
              <span className="text-sm text-muted">
                And choose what happens to this meal
              </span>
            </span>
          </Link>
          <button
            type="button"
            className={actionClass}
            disabled={portions.length === 0}
            onClick={() => {
              setStep("amount");
            }}
          >
            <span className={`${iconClass} bg-ground-dark text-ink-soft`}>
              <ScaleIcon className="size-5" aria-hidden="true" />
            </span>
            <span className="flex flex-col">
              Ate more or less
              <span className="text-sm text-muted">Half, ¾, double…</span>
            </span>
          </button>
          <button
            type="button"
            className={actionClass}
            onClick={() => {
              if (portions.length === 0) {
                change.mutate(eatingOut);
              } else {
                setStep("out");
              }
            }}
          >
            <span className={`${iconClass} bg-ground-dark text-ink-soft`}>
              <BuildingStorefrontIcon className="size-5" aria-hidden="true" />
            </span>
            Ate out
          </button>
          {portions.length > 0 && (
            <button
              type="button"
              className={actionClass}
              onClick={() => {
                setStep("skip");
              }}
            >
              <span className={`${iconClass} bg-ground-dark text-ink-soft`}>
                <XMarkIcon className="size-5" aria-hidden="true" />
              </span>
              Skipped it
            </button>
          )}
        </div>
      );
      break;
    }
    case "amount": {
      content = (
        <div className="flex flex-col gap-3">
          <p className="text-base text-ink-soft">How much of it was eaten?</p>
          <div className="flex flex-wrap gap-2">
            {AMOUNTS.map(({ factor, label }) => (
              <button
                key={label}
                type="button"
                className={`${choiceClass(false)} px-4 py-2.5 text-base`}
                disabled={change.isPending}
                onClick={() => {
                  change.mutate(async () => {
                    await scalePortions(client, portions, factor);
                    await markEaten(client, portions, date, true);
                  });
                }}
              >
                {label}
              </button>
            ))}
          </div>
        </div>
      );
      break;
    }
    default: {
      content = (
        <div className="flex flex-col gap-3">
          <p className="text-lg font-bold text-ink-soft">
            What about the {names.toLowerCase()}?
          </p>
          <DisplacedChoice
            week={week}
            personId={day.personId}
            date={date}
            today={today}
            time={et?.time ?? ""}
            portions={portions}
            name={`the ${names.toLowerCase()}`}
            then={step === "out" ? eatingOut : undefined}
            onDone={onClose}
          />
        </div>
      );
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={portions.length === 0 ? (et?.name ?? "") : names}
    >
      <div className="flex flex-col gap-3">
        <p className="-mt-2 text-sm text-muted">
          {et?.time} · {et?.name}
        </p>
        {content}
        <ErrorText error={change.error} />
      </div>
    </Modal>
  );
};

export default MealSheet;
