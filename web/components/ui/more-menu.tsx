import { Menu, MenuButton, MenuItem, MenuItems } from "@headlessui/react";
import { EllipsisHorizontalIcon } from "@heroicons/react/24/outline";
import React from "react";
import { Link } from "react-router";

// MenuItemClass styles an item of a ⋯ menu.
export const menuItemClass =
  "block w-full px-4 py-2.5 text-left text-sm text-ink-soft data-[focus]:bg-ground-dark/60 data-[focus]:outline-hidden";

// MoreItem is an item of a ⋯ menu: a link, or an action.
export interface MoreItem {
  label: string;
  // Where it goes, for a link.
  to?: string;
  onClick?: () => void;
  // Whether it deletes something, which shows in red.
  danger?: boolean;
  disabled?: boolean;
}

interface MoreMenuProps {
  items: MoreItem[];
  // What the menu is about, for screen readers, e.g. "More for Tofu".
  label?: string;
  className?: string;
}

/**
 * MoreMenu is the ⋯ button that opens a page's or a row's less common
 * actions, like editing or deleting, which keeps them out of the way of
 * the primary one.
 */
const MoreMenu: React.FC<MoreMenuProps> = ({
  items,
  label = "More",
  className = "flex size-10 items-center justify-center rounded-full text-ink-soft hover:bg-ground-dark/60",
}) => (
  <Menu as="div" className="relative shrink-0">
    <MenuButton className={className} aria-label={label} title={label}>
      <EllipsisHorizontalIcon className="size-6" />
    </MenuButton>
    <MenuItems
      transition
      anchor="bottom end"
      className="z-[1100] mt-1 w-60 rounded-2xl border border-line bg-white py-1 font-ui shadow-lg transition focus:outline-hidden data-[closed]:scale-95 data-[closed]:opacity-0"
    >
      {items.map((item) => {
        const itemClass = `${menuItemClass} ${item.danger === true ? "text-error" : ""} disabled:opacity-50`;
        return (
          <MenuItem key={item.label} disabled={item.disabled === true}>
            {item.to === undefined ? (
              <button
                type="button"
                className={itemClass}
                onClick={item.onClick}
              >
                {item.label}
              </button>
            ) : (
              <Link to={item.to} className={itemClass}>
                {item.label}
              </Link>
            )}
          </MenuItem>
        );
      })}
    </MenuItems>
  </Menu>
);

export default MoreMenu;
