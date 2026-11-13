import type { Feedback } from "@components/recipes/suggest-feedback";
import {
  NEVER,
  SNOOZES,
  snoozeFor,
} from "@components/recipes/suggest-feedback";
import Modal from "@components/ui/modal";
import type { MoreItem } from "@components/ui/more-menu";
import MoreMenu, { menuItemClass } from "@components/ui/more-menu";
import { sectionLabelClass } from "@components/ui/styles";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import {
  ArrowUturnLeftIcon,
  BuildingStorefrontIcon,
  CheckCircleIcon,
  ChevronDownIcon,
  LockClosedIcon,
  LockOpenIcon,
  PlusIcon,
} from "@heroicons/react/24/outline";
import type { RecipeSide } from "gen/recipes/v1/recipes_pb";
import type { PlanSlot, Suggestion } from "gen/suggestions/v1/suggestions_pb";
import { PlanSlotState } from "gen/suggestions/v1/suggestions_pb";
import { formatList, formatWeekday } from "helpers/format";
import { leftoverLabel, suggestionKey } from "helpers/suggestions";
import React, { useState } from "react";
import { Link } from "react-router";

// NO_SIDES is no sides to choose from, when none are given.
const NO_SIDES: RecipeSide[] = [];

// FirstReason returns a suggestion's best reason, or "" if none.
const firstReason = (suggestion: Suggestion): string =>
  suggestion.components.find((component) => component.points > 0)?.reason ?? "";

interface PlanSlotCardProps {
  slot: PlanSlot;
  // The name of the slot's meal, e.g. "Dinner".
  mealName: string;
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
  // Whether it's in a narrow column, with its buttons beside the meal's name.
  compact?: boolean | undefined;
  // The sides its recipe is served with, to choose from.
  sides?: RecipeSide[] | undefined;
  // Keeps the recipe with another side, or none with null.
  onSide?: ((side: RecipeSide | null) => void) | undefined;
}

// SideName names a side, by its recipe or food.
const sideName = (side: RecipeSide): string =>
  side.recipeId === "" ? side.foodName : side.recipeName;

// SameSide reports whether two sides are the same food or recipe.
const sameSide = (side: RecipeSide | undefined, other: RecipeSide): boolean =>
  side !== undefined &&
  (side.recipeId === ""
    ? side.foodId === other.foodId
    : side.recipeId === other.recipeId);

interface SidePickerProps {
  side: RecipeSide | undefined;
  sides: RecipeSide[];
  disabled: boolean;
  onSide: (side: RecipeSide | null) => void;
}

/**
 * SidePicker shows the side a meal is served with, like "with rice", and
 * changes it to another of its recipe's sides, or none, which plans the
 * rest again around it.
 */
const SidePicker: React.FC<SidePickerProps> = ({
  side,
  sides,
  disabled,
  onSide,
}) => (
  <Menu as="div" className="relative mt-1 self-start print:hidden">
    <MenuButton
      className="inline-flex items-center gap-1 rounded-full bg-ground-dark/60 px-2.5 py-1 text-xs font-bold text-ink-soft hover:bg-ground-dark disabled:opacity-50"
      disabled={disabled}
    >
      {side === undefined ? "No side" : `with ${sideName(side)}`}
      <ChevronDownIcon className="size-3.5" aria-hidden="true" />
    </MenuButton>
    <MenuItems
      transition
      anchor="bottom start"
      className="z-[1100] mt-1 w-56 rounded-2xl border border-line bg-white py-1 font-ui shadow-lg transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
    >
      {sides.map((option) => (
        <MenuItem
          key={option.recipeId === "" ? option.foodId : option.recipeId}
        >
          <button
            type="button"
            className={`${menuItemClass} ${sameSide(side, option) ? "font-bold" : ""}`}
            onClick={() => {
              onSide(option);
            }}
          >
            With {sideName(option)}
          </button>
        </MenuItem>
      ))}
      <MenuItem>
        <button
          type="button"
          className={`${menuItemClass} ${side === undefined ? "font-bold" : ""}`}
          onClick={() => {
            onSide(null);
          }}
        >
          Nothing on the side
        </button>
      </MenuItem>
    </MenuItems>
  </Menu>
);

/**
 * PlanSlotCard shows a meal of a suggested plan as a row: the recipe and
 * its best reason, a lock to keep it, and a ⋯ menu to swap it for an
 * alternative, stop suggesting it, eat out or leave the meal empty. Empty
 * and already planned meals say so.
 */
const PlanSlotCard: React.FC<PlanSlotCardProps> = ({
  slot,
  mealName,
  locked,
  index,
  disabled,
  onToggleLock,
  onSwap,
  onSkip,
  onFill,
  onEatingOut,
  onFeedback,
  compact = false,
  sides = NO_SIDES,
  onSide,
}) => {
  const [swapping, setSwapping] = useState(false);
  const delay = { animationDelay: `${Math.min(index * 40, 800)}ms` };
  const header = <span className={sectionLabelClass}>{mealName}</span>;
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
      <div
        className="flex flex-col gap-1 py-3 motion-safe:animate-rise"
        style={delay}
      >
        {header}
        {body}
      </div>
    );
  }

  const reason = firstReason(suggestion);
  const notes: string[] = [];
  if (suggestion.leftoverOf !== undefined) {
    notes.push(`${leftoverLabel(suggestion.leftoverOf)}, reheated`);
  }
  if (suggestion.cookDate !== "") {
    notes.push(`Cooked ${formatWeekday(suggestion.cookDate)}, from the fridge`);
  }
  if (reason !== "") {
    notes.push(reason);
  }
  const items: MoreItem[] = [];
  if (slot.alternatives.length > 0) {
    items.push({
      disabled,
      label: "Swap for another recipe",
      onClick: () => {
        setSwapping(true);
      },
    });
  }
  if (suggestion.leftoverOf === undefined) {
    for (const snooze of SNOOZES) {
      items.push({
        disabled,
        label: `Don't suggest it for ${snooze.label.replace(/^Not for /u, "")}`,
        onClick: () => {
          onFeedback(snoozeFor(snooze.days));
        },
      });
    }
    items.push({
      disabled,
      label: "Never suggest it",
      onClick: () => {
        onFeedback(NEVER);
      },
    });
  }
  items.push(
    {
      disabled,
      label: "Eating out",
      onClick: () => {
        onEatingOut(true);
      },
    },
    { disabled, label: "Leave empty", onClick: onSkip }
  );
  const buttons = (
    <div className="-mr-2 flex shrink-0 items-center print:hidden">
      <button
        type="button"
        className={`flex size-10 items-center justify-center rounded-full disabled:opacity-50 ${locked ? "text-pine" : "text-muted/70 hover:text-ink-soft"}`}
        aria-pressed={locked}
        aria-label={locked ? "Kept" : "Keep"}
        title={
          locked
            ? "Kept when you shuffle or change the criteria. Tap to let it change."
            : "Keep this recipe when you shuffle or change the criteria"
        }
        disabled={disabled}
        onClick={onToggleLock}
      >
        {locked ? (
          <LockClosedIcon className="size-5" />
        ) : (
          <LockOpenIcon className="size-5" />
        )}
      </button>
      <MoreMenu
        label={`More for ${suggestion.recipeName}`}
        className="flex size-10 items-center justify-center rounded-full text-muted hover:bg-ground-dark/60"
        items={items}
      />
    </div>
  );
  const recipe = (
    <>
      <Link
        to={`/recipes/${suggestion.recipeId}`}
        className="leading-snug font-bold text-ink-soft"
      >
        {suggestion.recipeName}
      </Link>
      {notes.length > 0 && (
        <p className="line-clamp-2 text-sm text-muted">{notes.join(" · ")}</p>
      )}
      {!suggestion.fitsRules && (
        <span className="mt-1 self-start rounded-full bg-warn-tint px-2.5 py-0.5 text-xs font-bold text-warn-dark">
          Breaks someone&apos;s rules
        </span>
      )}
      {suggestion.leftoverOf === undefined &&
        sides.length > 0 &&
        onSide !== undefined && (
          <SidePicker
            side={suggestion.side}
            sides={sides}
            disabled={disabled}
            onSide={onSide}
          />
        )}
    </>
  );
  return (
    <div
      className={`flex motion-safe:animate-rise ${compact ? "flex-col gap-0.5 py-2" : "items-start gap-3 py-3"}`}
      style={delay}
    >
      {compact ? (
        <>
          <div className="flex items-center justify-between gap-1">
            {header}
            {buttons}
          </div>
          {recipe}
        </>
      ) : (
        <>
          <div className="flex min-w-0 grow flex-col gap-0.5">
            {header}
            {recipe}
          </div>
          {buttons}
        </>
      )}
      <Modal
        open={swapping}
        title={`Instead of ${suggestion.recipeName}`}
        onClose={() => {
          setSwapping(false);
        }}
      >
        <div className="flex flex-col gap-2">
          <p className="-mt-2 text-sm text-muted">
            The next best, given the rest of the plan.
          </p>
          <ul className="divide-y divide-line">
            {slot.alternatives.map((alternative) => (
              <li key={suggestionKey(alternative)}>
                <button
                  type="button"
                  className="flex w-full flex-col items-start gap-0.5 py-3 text-left"
                  disabled={disabled}
                  onClick={() => {
                    setSwapping(false);
                    onSwap(alternative);
                  }}
                >
                  <span className="font-bold text-ink-soft">
                    {alternative.recipeName}
                    {alternative.leftoverOf !== undefined && (
                      <span className="font-normal text-muted">
                        {" "}
                        · {leftoverLabel(alternative.leftoverOf)}
                      </span>
                    )}
                  </span>
                  {firstReason(alternative) !== "" && (
                    <span className="line-clamp-2 text-sm text-muted">
                      {firstReason(alternative)}
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        </div>
      </Modal>
    </div>
  );
};

export default PlanSlotCard;
