import Loading from "@components/loading";
import DisplacedChoice from "@components/today/displaced-choice";
import BackLink from "@components/ui/back-link";
import { PageHeader } from "@components/ui/misc";
import QueryError, { ErrorText } from "@components/ui/query-error";
import Segmented from "@components/ui/segmented";
import {
  primaryButton,
  sectionLabelClass,
  secondaryButton,
} from "@components/ui/styles";
import type { ConnectError } from "@connectrpc/connect";
import { createClient } from "@connectrpc/connect";
import { useQuery, useTransport } from "@connectrpc/connect-query";
import { MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { AmountUnit } from "gen/foods/v1/foods_pb";
import { listPantry } from "gen/pantry/v1/pantry-PantryService_connectquery";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { PantryService } from "gen/pantry/v1/pantry_pb";
import {
  getWeek,
  listFrozenLeftovers,
} from "gen/plans/v1/plans-PlanService_connectquery";
import type { PlannedPortion } from "gen/plans/v1/plans_pb";
import { PlanService } from "gen/plans/v1/plans_pb";
import { ShoppingService } from "gen/shopping/v1/shopping_pb";
import { suggestFromPantry } from "gen/suggestions/v1/suggestions-SuggestionService_connectquery";
import { SuggestionService } from "gen/suggestions/v1/suggestions_pb";
import { isDateString, startOfWeek, today } from "helpers/dates";
import { formatHoursMinutes, formatList } from "helpers/format";
import { formatServings } from "helpers/labels";
import { invalidateServices } from "helpers/query";
import React, { useState } from "react";
import { useNavigate, useSearchParams } from "react-router";

// Choice is what was eaten instead, or on top: something from the pantry.
type Choice =
  | { kind: "fridge"; cookId: string; name: string; detail: string }
  | { kind: "freezer"; cookId: string; name: string; detail: string }
  | { kind: "recipe"; recipeId: string; name: string; detail: string }
  | { kind: "food"; item: PantryItem; name: string; detail: string };

// GRAMS_STEP is how much the stepper changes a food weighed in grams.
const GRAMS_STEP = 25;

// NowTime returns the time of day now, "HH:MM".
const nowTime = (): string => {
  const now = new Date();
  return `${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}`;
};

/**
 * TodayChangePage records what was eaten instead of a meal, or on top of
 * the day's meals, like a snack: something made, from the fridge or the
 * freezer, a recipe the pantry has everything for, or a food from the
 * pantry. It's marked eaten, taking it out of the pantry. A meal it
 * replaces then asks what happens to it.
 */
const TodayChangePage: React.FC = () => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const transport = useTransport();
  const [params] = useSearchParams();
  const now = today();
  const dateParam = params.get("date") ?? "";
  const date = isDateString(dateParam) ? dateParam : now;
  const personId = params.get("person") ?? "";
  const replacedId = params.get("et") ?? "";
  const week = useQuery(getWeek, { monday: startOfWeek(date) });
  const pantry = useQuery(listPantry, {});
  const frozen = useQuery(listFrozenLeftovers, {});
  const cookable = useQuery(suggestFromPantry, { date });
  const [search, setSearch] = useState("");
  const [chosen, setChosen] = useState<Choice | null>(null);
  const [count, setCount] = useState(1);
  const [eatingTimeId, setEatingTimeId] = useState("");
  const [replacing, setReplacing] = useState(false);
  /*
   * The portions the choice replaces, as they were before it, since the
   * plan read again has the choice at the same eating time too.
   */
  const [displaced, setDisplaced] = useState<PlannedPortion[]>([]);
  // Whether it was eaten out instead, marked once the meal's taken care of.
  const [outInstead, setOutInstead] = useState(false);

  const day = week.data?.days.find((other) => other.date === date);
  const person = day?.people.find((other) => other.personId === personId);
  const replaced = person?.eatingTimes.find(
    (planned) => planned.eatingTime?.id === replacedId
  );
  const eatingTimes = (person?.eatingTimes ?? []).flatMap((planned) =>
    planned.eatingTime === undefined ? [] : [planned.eatingTime]
  );
  // A snack goes at the eating time nearest now, unless another is chosen.
  let nearest = eatingTimes[0]?.id ?? "";
  for (const et of eatingTimes) {
    if (et.time !== "" && et.time <= nowTime()) {
      nearest = et.id;
    }
  }
  let at = replacedId;
  if (at === "") {
    at = eatingTimeId === "" ? nearest : eatingTimeId;
  }

  const record = useMutation<void, ConnectError, Choice>({
    mutationFn: async (choice) => {
      const client = createClient(PlanService, transport);
      let portionId = "";
      switch (choice.kind) {
        case "fridge": {
          const created = await client.createPortion({
            portion: {
              date,
              eatingTimeId: at,
              personId,
              what: {
                case: "cook",
                value: { cookId: choice.cookId, servings: 1 },
              },
            },
          });
          portionId = created.portion?.id ?? "";
          break;
        }
        case "freezer": {
          const taken = await client.takeFromFreezer({
            cookId: choice.cookId,
            date,
            eatingTimeId: at,
            personId,
            servings: 1,
          });
          portionId = taken.portion?.id ?? "";
          break;
        }
        case "recipe": {
          const created = await client.createPortion({
            newCook: { recipeId: choice.recipeId, servings: 0 },
            portion: {
              date,
              eatingTimeId: at,
              personId,
              what: { case: "cook", value: { cookId: "", servings: 1 } },
            },
          });
          portionId = created.portion?.id ?? "";
          // It was cooked, so its ingredients come out of the pantry.
          if (created.cook !== undefined) {
            await client.updateCook({
              cook: { done: true, id: created.cook.id },
              updateMask: { paths: ["done"] },
            });
          }
          break;
        }
        default: {
          const { item } = choice;
          let unit = AmountUnit.GRAMS;
          if (item.packageGrams > 0) {
            unit = AmountUnit.PACKAGES;
          } else if (item.gramsPerPiece > 0) {
            unit = AmountUnit.PIECES;
          }
          const created = await client.createPortion({
            portion: {
              date,
              eatingTimeId: at,
              personId,
              what: {
                case: "food",
                value: { amount: count, foodId: item.foodId, unit },
              },
            },
          });
          portionId = created.portion?.id ?? "";
        }
      }
      await client.setPortionEaten({ eaten: true, id: portionId });
    },
    onSuccess: async () => {
      await invalidateServices(queryClient, [
        PlanService,
        PantryService,
        ShoppingService,
        SuggestionService,
      ]);
      if (displaced.length > 0) {
        setReplacing(true);
      } else {
        void navigate("/");
      }
    },
  });

  const failed = week.error ?? pantry.error;
  let content: React.ReactNode = null;
  if (week.isPending || pantry.isPending) {
    content = <Loading />;
  } else if (failed !== null) {
    content = (
      <QueryError
        error={failed}
        onRetry={() => {
          void week.refetch();
          void pantry.refetch();
        }}
      />
    );
  } else if (replacing && week.data !== undefined) {
    content = (
      <div className="flex flex-col gap-3">
        <p className="text-lg font-bold text-ink-soft">
          What about the{" "}
          {formatList(displaced.map((portion) => portion.name)).toLowerCase()}?
        </p>
        <DisplacedChoice
          week={week.data}
          personId={personId}
          date={date}
          today={now}
          time={replaced?.eatingTime?.time ?? ""}
          portions={displaced}
          name="it"
          then={
            outInstead
              ? async (client) => {
                  await client.setEatingOut({
                    date,
                    eatingOut: true,
                    eatingTimeId: replacedId,
                    personId,
                  });
                }
              : undefined
          }
          onDone={() => {
            void navigate("/");
          }}
        />
      </div>
    );
  } else if (chosen === null) {
    const typed = search.trim().toLowerCase();
    const matches = (name: string): boolean =>
      typed === "" || name.toLowerCase().includes(typed);
    const fridge: Choice[] = (week.data?.cooks ?? [])
      .filter(
        (planned) =>
          planned.cook !== undefined &&
          planned.cook.done &&
          planned.cook.date <= date &&
          planned.leftServings >= 0.5
      )
      .map((planned) => ({
        cookId: planned.cook?.id ?? "",
        detail: `Fridge · ${formatServings(planned.leftServings)}`,
        kind: "fridge",
        name: planned.cook?.recipeName ?? "",
      }));
    const freezer: Choice[] = (frozen.data?.leftovers ?? [])
      .filter(
        (leftover) => !leftover.expired && leftover.remainingServings >= 1
      )
      .map((leftover) => ({
        cookId: leftover.cook?.id ?? "",
        detail: `Freezer · ${formatServings(leftover.remainingServings)}`,
        kind: "freezer",
        name: leftover.cook?.recipeName ?? "",
      }));
    const recipes: Choice[] = (cookable.data?.recipes ?? []).map((recipe) => ({
      detail:
        recipe.totalSeconds > 0 ? formatHoursMinutes(recipe.totalSeconds) : "",
      kind: "recipe",
      name: recipe.recipeName,
      recipeId: recipe.recipeId,
    }));
    const seen = new Set<string>();
    const foods: Choice[] = [];
    for (const item of pantry.data?.items ?? []) {
      if (!seen.has(item.foodId)) {
        seen.add(item.foodId);
        foods.push({
          detail: "Pantry",
          item,
          kind: "food",
          name: item.foodName,
        });
      }
    }
    const sections: [string, Choice[]][] = [
      [
        "Already made",
        [...fridge, ...freezer].filter((choice) => matches(choice.name)),
      ],
      [
        "Make from the pantry",
        recipes.filter((choice) => matches(choice.name)),
      ],
      [
        "In the pantry",
        foods
          .filter((choice) => matches(choice.name))
          .slice(0, typed === "" ? 6 : 20),
      ],
    ];
    content = (
      <div className="flex flex-col gap-4">
        <label className="flex items-center gap-2 rounded-xl border-2 border-pine bg-white px-3 py-2.5">
          <MagnifyingGlassIcon
            className="size-5 text-pine"
            aria-hidden="true"
          />
          <span className="sr-only">What did you have?</span>
          <input
            type="search"
            className="min-w-0 grow border-0 bg-transparent text-base outline-none"
            value={search}
            placeholder="What did you have?"
            onChange={(event) => {
              setSearch(event.target.value);
            }}
          />
        </label>
        {sections.map(([title, choices]) =>
          choices.length === 0 ? null : (
            <section key={title} className="flex flex-col">
              <h2 className={sectionLabelClass}>{title}</h2>
              <ul>
                {choices.map((choice) => (
                  <li key={`${choice.kind}-${choice.name}-${choice.detail}`}>
                    <button
                      type="button"
                      className="flex w-full items-center justify-between gap-3 border-b border-line py-3.5 text-left"
                      onClick={() => {
                        setChosen(choice);
                        setCount(
                          choice.kind === "food" &&
                            choice.item.packageGrams <= 0 &&
                            choice.item.gramsPerPiece <= 0
                            ? GRAMS_STEP * 2
                            : 1
                        );
                      }}
                    >
                      <span className="text-base font-medium text-ink-soft">
                        {choice.name}
                      </span>
                      <span className="shrink-0 text-sm text-muted">
                        {choice.detail}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          )
        )}
        {replaced !== undefined && (
          <button
            type="button"
            className={`${secondaryButton} self-start`}
            onClick={() => {
              setDisplaced(replaced.portions);
              setOutInstead(true);
              setReplacing(true);
            }}
          >
            Ate out instead
          </button>
        )}
      </div>
    );
  } else {
    let unit = "g";
    let step = GRAMS_STEP;
    if (chosen.kind === "food") {
      if (chosen.item.packageGrams > 0) {
        unit = count === 1 ? "package" : "packages";
        step = 1;
      } else if (chosen.item.gramsPerPiece > 0) {
        unit = count === 1 ? "piece" : "pieces";
        step = 1;
      }
    }
    content = (
      <div className="flex flex-col gap-4">
        <div className="flex flex-col gap-4 rounded-2xl border border-line bg-white p-4">
          <div className="flex items-baseline justify-between gap-3">
            <p className="text-lg font-bold text-ink-soft">{chosen.name}</p>
            <span className="text-sm text-muted">{chosen.detail}</span>
          </div>
          {chosen.kind === "food" && (
            <div className="flex items-center justify-between gap-3">
              <span className="text-base text-ink-soft">How much</span>
              <span className="flex items-center gap-2">
                <button
                  type="button"
                  className="size-10 rounded-full border border-line bg-white text-xl disabled:opacity-40"
                  aria-label="Less"
                  disabled={count <= step}
                  onClick={() => {
                    setCount(count - step);
                  }}
                >
                  −
                </button>
                <span className="min-w-24 text-center text-base font-bold tabular-nums">
                  {count.toLocaleString()} {unit}
                </span>
                <button
                  type="button"
                  className="size-10 rounded-full border border-line bg-white text-xl"
                  aria-label="More"
                  onClick={() => {
                    setCount(count + step);
                  }}
                >
                  +
                </button>
              </span>
            </div>
          )}
          {replacedId === "" && eatingTimes.length > 1 && (
            <div className="flex flex-col gap-2">
              <span className="text-base text-ink-soft">When</span>
              <Segmented
                label="When it was eaten"
                value={at}
                options={eatingTimes.map((et) => ({
                  label: et.name,
                  value: et.id,
                }))}
                onChange={setEatingTimeId}
              />
            </div>
          )}
        </div>
        <ErrorText error={record.error} />
        <div className="flex gap-2">
          <button
            type="button"
            className={`${primaryButton} grow py-3 text-base`}
            disabled={record.isPending || at === ""}
            onClick={() => {
              setDisplaced(replaced?.portions ?? []);
              record.mutate(chosen);
            }}
          >
            {record.isPending ? "Saving…" : "Ate this"}
          </button>
          <button
            type="button"
            className={secondaryButton}
            onClick={() => {
              setChosen(null);
            }}
          >
            Back
          </button>
        </div>
      </div>
    );
  }

  const title =
    replaced === undefined
      ? "Add to today"
      : `Instead of ${formatList(replaced.portions.map((portion) => portion.name)).toLowerCase()}`;
  return (
    <div className="mx-auto flex w-full max-w-xl flex-col gap-4">
      <BackLink to="/">Today</BackLink>
      <PageHeader title={replacing ? "One more thing" : title} />
      {content}
    </div>
  );
};

export default TodayChangePage;
