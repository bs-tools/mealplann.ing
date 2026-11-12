import Avatar from "@components/header/avatar";
import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import type { User } from "gen/users/v1/users_pb";
import React from "react";
import { Link } from "react-router";

interface MenuLink {
  to: string;
  label: string;
}

/*
 * The pages that aren't tabs: the household's own things first, then the
 * account's.
 */
const LINKS: MenuLink[] = [
  { label: "Your profile", to: "/profile" },
  { label: "People", to: "/people" },
  { label: "Foods", to: "/foods" },
  { label: "Cooks", to: "/cooks" },
  { label: "Plan templates", to: "/plan/templates" },
  { label: "Settings", to: "/settings" },
  { label: "Privacy policy", to: "/privacy" },
];

const itemClass =
  "block px-4 py-2.5 text-sm text-ink-soft data-[focus]:bg-ground-dark/60 data-[focus]:outline-hidden";

interface AccountMenuProps {
  user: User;
  /* Where the menu opens from the avatar: below it on Today, above it at the foot of the side rail. */
  anchor?: "bottom end" | "top start";
  className?: string;
}

/**
 * AccountMenu is the signed in user's avatar, opening a menu of the pages
 * that aren't tabs, like their profile, the household's people and foods,
 * and settings, as there's no top bar to hold them.
 */
const AccountMenu: React.FC<AccountMenuProps> = ({
  user,
  anchor = "bottom end",
  className = "",
}) => (
  <Menu as="div" className={`relative ${className}`}>
    <MenuButton className="flex items-center rounded-full focus:outline-hidden data-[focus]:ring-2 data-[focus]:ring-pine data-[focus]:ring-offset-2">
      <span className="sr-only">Open the menu</span>
      <Avatar
        avatarUrl={user.avatarUrl}
        name={user.name}
        className="size-10 bg-pine-50! text-sm text-pine!"
      />
    </MenuButton>
    <MenuItems
      transition
      anchor={anchor}
      className="z-[1100] mt-2 w-60 rounded-2xl border border-line bg-white py-1 font-ui shadow-lg transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0 data-[enter]:duration-100 data-[leave]:duration-75"
    >
      <div className="border-b border-line px-4 py-2.5">
        <p className="truncate text-sm font-bold text-ink-soft">{user.name}</p>
        {user.email !== "" && (
          <p className="truncate text-xs text-muted">{user.email}</p>
        )}
      </div>
      {LINKS.map((link) => (
        <MenuItem key={link.to}>
          <Link to={link.to} className={itemClass}>
            {link.label}
          </Link>
        </MenuItem>
      ))}
      <MenuItem>
        <a href="/logout" className={`${itemClass} border-t border-line`}>
          Sign out
        </a>
      </MenuItem>
    </MenuItems>
  </Menu>
);

export default AccountMenu;
