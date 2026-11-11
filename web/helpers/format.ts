import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { timestampDate } from "@bufbuild/protobuf/wkt";

import { daysBetween, parseDate, today } from "./dates";

const pad = (value: number): string => String(value).padStart(2, "0");

// FormatDuration formats seconds as h:mm, e.g. "3:05".
export const formatDuration = (seconds: number): string => {
  const sign = seconds < 0 ? "-" : "";
  const total = Math.round(Math.abs(seconds) / 60);
  const hours = Math.floor(total / 60);
  const minutes = total % 60;
  return `${sign}${hours}:${pad(minutes)}`;
};

// FormatDurationLong formats seconds as h:mm:ss, e.g. "3:05:09".
export const formatDurationLong = (seconds: number): string => {
  const sign = seconds < 0 ? "-" : "";
  const total = Math.round(Math.abs(seconds));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  return `${sign}${hours}:${pad(minutes)}:${pad(secs)}`;
};

/**
 * FormatHoursMinutes formats seconds as e.g. "3h 05m", or "34 min" under an
 * hour.
 */
export const formatHoursMinutes = (seconds: number): string => {
  const total = Math.round(seconds / 60);
  const hours = Math.floor(total / 60);
  const minutes = total % 60;
  if (hours === 0) {
    return `${minutes} min`;
  }
  return `${hours}h ${pad(minutes)}m`;
};

// FormatDate formats YYYY-MM-DD as e.g. "Mon, 12 Oct 2026".
export const formatDate = (value: string): string => {
  if (value === "") {
    return "";
  }
  return parseDate(value).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    weekday: "short",
    year: "numeric",
  });
};

// FormatShortDate formats YYYY-MM-DD as e.g. "12 Oct".
export const formatShortDate = (value: string): string => {
  if (value === "") {
    return "";
  }
  return parseDate(value).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
  });
};

/**
 * FormatNearDate formats YYYY-MM-DD as e.g. "12 Oct", with the year if it
 * isn't this year's, e.g. "12 Oct 2027".
 */
export const formatNearDate = (value: string): string => {
  if (value === "") {
    return "";
  }
  if (value.slice(0, 4) === today().slice(0, 4)) {
    return formatShortDate(value);
  }
  return parseDate(value).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
};

// FormatDay formats YYYY-MM-DD as a day of a week, e.g. "Mon, 12 Oct".
export const formatDay = (value: string): string => {
  if (value === "") {
    return "";
  }
  return parseDate(value).toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    weekday: "short",
  });
};

// FormatWeekday formats YYYY-MM-DD as its weekday, e.g. "Thu".
export const formatWeekday = (value: string): string => {
  if (value === "") {
    return "";
  }
  return parseDate(value).toLocaleDateString(undefined, { weekday: "short" });
};

// FormatMonth formats YYYY-MM-DD as e.g. "October 2026".
export const formatMonth = (value: string): string =>
  parseDate(value).toLocaleDateString(undefined, {
    month: "long",
    year: "numeric",
  });

// FormatShortMonth formats YYYY-MM-DD as e.g. "Oct".
export const formatShortMonth = (value: string): string =>
  parseDate(value).toLocaleDateString(undefined, { month: "short" });

// FormatTimestamp formats a protobuf timestamp as a local date and time.
export const formatTimestamp = (timestamp: Timestamp | undefined): string => {
  if (timestamp === undefined) {
    return "never";
  }
  return timestampDate(timestamp).toLocaleString(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
};

// FormatDaysUntil describes how far away a date is, e.g. "in 12 days".
export const formatDaysUntil = (value: string): string => {
  const days = daysBetween(today(), value);
  if (days === 0) {
    return "today";
  }
  if (days === 1) {
    return "tomorrow";
  }
  if (days === -1) {
    return "yesterday";
  }
  if (days < 0) {
    return `${-days} days ago`;
  }
  return `in ${days} days`;
};

// DOLLAR is the currency sign of prices, in Canadian dollars.
const DOLLAR = "$";

// FormatDollars formats cents as dollars, e.g. "$3.49".
export const formatDollars = (cents: number): string =>
  `${DOLLAR}${(Math.round(cents) / 100).toFixed(2)}`;

export const formatPercent = (fraction: number): string =>
  `${Math.round(fraction * 100)}%`;

/**
 * FormatRedirectTarget describes the app an OAuth client redirects to, from
 * its redirect URI, so users can tell which app they are connecting.
 */
export const formatRedirectTarget = (uri: string): string => {
  try {
    const url = new URL(uri);
    if (["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)) {
      return "an app on this computer";
    }
    if (url.protocol === "https:") {
      return url.host;
    }
    return `${url.protocol}//${url.host}`;
  } catch {
    return uri;
  }
};

// FormatBytes formats a size in bytes, e.g. "234 MB".
export const formatBytes = (bytes: number): string => {
  if (bytes < 1_000_000) {
    return `${Math.round(bytes / 1000)} KB`;
  }
  if (bytes < 1_000_000_000) {
    return `${Math.round(bytes / 1_000_000)} MB`;
  }
  return `${(bytes / 1_000_000_000).toFixed(2)} GB`;
};

// FormatList joins items into an English list, e.g. "Switzerland and
// Liechtenstein".
export const formatList = (items: string[]): string =>
  new Intl.ListFormat("en", { type: "conjunction" }).format(items);

// FormatGrams formats a weight, e.g. "450 g" or "1.25 kg".
export const formatGrams = (grams: number): string => {
  if (grams >= 1000) {
    return `${(grams / 1000).toLocaleString(undefined, { maximumFractionDigits: 2 })} kg`;
  }
  return `${Math.round(grams).toLocaleString()} g`;
};

/**
 * FormatStock formats an amount in stock in the units it's bought in: its
 * packages when the food has a package size, like "4 packages", else its
 * pieces, like "3 pieces", else its weight. Partial packages and pieces
 * are rounded to a quarter, prefixed by "~" when that's a guess.
 */
export const formatStock = (
  grams: number,
  gramsPerPiece: number,
  packageGrams = 0
): string => {
  const count = (size: number, singular: string, plural: string): string => {
    const exact = grams / size;
    const rounded = Math.round(exact * 4) / 4;
    const text = rounded === 0 ? "¼" : rounded.toLocaleString();
    const near = Math.abs(exact - rounded) > 0.01 ? "~" : "";
    return `${near}${text} ${rounded > 1 ? plural : singular}`;
  };
  if (packageGrams > 0) {
    return count(packageGrams, "package", "packages");
  }
  if (gramsPerPiece > 0) {
    return count(gramsPerPiece, "piece", "pieces");
  }
  return formatGrams(grams);
};
