/**
 * Generic auto-refresh controller.
 * Calls `fn` on a fixed interval, with pause/resume support.
 */
export interface AutoRefreshController {
  start(): void;
  stop(): void;
  isRunning(): boolean;
  setInterval(ms: number): void;
  setCallback(fn: () => void): void;
}

export function createAutoRefresh(
  fn: () => void,
  intervalMs: number
): AutoRefreshController {
  let timer: ReturnType<typeof setInterval> | null = null;
  let callback = fn;
  let interval = intervalMs;

  function start() {
    stop();
    timer = setInterval(() => callback(), interval);
  }

  function stop() {
    if (timer !== null) {
      clearInterval(timer);
      timer = null;
    }
  }

  function isRunning() {
    return timer !== null;
  }

  function setIntervalMs(ms: number) {
    interval = ms;
  }

  function setCallbackFn(fn: () => void) {
    callback = fn;
  }

  return { start, stop, isRunning, setInterval: setIntervalMs, setCallback: setCallbackFn };
}
