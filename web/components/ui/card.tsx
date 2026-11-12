import type { ReactNode } from "react";
import React from "react";

import { sectionLabelClass } from "./styles";

interface CardProps {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}

/**
 * Card is a section of a page in the app's design: its title a small
 * uppercase label above a soft white card, with any actions beside the
 * label, rather than in a bar across the card.
 */
const Card: React.FC<CardProps> = ({
  title,
  actions,
  children,
  className = "",
}) => (
  <section className={`flex flex-col gap-2 ${className}`}>
    {(title !== undefined || actions !== undefined) && (
      <div className="flex min-h-8 flex-wrap items-center justify-between gap-2">
        <h2 className={sectionLabelClass}>{title}</h2>
        {actions !== undefined && (
          <div className="flex flex-wrap items-center gap-1">{actions}</div>
        )}
      </div>
    )}
    <div className="rounded-2xl border border-line bg-white p-4 print:border-gray-300">
      {children}
    </div>
  </section>
);

export default Card;
