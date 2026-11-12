import { PlanFrozenDialog } from "@components/pantry/plan-frozen-dialog";
import FreezeDialog from "@components/plan/freeze-dialog";
import { tintOf } from "@components/recipes/tint";
import { ConfirmDialog } from "@components/ui/confirm-button";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import {
  ghostDangerButton,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import {
  getWeek,
  listFrozenLeftovers,
  takeFromFreezer,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { FrozenLeftover, PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { startOfWeek, today } from "helpers/dates";
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
    .map((planned): Leftover => ({
      detail: `Cooked ${formatShortDate(planned.cook?.date ?? "")}`,
      key: `fridge-${planned.cook?.id ?? ""}`,
      planned,
      recipeId: planned.cook?.recipeId ?? "",
      recipeName: planned.cook?.recipeName ?? "",
      servings: planned.leftServings,
      warn: false,
    }));
  const frozen = (freezer.data?.leftovers ?? [])
    .filter((leftover) => leftover.remainingServings > 0)
    .map((leftover): Leftover => ({
      detail: frozenDetail(leftover),
      frozen: leftover,
      key: `frozen-${leftover.cook?.id ?? ""}`,
      recipeId: leftover.cook?.recipeId ?? "",
      recipeName: leftover.cook?.recipeName ?? "",
      servings: leftover.remainingServings,
      warn: leftover.expired,
    }));
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

interface LeftoverRowProps {
  leftover: Leftover;
  onOpen: () => void;
}

/**
 * LeftoverRow shows leftovers like a pantry lot: the recipe, when it was
 * cooked and how long it keeps, and how many servings. Tapping it opens
 * what can be done with them.
 */
export const LeftoverRow: React.FC<LeftoverRowProps> = ({
  leftover,
  onOpen,
}) => (
  <li>
    <button
      type="button"
      className="flex w-full items-center gap-3 border-b border-line py-3.5 text-left"
      onClick={onOpen}
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
  </li>
);

interface LeftoverSheetProps {
  leftover: Leftover;
  onClose: () => void;
}

/**
 * LeftoverSheet is what can be done with leftovers: plan them for a meal,
 * freeze those in the fridge, or take frozen ones out, eaten without a
 * plan or thrown out.
 */
export const LeftoverSheet: React.FC<LeftoverSheetProps> = ({
  leftover,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const [step, setStep] = useState<"sheet" | "plan" | "freeze" | "throw">(
    "sheet"
  );
  const take = useMutation(takeFromFreezer, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
      onClose();
    },
  });
  const { frozen, planned } = leftover;
  const cookId = frozen?.cook?.id ?? planned?.cook?.id ?? "";
  const cookDate = frozen?.cook?.date ?? planned?.cook?.date ?? today();
  if (step === "plan" && frozen !== undefined) {
    return <PlanFrozenDialog leftover={frozen} onClose={onClose} />;
  }
  if (step === "freeze" && planned !== undefined) {
    return <FreezeDialog planned={planned} onClose={onClose} />;
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
            <Link
              to={`/plan?week=${startOfWeek(cookDate)}&day=${today()}`}
              className={`${primaryButton} py-3`}
            >
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
          <Link
            to={`/recipes/${leftover.recipeId}`}
            className={secondaryButton}
          >
            The recipe
          </Link>
          {frozen !== undefined && (
            <button
              type="button"
              className={`${ghostDangerButton} self-start`}
              disabled={take.isPending}
              onClick={() => {
                setStep("throw");
              }}
            >
              Throw them out
            </button>
          )}
        </div>
        <ErrorText error={take.error} />
      </div>
      <ConfirmDialog
        open={step === "throw"}
        title={`Throw out ${leftover.recipeName}?`}
        message="The frozen servings not planned yet are taken out of the freezer. Those already planned stay."
        confirmLabel="Throw out"
        onClose={() => {
          setStep("sheet");
        }}
        onConfirm={() => {
          take.mutate({ cookId, servings: leftover.servings });
        }}
      />
    </Modal>
  );
};
