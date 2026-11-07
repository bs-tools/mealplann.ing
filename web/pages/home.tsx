import Card from "@components/ui/card";
import { PageHeader } from "@components/ui/misc";
import { linkClass } from "@components/ui/styles";
import {
  BookOpenIcon,
  CalendarDaysIcon,
  ChevronRightIcon,
  ShoppingCartIcon,
  SparklesIcon,
  Squares2X2Icon,
} from "@heroicons/react/24/outline";
import type { User } from "gen/users/v1/users_pb";
import React from "react";
import { Link } from "react-router";

// The parts of the app that are ready, to go to.
const READY: {
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>;
  title: string;
  description: string;
  to: string;
}[] = [
  {
    description:
      "Your recipes, with nutrition per serving, entered here or by your AI agent.",
    icon: BookOpenIcon,
    title: "Recipes",
    to: "/recipes",
  },
  {
    description:
      "The ingredients you cook with, from USDA FoodData Central or a label.",
    icon: Squares2X2Icon,
    title: "Foods",
    to: "/foods",
  },
];

// The parts of the app still to come, in the order they're built.
const COMING: {
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>;
  title: string;
  description: string;
}[] = [
  {
    description:
      "Everyone's eating times and targets, and a week of shared meals split into the right portions.",
    icon: CalendarDaysIcon,
    title: "People and plans",
  },
  {
    description:
      "What's in the cupboard, and a shopping list for one shop a week, by store and aisle.",
    icon: ShoppingCartIcon,
    title: "Pantry and shopping",
  },
  {
    description:
      "Recipes that share ingredients, use up leftovers and fill nutrition gaps.",
    icon: SparklesIcon,
    title: "Suggestions",
  },
];

interface HomePageProps {
  user: User;
}

/**
 * HomePage welcomes the signed in user. It becomes the week at a glance once
 * there are plans to show.
 */
const HomePage: React.FC<HomePageProps> = ({ user }) => (
  <div className="flex flex-col gap-4">
    <PageHeader
      title={`Hi, ${user.name}`}
      subtitle="Vegan meal planning for the whole household."
    />
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <Card title="Cook">
        <ul className="-my-2 divide-y divide-gray-100">
          {READY.map(({ icon: Icon, title, description, to }) => (
            <li key={to}>
              <Link
                to={to}
                className="-mx-2 flex items-center gap-3 rounded px-2 py-3 hover:bg-gray-50"
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
      <Card title="Coming soon">
        <ul className="flex flex-col gap-4">
          {COMING.map(({ icon: Icon, title, description }) => (
            <li key={title} className="flex gap-3">
              <Icon
                className="mt-0.5 size-5 shrink-0 text-brand-600"
                aria-hidden="true"
              />
              <div>
                <p className="font-medium text-gray-900">{title}</p>
                <p className="text-sm text-gray-600">{description}</p>
              </div>
            </li>
          ))}
        </ul>
      </Card>
      <Card title="Get set up">
        <ul className="flex list-disc flex-col gap-2 pl-5 text-sm text-gray-700">
          <li>
            Name your household and invite the people you plan meals for in{" "}
            <Link to="/settings" className={linkClass}>
              settings
            </Link>
            .
          </li>
          <li>
            Connect your AI agent over MCP, also in{" "}
            <Link to="/settings" className={linkClass}>
              settings
            </Link>
            , to enter recipes and plan with it.
          </li>
        </ul>
      </Card>
    </div>
  </div>
);

export default HomePage;
