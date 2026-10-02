import { useEffect } from "react";

export function usePolling(
  load: (signal: AbortSignal) => Promise<void>,
  seconds: number,
  enabled = true,
) {
  useEffect(() => {
    if (!enabled) return;
    const controller = new AbortController();
    let running = false;
    let timer: ReturnType<typeof setTimeout>;
    async function refresh() {
      if (running) return;
      running = true;
      clearTimeout(timer);
      try {
        await load(controller.signal);
      } finally {
        running = false;
        if (!controller.signal.aborted)
          timer = setTimeout(refresh, seconds * 1000);
      }
    }
    const focus = () => {
      if (!document.hidden) void refresh();
    };
    void refresh();
    window.addEventListener("focus", focus);
    document.addEventListener("visibilitychange", focus);
    return () => {
      controller.abort();
      clearTimeout(timer);
      window.removeEventListener("focus", focus);
      document.removeEventListener("visibilitychange", focus);
    };
  }, [load, seconds, enabled]);
}
