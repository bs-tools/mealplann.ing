// Shared Tailwind class names for common elements.

export const primaryButton =
  "inline-flex items-center justify-center gap-1.5 rounded-xl bg-ink-soft px-4 py-2.5 text-sm font-bold text-white hover:bg-black disabled:cursor-not-allowed disabled:opacity-50";

export const secondaryButton =
  "inline-flex items-center justify-center gap-1.5 rounded-xl bg-white px-4 py-2.5 text-sm font-medium text-ink-soft ring-1 ring-line ring-inset hover:bg-ground-dark/50 disabled:cursor-not-allowed disabled:opacity-50";

export const dangerButton =
  "inline-flex items-center justify-center gap-1.5 rounded-xl bg-error px-4 py-2.5 text-sm font-bold text-white hover:bg-error/90 disabled:cursor-not-allowed disabled:opacity-50";

export const ghostButton =
  "inline-flex items-center justify-center gap-1 rounded-lg px-2 py-1 text-sm font-medium text-muted hover:bg-ground-dark hover:text-ink-soft disabled:cursor-not-allowed disabled:opacity-50";

// Destructive actions, such as deleting, in the secondary and ghost styles.
export const secondaryDangerButton =
  "inline-flex items-center justify-center gap-1.5 rounded-xl bg-white px-4 py-2.5 text-sm font-medium text-error ring-1 ring-error/40 ring-inset hover:bg-error-tint disabled:cursor-not-allowed disabled:opacity-50";

export const ghostDangerButton =
  "inline-flex items-center justify-center gap-1 rounded-lg px-2 py-1 text-sm font-medium text-error hover:bg-error-tint hover:text-error disabled:cursor-not-allowed disabled:opacity-50";

export const iconButton =
  "inline-flex items-center justify-center rounded-lg p-1.5 text-muted hover:bg-ground-dark hover:text-ink-soft disabled:cursor-not-allowed disabled:opacity-40";

export const inputClass =
  "block w-full rounded-xl border border-gray-300 bg-white px-3 py-2.5 text-sm text-ink-soft placeholder:text-gray-500 focus:border-pine focus:ring-1 focus:ring-pine focus:outline-none";

export const labelClass = "block text-xs font-medium text-muted";

export const linkClass =
  "font-medium text-pine hover:text-pine-dark hover:underline";

export const cardClass =
  "rounded-2xl border border-line bg-white print:border-gray-300";

export const tableClass = "min-w-full divide-y divide-gray-200 text-sm";

export const thClass =
  "px-2 py-2 text-left text-xs font-semibold tracking-wide text-gray-500 uppercase whitespace-nowrap";

export const tdClass = "px-2 py-2 align-top text-gray-800";

/**
 * ChoiceClass styles a button of a group of choices, like the days of a
 * week, filled when chosen.
 */
export const choiceClass = (active: boolean): string =>
  `rounded-full px-3 py-1.5 text-sm font-medium ring-1 ring-inset ${
    active
      ? "bg-ink-soft font-bold text-white ring-ink-soft"
      : "bg-white text-ink-soft ring-line hover:bg-ground-dark/50"
  }`;

// SegmentedClass styles the track of a segmented control.
export const segmentedClass = "flex rounded-xl bg-ground-dark p-[3px]";

/**
 * SegmentClass styles a segment of a segmented control, a white tab
 * sliding over the track when chosen.
 */
export const segmentClass = (active: boolean): string =>
  `flex-1 rounded-[10px] px-2 py-2 text-sm whitespace-nowrap ${
    active
      ? "bg-white font-bold text-ink-soft shadow-sm"
      : "text-muted hover:text-ink-soft"
  }`;

// SectionLabelClass styles the small uppercase label above a section.
export const sectionLabelClass =
  "text-xs font-bold tracking-[0.08em] text-muted uppercase";

/*
 * StickyBarClass is a page's bar of primary actions kept above the tab
 * bar: solid and edge to edge, with a hairline above it, as a translucent
 * rounded one showed a pale outline around its buttons over white cards.
 */
export const stickyBarClass =
  "sticky bottom-[3.75rem] z-10 -mx-4 border-t border-line bg-ground px-4 py-3 sm:-mx-6 sm:px-6 lg:bottom-0 print:hidden";
