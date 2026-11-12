import {
  ArchiveBoxIcon,
  BookOpenIcon,
  CalendarDaysIcon,
  ShoppingCartIcon,
  SunIcon,
} from "@heroicons/react/24/outline";
import React from "react";
import { NavLink } from "react-router";

interface Tab {
  to: string;
  label: string;
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>;
  end?: boolean;
}

// The app's main pages, a thumb away on a phone.
export const TABS: Tab[] = [
  { end: true, icon: SunIcon, label: "Today", to: "/" },
  { icon: CalendarDaysIcon, label: "Plan", to: "/plan" },
  { icon: BookOpenIcon, label: "Recipes", to: "/recipes" },
  { icon: ShoppingCartIcon, label: "Shop", to: "/shopping" },
  { icon: ArchiveBoxIcon, label: "Pantry", to: "/pantry" },
];

/**
 * BottomNav is the tab bar of the main pages on phones and tablets, at the
 * bottom of the screen, as words without icons, as in the design; larger
 * screens have them in the side rail.
 */
const BottomNav: React.FC = () => (
  <nav
    aria-label="Main"
    className="fixed inset-x-0 bottom-0 z-[1000] border-t border-line bg-ground/95 pb-[env(safe-area-inset-bottom)] backdrop-blur lg:hidden print:hidden"
  >
    <div className="mx-auto grid max-w-md grid-cols-5 px-2 pt-1 pb-3">
      {TABS.map(({ label, to, end }) => (
        <NavLink
          key={to}
          to={to}
          end={end ?? false}
          className={({ isActive }) =>
            `py-3 text-center text-xs ${
              isActive ? "font-bold text-ink-soft" : "text-muted"
            }`
          }
        >
          {label}
        </NavLink>
      ))}
    </div>
  </nav>
);

export default BottomNav;
