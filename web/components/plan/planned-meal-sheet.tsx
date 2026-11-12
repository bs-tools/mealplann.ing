import CookActions from "@components/plan/cook-actions";
import type { MealRow, RowSlot } from "@components/plan/plan-rows";
import { rowNames } from "@components/plan/plan-rows";
import type { Slot } from "@components/plan/portion-dialog";
import { Plate, SidesCard } from "@components/today/plate";
import WarningFix from "@components/today/warning-fix";
import Modal from "@components/ui/modal";
import { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  secondaryButton,
  ghostButton,
  ghostDangerButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  deletePortion,
  setEatingOut,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type {
  GetWeekResponse,
  PlanDay,
  PlannedPortion,
} from "gen/plans/v1/plans_pb";
import { PlanService, Severity } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { parseDate } from "helpers/dates";
import { formatList } from "helpers/format";
import { formatAmount } from "helpers/nutrients";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

// CookUse is what some servings of a cook go to, for its bar.
interface CookUse {
  key: string;
  servings: number;
  label: string;
  // Whether it's the meal shown, which stands out.
  own: boolean;
  kind: "eaten" | "frozen" | "left";
}

/**
 * CookUsesOf returns what a cook's servings go to over the week: each
 * portion, in order of date, then the freezer and what's left over.
 */
const cookUsesOf = (
  week: GetWeekResponse,
  cookId: string,
  shown: PlannedPortion[]
): CookUse[] => {
  const uses: CookUse[] = week.days.flatMap((day) =>
    day.people.flatMap((person) =>
      person.eatingTimes.flatMap((planned) =>
        planned.portions
          .filter(
            (portion) => portion.cookId === cookId && portion.portionId !== ""
          )
          .map((portion): CookUse => ({
            key: portion.portionId,
            kind: "eaten",
            label: `${parseDate(day.date).toLocaleDateString(undefined, { weekday: "short" })} · ${person.personName}`,
            own: shown.some((other) => other.portionId === portion.portionId),
            servings: portion.servings,
          }))
      )
    )
  );
  const planned = week.cooks.find((other) => other.cook?.id === cookId);
  const frozen = planned?.cook?.frozenServings ?? 0;
  if (frozen > 0) {
    uses.push({
      key: "frozen",
      kind: "frozen",
      label: "Freezer",
      own: false,
      servings: frozen,
    });
  }
  const left = planned?.leftServings ?? 0;
  if (left > 0.01) {
    uses.push({
      key: "left",
      kind: "left",
      label: "Left over",
      own: false,
      servings: left,
    });
  }
  return uses;
};

const USE_CLASS: Record<CookUse["kind"], string> = {
  eaten: "bg-cook/35",
  frozen: "bg-heat-tint",
  left: "border-2 border-dashed border-line",
};

interface CookBarProps {
  week: GetWeekResponse;
  cookId: string;
  portions: PlannedPortion[];
}

/**
 * CookBar shows what a cook's servings go to: the meal shown, the other
 * meals that eat from it, the freezer and what's left, so it's clear what
 * changing one does to the rest.
 */
const CookBar: React.FC<CookBarProps> = ({ week, cookId, portions }) => {
  const cook = week.cooks.find((other) => other.cook?.id === cookId)?.cook;
  const uses = cookUsesOf(week, cookId, portions);
  if (cook === undefined || uses.length < 2) {
    return null;
  }
  return (
    <section className="flex flex-col gap-2">
      <h3 className={sectionLabelClass}>
        This cook makes {formatAmount(cook.servings)}
      </h3>
      <div className="flex gap-1.5">
        {uses.map((use) => (
          <div
            key={use.key}
            className="flex min-w-0 flex-col gap-1.5"
            style={{ flexBasis: 0, flexGrow: use.servings }}
          >
            <span
              className={`h-6 rounded-lg ${use.own ? "bg-cook" : USE_CLASS[use.kind]}`}
            />
            <span
              className={`truncate text-xs ${use.own ? "font-bold text-ink-soft" : "text-muted"}`}
            >
              {use.label}
            </span>
          </div>
        ))}
      </div>
    </section>
  );
};

interface PlannedMealSheetProps {
  week: GetWeekResponse;
  day: PlanDay;
  row: MealRow;
  onEdit: (slot: Slot, portion: PlannedPortion) => void;
  onClose: () => void;
}

/**
 * PlannedMealSheet shows a planned meal: as a plate, with the sides it's
 * served with, its warnings and their fixes, and what its cook's servings
 * go to, for each person who eats it.
 */
const PlannedMealSheet: React.FC<PlannedMealSheetProps> = ({
  week,
  day,
  row,
  onEdit,
  onClose,
}) => {
  const queryClient = useQueryClient();
  const eaters = row.slots.filter(
    (slot) => slot.planned.eatingOut || slot.planned.portions.length > 0
  );
  const [personId, setPersonId] = useState(eaters[0]?.person.personId ?? "");
  /*
   * The slot is found again in the refreshed week, as changes to it come
   * back there.
   */
  const fresh = (slot: RowSlot | undefined): RowSlot | undefined => {
    if (slot === undefined) {
      return undefined;
    }
    const person = week.days
      .find((other) => other.date === day.date)
      ?.people.find((other) => other.personId === slot.person.personId);
    const planned = person?.eatingTimes.find(
      (other) => other.eatingTime?.id === slot.planned.eatingTime?.id
    );
    return person === undefined || planned === undefined
      ? slot
      : { person, planned };
  };
  const slot = fresh(
    eaters.find((other) => other.person.personId === personId) ?? eaters[0]
  );
  const onSuccess = async (): Promise<void> => {
    await invalidateServices(queryClient, [
      PlanService,
      PantryService,
      ShoppingService,
      SuggestionService,
    ]);
  };
  const remove = useMutation(deletePortion, { onSuccess });
  const eatIn = useMutation(setEatingOut, { onSuccess });

  const weekday = parseDate(day.date).toLocaleDateString(undefined, {
    weekday: "long",
  });
  const kicker = `${weekday} ${row.label.toLowerCase()}${row.time === "" ? "" : ` · ${row.time}`}`;
  let content: React.ReactNode = null;
  if (slot === undefined) {
    content = null;
  } else if (slot.planned.eatingOut) {
    content = (
      <div className="flex flex-col gap-3">
        <p className="text-base text-ink-soft">
          {slot.person.personName} is eating out
          {slot.planned.eatingOutNotes === ""
            ? "."
            : `: ${slot.planned.eatingOutNotes}`}
        </p>
        <button
          type="button"
          className={`${secondaryButton} self-start`}
          disabled={eatIn.isPending}
          onClick={() => {
            eatIn.mutate({
              date: day.date,
              eatingOut: false,
              eatingTimeId: slot.planned.eatingTime?.id ?? "",
              personId: slot.person.personId,
            });
          }}
        >
          Not eating out
        </button>
        <ErrorText error={eatIn.error} />
      </div>
    );
  } else {
    const { portions } = slot.planned;
    const main = portions.find((portion) => portion.cookId !== "");
    const cook = week.cooks.find(
      (other) => main !== undefined && other.cook?.id === main.cookId
    );
    const dayPortions = slot.person.eatingTimes.flatMap(
      (planned) => planned.portions
    );
    const eatingTimeId = slot.planned.eatingTime?.id ?? "";
    const warnings = [
      ...slot.planned.warnings
        .filter((warning) => warning.severity === Severity.WARNING)
        .map((warning) => ({ eatingTimeId, warning })),
      ...slot.person.warnings
        .filter((warning) => warning.severity === Severity.WARNING)
        .map((warning) => ({ eatingTimeId: "", warning })),
    ];
    content = (
      <div className="flex flex-col gap-5">
        <Plate nutrients={slot.planned.nutrients} />
        {main !== undefined && (
          <SidesCard
            personId={slot.person.personId}
            eatingTimeId={eatingTimeId}
            date={day.date}
          />
        )}
        {warnings.map(({ eatingTimeId: about, warning }) => (
          <WarningFix
            key={`${about}-${warning.kind}-${warning.nutrient}`}
            warning={warning}
            personName={eaters.length > 1 ? "" : slot.person.personName}
            personId={slot.person.personId}
            date={day.date}
            eatingTimeId={about}
            portions={dayPortions}
          />
        ))}
        {main !== undefined && (
          <CookBar week={week} cookId={main.cookId} portions={portions} />
        )}
        {cook !== undefined && main?.leftover === false && (
          <CookActions
            planned={cook}
            weekCooks={week.cooks}
            onDeleted={onClose}
          />
        )}
        <div className="-mx-2 -mb-2 flex flex-wrap gap-x-1 gap-y-1">
          {main !== undefined && (
            <button
              type="button"
              className={ghostButton}
              onClick={() => {
                onEdit(
                  {
                    date: day.date,
                    eatingTimeId,
                    personId: slot.person.personId,
                  },
                  main
                );
              }}
            >
              Change the portion
            </button>
          )}
          {main !== undefined && main.recipeId !== "" && (
            <Link to={`/recipes/${main.recipeId}`} className={ghostButton}>
              The recipe
            </Link>
          )}
          <button
            type="button"
            className={ghostDangerButton}
            disabled={remove.isPending}
            onClick={() => {
              void Promise.all(
                portions
                  .filter((portion) => portion.portionId !== "")
                  .map(async (portion) =>
                    remove.mutateAsync({ id: portion.portionId })
                  )
              ).then(onClose);
            }}
          >
            Remove
          </button>
        </div>
        <ErrorText error={remove.error} />
      </div>
    );
  }

  return (
    <Modal open onClose={onClose} title={formatList(rowNames(row))}>
      <div className="flex flex-col gap-4">
        <p className="-mt-2 text-sm text-muted">{kicker}</p>
        {eaters.length > 1 && (
          <Segmented
            label="Whose"
            value={slot?.person.personId ?? ""}
            options={eaters.map((other) => ({
              label: other.person.personName,
              value: other.person.personId,
            }))}
            onChange={setPersonId}
          />
        )}
        {content}
      </div>
    </Modal>
  );
};

export default PlannedMealSheet;
