import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";
import { startOfWeek, today } from "helpers/dates";
import React from "react";
import { Link } from "react-router";

interface WarningsLinkProps {
  // How many warnings there are; nothing shows for none.
  count: number;
  // A date of the week whose warnings to open.
  date: string;
  // What they're about, e.g. "this week", "today" or "for Charlotte's day".
  about: string;
}

/**
 * WarningsLink is one line saying how many warnings there are, leading to
 * the page that lists them, in place of showing each beside its meal.
 */
const WarningsLink: React.FC<WarningsLinkProps> = ({ count, date, about }) => {
  if (count === 0) {
    return null;
  }
  const monday = startOfWeek(date);
  return (
    <Link
      to={
        monday === startOfWeek(today())
          ? "/plan/warnings"
          : `/plan/warnings?week=${monday}`
      }
      className="inline-flex items-center gap-1.5 self-start rounded-xl bg-warn-tint px-2.5 py-1.5 text-sm font-bold text-warn-dark print:hidden"
    >
      <ExclamationTriangleIcon className="size-4 shrink-0" aria-hidden="true" />
      {count} {count === 1 ? "warning" : "warnings"} {about}
    </Link>
  );
};

export default WarningsLink;
