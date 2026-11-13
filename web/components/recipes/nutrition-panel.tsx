import { linkClass, tableClass, thClass } from "@components/ui/styles";
import {
  ChevronDownIcon,
  InformationCircleIcon,
} from "@heroicons/react/24/outline";
import type { Nutrients } from "gen/foods/v1/foods_pb";
import type { IngredientIssue, Recipe } from "gen/recipes/v1/recipes_pb";
import { isVolumeUnit } from "helpers/measures";
import type { NutrientInfo } from "helpers/nutrients";
import {
  NUTRIENT_GROUPS,
  NUTRIENTS,
  formatAmount,
  formatNutrient,
  nutrientLabel,
} from "helpers/nutrients";
import React, { useState } from "react";
import { Link } from "react-router";

interface NutrientValueProps {
  nutrients: Nutrients | undefined;
  nutrient: NutrientInfo;
  // Whether some ingredients lack the nutrient, so it's a lower bound.
  incomplete: boolean;
}

/**
 * NutrientValue shows an amount of a nutrient, flagging unknown and partly
 * known ones.
 */
const NutrientValue: React.FC<NutrientValueProps> = ({
  nutrients,
  nutrient,
  incomplete,
}) => {
  const value = nutrients?.[nutrient.key];
  if (value === undefined) {
    return <span className="text-muted/70 italic">Unknown</span>;
  }
  if (!incomplete) {
    return <span>{formatNutrient(value, nutrient.unit)}</span>;
  }
  return (
    <span title="Some ingredients lack this nutrient, so there's at least this much.">
      ≥ {formatNutrient(value, nutrient.unit)}
    </span>
  );
};

interface NutritionPanelProps {
  recipe: Recipe;
}

/**
 * IssueProblem says what an ingredient lacks for the recipe's nutrition,
 * and what to do, after its food's name, e.g. "has no density, so it's
 * left out. Set its density."
 */
export const issueProblem = (
  recipe: Recipe,
  issue: IngredientIssue | undefined
): string => {
  if (issue === undefined) {
    return "";
  }
  const unit = recipe.ingredients[issue.ingredientIndex]?.unit;
  if (issue.unmatched) {
    return "has no food yet, so it's left out. Choose its food.";
  }
  if (issue.unknownWeight) {
    return unit !== undefined && isVolumeUnit(unit)
      ? "has no density, so it's left out. Set its density."
      : "has no weight per piece, so it's left out. Set its weight per piece.";
  }
  return `has no data for ${issue.unknownNutrients
    .map((field) => nutrientLabel(field))
    .join(", ")}.`;
};

/**
 * NutritionPanel shows a recipe's nutrition per serving and per 100 g,
 * every nutrient tracked, macronutrients first, behind a chevron, with a
 * note of the ingredients whose foods lack data, to fix.
 */
const NutritionPanel: React.FC<NutritionPanelProps> = ({ recipe }) => {
  const { nutrition } = recipe;
  const incomplete = new Set(nutrition?.incompleteNutrients ?? []);
  const issues = nutrition?.issues ?? [];
  const [open, setOpen] = useState(false);
  return (
    <section className="overflow-hidden rounded-2xl border border-line bg-white">
      <button
        type="button"
        aria-expanded={open}
        className="flex w-full items-center justify-between gap-2 bg-ground-dark/60 px-4 py-3 text-left font-bold text-ink-soft"
        onClick={() => {
          setOpen(!open);
        }}
      >
        All nutrients, per serving
        <ChevronDownIcon
          className={`size-5 shrink-0 transition-transform ${open ? "rotate-180" : ""}`}
          aria-hidden="true"
        />
      </button>
      {open && (
        <div className="flex flex-col gap-4 p-4">
          <p className="text-sm text-muted">
            A serving is one of {recipe.servings}, weighing about{" "}
            <span className="font-medium text-ink-soft tabular-nums">
              {formatAmount(nutrition?.gramsPerServing ?? 0)} g
            </span>{" "}
            raw.
          </p>
          <div className="overflow-x-auto">
            <table className={`${tableClass} tabular-nums`}>
              <thead>
                <tr>
                  <th className={thClass}>Nutrient</th>
                  <th className={`${thClass} text-right`}>Serving</th>
                  <th className={`${thClass} text-right`}>100 g</th>
                </tr>
              </thead>
              {NUTRIENT_GROUPS.map((group) => (
                <tbody key={group} className="divide-y divide-line">
                  <tr>
                    <th
                      colSpan={3}
                      className="bg-ground px-2 py-1 text-left text-xs font-semibold text-muted"
                    >
                      {group}
                    </th>
                  </tr>
                  {NUTRIENTS.filter((nutrient) => nutrient.group === group).map(
                    (nutrient) => (
                      <tr key={nutrient.key}>
                        <td className="px-2 py-1.5 text-ink-soft">
                          {nutrient.label}
                        </td>
                        <td className="px-2 py-1.5 text-right whitespace-nowrap">
                          <NutrientValue
                            nutrients={nutrition?.perServing}
                            nutrient={nutrient}
                            incomplete={incomplete.has(nutrient.field)}
                          />
                        </td>
                        <td className="px-2 py-1.5 text-right whitespace-nowrap text-muted">
                          <NutrientValue
                            nutrients={nutrition?.perHundredGrams}
                            nutrient={nutrient}
                            incomplete={incomplete.has(nutrient.field)}
                          />
                        </td>
                      </tr>
                    )
                  )}
                </tbody>
              ))}
            </table>
          </div>
          {issues.length > 0 && (
            <div className="flex flex-col gap-2 rounded-xl bg-ground-dark/60 p-3 text-sm text-muted">
              <p className="flex items-center gap-1.5 font-medium text-ink-soft">
                <InformationCircleIcon className="size-4" aria-hidden="true" />
                Some ingredients lack data, so these may be a little low
              </p>
              <ul className="flex flex-col gap-1">
                {issues.map((issue) => {
                  const problem = issueProblem(recipe, issue);
                  return (
                    <li key={issue.ingredientIndex}>
                      {issue.foodId === "" ? (
                        <span className="font-medium">{issue.foodName}</span>
                      ) : (
                        <Link
                          to={`/foods/${issue.foodId}`}
                          className={linkClass}
                        >
                          {issue.foodName}
                        </Link>
                      )}{" "}
                      {problem}
                    </li>
                  );
                })}
              </ul>
            </div>
          )}
        </div>
      )}
    </section>
  );
};

export default NutritionPanel;
