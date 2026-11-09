import Loading from "@components/loading";
import { PortionName } from "@components/plan/plan-bits";
import Card from "@components/ui/card";
import { PageHeader, ProgressBar } from "@components/ui/misc";
import QueryError from "@components/ui/query-error";
import { linkClass } from "@components/ui/styles";
import { useQuery } from "@connectrpc/connect-query";
import {
  ArchiveBoxIcon,
  BookOpenIcon,
  CalendarDaysIcon,
  ChevronRightIcon,
  Cog6ToothIcon,
  ExclamationTriangleIcon,
  ShoppingCartIcon,
  Squares2X2Icon,
  UserGroupIcon,
} from "@heroicons/react/24/outline";
import { listPantry } from "gen/pantry/v1/pantry-PantryService_connectquery";
import type { PantryItem } from "gen/pantry/v1/pantry_pb";
import { EatingTimeKind } from "gen/people/v1/people_pb";
import { getWeek } from "gen/plans/v1/plans-PlanService_connectquery";
import type { PersonDay } from "gen/plans/v1/plans_pb";
import { Severity } from "gen/plans/v1/plans_pb";
import { listShoppingLists } from "gen/shopping/v1/shopping-ShoppingService_connectquery";
import type { User } from "gen/users/v1/users_pb";
import { daysBetween, startOfWeek, today } from "helpers/dates";
import {
  formatDay,
  formatNearDate,
  formatShortDate,
  formatStock,
} from "helpers/format";
import React from "react";
import { Link } from "react-router";

// SOON_DAYS is how many days left count as soon to use, like the pantry's.
const SOON_DAYS = 3;

// The parts of the app, to go to.
const PARTS: {
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>;
  title: string;
  description: string;
  to: string;
}[] = [
  {
    description:
      "The week's meals with suggestions, split into the right portions for everyone.",
    icon: CalendarDaysIcon,
    title: "Plan",
    to: "/plan",
  },
  {
    description: "One shop a week, by store and aisle, ticked off together.",
    icon: ShoppingCartIcon,
    title: "Shopping",
    to: "/shopping",
  },
  {
    description: "What's in stock and what to use soon.",
    icon: ArchiveBoxIcon,
    title: "Pantry",
    to: "/pantry",
  },
  {
    description: "When everyone eats, with their care team's rules.",
    icon: UserGroupIcon,
    title: "People",
    to: "/people",
  },
  {
    description: "Your recipes, with nutrition per serving.",
    icon: BookOpenIcon,
    title: "Recipes",
    to: "/recipes",
  },
  {
    description: "The ingredients you cook with.",
    icon: Squares2X2Icon,
    title: "Foods",
    to: "/foods",
  },
  {
    description:
      "Your household, invitations, and connecting your AI agent over MCP.",
    icon: Cog6ToothIcon,
    title: "Settings",
    to: "/settings",
  },
];

interface PersonTodayProps {
  day: PersonDay;
}

/**
 * PersonToday lists what a person eats today, by eating time, with how many
 * warnings their day has.
 */
const PersonToday: React.FC<PersonTodayProps> = ({ day }) => {
  const warnings =
    day.warnings.filter((warning) => warning.severity === Severity.WARNING)
      .length +
    day.eatingTimes
      .flatMap((et) => et.warnings)
      .filter((warning) => warning.severity === Severity.WARNING).length;
  return (
    <div className="flex flex-col gap-1">
      <h3 className="flex items-center gap-2 text-sm font-semibold text-gray-900">
        {day.personName}
        {warnings > 0 && (
          <span className="inline-flex items-center gap-0.5 text-xs font-normal text-amber-800">
            <ExclamationTriangleIcon
              className="size-3.5 text-amber-600"
              aria-hidden="true"
            />
            {warnings === 1 ? "1 warning" : `${warnings} warnings`}
          </span>
        )}
      </h3>
      {day.eatingTimes.length === 0 ? (
        <p className="text-sm text-gray-500">No eating times yet.</p>
      ) : (
        <ul className="flex flex-col gap-0.5 text-sm">
          {day.eatingTimes.map((planned) => {
            const et = planned.eatingTime;

            return (
              <li key={et?.id} className="flex gap-2">
                <span
                  className={`w-28 shrink-0 truncate text-gray-500 ${et?.kind === EatingTimeKind.SMALL ? "pl-3" : ""}`}
                >
                  {et?.name}
                </span>
                <span
                  className={`min-w-0 ${planned.portions.length === 0 ? "text-gray-400" : "text-gray-900"}`}
                >
                  {planned.portions.length === 0
                    ? "Nothing planned"
                    : planned.portions.map((portion, index) => (
                        <React.Fragment
                          key={
                            portion.portionId === ""
                              ? `staple-${portion.stapleId}`
                              : portion.portionId
                          }
                        >
                          {index > 0 && ", "}
                          <PortionName portion={portion} />
                        </React.Fragment>
                      ))}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
};

/**
 * TodayCard shows what everyone eats today, from the week's plan.
 */
const TodayCard: React.FC = () => {
  const now = today();
  const week = useQuery(getWeek, { monday: startOfWeek(now) });
  let content: React.ReactNode = null;
  if (week.isPending) {
    content = <Loading />;
  } else if (week.isError) {
    content = (
      <QueryError
        error={week.error}
        onRetry={() => {
          void week.refetch();
        }}
      />
    );
  } else {
    const people = week.data.days.find((day) => day.date === now)?.people ?? [];
    content =
      people.length === 0 ? (
        <p className="text-sm text-gray-700">
          Add the people you cook for, with when they eat, on the{" "}
          <Link to="/people" className={linkClass}>
            People
          </Link>{" "}
          page, then plan the week.
        </p>
      ) : (
        <div className="flex flex-col gap-4">
          {people.map((day) => (
            <PersonToday key={day.personId} day={day} />
          ))}
        </div>
      );
  }
  return (
    <Card
      title={`Today, ${formatDay(now)}`}
      actions={
        <Link to="/plan" className={`${linkClass} text-sm`}>
          Plan the week
        </Link>
      }
    >
      {content}
    </Card>
  );
};

/**
 * ShoppingCard shows the state of the current shopping list: the latest
 * whose shopping isn't complete and whose meals aren't all past.
 */
const ShoppingCard: React.FC = () => {
  const now = today();
  const lists = useQuery(listShoppingLists, { pageSize: 10 });
  let content: React.ReactNode = null;
  if (lists.isPending) {
    content = <Loading />;
  } else if (lists.isError) {
    content = (
      <QueryError
        error={lists.error}
        onRetry={() => {
          void lists.refetch();
        }}
      />
    );
  } else {
    const list = lists.data.lists.find(
      (other) => !other.complete && other.endDate >= now
    );
    if (list === undefined) {
      content = (
        <p className="text-sm text-gray-700">
          No list to shop for yet.{" "}
          <Link to="/shopping" className={linkClass}>
            Make one from the plan
          </Link>
          .
        </p>
      );
    } else {
      const left = list.itemCount - list.checkedCount;
      content = (
        <div className="flex flex-col gap-2 text-sm text-gray-700">
          <p>
            Shop on{" "}
            <span className="font-medium text-gray-900">
              {formatDay(list.startDate)}
            </span>{" "}
            for the meals to {formatShortDate(list.endDate)}.
          </p>
          <ProgressBar value={list.checkedCount} target={list.itemCount} />
          <p className="tabular-nums">
            {list.checkedCount} of {list.itemCount} items checked
            {left === 0 && list.itemCount > 0 ? ": ready to complete" : ""}
            {list.topUpCount > 0 &&
              ` · ${list.topUpCount} to top up later in the week`}
          </p>
        </div>
      );
    }
  }
  return (
    <Card
      title="Shopping"
      actions={
        <Link to="/shopping" className={`${linkClass} text-sm`}>
          Open the list
        </Link>
      }
    >
      {content}
    </Card>
  );
};

// DescribeUseBy says when a lot is to be used by, e.g. "use by tomorrow".
const describeUseBy = (item: PantryItem, now: string): string => {
  const days = daysBetween(now, item.useByDate);
  if (days < 0) {
    return `expired ${formatNearDate(item.useByDate)}`;
  }
  if (days === 0) {
    return "use today";
  }
  if (days === 1) {
    return "use tomorrow";
  }
  return `use by ${formatDay(item.useByDate)}`;
};

/**
 * UseSoonCard lists the pantry's lots to use within a few days, soonest
 * first, which the plan's suggestions favor.
 */
const UseSoonCard: React.FC = () => {
  const now = today();
  const pantry = useQuery(listPantry, {});
  let content: React.ReactNode = null;
  if (pantry.isPending) {
    content = <Loading />;
  } else if (pantry.isError) {
    content = (
      <QueryError
        error={pantry.error}
        onRetry={() => {
          void pantry.refetch();
        }}
      />
    );
  } else {
    // The pantry is by food: insert the lots by use-by date, soonest first.
    const soon: PantryItem[] = [];
    for (const item of pantry.data.items) {
      if (
        item.useByDate !== "" &&
        daysBetween(now, item.useByDate) <= SOON_DAYS
      ) {
        const at = soon.findIndex((other) => other.useByDate > item.useByDate);
        soon.splice(at < 0 ? soon.length : at, 0, item);
      }
    }
    content =
      soon.length === 0 ? (
        <p className="text-sm text-gray-700">
          Nothing to use in the next {SOON_DAYS} days.
        </p>
      ) : (
        <ul className="flex flex-col gap-1 text-sm">
          {soon.map((item) => {
            const expired = daysBetween(now, item.useByDate) < 0;
            return (
              <li key={item.id} className="flex flex-wrap gap-x-2">
                <span className="font-medium text-gray-900">
                  {item.foodName}
                </span>
                <span className="text-gray-600 tabular-nums">
                  {formatStock(item.grams, item.gramsPerPiece)}
                </span>
                <span className={expired ? "text-red-700" : "text-amber-800"}>
                  {describeUseBy(item, now)}
                </span>
              </li>
            );
          })}
        </ul>
      );
  }
  return (
    <Card
      title="Use soon"
      actions={
        <Link to="/pantry" className={`${linkClass} text-sm`}>
          Pantry
        </Link>
      }
    >
      {content}
    </Card>
  );
};

interface HomePageProps {
  user: User;
}

/**
 * HomePage is the household's week at a glance: what everyone eats today,
 * the shopping list, and what to use soon from the pantry, with the parts
 * of the app to go to.
 */
const HomePage: React.FC<HomePageProps> = ({ user }) => (
  <div className="flex flex-col gap-4">
    <PageHeader
      title={`Hi, ${user.name}`}
      subtitle="Vegan meal planning for the whole household."
    />
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <TodayCard />
      <div className="flex flex-col gap-4">
        <ShoppingCard />
        <UseSoonCard />
      </div>
      <Card title="Plan, shop and cook" className="lg:col-span-2">
        <ul className="-my-1 grid grid-cols-1 gap-x-6 sm:grid-cols-2 lg:grid-cols-3">
          {PARTS.map(({ icon: Icon, title, description, to }) => (
            <li key={to}>
              <Link
                to={to}
                className="-mx-2 flex items-center gap-3 rounded px-2 py-2 hover:bg-gray-50"
              >
                <Icon
                  className="size-5 shrink-0 text-brand-600"
                  aria-hidden="true"
                />
                <div className="grow">
                  <p className="font-medium text-gray-900">{title}</p>
                  <p className="text-sm text-gray-600">{description}</p>
                </div>
                <ChevronRightIcon
                  className="size-4 shrink-0 text-gray-400"
                  aria-hidden="true"
                />
              </Link>
            </li>
          ))}
        </ul>
      </Card>
    </div>
  </div>
);

export default HomePage;
