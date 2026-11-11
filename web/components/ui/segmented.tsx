import { segmentClass, segmentedClass } from "@components/ui/styles";
import React from "react";

export interface SegmentOption<Value extends string> {
  value: Value;
  label: string;
}

interface SegmentedProps<Value extends string> {
  // The control's name, for screen readers.
  label: string;
  options: SegmentOption<Value>[];
  value: Value;
  onChange: (value: Value) => void;
  className?: string;
}

/**
 * Segmented is a row of choices in a track, the chosen one a white tab
 * over it, like Week and Month on the plan.
 */
const Segmented = <Value extends string>({
  label,
  options,
  value,
  onChange,
  className = "",
}: SegmentedProps<Value>): React.ReactElement => (
  <div
    role="radiogroup"
    aria-label={label}
    className={`${segmentedClass} ${className}`}
  >
    {options.map((option) => (
      <button
        key={option.value}
        type="button"
        role="radio"
        aria-checked={option.value === value}
        className={segmentClass(option.value === value)}
        onClick={() => {
          onChange(option.value);
        }}
      >
        {option.label}
      </button>
    ))}
  </div>
);

export default Segmented;
