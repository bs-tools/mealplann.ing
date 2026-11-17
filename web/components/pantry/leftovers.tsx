import CorrectServingsDialog from "@components/pantry/correct-servings";
import { PlanFrozenDialog } from "@components/pantry/plan-frozen-dialog";
import FreezeDialog from "@components/plan/freeze-dialog";
import { tintOf } from "@components/recipes/tint";
import { ConfirmDialog } from "@components/ui/confirm-button";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  ghostDangerButton,
  pillButton,
  pillPrimaryButton,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  getWeek,
  listFrozenLeftovers,
  takeFromFreezer,
  updateCook,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { FrozenLeftover, PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { daysBetween, startOfWeek, today } from "helpers/dates";
import { formatShortDate } from "helpers/format";
import { formatServings } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

/**
 * Leftover is a cook's servings in the kitchen not planned yet: in the
 * fridge, cooked and keeping by the recipe's fridge days, or frozen.
 */
export interface Leftover {
  key: string;
  recipeId: string;
  recipeName: string;
  servings: number;
  // What to say under its name, like "Cooked Nov 8 · keeps until Dec 8".
  detail: string;
  // Whether it should be eaten or thrown out soon.
  warn: boolean;
  /*
   * Whether it's to be planned or frozen soon, as it doesn't keep long
   * after: not yet past keeping, which is for throwing out.
   */
  soon: boolean;
  // The cook, for fridge leftovers.
  planned?: PlannedCook | undefined;
  // The frozen servings, for frozen leftovers.
  frozen?: FrozenLeftover | undefined;
}

// FrozenDetail says when frozen leftovers were cooked and how long they keep.
const frozenDetail = (leftover: FrozenLeftover): string => {
  const cooked = `Cooked ${formatShortDate(leftover.cook?.date ?? "")}`;
  if (leftover.expired) {
    return `${cooked} · past its freezer days`;
  }
  return leftover.keepsUntil === ""
    ? cooked
    : `${cooked} · keeps until ${formatShortDate(leftover.keepsUntil)}`;
};

/*
 * FRIDGE_SOON_DAYS and FREEZER_SOON_DAYS are how many days before leftovers
 * stop keeping they're to be used soon: fridge leftovers keep only days,
 * so only their last day or two count.
 */
const FRIDGE_SOON_DAYS = 1;
const FREEZER_SOON_DAYS = 7;

// KeepsSoon reports whether leftovers keeping until a date run out soon.
const keepsSoon = (keepsUntil: string, now: string, days: number): boolean => {
  if (keepsUntil === "") {
    return false;
  }
  const left = daysBetween(now, keepsUntil);
  return left >= 0 && left <= days;
};

// FridgeDetail says when fridge leftovers were cooked and how long they keep.
const fridgeDetail = (planned: PlannedCook, now: string): string => {
  const cooked = `Cooked ${formatShortDate(planned.cook?.date ?? "")}`;
  if (planned.keepsUntil === "") {
    return cooked;
  }
  const left = daysBetween(now, planned.keepsUntil);
  if (left === 0) {
    return `${cooked} · eat or freeze today`;
  }
  if (left === 1) {
    return `${cooked} · eat or freeze by tomorrow`;
  }
  return `${cooked} · keeps until ${formatShortDate(planned.keepsUntil)}`;
};

// FRIDGE_EPSILON ignores rounding left over from splitting servings.
const FRIDGE_EPSILON = 0.01;

/**
 * UseLeftovers returns the household's leftovers: those in the fridge,
 * from the cooks of this week done by today and the earlier ones still
 * keeping, and those in the freezer.
 */
export const useLeftovers = (): {
  fridge: Leftover[];
  frozen: Leftover[];
  isPending: boolean;
} => {
  const now = today();
  const week = useQuery(getWeek, { monday: startOfWeek(now) });
  const freezer = useQuery(listFrozenLeftovers, {});
  const cooks = [
    ...(week.data?.earlierLeftovers ?? []),
    ...(week.data?.cooks ?? []),
  ];
  const fridge = cooks
    .filter(
      (planned) =>
        planned.cook?.done === true &&
        planned.cook.date <= now &&
        planned.leftServings > FRIDGE_EPSILON
    )
    .map((planned): Leftover => {
      const soon = keepsSoon(planned.keepsUntil, now, FRIDGE_SOON_DAYS);
      return {
        detail: fridgeDetail(planned, now),
        key: `fridge-${planned.cook?.id ?? ""}`,
        planned,
        recipeId: planned.cook?.recipeId ?? "",
        recipeName: planned.cook?.recipeName ?? "",
        servings: planned.leftServings,
        soon,
        warn: soon,
      };
    });
  const frozen = (freezer.data?.leftovers ?? [])
    .filter((leftover) => leftover.remainingServings > 0)
    .map((leftover): Leftover => {
      const soon =
        !leftover.expired &&
        keepsSoon(leftover.keepsUntil, now, FREEZER_SOON_DAYS);
      return {
        detail: frozenDetail(leftover),
        frozen: leftover,
        key: `frozen-${leftover.cook?.id ?? ""}`,
        recipeId: leftover.cook?.recipeId ?? "",
        recipeName: leftover.cook?.recipeName ?? "",
        servings: leftover.remainingServings,
        soon,
        warn: leftover.expired || soon,
      };
    });
  return { fridge, frozen, isPending: week.isPending || freezer.isPending };
};

/**
 * MatchesLeftover reports whether a leftover matches a search: whether
 * each of its words is in the recipe's name, ignoring case.
 */
export const matchesLeftover = (leftover: Leftover, search: string): boolean =>
  search
    .toLowerCase()
    .split(/\s+/u)
    .every((word) => leftover.recipeName.toLowerCase().includes(word));

/*
 * LeftoverStep is what's done with leftovers: chosen in their sheet, or
 * straight from their row.
 */
export type LeftoverStep = "sheet" | "plan" | "freeze" | "throw" | "correct";

/*
 * PlanLeftoversLink links to planning a meal of leftovers from the fridge,
 * today.
 */
export const planLeftoversLink = (): string =>
  `/plan?week=${startOfWeek(today())}&day=${today()}&leftovers=1`;

interface LeftoverRowProps {
  leftover: Leftover;
  // Called to open their sheet, at a step if one is chosen in the row.
  onOpen: (step?: LeftoverStep) => void;
}

/**
 * LeftoverRow shows leftovers like a pantry lot: the recipe, when it was
 * cooked and how long it keeps, and how many servings. Tapping it opens
 * what can be done with them; those to use soon offer to plan or freeze
 * them right in the row, before they go off.
 */
export const LeftoverRow: React.FC<LeftoverRowProps> = ({
  leftover,
  onOpen,
}) => (
  <li className="border-b border-line">
    <button
      type="button"
      className="flex w-full items-center gap-3 py-3.5 text-left"
      onClick={() => {
        onOpen();
      }}
    >
      <span
        className={`size-11 shrink-0 rounded-xl ${tintOf(leftover.recipeId)}`}
        aria-hidden="true"
      />
      <span className="flex min-w-0 grow flex-col">
        <span className="text-base font-bold text-ink-soft">
          {leftover.recipeName}
          <span className="font-normal text-muted"> · leftovers</span>
        </span>
        <span
          className={`text-sm ${leftover.warn ? "font-bold text-warn-dark" : "text-muted"}`}
        >
          {leftover.detail}
        </span>
      </span>
      <span className="shrink-0 text-base font-bold text-ink-soft tabular-nums">
        {formatServings(leftover.servings)}
      </span>
    </button>
    {leftover.soon && (
      <div className="-mt-1 flex flex-wrap gap-2 pb-3.5 pl-14">
        {leftover.frozen === undefined ? (
          <Link to={planLeftoversLink()} className={pillPrimaryButton}>
            Plan them
          </Link>
        ) : (
          <button
            type="button"
            className={pillPrimaryButton}
            onClick={() => {
              onOpen("plan");
            }}
          >
            Plan them
          </button>
        )}
        {leftover.frozen === undefined && (
          <button
            type="button"
            className={pillButton}
            onClick={() => {
              onOpen("freeze");
            }}
          >
            Freeze them
          </button>
        )}
      </div>
    )}
  </li>
);

interface LeftoverSheetProps {
  leftover: Leftover;
  // The step to open at, the sheet if undefined.
  step?: LeftoverStep | undefined;
  onClose: () => void;
}

/**
 * LeftoverSheet is what can be done with leftovers: plan them for a meal,
 * freeze those in the fridge, correct how many servings there are, or take
 * them out, eaten without a plan, thrown out, or not there after all.
 */
export const LeftoverSheet: React.FC<LeftoverSheetProps> = ({
  leftover,
  step: initialStep = "sheet",
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [step, setStep] = useState<LeftoverStep>(initialStep);
  const take = useMutation(takeFromFreezer, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
      onClose();
    },
  });
  /*
   * Remove takes fridge leftovers out of the pantry: servings that aren't
   * there, as the plan can get the numbers wrong, or were eaten, given
   * away or thrown out without a plan.
   */
  const remove = useMutation(updateCook, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        SuggestionService,
      ]);
      onClose();
    },
  });
  const { frozen, planned } = leftover;
  const cookId = frozen?.cook?.id ?? planned?.cook?.id ?? "";
  if (step === "plan" && frozen !== undefined) {
    return <PlanFrozenDialog leftover={frozen} onClose={onClose} />;
  }
  if (step === "freeze" && planned !== undefined) {
    return <FreezeDialog planned={planned} onClose={onClose} />;
  }
  const cook = frozen?.cook ?? planned?.cook;
  if (step === "correct" && cook !== undefined) {
    return (
      <CorrectServingsDialog
        cook={cook}
        recipeName={leftover.recipeName}
        servings={leftover.servings}
        frozen={frozen !== undefined}
        onClose={onClose}
      />
    );
  }
  return (
    <Modal open title={leftover.recipeName} onClose={onClose}>
      <div className="flex flex-col gap-4">
        <p className="-mt-2 text-sm text-muted">
          {formatServings(leftover.servings)} in the{" "}
          {frozen === undefined ? "fridge" : "freezer"}, not planned yet ·{" "}
          {leftover.detail}
        </p>
        <div className="flex flex-col gap-2">
          {frozen === undefined ? (
            <Link to={planLeftoversLink()} className={`${primaryButton} py-3`}>
              Plan them
            </Link>
          ) : (
            <button
              type="button"
              className={`${primaryButton} py-3`}
              onClick={() => {
                setStep("plan");
              }}
            >
              Plan them
            </button>
          )}
          {frozen === undefined ? (
            <button
              type="button"
              className={`${secondaryButton} py-3`}
              onClick={() => {
                setStep("freeze");
              }}
            >
              Freeze them
            </button>
          ) : (
            <button
              type="button"
              className={`${secondaryButton} py-3`}
              title="Take a serving out of the freezer, eaten without a plan"
              disabled={take.isPending}
              onClick={() => {
                take.mutate({
                  cookId,
                  servings: Math.min(1, leftover.servings),
                });
              }}
            >
              Ate a serving
            </button>
          )}
          <button
            type="button"
            className={secondaryButton}
            title="Set how many servings there really are, when the plan has it wrong"
            onClick={() => {
              setStep("correct");
            }}
          >
            Correct the servings
          </button>
          <Link
            to={`/recipes/${leftover.recipeId}`}
            className={secondaryButton}
          >
            The recipe
          </Link>
          <button
            type="button"
            className={`${ghostDangerButton} self-start`}
            disabled={take.isPending || remove.isPending}
            onClick={() => {
              setStep("throw");
            }}
          >
            {frozen === undefined ? "Remove from the pantry" : "Throw them out"}
          </button>
        </div>
        <ErrorText error={take.error ?? remove.error} />
      </div>
      <ConfirmDialog
        open={step === "throw"}
        title={
          frozen === undefined
            ? `Remove ${leftover.recipeName}?`
            : `Throw out ${leftover.recipeName}?`
        }
        message={
          frozen === undefined
            ? `The ${formatServings(leftover.servings)} not planned yet are taken out of the pantry, like leftovers that aren't there or were eaten without a plan. Those already planned stay.`
            : "The frozen servings not planned yet are taken out of the freezer. Those already planned stay."
        }
        confirmLabel={frozen === undefined ? "Remove" : "Throw out"}
        onClose={() => {
          setStep("sheet");
        }}
        onConfirm={() => {
          if (frozen === undefined) {
            remove.mutate({
              cook: {
                id: cookId,
                removedServings:
                  (planned?.cook?.removedServings ?? 0) + leftover.servings,
              },
              updateMask: { paths: ["removed_servings"] },
            });
            return;
          }
          take.mutate({ cookId, servings: leftover.servings });
        }}
      />
    </Modal>
  );
};
