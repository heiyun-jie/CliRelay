import { useEffect, useRef } from "react";
import type { ECBasicOption } from "echarts/types/dist/shared";
import ReactECharts from "echarts-for-react";
import { useTheme } from "@/modules/ui/ThemeProvider";

export type EChartEvents = Record<string, (params: unknown, chart: unknown) => void>;

export function EChart({
  option,
  className,
  onEvents,
  notMerge = false,
  replaceMerge,
}: {
  option: ECBasicOption;
  className?: string;
  onEvents?: EChartEvents;
  notMerge?: boolean;
  replaceMerge?: string | string[];
}) {
  const {
    state: { mode },
  } = useTheme();

  const containerRef = useRef<HTMLDivElement | null>(null);
  const chartRef = useRef<any>(null);
  const instanceRef = useRef<any>(null);
  const prevInstanceRef = useRef<any>(null);
  const finishedHandlerRef = useRef<((...args: any[]) => void) | null>(null);
  const unlockTimerRef = useRef<number | null>(null);
  const canResizeRef = useRef(false);
  const pendingResizeRef = useRef(false);
  const rafIdRef = useRef<number | null>(null);
  const lastSizeRef = useRef<{ width: number; height: number } | null>(null);
  const hasSeenFirstSizeRef = useRef(false);

  const requestResize = (width: number, height: number) => {
    const container = containerRef.current;
    if (!container) return;

    if (!hasSeenFirstSizeRef.current) {
      hasSeenFirstSizeRef.current = true;
      lastSizeRef.current = { width, height };
      return;
    }

    const prev = lastSizeRef.current;
    if (prev && Math.abs(prev.width - width) < 0.5 && Math.abs(prev.height - height) < 0.5) return;
    lastSizeRef.current = { width, height };

    pendingResizeRef.current = true;
    if (!canResizeRef.current) return;
    if (rafIdRef.current !== null) return;

    rafIdRef.current = window.requestAnimationFrame(() => {
      rafIdRef.current = null;
      pendingResizeRef.current = false;

      const instance = instanceRef.current ?? chartRef.current?.getEchartsInstance?.();
      if (!instance) return;

      try {
        const containerWidth = container.clientWidth;
        const containerHeight = container.clientHeight;
        const chartWidth = instance.getWidth?.();
        const chartHeight = instance.getHeight?.();

        if (
          typeof chartWidth === "number" &&
          typeof chartHeight === "number" &&
          Math.abs(chartWidth - containerWidth) < 1 &&
          Math.abs(chartHeight - containerHeight) < 1
        ) {
          return;
        }

        instance.resize?.({
          width: "auto",
          height: "auto",
          animation: { duration: 0 },
        });
      } catch {
        // 忽略 resize 异常（例如实例尚未就绪）
      }
    });
  };

  useEffect(() => {
    const element = containerRef.current;
    if (!element) return;

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      if (!entry) return;
      requestResize(entry.contentRect.width, entry.contentRect.height);
    });

    observer.observe(element);
    return () => {
      observer.disconnect();
      if (rafIdRef.current !== null) {
        window.cancelAnimationFrame(rafIdRef.current);
        rafIdRef.current = null;
      }
      if (unlockTimerRef.current !== null) {
        window.clearTimeout(unlockTimerRef.current);
        unlockTimerRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    instanceRef.current = null;
    prevInstanceRef.current = null;
    canResizeRef.current = false;
    pendingResizeRef.current = false;
    hasSeenFirstSizeRef.current = false;
    lastSizeRef.current = null;
    if (unlockTimerRef.current !== null) {
      window.clearTimeout(unlockTimerRef.current);
      unlockTimerRef.current = null;
    }
  }, [mode]);

  return (
    <div ref={containerRef} className={["w-full min-w-0", className].filter(Boolean).join(" ")}>
      <ReactECharts
        ref={chartRef}
        option={option}
        theme={mode === "dark" ? "dark" : undefined}
        showLoading={false}
        notMerge={notMerge}
        replaceMerge={replaceMerge}
        lazyUpdate
        autoResize={false}
        className="h-full w-full"
        onEvents={onEvents}
        onChartReady={(instance: any) => {
          const prevInstance = prevInstanceRef.current;
          if (prevInstance && finishedHandlerRef.current) {
            try {
              prevInstance?.off?.("finished", finishedHandlerRef.current);
            } catch {
              // ignore
            }
          }

          prevInstanceRef.current = instance;
          instanceRef.current = instance;
          canResizeRef.current = false;

          try {
            instance?.hideLoading?.();
          } catch {
            // ignore
          }

          if (unlockTimerRef.current !== null) {
            window.clearTimeout(unlockTimerRef.current);
            unlockTimerRef.current = null;
          }

          const handler = () => {
            canResizeRef.current = true;
            try {
              instance?.off?.("finished", handler);
            } catch {
              // ignore
            }

            if (unlockTimerRef.current !== null) {
              window.clearTimeout(unlockTimerRef.current);
              unlockTimerRef.current = null;
            }

            if (!pendingResizeRef.current) return;
            const size = lastSizeRef.current;
            if (!size) return;
            requestResize(size.width, size.height);
          };

          finishedHandlerRef.current = handler;
          try {
            instance?.on?.("finished", handler);
          } catch {
            // ignore
            canResizeRef.current = true;
          }

          unlockTimerRef.current = window.setTimeout(() => {
            unlockTimerRef.current = null;
            if (canResizeRef.current) return;
            canResizeRef.current = true;
            if (!pendingResizeRef.current) return;
            const size = lastSizeRef.current;
            if (!size) return;
            requestResize(size.width, size.height);
          }, 700);
        }}
      />
    </div>
  );
}
