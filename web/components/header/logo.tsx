import React from "react";

/*
 * The mark's shapes, in a 64 unit box: a lowercase m, and a leaf sprouting
 * from its right shoulder, with a vein cut out of it in the ground's color.
 */
export const MARK_M =
  "M8 54V33c0-6 3.5-10 8-10s8 4 8 10v21M24 33c0-6 3.5-10 8-10s8 4 8 10v21";
export const MARK_LEAF =
  "M40 29C40 17 46.5 10.5 58 9.5c0 11.5-6.5 18.5-18 19.5z";
export const MARK_VEIN = "M42.5 25.5C46 20 50 15.5 55 12";

interface LogoMarkProps {
  // The color of the m: light on dark grounds, pine on light ones.
  letter?: string;
  // The color of the leaf.
  leaf?: string;
  // The color of the ground, which the leaf's vein is cut out in.
  ground?: string;
  className?: string;
}

/**
 * LogoMark is the mealplann.ing mark: a lowercase m with a leaf sprouting
 * from its shoulder. Its defaults suit a dark ground.
 */
export const LogoMark: React.FC<LogoMarkProps> = ({
  letter = "#eef1ec",
  leaf = "#7ed3a6",
  ground = "#111111",
  className = "size-8",
}) => (
  <svg aria-hidden="true" viewBox="4 6 56 52" fill="none" className={className}>
    <path
      d={MARK_M}
      stroke={letter}
      strokeWidth="6.5"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
    <path d={MARK_LEAF} fill={leaf} />
    <path
      d={MARK_VEIN}
      stroke={ground}
      strokeWidth="1.6"
      strokeLinecap="round"
    />
  </svg>
);

// Wordmark is the mealplann.ing name, with .ing in the accent color.
export const Wordmark: React.FC<{ className?: string }> = ({
  className = "",
}) => (
  <span className={`font-brand font-bold ${className}`}>
    mealplann<span className="text-leaf">.ing</span>
  </span>
);
