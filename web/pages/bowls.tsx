import BowlCard, {
  BowlParts,
  Macros,
  OnHandBadge,
  ROLES,
  useBowlActions,
} from "@components/bowls/bowl-card";
import Loading from "@components/loading";
import { EmptyState, PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  cardClass,
  inputClass,
  linkClass,
  primaryButton,
  secondaryButton,
  sectionLabelClass,
} from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import { ArrowPathIcon, XMarkIcon } from "@heroicons/react/24/outline";
import {
  composeBowl,
  listBowlParts,
  suggestBowls,
} from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import type { Bowl, BowlPart } from "gen/suggestions/v1/suggestions_pb";
import { BowlRole } from "gen/suggestions/v1/suggestions_pb";
import React, { useState } from "react";
import { Link } from "react-router";

type Tab = "suggested" | "build";

/*
 * A part chosen for the bowl being built, with its amount for a serving
 * as typed.
 */
interface Choice {
  key: string;
  amount: string;
}

// SERVINGS are the servings a bowl can be made for.
const SERVINGS = ["1", "2", "3", "4"] as const;

/**
 * BowlsPage builds bowls: a carb, a protein, vegetables and a sauce. It
 * suggests a few from what's on hand, and builds one part by part, with
 * its macros, to cook now or plan.
 */
const BowlsPage: React.FC = () => {
  const [tab, setTab] = useState<Tab>("suggested");
  const [servings, setServings] = useState("2");
  const [choices, setChoices] = useState<Choice[]>([]);
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        kicker="From what's on hand"
        title="Bowl builder"
        subtitle="A carb, a protein, vegetables and a sauce. Take a suggestion, or build your own."
      />
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Segmented
          label="Suggested or built"
          value={tab}
          options={[
            { label: "Suggested", value: "suggested" },
            { label: "Build your own", value: "build" },
          ]}
          onChange={setTab}
        />
        <label className="flex items-center gap-2 text-sm text-muted">
          Servings
          <Segmented
            label="Servings"
            value={servings}
            options={SERVINGS.map((value) => ({ label: value, value }))}
            onChange={setServings}
          />
        </label>
      </div>
      {tab === "suggested" ? (
        <Suggested
          servings={Number(servings)}
          onChange={(bowl) => {
            setChoices(
              bowl.parts.map((part) => ({
                amount: String(part.amount),
                key: part.key,
              }))
            );
            setTab("build");
          }}
        />
      ) : (
        <Builder
          servings={Number(servings)}
          choices={choices}
          onChoices={setChoices}
        />
      )}
    </div>
  );
};

interface SuggestedProps {
  servings: number;
  onChange: (bowl: Bowl) => void;
}

// Suggested lists bowls suggested from what's on hand, to shuffle.
const Suggested: React.FC<SuggestedProps> = ({ servings, onChange }) => {
  // A seed of 0 asks for new bowls; the response's keeps them.
  const [seed, setSeed] = useState(0n);
  const bowls = useQuery(suggestBowls, { maxBowls: 3, seed, servings });
  if (bowls.isPending) {
    return <Loading />;
  }
  if (bowls.isError) {
    return (
      <QueryError
        error={bowls.error}
        onRetry={() => {
          void bowls.refetch();
        }}
      />
    );
  }
  if (bowls.data.bowls.length === 0) {
    return (
      <EmptyState title="Nothing on hand makes a bowl yet">
        A bowl needs a carb, a protein, a vegetable and everything for a sauce
        on hand. Build one of what you have, and buy the rest, or{" "}
        <Link to="/pantry/add" className={linkClass}>
          add to the pantry
        </Link>
        .
      </EmptyState>
    );
  }
  return (
    <div className="flex flex-col gap-4">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        {bowls.data.bowls.map((bowl) => (
          <BowlCard
            key={bowl.name}
            bowl={bowl}
            onChange={() => {
              onChange(bowl);
            }}
          />
        ))}
      </div>
      <button
        type="button"
        className={`${secondaryButton} self-start`}
        disabled={bowls.isFetching}
        onClick={() => {
          // A new seed, other than the one shown, for other bowls.
          setSeed(bowls.data.seed + 1n);
        }}
      >
        <ArrowPathIcon className="size-4" aria-hidden="true" />
        Shuffle
      </button>
    </div>
  );
};

interface BuilderProps {
  servings: number;
  choices: Choice[];
  onChoices: (choices: Choice[]) => void;
}

// UnitOf says what a part's amount for a serving is in.
const unitOf = (part: BowlPart): string => {
  if (part.foodId !== "") {
    return "g";
  }
  return part.role === BowlRole.SAUCE ? "× its sauce" : "servings";
};

/**
 * Builder builds a bowl part by part: carbs, proteins and vegetables, as
 * many as wanted, and one sauce, each in an amount for a serving, showing
 * the bowl's macros once it has each.
 */
const Builder: React.FC<BuilderProps> = ({ servings, choices, onChoices }) => {
  const parts = useQuery(listBowlParts, {});
  const byKey = new Map(
    (parts.data?.parts ?? []).map((part) => [part.key, part])
  );
  const chosen = choices.flatMap((choice) => {
    const part = byKey.get(choice.key);
    return part === undefined ? [] : [{ choice, part }];
  });
  const complete = ROLES.every(({ role, many }) => {
    const count = chosen.filter(({ part }) => part.role === role).length;
    return many ? count >= 1 : count === 1;
  });
  const bowl = useQuery(
    composeBowl,
    {
      parts: chosen.map(({ choice }) => ({
        amount: Number(choice.amount) > 0 ? Number(choice.amount) : 0,
        key: choice.key,
      })),
      servings,
    },
    { enabled: complete, placeholderData: (previous) => previous }
  );
  const actions = useBowlActions();
  const composed = bowl.data?.bowl;
  if (parts.isPending) {
    return <Loading />;
  }
  if (parts.isError) {
    return (
      <QueryError
        error={parts.error}
        onRetry={() => {
          void parts.refetch();
        }}
      />
    );
  }
  const missing = ROLES.filter(
    ({ role }) => !chosen.some(({ part }) => part.role === role)
  ).map(({ label }) => label.toLowerCase());
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2 lg:items-start">
      <div className="flex flex-col gap-4">
        {ROLES.map(({ role, label, many }) => {
          const ofRole = chosen.filter(({ part }) => part.role === role);
          const options = parts.data.parts.filter(
            (part) =>
              part.role === role &&
              !choices.some((choice) => choice.key === part.key)
          );
          return (
            <section
              key={role}
              className={`${cardClass} flex flex-col gap-3 p-4`}
            >
              <h2 className={sectionLabelClass}>{label}</h2>
              {ofRole.map(({ choice, part }) => (
                <div key={part.key} className="flex items-center gap-2">
                  <span className="min-w-0 grow text-sm text-ink-soft">
                    <span className="font-medium">{part.name}</span>
                    {part.detail !== "" && (
                      <span className="block text-xs text-muted">
                        {part.detail}
                      </span>
                    )}
                  </span>
                  <OnHandBadge part={part} />
                  <label className="flex shrink-0 items-center gap-1 text-xs text-muted">
                    <input
                      aria-label={`${part.name} a serving`}
                      className={`${inputClass} w-16 px-2 py-1.5 tabular-nums`}
                      inputMode="decimal"
                      value={choice.amount}
                      onChange={(event) => {
                        onChoices(
                          choices.map((other) =>
                            other.key === part.key
                              ? { ...other, amount: event.target.value }
                              : other
                          )
                        );
                      }}
                    />
                    {unitOf(part)}
                  </label>
                  <button
                    type="button"
                    className="rounded-full p-1.5 text-muted hover:bg-ground-dark"
                    aria-label={`Remove ${part.name}`}
                    onClick={() => {
                      onChoices(
                        choices.filter((other) => other.key !== part.key)
                      );
                    }}
                  >
                    <XMarkIcon className="size-4" aria-hidden="true" />
                  </button>
                </div>
              ))}
              {(many || ofRole.length === 0) && options.length > 0 && (
                <select
                  aria-label={`Add to ${label.toLowerCase()}`}
                  className={inputClass}
                  value=""
                  onChange={(event) => {
                    const part = byKey.get(event.target.value);
                    if (part === undefined) {
                      return;
                    }
                    onChoices([
                      ...choices,
                      { amount: String(part.amount), key: part.key },
                    ]);
                  }}
                >
                  <option value="">
                    {ofRole.length === 0 ? "Choose…" : "Add another…"}
                  </option>
                  <optgroup label="On hand">
                    {options
                      .filter((part) => part.onHand)
                      .map((part) => (
                        <option key={part.key} value={part.key}>
                          {part.name}
                          {part.detail === "" ? "" : ` (${part.detail})`}
                        </option>
                      ))}
                  </optgroup>
                  <optgroup label="To buy">
                    {options
                      .filter((part) => !part.onHand)
                      .map((part) => (
                        <option key={part.key} value={part.key}>
                          {part.name}
                          {part.detail === "" ? "" : ` (${part.detail})`}
                        </option>
                      ))}
                  </optgroup>
                </select>
              )}
            </section>
          );
        })}
      </div>
      <section
        className={`${cardClass} flex flex-col gap-4 p-4 lg:sticky lg:top-4`}
        aria-label="Your bowl"
      >
        {!complete || composed === undefined ? (
          <p className="text-sm text-muted">
            {missing.length > 0
              ? `Add ${missing.join(", ")} to see the bowl and its macros.`
              : "A bowl has one sauce."}
          </p>
        ) : (
          <>
            <h2 className="text-lg leading-snug font-bold text-ink-soft">
              {composed.name}
            </h2>
            <BowlParts parts={composed.parts} servings={servings} />
            <div className="flex flex-col gap-1.5">
              <span className="text-xs text-muted">A serving</span>
              <Macros nutrients={composed.nutrients} />
            </div>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className={primaryButton}
                disabled={actions.pending || bowl.isFetching}
                onClick={() => {
                  actions.cook(composed);
                }}
              >
                Cook now
              </button>
              <button
                type="button"
                className={secondaryButton}
                disabled={actions.pending || bowl.isFetching}
                onClick={() => {
                  actions.plan(composed);
                }}
              >
                Plan it
              </button>
            </div>
          </>
        )}
        <ErrorText error={bowl.error ?? actions.error} />
      </section>
    </div>
  );
};

export default BowlsPage;
