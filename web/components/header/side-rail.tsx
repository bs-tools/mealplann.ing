import AccountMenu from "@components/header/account-menu";
import { TABS } from "@components/header/bottom-nav";
import { LogoMark, Wordmark } from "@components/header/logo";
import type { User } from "gen/users/v1/users_pb";
import React from "react";
import { Link, NavLink } from "react-router";

interface SideRailProps {
  user: User;
}

/**
 * SideRail holds the main pages on large screens, down the left side, with
 * the account menu at its foot: the tab bar's place on a phone, as the
 * design has no top bar.
 */
const SideRail: React.FC<SideRailProps> = ({ user }) => (
  <nav
    aria-label="Main"
    className="sticky top-0 hidden h-screen w-56 shrink-0 flex-col border-r border-line bg-white px-3 py-5 lg:flex print:hidden"
  >
    <Link to="/" className="mb-6 flex items-center gap-2 px-3">
      <LogoMark leaf="#1b1f1d" ground="#ffffff" />
      <Wordmark className="text-[15px] text-ink-soft" />
    </Link>
    <div className="flex flex-col gap-1">
      {TABS.map(({ icon: Icon, label, to, end }) => (
        <NavLink
          key={to}
          to={to}
          end={end ?? false}
          className={({ isActive }) =>
            `flex items-center gap-3 rounded-xl px-3 py-2.5 text-sm ${
              isActive
                ? "bg-ground-dark/70 font-bold text-ink-soft"
                : "text-muted hover:bg-ground-dark/40 hover:text-ink-soft"
            }`
          }
        >
          {({ isActive }) => (
            <>
              <Icon
                className={`size-5 ${isActive ? "stroke-2 text-pine" : ""}`}
                aria-hidden="true"
              />
              {label}
            </>
          )}
        </NavLink>
      ))}
    </div>
    <AccountMenu user={user} anchor="top start" className="mt-auto px-3" />
  </nav>
);

export default SideRail;
