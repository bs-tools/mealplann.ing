import { CheckIcon, ClipboardIcon } from "@heroicons/react/24/outline";
import type { ReactNode } from "react";
import React, { useState } from "react";

import { secondaryButton } from "./styles";

interface EmptyStateProps {
  title: string;
  children?: ReactNode;
}

export const EmptyState: React.FC<EmptyStateProps> = ({ title, children }) => (
  <div className="flex flex-col items-center gap-2 rounded-2xl border-2 border-dashed border-line px-4 py-8 text-center">
    <p className="font-bold text-ink-soft">{title}</p>
    {children !== undefined && (
      <div className="text-sm text-muted">{children}</div>
    )}
  </div>
);

interface PageHeaderProps {
  // A line above the title, saying where the page is or what it's for.
  kicker?: ReactNode;
  title: ReactNode;
  subtitle?: ReactNode;
  actions?: ReactNode;
}

export const PageHeader: React.FC<PageHeaderProps> = ({
  kicker,
  title,
  subtitle,
  actions,
}) => (
  <div className="mb-4 flex flex-wrap items-end justify-between gap-3">
    <div className="min-w-0">
      {kicker !== undefined && (
        <p className="text-sm font-medium text-muted">{kicker}</p>
      )}
      <h1 className="text-[28px] leading-tight font-bold tracking-tight text-ink-soft">
        {title}
      </h1>
      {subtitle !== undefined && (
        <div className="mt-1 text-sm text-muted">{subtitle}</div>
      )}
    </div>
    {actions !== undefined && (
      <div className="flex flex-wrap items-center gap-2">{actions}</div>
    )}
  </div>
);

interface StatProps {
  label: string;
  value: ReactNode;
  sub?: ReactNode;
}

export const Stat: React.FC<StatProps> = ({ label, value, sub }) => (
  <div className="flex flex-col rounded-2xl border border-line bg-white px-3.5 py-3">
    <span className="text-xs font-bold tracking-[0.08em] text-muted uppercase">
      {label}
    </span>
    <span className="text-lg font-bold text-ink-soft tabular-nums">
      {value}
    </span>
    {sub !== undefined && <span className="text-xs text-muted">{sub}</span>}
  </div>
);

interface BadgeProps {
  children: ReactNode;
  className?: string;
  title?: string;
}

export const Badge: React.FC<BadgeProps> = ({
  children,
  className = "bg-ground-dark text-ink-soft ring-transparent",
  title,
}) => (
  <span
    title={title}
    className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium whitespace-nowrap ring-1 ring-inset ${className}`}
  >
    {children}
  </span>
);

interface ProgressBarProps {
  value: number;
  target: number;
  className?: string;
}

/**
 * ProgressBar shows value against target. Overshooting the target shows a
 * full bar in a darker color.
 */
export const ProgressBar: React.FC<ProgressBarProps> = ({
  value,
  target,
  className = "bg-pine",
}) => {
  if (target <= 0) {
    return null;
  }
  const fraction = Math.min(value / target, 1);
  const over = value > target * 1.1;
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-ground-dark">
      <div
        className={`h-full rounded-full ${over ? "bg-warn" : className}`}
        style={{ width: `${fraction * 100}%` }}
      />
    </div>
  );
};

interface CopyFieldProps {
  value: string;
  multiline?: boolean;
}

// CopyField shows a value in a monospace box with a copy button.
export const CopyField: React.FC<CopyFieldProps> = ({
  value,
  multiline = false,
}) => {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard.writeText(value).then(() => {
      setCopied(true);
      globalThis.setTimeout(() => {
        setCopied(false);
      }, 1500);
    });
  };
  /*
   * It shrinks to its container, scrolling a long value, rather than
   * widening a column or dialog to the value's width.
   */
  return (
    <div className="flex w-full min-w-0 items-start gap-2">
      {multiline ? (
        <pre className="min-w-0 grow overflow-x-auto rounded-xl bg-ink-soft p-3 font-mono text-xs text-gray-100">
          {value}
        </pre>
      ) : (
        <code className="min-w-0 grow overflow-x-auto rounded-xl bg-ground-dark px-2 py-1.5 font-mono text-xs whitespace-nowrap text-ink-soft ring-1 ring-line">
          {value}
        </code>
      )}
      <button
        type="button"
        className={`${secondaryButton} shrink-0`}
        onClick={copy}
        aria-label="Copy to clipboard"
      >
        {copied ? (
          <CheckIcon className="size-4 text-ready" />
        ) : (
          <ClipboardIcon className="size-4" />
        )}
        <span className="hidden sm:inline">{copied ? "Copied" : "Copy"}</span>
      </button>
    </div>
  );
};
