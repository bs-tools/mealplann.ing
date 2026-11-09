/**
 * Parse helpers for form inputs. Parsers return null for invalid input and
 * treat an empty string as zero, which the API uses for "not set".
 */

const DIGITS = /^\d+$/u;

/**
 * ParseDuration parses "h:mm", "h:mm:ss" or plain minutes ("45") into
 * seconds.
 */
export const parseDuration = (text: string): number | null => {
  const trimmed = text.trim();
  if (trimmed === "") {
    return 0;
  }
  const parts = trimmed.split(":");
  for (const part of parts) {
    if (!DIGITS.test(part)) {
      return null;
    }
  }
  const numbers = parts.map(Number);
  const [first = 0, second = 0, third = 0] = numbers;
  if (numbers.length === 1) {
    return first * 60;
  }
  if (numbers.length === 2) {
    if (second >= 60) {
      return null;
    }
    return first * 3600 + second * 60;
  }
  if (numbers.length === 3) {
    if (second >= 60 || third >= 60) {
      return null;
    }
    return first * 3600 + second * 60 + third;
  }
  return null;
};

const pad = (value: number): string => String(value).padStart(2, "0");

/**
 * DurationInput formats seconds for a duration text input: "h:mm" or
 * "h:mm:ss" with seconds. Zero is formatted as an empty string.
 */
export const durationInput = (
  seconds: number,
  withSeconds: boolean
): string => {
  if (seconds <= 0) {
    return "";
  }
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const secs = Math.round(seconds % 60);
  if (withSeconds) {
    return `${hours}:${pad(minutes)}:${pad(secs)}`;
  }
  return `${hours}:${pad(minutes)}`;
};

// ParseNumber parses a decimal number. Empty input is zero.
export const parseNumber = (text: string): number | null => {
  const trimmed = text.trim();
  if (trimmed === "") {
    return 0;
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < 0) {
    return null;
  }
  return value;
};

// NumberInput formats a number for a text input. Zero is an empty string.
export const numberInput = (value: number): string => {
  if (value === 0) {
    return "";
  }
  return String(Math.round(value * 100) / 100);
};

/**
 * ParseEdited parses an input that was initialized with format(original),
 * returning original if the input still shows it. Inputs show values
 * rounded, e.g. durations without seconds, so parsing
 * an input the user didn't touch would change its value when saving.
 */
export const parseEdited = (
  text: string,
  original: number,
  format: (value: number) => string,
  parse: (text: string) => number | null
): number | null => {
  if (text === format(original)) {
    return original;
  }
  return parse(text);
};

// HoursMinutesInput formats seconds as "h:mm" for an input.
export const hoursMinutesInput = (seconds: number): string =>
  durationInput(seconds, false);

/**
 * ParseOptionalNumber parses a decimal number that may be unknown: empty
 * input is undefined, which is different from 0. It returns null for
 * invalid input. A comma is taken as the decimal point, as many phones type
 * it.
 */
export const parseOptionalNumber = (
  text: string
): number | undefined | null => {
  const trimmed = text.trim().replace(",", ".");
  if (trimmed === "") {
    return undefined;
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < 0) {
    return null;
  }
  return value;
};

// OptionalNumberInput formats a number that may be unknown for an input.
export const optionalNumberInput = (value: number | undefined): string =>
  value === undefined ? "" : String(value);

// VULGAR_FRACTIONS are the fraction characters recipes write, by value.
const VULGAR_FRACTIONS: Record<string, number> = {
  "¼": 1 / 4,
  "½": 1 / 2,
  "¾": 3 / 4,
  "⅓": 1 / 3,
  "⅔": 2 / 3,
  "⅛": 1 / 8,
};

/**
 * ParseQuantity parses an amount as recipes write it: a decimal number,
 * or a fraction, like "1/2", "1 1/2", "½" or "1½". Like
 * parseOptionalNumber, empty input is undefined and invalid input null.
 */
export const parseQuantity = (text: string): number | undefined | null => {
  let trimmed = text.trim();
  for (const [fraction, value] of Object.entries(VULGAR_FRACTIONS)) {
    trimmed = trimmed.replace(fraction, ` ${value}`);
  }
  const parts = trimmed.split(/\s+/u).filter((part) => part !== "");
  if (parts.length === 0) {
    return undefined;
  }
  let total = 0;
  for (const part of parts) {
    const [numerator, denominator, ...rest] = part.split("/");
    if (rest.length > 0) {
      return null;
    }
    const value =
      denominator === undefined
        ? parseOptionalNumber(numerator ?? "")
        : (parseOptionalNumber(numerator ?? "") ?? Number.NaN) /
          (parseOptionalNumber(denominator) ?? Number.NaN);
    if (
      value === null ||
      value === undefined ||
      !Number.isFinite(value) ||
      value < 0
    ) {
      return null;
    }
    total += value;
  }
  return total;
};
