import FreezeDialog from "@components/plan/freeze-dialog";
import ConfirmButton from "@components/ui/confirm-button";
import { ErrorText } from "@components/ui/query-error";
import {
  pillButton,
  pillDangerButton,
  pillPrimaryButton,
} from "@components/ui/styles";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  deleteCook,
  setCookSupplier,
  updateCook,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { CookSupply, PlannedCook } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { formatGrams, formatList, formatShortDate } from "helpers/format";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { Link } from "react-router";

interface SupplyPickerProps {
  cookId: string;
  supply: CookSupply;
  weekCooks: PlannedCook[];
  disabled: boolean;
}

/**
 * SupplyPicker says which cook makes a component of a cook, e.g. "Cooked
 * chickpeas from Fri 6 Nov", to choose another of the week's cooks of it,
 * like one pot for two meals.
 */
const SupplyPicker: React.FC<SupplyPickerProps> = ({
  cookId,
  supply,
  weekCooks,
  disabled,
}) => {
  const queryClient = useQueryClient();
  const set = useMutation(setCookSupplier, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [PlanService]);
    },
  });
  const options = weekCooks.filter(
    (other) =>
      other.cook !== undefined &&
      other.cook.recipeId === supply.componentRecipeId
  );
  return (
    <div className="flex flex-col gap-0.5">
      <label className="flex flex-wrap items-center gap-1.5 text-xs text-muted">
        <span>
          {supply.supplierRecipeName}{" "}
          {supply.fromFreezer ? "from the freezer, cooked" : "from"}
        </span>
        <select
          className="rounded border border-line bg-white px-1 py-0.5 text-xs text-ink-soft"
          value={supply.supplierCookId}
          disabled={disabled || set.isPending || options.length < 2}
          title="The cook that makes it: choose another to share one pot between meals"
          onChange={(event) => {
            set.mutate({
              componentRecipeId: supply.componentRecipeId,
              cookId,
              supplierCookId: event.target.value,
            });
          }}
        >
          {!options.some(
            (other) => other.cook?.id === supply.supplierCookId
          ) && (
            <option value={supply.supplierCookId}>
              {formatShortDate(supply.supplierDate)}
            </option>
          )}
          {options.map((other) => (
            <option key={other.cook?.id} value={other.cook?.id}>
              {formatShortDate(other.cook?.date ?? "")}
              {other.cook?.id === supply.supplierCookId ? "" : " (share)"}
            </option>
          ))}
        </select>
      </label>
      <ErrorText error={set.error} />
    </div>
  );
};

interface CookActionsProps {
  planned: PlannedCook;
  // The week's cooks, which can supply its components.
  weekCooks: PlannedCook[];
  // Called once the cook is deleted, as the meal is gone with it.
  onDeleted: () => void;
  // Whether to leave out not cooking it, as a meal offers removing itself.
  hideDelete?: boolean | undefined;
}

/**
 * CookActions are what can be done with a meal's cook: mark it cooked,
 * which takes its ingredients out of the pantry, freeze its leftovers,
 * choose which cook makes its components, or not cook it at all.
 */
const CookActions: React.FC<CookActionsProps> = ({
  planned,
  weekCooks,
  onDeleted,
  hideDelete = false,
}) => {
  const queryClient = useQueryClient();
  const [freezing, setFreezing] = useState(false);
  const [note, setNote] = useState("");
  const done = useMutation(updateCook, {
    onSuccess: async (response) => {
      const amounts = response.pantryChanges.map(
        (change) => `${formatGrams(change.grams)} of ${change.foodName}`
      );
      if (amounts.length === 0) {
        setNote("");
      } else if (response.pantryRestored) {
        setNote(`Put ${formatList(amounts)} back into the pantry.`);
      } else {
        setNote(`Took ${formatList(amounts)} out of the pantry.`);
      }
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
      ]);
    },
  });
  const remove = useMutation(deleteCook, {
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
      ]);
      onDeleted();
    },
  });
  const { cook } = planned;
  if (cook === undefined) {
    return null;
  }
  return (
    <section className="flex flex-col gap-2">
      {cook.supplies.map((supply) => (
        <SupplyPicker
          key={supply.componentRecipeId}
          cookId={cook.id}
          supply={supply}
          weekCooks={weekCooks}
          disabled={false}
        />
      ))}
      <div className="flex flex-wrap gap-2">
        {/* Cook mode cooks this cook's servings, and marks it cooked. */}
        {!cook.done && (
          <Link
            to={`/recipes/${cook.recipeId}/cook?cook=${cook.id}&servings=${cook.servings}`}
            className={pillPrimaryButton}
          >
            Start cooking
          </Link>
        )}
        <button
          type="button"
          className={cook.done ? pillPrimaryButton : pillButton}
          disabled={done.isPending}
          aria-pressed={cook.done}
          onClick={() => {
            done.mutate({
              cook: { done: !cook.done, id: cook.id },
              updateMask: { paths: ["done"] },
            });
          }}
        >
          {cook.done ? "Cooked ✓" : "Mark as cooked"}
        </button>
        {(planned.leftServings > 0 || cook.frozenServings > 0) && (
          <button
            type="button"
            className={pillButton}
            onClick={() => {
              setFreezing(true);
            }}
          >
            {cook.frozenServings > 0 ? "Frozen servings" : "Freeze leftovers"}
          </button>
        )}
        {!hideDelete && (
          <ConfirmButton
            className={pillDangerButton}
            title={`Don't cook ${cook.recipeName}?`}
            message="The cook is deleted with all its portions, leftovers on other days too."
            disabled={remove.isPending}
            onConfirm={() => {
              remove.mutate({ id: cook.id });
            }}
          >
            Don&apos;t cook it
          </ConfirmButton>
        )}
      </div>
      {note !== "" && <p className="text-sm text-muted">{note}</p>}
      <ErrorText error={done.error ?? remove.error} />
      {freezing && (
        <FreezeDialog
          planned={planned}
          onClose={() => {
            setFreezing(false);
          }}
        />
      )}
    </section>
  );
};

export default CookActions;
