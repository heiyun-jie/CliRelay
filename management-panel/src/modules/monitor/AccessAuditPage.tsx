import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { RefreshCw, ShieldCheck, ShieldX } from "lucide-react";
import { accessAuditApi } from "@/lib/http/apis";
import type { AccessAuditItem, AccessAuditResponse } from "@/lib/http/apis/access-audit";
import { Tabs, TabsList, TabsTrigger } from "@/modules/ui/Tabs";
import { Select } from "@/modules/ui/Select";
import { OverflowTooltip } from "@/modules/ui/Tooltip";
import { useToast } from "@/modules/ui/ToastProvider";
import { VirtualTable, type VirtualTableColumn } from "@/modules/ui/VirtualTable";

type TimeRange = 1 | 7 | 14 | 30;
type AllowedFilter = "" | "allowed" | "denied";

interface AuditRow {
  id: string;
  timestamp: string;
  method: string;
  path: string;
  statusCode: number;
  allowed: boolean;
  authSubject: string;
  clientIp: string;
  forwardedFor: string;
  userAgent: string;
}

const PAGE_SIZE = 50;
const TIME_RANGES: readonly TimeRange[] = [1, 7, 14, 30] as const;

const formatTimestamp = (value: string): string => {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || "--";
  return date.toLocaleString();
};

const TimeRangeSelector = ({
  value,
  onChange,
}: {
  value: TimeRange;
  onChange: (next: TimeRange) => void;
}) => (
  <Tabs value={String(value)} onValueChange={(next) => onChange(Number(next) as TimeRange)}>
    <TabsList>
      {TIME_RANGES.map((range) => (
        <TabsTrigger key={range} value={String(range)}>
          {range === 1 ? "今天" : `${range} 天`}
        </TabsTrigger>
      ))}
    </TabsList>
  </Tabs>
);

const columns: VirtualTableColumn<AuditRow>[] = [
  {
    key: "timestamp",
    label: "时间",
    width: "w-52",
    cellClassName: "font-mono text-xs tabular-nums text-slate-700 dark:text-slate-200",
    render: (row) => (
      <OverflowTooltip content={formatTimestamp(row.timestamp)} className="block min-w-0">
        <span className="block min-w-0 truncate">{formatTimestamp(row.timestamp)}</span>
      </OverflowTooltip>
    ),
  },
  {
    key: "allowed",
    label: "结果",
    width: "w-24",
    render: (row) =>
      row.allowed ? (
        <span className="inline-flex min-w-[56px] items-center justify-center gap-1 rounded-full bg-emerald-50 px-2.5 py-1 text-xs font-semibold text-emerald-600 dark:bg-emerald-500/15 dark:text-emerald-300">
          <ShieldCheck size={12} />
          允许
        </span>
      ) : (
        <span className="inline-flex min-w-[56px] items-center justify-center gap-1 rounded-full bg-rose-50 px-2.5 py-1 text-xs font-semibold text-rose-600 dark:bg-rose-500/15 dark:text-rose-300">
          <ShieldX size={12} />
          拒绝
        </span>
      ),
  },
  {
    key: "statusCode",
    label: "状态码",
    width: "w-24",
    cellClassName: "text-right font-mono text-xs tabular-nums text-slate-700 dark:text-slate-200",
    render: (row) => <span className="block min-w-0 truncate">{row.statusCode}</span>,
  },
  {
    key: "authSubject",
    label: "认证结果",
    width: "w-32",
    cellClassName: "font-mono text-xs text-slate-600 dark:text-white/60",
    render: (row) => (
      <OverflowTooltip content={row.authSubject || "--"} className="block min-w-0">
        <span className="block min-w-0 truncate">{row.authSubject || "--"}</span>
      </OverflowTooltip>
    ),
  },
  {
    key: "method",
    label: "方法",
    width: "w-20",
    cellClassName: "font-mono text-xs text-slate-700 dark:text-slate-200",
    render: (row) => <span>{row.method || "--"}</span>,
  },
  {
    key: "path",
    label: "路径",
    width: "w-64",
    cellClassName: "font-mono text-xs text-slate-700 dark:text-slate-200",
    render: (row) => (
      <OverflowTooltip content={row.path || "--"} className="block min-w-0">
        <span className="block min-w-0 truncate">{row.path || "--"}</span>
      </OverflowTooltip>
    ),
  },
  {
    key: "clientIp",
    label: "来源 IP",
    width: "w-32",
    cellClassName: "font-mono text-xs text-slate-700 dark:text-slate-200",
    render: (row) => (
      <OverflowTooltip content={row.clientIp || "--"} className="block min-w-0">
        <span className="block min-w-0 truncate">{row.clientIp || "--"}</span>
      </OverflowTooltip>
    ),
  },
  {
    key: "forwardedFor",
    label: "转发链",
    width: "w-56",
    cellClassName: "font-mono text-xs text-slate-500 dark:text-white/55",
    render: (row) => (
      <OverflowTooltip content={row.forwardedFor || "--"} className="block min-w-0">
        <span className="block min-w-0 truncate">{row.forwardedFor || "--"}</span>
      </OverflowTooltip>
    ),
  },
  {
    key: "userAgent",
    label: "客户端",
    width: "w-64",
    render: (row) => (
      <OverflowTooltip content={row.userAgent || "--"} className="block min-w-0">
        <span className="block min-w-0 truncate text-xs text-slate-600 dark:text-white/60">
          {row.userAgent || "--"}
        </span>
      </OverflowTooltip>
    ),
  },
];

const toAuditRow = (item: AccessAuditItem): AuditRow => ({
  id: String(item.id),
  timestamp: item.timestamp,
  method: item.method,
  path: item.path,
  statusCode: item.status_code,
  allowed: item.allowed,
  authSubject: item.auth_subject,
  clientIp: item.client_ip,
  forwardedFor: item.forwarded_for,
  userAgent: item.user_agent,
});

export function AccessAuditPage() {
  const { notify } = useToast();
  const [rows, setRows] = useState<AuditRow[]>([]);
  const [timeRange, setTimeRange] = useState<TimeRange>(7);
  const [allowedFilter, setAllowedFilter] = useState<AllowedFilter>("");
  const [totalCount, setTotalCount] = useState(0);
  const [currentPage, setCurrentPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [lastUpdatedAt, setLastUpdatedAt] = useState<number | null>(null);
  const fetchInFlightRef = useRef(false);

  const fetchLogs = useCallback(
    async (page: number) => {
      if (fetchInFlightRef.current) return;
      fetchInFlightRef.current = true;
      if (page === 1) {
        setLoading(true);
      } else {
        setLoadingMore(true);
      }

      try {
        const response: AccessAuditResponse = await accessAuditApi.getAccessLogs({
          page,
          size: PAGE_SIZE,
          days: timeRange,
          allowed: allowedFilter,
        });
        const nextRows = (response.items ?? []).map(toAuditRow);
        if (page === 1) {
          setRows(nextRows);
        } else {
          setRows((prev) => [...prev, ...nextRows]);
        }
        setTotalCount(response.total ?? 0);
        setCurrentPage(page);
        setLastUpdatedAt(Date.now());
      } catch (err) {
        const message = err instanceof Error ? err.message : "访问审计刷新失败";
        notify({ type: "error", message });
      } finally {
        fetchInFlightRef.current = false;
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [allowedFilter, notify, timeRange],
  );

  useEffect(() => {
    void fetchLogs(1);
  }, [fetchLogs]);

  const hasMore = rows.length < totalCount;
  const loadNextPage = useCallback(() => {
    if (hasMore && !loading && !loadingMore) {
      void fetchLogs(currentPage + 1);
    }
  }, [currentPage, fetchLogs, hasMore, loading, loadingMore]);

  const lastUpdatedText = useMemo(() => {
    if (loading) return "刷新中…";
    if (!lastUpdatedAt) return "尚未刷新";
    return `更新于 ${new Date(lastUpdatedAt).toLocaleTimeString()}`;
  }, [lastUpdatedAt, loading]);

  return (
    <section className="flex flex-1 flex-col">
      <h1 className="sr-only">访问审计</h1>
      <div className="flex flex-1 flex-col rounded-2xl border border-slate-200 bg-white shadow-sm dark:border-neutral-800 dark:bg-neutral-950/70">
        <div className="flex flex-wrap items-center justify-between gap-3 px-5 pt-5 pb-3">
          <h2 className="text-base font-semibold text-slate-900 dark:text-white">访问审计</h2>
          <div className="flex flex-wrap items-center gap-2">
            <TimeRangeSelector value={timeRange} onChange={setTimeRange} />
            <button
              type="button"
              onClick={() => void fetchLogs(1)}
              disabled={loading}
              aria-busy={loading}
              className="inline-flex h-9 w-9 items-center justify-center rounded-2xl bg-slate-900 text-white transition hover:bg-slate-800 disabled:cursor-not-allowed disabled:opacity-70 dark:bg-white dark:text-neutral-950 dark:hover:bg-slate-200"
            >
              <RefreshCw size={14} className={loading ? "motion-safe:animate-spin" : ""} />
            </button>
          </div>
        </div>

        <div className="flex flex-wrap items-center gap-2 border-t border-slate-100 px-5 py-3 dark:border-neutral-800/60">
          <Select
            value={allowedFilter}
            onChange={(value) => setAllowedFilter(value as AllowedFilter)}
            options={[
              { value: "", label: "全部结果" },
              { value: "allowed", label: "仅允许" },
              { value: "denied", label: "仅拒绝" },
            ]}
            aria-label="按访问结果过滤"
            name="allowedFilter"
          />
          <div className="flex-1" />
          <span className="text-xs text-slate-500 dark:text-white/45">
            {totalCount.toLocaleString()} 条
            <span className="mx-2 text-slate-300 dark:text-white/10">·</span>
            {lastUpdatedText}
          </span>
        </div>

        <div className="relative px-5 pb-5">
          <VirtualTable<AuditRow>
            rows={rows}
            columns={columns}
            rowKey={(row) => row.id}
            loading={loading}
            hasMore={hasMore}
            loadingMore={loadingMore}
            onScrollBottom={loadNextPage}
            rowHeight={44}
            caption="访问审计表格"
            emptyText="暂无访问审计数据"
          />
        </div>
      </div>
    </section>
  );
}
