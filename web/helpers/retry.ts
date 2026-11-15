import type { Interceptor } from "@connectrpc/connect";
import { Code, ConnectError } from "@connectrpc/connect";

// MAX_RETRIES is how many times a request turned away is sent again.
const MAX_RETRIES = 4;

// FIRST_DELAY_MS is the wait before the first retry, doubled for each one.
const FIRST_DELAY_MS = 500;

/**
 * TurnedAway reports whether an error is Cloud Run turning a request away
 * before the server saw it: "Rate exceeded.", an HTTP 429, while its one
 * instance starts, like after a deploy or when it scaled to zero, or is
 * busy. The server's own errors, like a request too large, say otherwise.
 */
export const turnedAway = (error: unknown): boolean =>
  error instanceof ConnectError &&
  (error.code === Code.Unavailable || error.code === Code.ResourceExhausted) &&
  /rate exceeded|\b429\b/iu.test(error.rawMessage);

const sleep = async (ms: number): Promise<void> => {
  await new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
};

/**
 * RetryTurnedAway sends a request again, after a growing wait, when Cloud
 * Run turned it away (see turnedAway). It's safe for every request,
 * changes too, as the server never saw it.
 */
export const retryTurnedAway: Interceptor = (next) => async (request) => {
  for (let attempt = 0; ; attempt += 1) {
    try {
      // oxlint-disable-next-line no-await-in-loop
      return await next(request);
    } catch (error) {
      if (attempt >= MAX_RETRIES || !turnedAway(error)) {
        throw error;
      }
      // oxlint-disable-next-line no-await-in-loop
      await sleep(FIRST_DELAY_MS * 2 ** attempt * (0.75 + Math.random() / 2));
    }
  }
};
