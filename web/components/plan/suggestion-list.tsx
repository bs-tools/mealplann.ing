import type { MessageInitShape } from "@bufbuild/protobuf";
import Loading from "@components/loading";
import {
  KIND_LABELS,
  WarningList,
  formatPoints,
} from "@components/plan/plan-bits";
import QueryError from "@components/ui/query-error";
import { useQuery } from "@connectrpc/connect-query";
import { suggestRecipes } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type {
  SuggestRecipesRequestSchema,
  Suggestion,
} from "gen/suggestions/v1/suggestions_pb";
import { formatShortDate, formatList } from "helpers/format";
import { leftoverLabel, suggestionKey } from "helpers/suggestions";
import React, { useState } from "react";
import { Link } from "react-router";

/**
 * PortionsLine describes who a suggestion feeds, e.g. "Anna 196 g at
 * Dinner + 181 g at Evening · Ben 378 g at Dinner".
 */
const portionsLine = (suggestion: Suggestion): string => {
  const byPerson = new Map<string, string[]>();
  const first = suggestion.portions[0]?.date ?? "";
  for (const portion of suggestion.portions) {
    const parts = byPerson.get(portion.personName) ?? [];
    const later =
      portion.date === first ? "" : `, ${formatShortDate(portion.date)}`;
    const weight =
      portion.grams > 0 ? `${Math.round(portion.grams)} g` : "a portion";
    parts.push(`${weight} at ${portion.eatingTimeName}${later}`);
    byPerson.set(portion.personName, parts);
  }
  return [...byPerson.entries()]
    .map(([name, parts]) => `${name} ${parts.join(" + ")}`)
    .join(" · ");
};

// FirstReason returns a suggestion's best reason, or "" if none.
const firstReason = (suggestion: Suggestion): string =>
  suggestion.components.find((component) => component.points > 0)?.reason ?? "";

interface SuggestionRowProps {
  suggestion: Suggestion;
  // Whether it's being planned, and whether planning is disabled.
  planning: boolean;
  disabled: boolean;
  onPlan: () => void;
}

/**
 * SuggestionRow shows a suggested recipe as a row: its name and best
 * reason, and whether it breaks a rule, with every reason, who it feeds
 * and what it needs bought a tap away.
 */
const SuggestionRow: React.FC<SuggestionRowProps> = ({
  suggestion,
  planning,
  disabled,
  onPlan,
}) => {
  const [expanded, setExpanded] = useState(false);
  const toBuy = suggestion.itemsToBuy.map(
    (item) => `${item.foodName} (${item.amountText})`
  );
  const reason = firstReason(suggestion);
  return (
    <li className="flex flex-col gap-2 py-3">
      <div className="flex items-start gap-3">
        <div className="flex min-w-0 grow flex-col gap-0.5">
          <Link
            to={`/recipes/${suggestion.recipeId}`}
            className="leading-snug font-bold text-ink-soft"
          >
            {suggestion.recipeName}
          </Link>
          <p className="text-sm text-muted">
            {[
              suggestion.leftoverOf === undefined
                ? ""
                : leftoverLabel(suggestion.leftoverOf),
              suggestion.side === undefined
                ? ""
                : `with ${suggestion.side.recipeId === "" ? suggestion.side.foodName : suggestion.side.recipeName}`,
              reason,
            ]
              .filter((part) => part !== "")
              .join(" · ")}
          </p>
          <div className="mt-1 flex flex-wrap items-center gap-2">
            {!suggestion.fitsRules && (
              <span className="rounded-full bg-warn-tint px-2.5 py-0.5 text-xs font-bold text-warn-dark">
                Breaks someone&apos;s rules
              </span>
            )}
            <button
              type="button"
              className="text-xs font-bold text-pine"
              aria-expanded={expanded}
              onClick={() => {
                setExpanded(!expanded);
              }}
            >
              {expanded ? "Less" : "Why"}
            </button>
          </div>
        </div>
        <button
          type="button"
          className="shrink-0 rounded-full bg-pine-50 px-3.5 py-2 text-sm font-bold text-pine hover:bg-pine-100 disabled:opacity-50"
          disabled={disabled}
          onClick={onPlan}
        >
          {planning ? "Planning…" : "Plan this"}
        </button>
      </div>
      {expanded && (
        <div className="flex flex-col gap-2 rounded-xl bg-ground p-3 text-sm">
          <ul className="flex flex-col gap-1">
            {suggestion.components.map((component) => (
              <li
                key={`${component.kind}-${component.reason}`}
                className="flex gap-2"
                title={KIND_LABELS[component.kind]}
              >
                <span
                  className={`w-10 shrink-0 text-right font-bold tabular-nums ${component.points < 0 ? "text-warn-dark" : "text-pine"}`}
                >
                  {formatPoints(component.points)}
                </span>
                <span className="text-ink-soft">{component.reason}</span>
              </li>
            ))}
          </ul>
          <p className="text-muted">
            {portionsLine(suggestion)}
            {" · "}
            {toBuy.length === 0
              ? "Nothing more to buy"
              : `To buy: ${formatList(toBuy)}`}
          </p>
          <WarningList warnings={suggestion.warnings} />
        </div>
      )}
    </li>
  );
};

interface SuggestionListProps {
  // The slot to suggest for.
  request: MessageInitShape<typeof SuggestRecipesRequestSchema>;
  // What's being planned, as its suggestionKey, empty if nothing.
  planningId: string;
  // Whether planning is disabled, e.g. while one is being planned.
  disabled: boolean;
  onPlan: (suggestion: Suggestion) => void;
}

/**
 * SuggestionList suggests the household's recipes for a slot of the plan,
 * best first, with why, each to plan in one click.
 */
const SuggestionList: React.FC<SuggestionListProps> = ({
  request,
  planningId,
  disabled,
  onPlan,
}) => {
  const suggestions = useQuery(suggestRecipes, request);
  if (suggestions.isPending) {
    return <Loading />;
  }
  if (suggestions.isError) {
    return (
      <QueryError
        error={suggestions.error}
        onRetry={() => {
          void suggestions.refetch();
        }}
      />
    );
  }
  const list = suggestions.data.suggestions;
  if (list.length === 0) {
    return (
      <p className="text-sm text-ink-soft">
        There are no recipes to suggest yet.{" "}
        <Link
          to="/recipes/new"
          className="font-medium text-pine hover:underline"
        >
          Add a recipe
        </Link>{" "}
        first.
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-2">
      <p className="text-sm text-muted">
        Best first, for what the week buys, the pantry, everyone&apos;s targets
        and rules, and the shop on {formatShortDate(suggestions.data.shopDate)}.
      </p>
      <ul className="divide-y divide-line rounded-2xl border border-line bg-white px-4">
        {list.map((suggestion) => (
          <SuggestionRow
            key={suggestionKey(suggestion)}
            suggestion={suggestion}
            planning={planningId === suggestionKey(suggestion)}
            disabled={disabled}
            onPlan={() => {
              onPlan(suggestion);
            }}
          />
        ))}
      </ul>
    </div>
  );
};

export default SuggestionList;
