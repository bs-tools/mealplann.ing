import Card from "@components/ui/card";
import { linkClass, tableClass, thClass } from "@components/ui/styles";
import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import type { Nutrients } from "gen/foods/v1/foods_pb";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import type { Recipe } from "gen/recipes/v1/recipes_pb";
import type { NutrientInfo } from "helpers/nutrients";
import {
  NUTRIENT_GROUPS,
  NUTRIENTS,
  formatAmount,
  formatNutrient,
  nutrientLabel,
} from "helpers/nutrients";
import React from "react";
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
    return <span className="text-gray-400 italic">Unknown</span>;
  }
  if (!incomplete) {
    return <span>{formatNutrient(value, nutrient.unit)}</span>;
  }
  return (
    <span
      className="inline-flex items-center gap-1 text-amber-800"
      title="Some ingredients lack this nutrient, so there's at least this much."
    >
      ≥ {formatNutrient(value, nutrient.unit)}
      <ExclamationTriangleIcon className="size-3.5" aria-hidden="true" />
    </span>
  );
};

interface NutritionPanelProps {
  recipe: Recipe;
}

/**
 * NutritionPanel shows a recipe's nutrition per serving and per 100 g, with
 * every nutrient tracked, and the ingredients whose foods lack data, to fix.
 */
const NutritionPanel: React.FC<NutritionPanelProps> = ({ recipe }) => {
  const { nutrition } = recipe;
  const incomplete = new Set(nutrition?.incompleteNutrients ?? []);
  const issues = nutrition?.issues ?? [];
  return (
    <Card title="Nutrition per serving">
      <div className="flex flex-col gap-4">
        <p className="text-sm text-gray-600">
          A serving is one of {recipe.servings}, weighing about{" "}
          <span className="font-medium text-gray-900 tabular-nums">
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
              <tbody key={group} className="divide-y divide-gray-100">
                <tr>
                  <th
                    colSpan={3}
                    className="bg-gray-50 px-2 py-1 text-left text-xs font-semibold text-gray-600"
                  >
                    {group}
                  </th>
                </tr>
                {NUTRIENTS.filter((nutrient) => nutrient.group === group).map(
                  (nutrient) => (
                    <tr key={nutrient.key}>
                      <td className="px-2 py-1.5 text-gray-800">
                        {nutrient.label}
                      </td>
                      <td className="px-2 py-1.5 text-right whitespace-nowrap">
                        <NutrientValue
                          nutrients={nutrition?.perServing}
                          nutrient={nutrient}
                          incomplete={incomplete.has(nutrient.field)}
                        />
                      </td>
                      <td className="px-2 py-1.5 text-right whitespace-nowrap text-gray-600">
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
          <div className="flex flex-col gap-2 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
            <p className="flex items-center gap-1.5 font-medium">
              <ExclamationTriangleIcon className="size-4" aria-hidden="true" />
              Some ingredients lack data
            </p>
            <ul className="flex flex-col gap-1">
              {issues.map((issue) => {
                const unit = recipe.ingredients[issue.ingredientIndex]?.unit;
                let problem = "";
                if (issue.unknownWeight) {
                  problem =
                    unit === AmountUnit.MILLILITERS
                      ? "has no density, so it's left out. Set its density."
                      : "has no weight per piece, so it's left out. Set its weight per piece.";
                } else {
                  problem = `has no data for ${issue.unknownNutrients
                    .map((field) => nutrientLabel(field))
                    .join(", ")}.`;
                }
                return (
                  <li key={issue.ingredientIndex}>
                    <Link to={`/foods/${issue.foodId}`} className={linkClass}>
                      {issue.foodName}
                    </Link>{" "}
                    {problem}
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </div>
    </Card>
  );
};

export default NutritionPanel;
