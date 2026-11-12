import { ChevronLeftIcon } from "@heroicons/react/24/outline";
import React from "react";
import { Link } from "react-router";

interface BackLinkProps {
  to: string;
  children: React.ReactNode;
}

/**
 * BackLink leads back to the page a page belongs to, as a small line above
 * its title, e.g. "‹ Pantry" above "Add", so it doesn't compete with it.
 */
const BackLink: React.FC<BackLinkProps> = ({ to, children }) => (
  <Link
    to={to}
    className="-mb-2 inline-flex items-center gap-0.5 self-start text-sm font-medium text-pine hover:text-pine-dark print:hidden"
  >
    <ChevronLeftIcon className="size-4 stroke-2" aria-hidden="true" />
    {children}
  </Link>
);

export default BackLink;
