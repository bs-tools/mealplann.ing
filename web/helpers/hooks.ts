import type { RefObject } from "react";
import { useEffect, useState } from "react";
import { useLocation } from "react-router";

/**
 * UseDebouncedValue returns the value once it hasn't changed for delayMs,
 * e.g. to search as the user types without a request per keystroke.
 */
export const useDebouncedValue = <Value>(
  value: Value,
  delayMs = 250
): Value => {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timeout = globalThis.setTimeout(() => {
      setDebounced(value);
    }, delayMs);
    return () => {
      globalThis.clearTimeout(timeout);
    };
  }, [value, delayMs]);
  return debounced;
};

// UseElementWidth tracks the rendered width of an element in pixels.
export const useElementWidth = (ref: RefObject<HTMLElement | null>): number => {
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const element = ref.current;
    if (element === null) {
      return undefined;
    }
    setWidth(element.getBoundingClientRect().width);
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setWidth(entry.contentRect.width);
      }
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, [ref]);
  return width;
};

/**
 * UseFragmentToken returns the secret token in the fragment of the page's
 * URL, e.g. of an invitation link, /invite#<token>. Links carry their tokens
 * there because browsers don't send fragments to the server, so the tokens
 * stay out of request logs. It's "" if there is none.
 */
export const useFragmentToken = (): string => {
  const fragment = useLocation().hash.slice(1);
  try {
    return decodeURIComponent(fragment);
  } catch {
    return fragment;
  }
};
