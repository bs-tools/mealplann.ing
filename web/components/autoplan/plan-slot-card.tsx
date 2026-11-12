import { ComponentChip } from "@components/plan/plan-bits";
import type { Feedback } from "@components/recipes/suggest-feedback";
import {
  NEVER,
  SNOOZES,
  snoozeFor,
} from "@components/recipes/suggest-feedback";
import { Badge } from "@components/ui/misc";
import { iconButton } from "@components/ui/styles";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import {
  ArrowUturnLeftIcon,
  ArrowsRightLeftIcon,
  BuildingStorefrontIcon,
  CheckCircleIcon,
  HandThumbDownIcon,
  LockClosedIcon,
  LockOpenIcon,
  PlusIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import type { PlanSlot, Suggestion } from "gen/suggestions/v1/suggestions_pb";
import { PlanSlotState } from "gen/suggestions/v1/suggestions_pb";
import { formatList, formatWeekday } from "helpers/format";
import { formatAmount } from "helpers/nutrients";
import { leftoverLabel, suggestionKey } from "helpers/suggestions";
import React from "react";
import { Link } from "react-router";

// SHOWN_REASONS is how many of the best reasons a slot shows.
const SHOWN_REASONS = 2;

// FirstReason returns a suggestion's best reason, or "" if none.
const firstReason = (suggestion: Suggestion): string =>
  suggestion.components.find((component) => component.points > 0)?.reason ?? "";

interface PlanSlotCardProps {
  slot: PlanSlot;
  // The name of the slot's meal, e.g. "Dinner".
  mealName: string;
  // The plan's best score, which the score bars are relative to.
  maxScore: number;
  // Whether the user keeps the slot's recipe when planning again.
  locked: boolean;
  // Its position in the plan, which staggers its entrance.
  index: number;
  disabled: boolean;
  onToggleLock: () => void;
  onSwap: (alternative: Suggestion) => void;
  onSkip: () => void;
  onFill: () => void;
  // Marks everyone at the slot eating out, or eating at home again.
  onEatingOut: (eatingOut: boolean) => void;
  // Leaves the suggested recipe out of suggestions, for a while or for good.
  onFeedback: (feedback: Feedback) => void;
}

/**
 * PlanSlotCard shows a meal of a suggested plan: the recipe, its score and
 * best reasons, with buttons to keep it, swap it for an alternative, or
 * leave the meal empty. Empty and already planned meals say so.
 */
const PlanSlotCard: React.FC<PlanSlotCardProps> = ({
  slot,
  mealName,
  maxScore,
  locked,
  index,
  disabled,
  onToggleLock,
  onSwap,
  onSkip,
  onFill,
  onEatingOut,
  onFeedback,
}) => {
  const delay = { animationDelay: `${Math.min(index * 40, 800)}ms` };
  const header = (
    <span className="text-[11px] font-semibold tracking-wide text-muted uppercase">
      {mealName}
    </span>
  );
  const { suggestion } = slot;
  if (suggestion === undefined) {
    let body: React.ReactNode = null;
    switch (slot.state) {
      case PlanSlotState.PLANNED: {
        body = (
          <p className="flex items-start gap-1.5 text-sm text-muted">
            <CheckCircleIcon
              className="mt-0.5 size-4 shrink-0 text-pine"
              aria-hidden="true"
            />
            <span>Planned: {formatList(slot.plannedRecipeNames)}</span>
          </p>
        );
        break;
      }
      case PlanSlotState.SKIPPED: {
        body = (
          <div className="flex items-center justify-between gap-2">
            <span className="text-sm text-muted italic">Left empty</span>
            <button
              type="button"
              className="inline-flex items-center gap-1 text-xs font-medium text-pine hover:underline disabled:opacity-50"
              disabled={disabled}
              onClick={onFill}
            >
              <PlusIcon className="size-3.5" aria-hidden="true" />
              Suggest one
            </button>
          </div>
        );
        break;
      }
      case PlanSlotState.EATING_OUT: {
        body = (
          <div className="flex items-center justify-between gap-2">
            <span className="flex items-center gap-1.5 text-sm text-muted">
              <BuildingStorefrontIcon
                className="size-4 shrink-0 text-muted"
                aria-hidden="true"
              />
              Eating out
            </span>
            <button
              type="button"
              className="inline-flex items-center gap-1 text-xs font-medium text-pine hover:underline disabled:opacity-50"
              disabled={disabled}
              onClick={() => {
                onEatingOut(false);
              }}
            >
              <ArrowUturnLeftIcon className="size-3.5" aria-hidden="true" />
              Eat at home
            </button>
          </div>
        );
        break;
      }
      case PlanSlotState.FILLED_BY_MEAL: {
        body = (
          <p className="text-sm text-muted">
            The rest of an earlier meal, which is more than the most they eat at
            once.
          </p>
        );
        break;
      }
      case PlanSlotState.UNSPECIFIED:
      case PlanSlotState.SUGGESTED:
      case PlanSlotState.KEPT:
      case PlanSlotState.UNFILLED: {
        body = (
          <p className="text-sm text-muted">
            {slot.unfilledReason === ""
              ? "No recipe meets the criteria."
              : slot.unfilledReason}
          </p>
        );
        break;
      }
    }
    return (
      <li
        className="flex flex-col gap-1 rounded-xl border border-dashed border-line bg-ground/60 p-3 motion-safe:animate-rise"
        style={delay}
      >
        {header}
        {body}
      </li>
    );
  }

  const fraction = maxScore > 0 ? Math.max(suggestion.score, 0) / maxScore : 0;
  const reasons = suggestion.components
    .filter((component) => component.points > 0)
    .slice(0, SHOWN_REASONS);
  const penalties = suggestion.components.filter(
    (component) => component.points < 0
  );
  return (
    <li
      className={`flex flex-col gap-2 rounded-xl border bg-white p-3 shadow-sm motion-safe:animate-rise ${
        locked ? "border-brand-400 ring-1 ring-pine-200" : "border-line"
      }`}
      style={delay}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex min-w-0 flex-col gap-0.5">
          {header}
          <Link
            to={`/recipes/${suggestion.recipeId}`}
            className="font-medium text-ink-soft hover:underline"
          >
            {suggestion.recipeName}
          </Link>
          {suggestion.leftoverOf !== undefined && (
            <span className="text-xs text-muted">
              {leftoverLabel(suggestion.leftoverOf)}, reheated
            </span>
          )}
          {suggestion.cookDate !== "" && (
            <span className="text-xs text-muted">
              Cooked {formatWeekday(suggestion.cookDate)}, from the fridge
            </span>
          )}
        </div>
        <div className="-mt-1 -mr-1 flex shrink-0 items-center print:hidden">
          <button
            type="button"
            className={`${iconButton} ${locked ? "text-pine" : ""}`}
            aria-pressed={locked}
            aria-label={locked ? "Kept" : "Keep"}
            title={
              locked
                ? "Kept when you shuffle or change the criteria. Click to let it change."
                : "Keep this recipe when you shuffle or change the criteria"
            }
            disabled={disabled}
            onClick={onToggleLock}
          >
            {locked ? (
              <LockClosedIcon className="size-4" />
            ) : (
              <LockOpenIcon className="size-4" />
            )}
          </button>
          {slot.alternatives.length > 0 && (
            <Menu as="div" className="relative">
              <MenuButton
                className={iconButton}
                aria-label="Swap"
                title="Swap for another recipe"
                disabled={disabled}
              >
                <ArrowsRightLeftIcon className="size-4" />
              </MenuButton>
              <MenuItems
                transition
                className="absolute right-0 z-[1100] mt-1 w-72 origin-top-right rounded-xl bg-white py-1 shadow-lg ring-1 ring-black/5 transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
              >
                <p className="px-3 py-1 text-xs text-muted">
                  Next best, given the rest of the plan
                </p>
                {slot.alternatives.map((alternative) => (
                  <MenuItem key={suggestionKey(alternative)}>
                    <button
                      type="button"
                      className="flex w-full flex-col items-start gap-0.5 px-3 py-2 text-left data-[focus]:bg-ground-dark"
                      onClick={() => {
                        onSwap(alternative);
                      }}
                    >
                      <span className="flex w-full items-baseline justify-between gap-2">
                        <span className="text-sm font-medium text-ink-soft">
                          {alternative.recipeName}
                          {alternative.leftoverOf !== undefined && (
                            <span className="font-normal text-muted">
                              {" "}
                              · {leftoverLabel(alternative.leftoverOf)}
                            </span>
                          )}
                        </span>
                        <span className="shrink-0 text-xs text-muted tabular-nums">
                          {formatAmount(alternative.score)} points
                        </span>
                      </span>
                      {firstReason(alternative) !== "" && (
                        <span className="line-clamp-2 text-xs text-muted">
                          {firstReason(alternative)}
                        </span>
                      )}
                    </button>
                  </MenuItem>
                ))}
              </MenuItems>
            </Menu>
          )}
          {suggestion.leftoverOf === undefined && (
            <Menu as="div" className="relative">
              <MenuButton
                className={iconButton}
                aria-label="Don't suggest it"
                title="Don't suggest this recipe for a while, or ever"
                disabled={disabled}
              >
                <HandThumbDownIcon className="size-4" />
              </MenuButton>
              <MenuItems
                transition
                className="absolute right-0 z-[1100] mt-1 w-52 origin-top-right rounded-xl bg-white py-1 text-sm shadow-lg ring-1 ring-black/5 transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
              >
                {SNOOZES.map((snooze) => (
                  <MenuItem key={snooze.days}>
                    <button
                      type="button"
                      className="w-full px-3 py-2 text-left data-[focus]:bg-ground-dark"
                      onClick={() => {
                        onFeedback(snoozeFor(snooze.days));
                      }}
                    >
                      {snooze.label}
                    </button>
                  </MenuItem>
                ))}
                <MenuItem>
                  <button
                    type="button"
                    className="w-full px-3 py-2 text-left data-[focus]:bg-ground-dark"
                    onClick={() => {
                      onFeedback(NEVER);
                    }}
                  >
                    Never suggest it
                  </button>
                </MenuItem>
              </MenuItems>
            </Menu>
          )}
          <button
            type="button"
            className={iconButton}
            aria-label="Eating out"
            title="Eating out: leave it out of the plan, and of the next plans too"
            disabled={disabled}
            onClick={() => {
              onEatingOut(true);
            }}
          >
            <BuildingStorefrontIcon className="size-4" />
          </button>
          <button
            type="button"
            className={iconButton}
            aria-label="Leave empty"
            title="Leave this meal empty"
            disabled={disabled}
            onClick={onSkip}
          >
            <XMarkIcon className="size-4" />
          </button>
        </div>
      </div>
      <div
        className="flex items-center gap-2"
        title="Score, relative to the plan's best meal"
      >
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-ground-dark">
          <div
            className={`h-full rounded-full transition-[width] duration-700 ${suggestion.fitsRules ? "bg-pine" : "bg-warn"}`}
            style={{ width: `${fraction * 100}%` }}
          />
        </div>
        <span className="text-xs whitespace-nowrap text-muted tabular-nums">
          {formatAmount(suggestion.score)}
        </span>
      </div>
      {(reasons.length > 0 ||
        penalties.length > 0 ||
        !suggestion.fitsRules) && (
        <ul className="flex flex-wrap gap-1">
          {!suggestion.fitsRules && (
            <li>
              <Badge className="bg-warn-tint text-warn-dark ring-warn-line">
                Breaks rules
              </Badge>
            </li>
          )}
          {[...reasons, ...penalties.slice(0, 1)].map((component) => (
            <ComponentChip
              key={`${component.kind}-${component.reason}`}
              component={component}
            />
          ))}
        </ul>
      )}
    </li>
  );
};

export default PlanSlotCard;
