import React from "react";

interface LogoMarkProps {
  // The color of the leaf: white on dark grounds, ink on light ones.
  leaf?: string;
  // The color of the ground, which the leaf's midrib is cut out in.
  ground?: string;
  className?: string;
}

// LogoMark is the foodplann.ing mark: a leaf sprouting from a bowl.
export const LogoMark: React.FC<LogoMarkProps> = ({
  leaf = "#ffffff",
  ground = "#111111",
  className = "h-[25px] w-[38px]",
}) => (
  <svg aria-hidden="true" viewBox="0 0 40 26" fill="none" className={className}>
    <path
      d="M3 13 L37 13 C 35 20, 28 23, 20 23 C 12 23, 5 20, 3 13"
      className="stroke-leaf"
      strokeWidth="2.6"
      strokeLinecap="round"
      strokeLinejoin="round"
    />
    <path
      d="M20 12 C 13.5 10.5, 13 4, 21.5 1.5 C 26.5 5.5, 25.5 10.5, 20 12 Z"
      fill={leaf}
    />
    <path
      d="M20 11.5 Q 19.6 7, 21.4 2.6"
      stroke={ground}
      strokeWidth="0.9"
      strokeLinecap="round"
    />
    <path
      d="M15 25 L25 25"
      stroke="#404040"
      strokeWidth="1.5"
      strokeLinecap="round"
    />
  </svg>
);

// Wordmark is the foodplann.ing name, with .ing in the accent color.
export const Wordmark: React.FC<{ className?: string }> = ({
  className = "",
}) => (
  <span className={`font-brand font-bold ${className}`}>
    foodplann<span className="text-leaf">.ing</span>
  </span>
);
