import { clone } from "@bufbuild/protobuf";
import { Badge } from "@components/ui/misc";
import { ErrorText } from "@components/ui/query-error";
import {
  cardClass,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { useMutation } from "@connectrpc/connect-query";
import { useQueryClient } from "@tanstack/react-query";
import type { Nutrients } from "gen/foods/v1/foods_pb";
import { HouseholdMeal } from "gen/people/v1/people_pb";
import { planMeal } from "gen/plans/v1/plans-PlanService_connectquery";
import { PlanService } from "gen/plans/v1/plans_pb";
import {
  createRecipe,
  updateRecipe,
} from "gen/recipes/v1/recipes-RecipeService_connectquery";
import { RecipeSchema, RecipeService } from "gen/recipes/v1/recipes_pb";
import type { Bowl, BowlPart } from "gen/suggestions/v1/suggestions_pb";
import { BowlRole } from "gen/suggestions/v1/suggestions_pb";
import { today } from "helpers/dates";
import { invalidateServices } from "helpers/query";
import React from "react";
import { useNavigate } from "react-router";

// ROLES are a bowl's roles in the order it's built, with their names.
export const ROLES: { role: BowlRole; label: string; many: boolean }[] = [
  { label: "Carbs", many: true, role: BowlRole.CARB },
  { label: "Protein", many: true, role: BowlRole.PROTEIN },
  { label: "Vegetables", many: true, role: BowlRole.VEGETABLE },
  { label: "Sauce", many: false, role: BowlRole.SAUCE },
];

/**
 * OnHandBadge says whether a part is on hand, and where, or that it's to
 * buy.
 */
export const OnHandBadge: React.FC<{ part: BowlPart }> = ({ part }) => {
  if (!part.onHand) {
    return (
      <Badge className="bg-warn-tint text-warn-dark ring-warn-line">
        To buy
      </Badge>
    );
  }
  let label = "On hand";
  if (part.leftovers) {
    label = "Cooked";
  } else if (part.useSoon) {
    label = "Use soon";
  }
  return (
    <Badge
      className={
        part.useSoon || part.leftovers
          ? "bg-pine text-white ring-pine"
          : "bg-pine-50 text-pine-dark ring-pine-200"
      }
      title={part.onHandNote}
    >
      {label}
    </Badge>
  );
};

// MACROS are the nutrients a bowl shows, per serving.
const MACROS: { key: keyof Nutrients; label: string; unit: string }[] = [
  { key: "energyKcal", label: "Energy", unit: "kcal" },
  { key: "proteinGrams", label: "Protein", unit: "g" },
  { key: "carbohydrateGrams", label: "Carbs", unit: "g" },
  { key: "fatGrams", label: "Fat", unit: "g" },
  { key: "fiberGrams", label: "Fiber", unit: "g" },
];

/** Macros shows a bowl's energy and macronutrients for a serving. */
export const Macros: React.FC<{ nutrients: Nutrients | undefined }> = ({
  nutrients,
}) => (
  <dl className="grid grid-cols-5 gap-1.5 text-center">
    {MACROS.map(({ key, label, unit }) => {
      const value = nutrients?.[key];
      return (
        <div key={key} className="rounded-xl bg-ground-dark/60 px-1 py-2">
          <dt className="text-[11px] font-bold tracking-wide text-muted uppercase">
            {label}
          </dt>
          <dd className="text-base font-bold text-ink-soft tabular-nums">
            {typeof value === "number" ? Math.round(value) : "–"}
            <span className="text-xs font-normal text-muted"> {unit}</span>
          </dd>
        </div>
      );
    })}
  </dl>
);

/**
 * UseBowlActions saves a bowl as its recipe, the one made before if any,
 * and then cooks it now, planned for today's next meal and opened in cook
 * mode, or opens the plan to plan it.
 */
export const useBowlActions = (): {
  cook: (bowl: Bowl) => void;
  plan: (bowl: Bowl) => void;
  pending: boolean;
  error: ConnectError | null;
} => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const create = useMutation(createRecipe);
  const update = useMutation(updateRecipe);
  const plan = useMutation(planMeal);
  const save = async (bowl: Bowl): Promise<string> => {
    if (bowl.existingRecipeId !== "" && bowl.recipe !== undefined) {
      /*
       * The bowl made before is updated, so it's one recipe however often
       * it's made.
       */
      const recipe = clone(RecipeSchema, bowl.recipe);
      recipe.id = bowl.existingRecipeId;
      await update.mutateAsync({
        recipe,
        updateMask: {
          paths: [
            "ingredients",
            "ingredients.alternatives",
            "instructions",
            "servings",
            "description",
            "prep_seconds",
            "cook_seconds",
          ],
        },
      });
      return bowl.existingRecipeId;
    }
    const created = await create.mutateAsync({ recipe: bowl.recipe });
    return created.recipe?.id ?? "";
  };
  return {
    cook: (bowl) => {
      void (async () => {
        const id = await save(bowl);
        // Before mid-afternoon it's lunch, after it dinner.
        const meal =
          new Date().getHours() < 15
            ? HouseholdMeal.LUNCH
            : HouseholdMeal.DINNER;
        const planned = await plan.mutateAsync({
          date: today(),
          meal,
          recipeId: id,
          servings: bowl.servings,
        });
        await invalidateServices(queryClient, [RecipeService, PlanService]);
        await navigate(
          `/recipes/${id}/cook?${new URLSearchParams({
            cook: planned.cook?.id ?? "",
            servings: String(bowl.servings),
          }).toString()}`
        );
      })();
    },
    error: create.error ?? update.error ?? plan.error,
    pending: create.isPending || update.isPending || plan.isPending,
    plan: (bowl) => {
      void (async () => {
        const id = await save(bowl);
        await invalidateServices(queryClient, [RecipeService]);
        await navigate(`/plan?plan=${id}`);
      })();
    },
  };
};

interface BowlCardProps {
  bowl: Bowl;
  // Opens the bowl in the builder, to change it.
  onChange?: () => void;
}

/**
 * BowlCard shows a bowl: its parts by role, in the amounts for its
 * servings, whether each is on hand, its macros for a serving and why it
 * was suggested, to cook now or plan.
 */
const BowlCard: React.FC<BowlCardProps> = ({ bowl, onChange }) => {
  const actions = useBowlActions();
  return (
    <article className={`${cardClass} flex flex-col gap-4 p-4`}>
      <div className="flex flex-col gap-1">
        {bowl.style !== "" && (
          <span className="text-xs font-bold tracking-wide text-pine uppercase">
            {bowl.style}
          </span>
        )}
        <h2 className="text-lg leading-snug font-bold text-ink-soft">
          {bowl.name}
        </h2>
        {bowl.reasons.length > 0 && (
          <p className="text-sm text-muted">{bowl.reasons.join(" · ")}</p>
        )}
      </div>
      <BowlParts parts={bowl.parts} servings={bowl.servings} />
      <div className="flex flex-col gap-1.5">
        <span className="text-xs text-muted">A serving</span>
        <Macros nutrients={bowl.nutrients} />
      </div>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          className={primaryButton}
          disabled={actions.pending}
          onClick={() => {
            actions.cook(bowl);
          }}
        >
          Cook now
        </button>
        <button
          type="button"
          className={secondaryButton}
          disabled={actions.pending}
          onClick={() => {
            actions.plan(bowl);
          }}
        >
          Plan it
        </button>
        {onChange !== undefined && (
          <button
            type="button"
            className={secondaryButton}
            disabled={actions.pending}
            onClick={onChange}
          >
            Change it
          </button>
        )}
      </div>
      <ErrorText error={actions.error} />
    </article>
  );
};

/**
 * BowlParts lists a bowl's parts by role, each with its amount for the
 * servings and whether it's on hand.
 */
export const BowlParts: React.FC<{ parts: BowlPart[]; servings: number }> = ({
  parts,
  servings,
}) => (
  <dl className="flex flex-col gap-2.5">
    {ROLES.map(({ role, label }) => {
      const ofRole = parts.filter((part) => part.role === role);
      if (ofRole.length === 0) {
        return null;
      }
      return (
        <div key={role} className="flex flex-col gap-1">
          <dt className="text-xs font-bold tracking-wide text-muted uppercase">
            {label}
          </dt>
          {ofRole.map((part) => (
            <dd
              key={part.key}
              className="flex items-start justify-between gap-3"
            >
              <span className="min-w-0 text-sm text-ink-soft">
                <span className="font-medium">{part.name}</span>
                <span className="text-muted">
                  {" "}
                  · {formatPartAmount(part, servings)}
                </span>
                {part.detail !== "" && (
                  <span className="block text-xs text-muted">
                    {part.detail}
                  </span>
                )}
              </span>
              <OnHandBadge part={part} />
            </dd>
          ))}
        </div>
      );
    })}
  </dl>
);

/**
 * FormatPartAmount formats how much of a part servings of a bowl have,
 * like "140 g" or "1.5 servings".
 */
export const formatPartAmount = (part: BowlPart, servings: number): string => {
  const total = part.amount * servings;
  if (part.foodId !== "") {
    return `${Math.round(total / 5) * 5} g`;
  }
  const quarters = Math.round(total * 4) / 4;
  const unit = part.role === BowlRole.SAUCE ? "servings' worth" : "servings";
  if (quarters === 1) {
    return part.role === BowlRole.SAUCE ? "1 serving's worth" : "1 serving";
  }
  return `${quarters} ${unit}`;
};

export default BowlCard;
