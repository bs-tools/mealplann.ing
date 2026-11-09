import type { MessageInitShape } from "@bufbuild/protobuf";
import Loading from "@components/loading";
import { ComponentChip, WarningList } from "@components/plan/plan-bits";
import { Badge } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import { primaryButton } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { SparklesIcon } from "@heroicons/react/24/outline";
import { suggestRecipes } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type {
  SuggestRecipesRequestSchema,
  Suggestion,
} from "gen/suggestions/v1/suggestions_pb";
import { formatShortDate, formatList } from "helpers/format";
import { formatAmount } from "helpers/nutrients";
import { leftoverLabel, suggestionKey } from "helpers/suggestions";
import React, { useState } from "react";
import { Link } from "react-router";

// SHOWN_REASONS is how many of the best reasons show before expanding.
const SHOWN_REASONS = 4;

// MoreLabel says what expanding a suggestion shows.
const moreLabel = (reasons: number, warnings: number): string => {
  const parts: string[] = [];
  if (reasons > 0) {
    parts.push(reasons === 1 ? "1 more reason" : `${reasons} more reasons`);
  }
  if (warnings > 0) {
    parts.push("what to change");
  }
  return `Show ${formatList(parts)}`;
};

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

interface SuggestionCardProps {
  suggestion: Suggestion;
  // The highest score of the list, which the score bars are relative to.
  maxScore: number;
  // Whether it's being planned, and whether planning is disabled.
  planning: boolean;
  disabled: boolean;
  onPlan: () => void;
}

/**
 * SuggestionCard shows a suggested recipe: its score as a bar, the reasons
 * as chips, who it feeds, what it needs bought, and the rules it breaks.
 */
const SuggestionCard: React.FC<SuggestionCardProps> = ({
  suggestion,
  maxScore,
  planning,
  disabled,
  onPlan,
}) => {
  const [expanded, setExpanded] = useState(false);
  const fraction = maxScore > 0 ? Math.max(suggestion.score, 0) / maxScore : 0;
  const toBuy = suggestion.itemsToBuy.map(
    (item) => `${item.foodName} (${item.amountText})`
  );
  /*
   * The best reasons and every penalty show at first, the rest and the
   * rules' advice when expanded, so a list of suggestions stays scannable.
   */
  const shown = expanded
    ? suggestion.components
    : suggestion.components.filter(
        (component, index) => index < SHOWN_REASONS || component.points < 0
      );
  const hidden = suggestion.components.length - shown.length;
  return (
    <li className="flex flex-col gap-2 rounded-md border border-gray-200 bg-white p-3">
      <div className="flex items-start gap-3">
        <div className="flex min-w-0 grow flex-col gap-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <Link
              to={`/recipes/${suggestion.recipeId}`}
              className="font-medium text-gray-900 hover:underline"
            >
              {suggestion.recipeName}
            </Link>
            {suggestion.leftoverOf !== undefined && (
              <Badge>{leftoverLabel(suggestion.leftoverOf)}</Badge>
            )}
            {!suggestion.fitsRules && (
              <Badge className="bg-amber-50 text-amber-900 ring-amber-300">
                Breaks rules
              </Badge>
            )}
          </div>
          <div
            className="flex items-center gap-2"
            title="Score, relative to the best suggestion"
          >
            <div className="h-1.5 w-full max-w-48 min-w-16 overflow-hidden rounded-full bg-gray-200">
              <div
                className={`h-full rounded-full ${suggestion.fitsRules ? "bg-brand-500" : "bg-amber-500"}`}
                style={{ width: `${fraction * 100}%` }}
              />
            </div>
            <span className="text-xs whitespace-nowrap text-gray-600 tabular-nums">
              {formatAmount(suggestion.score)} points
            </span>
          </div>
        </div>
        <button
          type="button"
          className={`${primaryButton} shrink-0`}
          disabled={disabled}
          onClick={onPlan}
        >
          {planning ? "Planning…" : "Plan this"}
        </button>
      </div>
      {shown.length > 0 && (
        <ul className="flex flex-wrap gap-1">
          {shown.map((component) => (
            <ComponentChip
              key={`${component.kind}-${component.reason}`}
              component={component}
            />
          ))}
        </ul>
      )}
      <p className="text-xs text-gray-600">
        {portionsLine(suggestion)}
        {" · "}
        {toBuy.length === 0
          ? "Nothing more to buy"
          : `To buy: ${formatList(toBuy)}`}
      </p>
      {expanded && <WarningList warnings={suggestion.warnings} />}
      {(hidden > 0 || suggestion.warnings.length > 0) && (
        <button
          type="button"
          className="self-start text-xs font-medium text-brand-700 hover:underline"
          aria-expanded={expanded}
          onClick={() => {
            setExpanded(!expanded);
          }}
        >
          {expanded
            ? "Show less"
            : moreLabel(hidden, suggestion.warnings.length)}
        </button>
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
      <p className="text-sm text-gray-700">
        There are no recipes to suggest yet.{" "}
        <Link
          to="/recipes/new"
          className="font-medium text-brand-600 hover:underline"
        >
          Add a recipe
        </Link>{" "}
        first.
      </p>
    );
  }
  const maxScore = Math.max(...list.map((suggestion) => suggestion.score));
  return (
    <div className="flex flex-col gap-2">
      <p className="flex items-start gap-1.5 text-xs text-gray-600">
        <SparklesIcon
          className="mt-px size-3.5 shrink-0 text-brand-600"
          aria-hidden="true"
        />
        <span>
          Ranked by what the rest of the week buys, the pantry, everyone&apos;s
          targets and rules, and a single shop on{" "}
          {formatShortDate(suggestions.data.shopDate)}.
        </span>
      </p>
      <ul className="flex flex-col gap-2">
        {list.map((suggestion) => (
          <SuggestionCard
            key={suggestionKey(suggestion)}
            suggestion={suggestion}
            maxScore={maxScore}
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
