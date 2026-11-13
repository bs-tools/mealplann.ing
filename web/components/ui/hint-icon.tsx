import React from "react";

interface HintIconProps {
  // The icon, like ExclamationTriangleIcon.
  icon: React.ComponentType<React.SVGProps<SVGSVGElement>>;
  // What it means, shown on hover and focus, and read by screen readers.
  hint: string;
  className?: string;
}

/**
 * HintIcon is a small icon that says what it means when hovered or
 * focused, like a warning triangle beside an ingredient lacking data, so
 * it isn't a mystery.
 */
const HintIcon: React.FC<HintIconProps> = ({
  icon: Icon,
  hint,
  className = "",
}) => (
  /*
   * A focusable span with a title shows the hint on hover, and to keyboard
   * and screen reader users through its label.
   */
  // oxlint-disable-next-line jsx-a11y/no-noninteractive-tabindex
  <span
    role="img"
    aria-label={hint}
    title={hint}
    tabIndex={0}
    className={`inline-flex shrink-0 cursor-help self-center rounded outline-none focus-visible:ring-2 focus-visible:ring-pine ${className}`}
  >
    <Icon className="size-4" aria-hidden="true" />
  </span>
);

export default HintIcon;
