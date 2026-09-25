import type { Clock } from "./types.ts";

export const systemClock: Clock = {
  now: () => Date.now(),
  setTimer: (delayMs, fn) => setTimeout(fn, delayMs),
  clearTimer: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
  sleep: (delayMs) => new Promise((resolve) => setTimeout(resolve, delayMs)),
};
