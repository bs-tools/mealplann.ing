import Loading from "@components/loading";
import QueryError from "@components/ui/query-error";
import {
  iconButton,
  primaryButton,
  secondaryButton,
} from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  ArrowLeftIcon,
  ArrowRightIcon,
  CheckIcon,
  ClockIcon,
  MinusIcon,
  PlusIcon,
  XMarkIcon,
} from "@heroicons/react/24/outline";
import { getRecipe } from "gen/recipes/v1/recipes-RecipeService_connectquery";
import type { Recipe, Step } from "gen/recipes/v1/recipes_pb";
import { formatServings } from "helpers/labels";
import {
  formatClock,
  formatStepDuration,
  ingredientAmount,
  ingredientName,
} from "helpers/steps";
import { useUnitSystem } from "helpers/user";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { Link, useParams, useSearchParams } from "react-router";

// Timer is a running timer of a step.
interface Timer {
  id: number;
  label: string;
  // When it ends, in milliseconds since the epoch.
  endsAt: number;
  // The timeout that chimes when it ends.
  timeout: ReturnType<typeof setTimeout>;
}

let nextTimerID = 0;

/**
 * Chime plays a short tone to say a timer is done, and vibrates phones.
 * Browsers only allow sound after the user has touched the page, which
 * starting the timer did.
 */
const chime = () => {
  try {
    const audio = new AudioContext();
    for (const [index, frequency] of [880, 660, 880].entries()) {
      const oscillator = audio.createOscillator();
      const gain = audio.createGain();
      oscillator.frequency.value = frequency;
      gain.gain.value = 0.2;
      oscillator.connect(gain).connect(audio.destination);
      const start = audio.currentTime + index * 0.3;
      oscillator.start(start);
      oscillator.stop(start + 0.25);
    }
  } catch {
    // No sound, but the timer still shows it's done.
  }
  navigator.vibrate?.([300, 150, 300]);
};

/**
 * UseWakeLock keeps the screen on while cooking, as hands are busy, and
 * takes the lock again when the page is shown again, as browsers release
 * it when it's hidden.
 */
const useWakeLock = () => {
  useEffect(() => {
    const supported = "wakeLock" in navigator;
    let lock: WakeLockSentinel | undefined = undefined;
    let cancelled = false;
    const request = async () => {
      if (!supported) {
        return;
      }
      try {
        const sentinel = await navigator.wakeLock.request("screen");
        if (cancelled) {
          await sentinel.release();
        } else {
          lock = sentinel;
        }
      } catch {
        // Some browsers refuse, like on low battery: cooking still works.
      }
    };
    const onVisible = () => {
      if (document.visibilityState === "visible") {
        void request();
      }
    };
    void request();
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      cancelled = true;
      document.removeEventListener("visibilitychange", onVisible);
      void lock?.release();
    };
  }, []);
};

// UseNow returns the time, updated every quarter second while active.
const useNow = (active: boolean): number => {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const interval = active
      ? globalThis.setInterval(() => {
          setNow(Date.now());
        }, 250)
      : undefined;
    return () => {
      globalThis.clearInterval(interval);
    };
  }, [active]);
  return now;
};

interface StepPageProps {
  recipe: Recipe;
  step: Step;
  index: number;
  count: number;
  scale: number;
  onTimer: (label: string, seconds: number) => void;
}

// StepPage shows one step: its text, ingredients and timer.
const StepPage: React.FC<StepPageProps> = ({
  recipe,
  step,
  index,
  count,
  scale,
  onTimer,
}) => {
  const system = useUnitSystem();
  const ingredients = step.ingredientIndexes.flatMap((position) => {
    const ingredient = recipe.ingredients[position];
    return ingredient === undefined ? [] : [{ ingredient, position }];
  });
  const duration = formatStepDuration(step.durationSeconds);
  let after = "";
  if (step.fromStart) {
    after = "Can start right away";
  } else if (step.afterStepIndexes.length > 0) {
    after = `After step ${step.afterStepIndexes.map((other) => other + 1).join(" and ")}`;
  }
  return (
    <div className="flex flex-col gap-5">
      <p className="text-sm font-semibold tracking-wide text-gray-500 uppercase">
        Step {index + 1} of {count}
        {step.section !== "" && ` · ${step.section}`}
      </p>
      <p className="text-2xl leading-snug text-gray-900 sm:text-3xl">
        {step.text}
      </p>
      {ingredients.length > 0 && (
        <ul className="flex flex-col gap-1.5 rounded-lg bg-brand-50 p-4 text-lg">
          {ingredients.map(({ ingredient, position }) => (
            <li key={position} className="flex gap-2">
              <span className="min-w-20 font-semibold text-gray-900 tabular-nums">
                {ingredientAmount(ingredient, scale, system)}
              </span>
              <span className="text-gray-800">
                {ingredientName(ingredient)}
                {ingredient.note !== "" && (
                  <span className="text-gray-500">, {ingredient.note}</span>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className="flex flex-wrap items-center gap-3 text-sm text-gray-600">
        {duration !== "" && (
          <button
            type="button"
            className={secondaryButton}
            onClick={() => {
              onTimer(`Step ${index + 1}`, step.durationSeconds);
            }}
          >
            <ClockIcon className="size-4" aria-hidden="true" />
            Start a {duration} timer
          </button>
        )}
        {step.waiting && (
          <span>
            Waiting time: you&apos;re free to get on with other steps.
          </span>
        )}
        {after !== "" && <span>{after}</span>}
      </div>
    </div>
  );
};

/**
 * CookModePage walks through a recipe one step at a time, in large type,
 * with each step's ingredients scaled to the servings cooked, timers, and
 * the screen kept on. Arrow keys and swipes move between steps.
 */
const CookModePage: React.FC = () => {
  const system = useUnitSystem();
  const { id = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const recipe = useQuery(getRecipe, { id });
  // Page 0 gets everything ready; the steps follow, then the end.
  const [page, setPage] = useState(0);
  const [timers, setTimers] = useState<Timer[]>([]);
  const now = useNow(timers.length > 0);
  const touchStart = useRef<number | null>(null);
  useWakeLock();

  const data = recipe.data?.recipe;
  const steps = data?.instructions ?? [];
  const lastPage = steps.length + 1;
  const servings =
    Number(params.get("servings")) > 0
      ? Number(params.get("servings"))
      : (data?.servings ?? 1);
  const scale = data === undefined ? 1 : servings / data.servings;

  const go = useCallback(
    (offset: number) => {
      setPage((current) => Math.min(Math.max(current + offset, 0), lastPage));
    },
    [lastPage]
  );
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.target instanceof HTMLInputElement) {
        return;
      }
      if (event.key === "ArrowRight" || event.key === " ") {
        event.preventDefault();
        go(1);
      } else if (event.key === "ArrowLeft") {
        go(-1);
      }
    };
    globalThis.addEventListener("keydown", onKey);
    return () => {
      globalThis.removeEventListener("keydown", onKey);
    };
  }, [go]);
  // Timers left running stop chiming once cook mode is left.
  const timersRef = useRef(timers);
  useEffect(() => {
    timersRef.current = timers;
  }, [timers]);
  useEffect(
    () => () => {
      for (const timer of timersRef.current) {
        globalThis.clearTimeout(timer.timeout);
      }
    },
    []
  );

  if (recipe.isPending) {
    return <Loading />;
  }
  if (recipe.isError) {
    return (
      <QueryError
        error={recipe.error}
        onRetry={() => {
          void recipe.refetch();
        }}
      />
    );
  }

  if (data === undefined) {
    return null;
  }
  const setServings = (next: number) => {
    if (next >= 1 && next <= 100) {
      setParams({ servings: String(next) }, { replace: true });
    }
  };
  const step = steps[page - 1];
  let body: React.ReactNode = null;
  if (page === 0) {
    body = (
      <div className="flex flex-col gap-5">
        <p className="text-sm font-semibold tracking-wide text-gray-500 uppercase">
          Get everything ready
        </p>
        <ul className="flex flex-col gap-2 text-lg">
          {data.ingredients.map((ingredient, index) => (
            // Ingredients are only ever shown in order.
            // oxlint-disable-next-line react/no-array-index-key
            <li key={index}>
              <label className="flex cursor-pointer items-start gap-3">
                <input
                  type="checkbox"
                  className="mt-1.5 size-5 accent-brand-700"
                />
                <span>
                  <span className="font-semibold text-gray-900 tabular-nums">
                    {ingredientAmount(ingredient, scale, system)}
                  </span>{" "}
                  {ingredientName(ingredient)}
                  {ingredient.note !== "" && (
                    <span className="text-gray-500">, {ingredient.note}</span>
                  )}
                  {ingredient.optional && (
                    <span className="text-gray-500"> (optional)</span>
                  )}
                </span>
              </label>
            </li>
          ))}
        </ul>
        {steps.length === 0 && (
          <p className="text-gray-600">This recipe has no steps yet.</p>
        )}
      </div>
    );
  } else if (step === undefined) {
    body = (
      <div className="flex flex-col items-center gap-4 py-10 text-center">
        <CheckIcon className="size-12 text-brand-600" aria-hidden="true" />
        <p className="text-2xl text-gray-900">Done. Enjoy your {data.name}!</p>
        <Link to={`/recipes/${data.id}`} className={primaryButton}>
          Back to the recipe
        </Link>
      </div>
    );
  } else {
    body = (
      <StepPage
        recipe={data}
        step={step}
        index={page - 1}
        count={steps.length}
        scale={scale}
        onTimer={(label, seconds) => {
          nextTimerID += 1;
          setTimers([
            ...timers,
            {
              endsAt: Date.now() + seconds * 1000,
              id: nextTimerID,
              label,
              timeout: globalThis.setTimeout(chime, seconds * 1000),
            },
          ]);
        }}
      />
    );
  }

  return (
    <div
      className="mx-auto flex min-h-[70vh] max-w-3xl flex-col gap-6"
      onTouchStart={(event) => {
        touchStart.current = event.touches[0]?.clientX ?? null;
      }}
      onTouchEnd={(event) => {
        const start = touchStart.current;
        const end = event.changedTouches[0]?.clientX;
        touchStart.current = null;
        if (start !== null && end !== undefined && Math.abs(end - start) > 60) {
          go(end < start ? 1 : -1);
        }
      }}
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-col">
          <Link
            to={`/recipes/${data.id}`}
            className="inline-flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900"
          >
            <XMarkIcon className="size-4" aria-hidden="true" />
            Leave cook mode
          </Link>
          <h1 className="truncate text-xl font-semibold text-gray-900">
            {data.name}
          </h1>
        </div>
        <div className="flex items-center gap-1 text-sm text-gray-700">
          <button
            type="button"
            className={iconButton}
            aria-label="Fewer servings"
            onClick={() => {
              setServings(servings - 1);
            }}
          >
            <MinusIcon className="size-4" />
          </button>
          <span className="tabular-nums">{formatServings(servings)}</span>
          <button
            type="button"
            className={iconButton}
            aria-label="More servings"
            onClick={() => {
              setServings(servings + 1);
            }}
          >
            <PlusIcon className="size-4" />
          </button>
        </div>
      </div>
      {timers.length > 0 && (
        <ul className="flex flex-wrap gap-2" aria-label="Timers">
          {timers.map((timer) => {
            const left = (timer.endsAt - now) / 1000;
            const finished = left <= 0;
            return (
              <li
                key={timer.id}
                className={`flex items-center gap-2 rounded-full px-3 py-1.5 text-sm font-medium ring-1 ${
                  finished
                    ? "bg-amber-100 text-amber-900 ring-amber-300 motion-safe:animate-pulse"
                    : "bg-white text-gray-800 ring-gray-200"
                }`}
              >
                <ClockIcon className="size-4" aria-hidden="true" />
                {timer.label}:{" "}
                <span className="tabular-nums">
                  {finished ? "done" : formatClock(left)}
                </span>
                <button
                  type="button"
                  className="text-gray-500 hover:text-gray-900"
                  aria-label={`Stop the ${timer.label} timer`}
                  onClick={() => {
                    globalThis.clearTimeout(timer.timeout);
                    setTimers(timers.filter((other) => other.id !== timer.id));
                  }}
                >
                  <XMarkIcon className="size-4" />
                </button>
              </li>
            );
          })}
        </ul>
      )}
      <div
        className="flex gap-1"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={lastPage}
        aria-valuenow={page}
      >
        {Array.from({ length: lastPage + 1 }, (_unused, index) => (
          <span
            key={index}
            className={`h-1.5 grow rounded-full ${index <= page ? "bg-brand-600" : "bg-gray-200"}`}
          />
        ))}
      </div>
      <div className="grow">{body}</div>
      <div className="sticky bottom-0 flex gap-3 bg-gray-50/90 py-3 backdrop-blur">
        <button
          type="button"
          className={`${secondaryButton} grow justify-center py-3 text-base`}
          disabled={page === 0}
          onClick={() => {
            go(-1);
          }}
        >
          <ArrowLeftIcon className="size-5" aria-hidden="true" />
          Back
        </button>
        <button
          type="button"
          className={`${primaryButton} grow justify-center py-3 text-base`}
          disabled={page === lastPage}
          onClick={() => {
            go(1);
          }}
        >
          {page === 0 ? "Start cooking" : "Next"}
          <ArrowRightIcon className="size-5" aria-hidden="true" />
        </button>
      </div>
    </div>
  );
};

export default CookModePage;
