import type { Nutrients } from "gen/foods/v1/foods_pb";
import type { NutrientKey } from "helpers/nutrients";
import React from "react";

/**
 * A nutrient on the balance: its daily value, the general reference on
 * food labels, and whether more of it is better, like fiber, or less,
 * like sodium. They're general health metrics, not anyone's targets.
 */
interface BalanceNutrient {
  key: NutrientKey;
  label: string;
  dailyValue: number;
  moreIsBetter: boolean;
}

// BALANCE are the nutrients a recipe's balance shows, per serving.
const BALANCE: BalanceNutrient[] = [
  { dailyValue: 50, key: "proteinGrams", label: "Protein", moreIsBetter: true },
  { dailyValue: 28, key: "fiberGrams", label: "Fiber", moreIsBetter: true },
  {
    dailyValue: 2300,
    key: "sodiumMilligrams",
    label: "Sodium",
    moreIsBetter: false,
  },
  { dailyValue: 50, key: "sugarsGrams", label: "Sugars", moreIsBetter: false },
];

/*
 * HIGH and LOW are the shares of a daily value that count as a lot or a
 * little in a serving, as on food labels: 20% or more, 5% or less.
 */
const HIGH = 0.2;
const LOW = 0.05;

interface Rating {
  word: string;
  // Whether it's good news, which shows in pine, else in orange.
  good: boolean;
}

// RatingOf says what a serving's share of a daily value means.
const ratingOf = (nutrient: BalanceNutrient, share: number): Rating => {
  if (nutrient.moreIsBetter) {
    if (share >= HIGH) {
      return { good: true, word: "Good" };
    }
    return share <= LOW
      ? { good: false, word: "Low" }
      : { good: true, word: "Some" };
  }
  if (share >= HIGH) {
    return { good: false, word: "High" };
  }
  return { good: true, word: share <= LOW ? "Low" : "Some" };
};

interface RecipeBalanceProps {
  perServing: Nutrients | undefined;
}

/**
 * RecipeBalance shows at a glance how balanced a serving is: bars of its
 * protein, fiber, sodium and sugars against their daily values, each
 * rated in a word.
 */
const RecipeBalance: React.FC<RecipeBalanceProps> = ({ perServing }) => {
  const rows = BALANCE.flatMap((nutrient) => {
    const value = perServing?.[nutrient.key];
    if (value === undefined) {
      return [];
    }
    return [{ nutrient, share: value / nutrient.dailyValue }];
  });
  if (rows.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-col gap-2.5">
      <h2 className="text-xs font-bold tracking-[0.08em] text-muted uppercase">
        Balance per serving
      </h2>
      <div className="grid grid-cols-[4.5rem_minmax(0,1fr)_3rem] items-center gap-x-3 gap-y-2.5 text-sm">
        {rows.map(({ nutrient, share }) => {
          const rating = ratingOf(nutrient, share);
          return (
            <React.Fragment key={nutrient.key}>
              <span className="text-ink-soft">{nutrient.label}</span>
              <span
                className="h-2.5 rounded-full bg-ground-dark"
                title={`${Math.round(share * 100)}% of a daily value`}
              >
                <span
                  className={`block h-2.5 rounded-full ${rating.good ? "bg-pine" : "bg-cook"}`}
                  style={{ width: `${Math.min(share / 0.4, 1) * 100}%` }}
                />
              </span>
              <span
                className={`font-bold ${rating.good ? "text-pine" : "text-cook-dark"}`}
              >
                {rating.word}
              </span>
            </React.Fragment>
          );
        })}
      </div>
    </div>
  );
};

export default RecipeBalance;
