import { useCallback, useEffect, useMemo, useState, useTransition } from "react";
import {
  Brain,
  RefreshCw,
  Search,
  Sparkles,
} from "lucide-react";
import { memoryApi } from "@/lib/http/apis";
import type { ConversationTurn, MemoryApplicationLog, MemoryEntry } from "@/lib/http/types";
import { Button } from "@/modules/ui/Button";
import { Card } from "@/modules/ui/Card";
import { EmptyState } from "@/modules/ui/EmptyState";
import { TextInput } from "@/modules/ui/Input";
import { Select } from "@/modules/ui/Select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/modules/ui/Tabs";
import { ToggleSwitch } from "@/modules/ui/ToggleSwitch";
import { useToast } from "@/modules/ui/ToastProvider";

type MemoryTab = "entries" | "applications" | "turns";
type MemoryScope = "all" | "global" | "api_key";

type MemoryDraft = {
  scopeType: "global" | "api_key";
  scopeValue: string;
  kind: string;
  content: string;
  tagsText: string;
  source: string;
  priorityText: string;
  alwaysApply: boolean;
  active: boolean;
};

const defaultDraft: MemoryDraft = {
  scopeType: "global",
  scopeValue: "",
  kind: "note",
  content: "",
  tagsText: "",
  source: "manual",
  priorityText: "0",
  alwaysApply: false,
  active: true,
};

const scopeOptions = [
  { value: "all", label: "全部范围" },
  { value: "global", label: "全局" },
  { value: "api_key", label: "API Key" },
] as const;

const createScopeOptions = [
  { value: "global", label: "全局记忆" },
  { value: "api_key", label: "API Key 记忆" },
] as const;

const kindOptions = [
  { value: "note", label: "普通备注" },
  { value: "preference", label: "偏好" },
  { value: "project", label: "项目上下文" },
  { value: "constraint", label: "约束" },
  { value: "decision", label: "决策" },
] as const;

const trimToEmpty = (value: string) => value.trim();

const formatDateTime = (value: string) => {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value || "--";
  return date.toLocaleString("zh-CN");
};

const truncate = (value: string, length: number) => {
  const text = String(value ?? "").trim();
  if (!text) return "--";
  return text.length > length ? `${text.slice(0, length)}…` : text;
};

const scopeLabel = (entry: Pick<MemoryEntry, "scope_type" | "scope_value">) => {
  if (entry.scope_type === "api_key") {
    return entry.scope_value ? `API Key · ${truncate(entry.scope_value, 16)}` : "API Key";
  }
  return "全局";
};

const reasonLabel = (value: string) => {
  if (value === "always_apply") return "常驻命中";
  if (value === "query_match") return "查询匹配";
  return value || "--";
};

const parseTags = (value: string) =>
  value
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean);

function StatChip({
  label,
  value,
  tone = "slate",
}: {
  label: string;
  value: string | number;
  tone?: "slate" | "emerald" | "amber" | "blue";
}) {
  const toneClass: Record<typeof tone, string> = {
    slate: "bg-slate-100 text-slate-700 dark:bg-white/10 dark:text-white/75",
    emerald: "bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300",
    amber: "bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300",
    blue: "bg-blue-100 text-blue-700 dark:bg-blue-500/15 dark:text-blue-300",
  };

  return (
    <span
      className={[
        "inline-flex items-center gap-1 rounded-full px-2.5 py-1 text-xs font-medium",
        toneClass[tone],
      ].join(" ")}
    >
      <span className="opacity-70">{label}</span>
      <span>{value}</span>
    </span>
  );
}

export function MemoryPage() {
  const { notify } = useToast();
  const [isPending, startTransition] = useTransition();
  const [tab, setTab] = useState<MemoryTab>("entries");
  const [scopeFilter, setScopeFilter] = useState<MemoryScope>("all");
  const [apiKeyFilterInput, setApiKeyFilterInput] = useState("");
  const [apiKeyFilter, setApiKeyFilter] = useState("");
  const [activeOnly, setActiveOnly] = useState(true);
  const [entries, setEntries] = useState<MemoryEntry[]>([]);
  const [applications, setApplications] = useState<MemoryApplicationLog[]>([]);
  const [turns, setTurns] = useState<ConversationTurn[]>([]);
  const [entriesLoading, setEntriesLoading] = useState(true);
  const [applicationsLoading, setApplicationsLoading] = useState(true);
  const [turnsLoading, setTurnsLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [draft, setDraft] = useState<MemoryDraft>(defaultDraft);

  const loadEntries = useCallback(async () => {
    setEntriesLoading(true);
    try {
      const data = await memoryApi.listEntries({
        scopeType: scopeFilter,
        apiKey: scopeFilter === "api_key" ? apiKeyFilter : undefined,
        activeOnly,
        limit: 100,
      });
      startTransition(() => {
        setEntries(data);
      });
    } catch (error) {
      notify({ type: "error", message: error instanceof Error ? error.message : "记忆条目加载失败" });
    } finally {
      setEntriesLoading(false);
    }
  }, [activeOnly, apiKeyFilter, notify, scopeFilter]);

  const loadApplications = useCallback(async () => {
    setApplicationsLoading(true);
    try {
      const data = await memoryApi.listApplications({
        apiKey: apiKeyFilter || undefined,
        limit: 80,
      });
      startTransition(() => {
        setApplications(data);
      });
    } catch (error) {
      notify({ type: "error", message: error instanceof Error ? error.message : "命中记录加载失败" });
    } finally {
      setApplicationsLoading(false);
    }
  }, [apiKeyFilter, notify]);

  const loadTurns = useCallback(async () => {
    if (!apiKeyFilter.trim()) {
      setTurns([]);
      setTurnsLoading(false);
      return;
    }
    setTurnsLoading(true);
    try {
      const data = await memoryApi.listTurns({
        apiKey: apiKeyFilter,
        limit: 30,
      });
      startTransition(() => {
        setTurns(data);
      });
    } catch (error) {
      notify({ type: "error", message: error instanceof Error ? error.message : "最近轮次加载失败" });
    } finally {
      setTurnsLoading(false);
    }
  }, [apiKeyFilter, notify]);

  const refreshAll = useCallback(async () => {
    await Promise.all([loadEntries(), loadApplications(), loadTurns()]);
  }, [loadApplications, loadEntries, loadTurns]);

  useEffect(() => {
    void refreshAll();
  }, [refreshAll]);

  const applyFilter = useCallback(() => {
    setApiKeyFilter(trimToEmpty(apiKeyFilterInput));
  }, [apiKeyFilterInput]);

  const createMemory = useCallback(async () => {
    const content = trimToEmpty(draft.content);
    const scopeValue = trimToEmpty(draft.scopeValue);
    const priority = Number(draft.priorityText.trim());

    if (!content) {
      notify({ type: "warning", message: "请先填写记忆内容" });
      return;
    }
    if (draft.scopeType === "api_key" && !scopeValue) {
      notify({ type: "warning", message: "API Key 作用域必须填写 scope value" });
      return;
    }
    if (!Number.isFinite(priority)) {
      notify({ type: "warning", message: "priority 必须是数字" });
      return;
    }

    setCreating(true);
    try {
      await memoryApi.createEntry({
        scopeType: draft.scopeType,
        scopeValue,
        kind: draft.kind,
        content,
        tags: parseTags(draft.tagsText),
        source: trimToEmpty(draft.source) || "manual",
        priority,
        alwaysApply: draft.alwaysApply,
        active: draft.active,
      });
      notify({ type: "success", message: "记忆条目已创建" });
      setDraft(defaultDraft);
      if (draft.scopeType === "api_key" && scopeValue) {
        setScopeFilter("api_key");
        setApiKeyFilterInput(scopeValue);
        setApiKeyFilter(scopeValue);
      }
      await refreshAll();
      setTab("entries");
    } catch (error) {
      notify({ type: "error", message: error instanceof Error ? error.message : "创建记忆失败" });
    } finally {
      setCreating(false);
    }
  }, [draft, notify, refreshAll]);

  const entrySummary = useMemo(() => {
    const alwaysApplyCount = entries.filter((entry) => entry.always_apply).length;
    const apiScopedCount = entries.filter((entry) => entry.scope_type === "api_key").length;
    return { alwaysApplyCount, apiScopedCount };
  }, [entries]);

  const latestHitAt = applications[0]?.timestamp ?? "";
  const latestTurnAt = turns[0]?.timestamp ?? "";

  return (
    <div className="space-y-4">
      <section className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-neutral-800 dark:bg-neutral-950/70">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-2">
            <h2 className="flex items-center gap-2 text-lg font-semibold text-slate-900 dark:text-white">
              <Brain size={18} />
              <span>记忆中心</span>
            </h2>
            <p className="max-w-2xl text-sm text-slate-600 dark:text-white/65">
              管理长时记忆、查看命中记录，并按 API Key 回放最近用户轮次。后端现在已经支持自动注入与自动持久化，这里负责可视化管理。
            </p>
            <div className="flex flex-wrap gap-2">
              <StatChip label="记忆条目" value={entries.length} tone="blue" />
              <StatChip label="常驻" value={entrySummary.alwaysApplyCount} tone="emerald" />
              <StatChip label="API Key 作用域" value={entrySummary.apiScopedCount} tone="amber" />
              <StatChip label="最近命中" value={latestHitAt ? formatDateTime(latestHitAt) : "--"} />
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Select
              value={scopeFilter}
              onChange={(value) => setScopeFilter(value as MemoryScope)}
              options={scopeOptions.map((item) => ({ value: item.value, label: item.label }))}
              aria-label="记忆范围过滤"
              className="min-w-[120px]"
            />
            <div className="inline-flex items-center gap-1.5 rounded-2xl border border-slate-200 bg-white px-2.5 py-1.5 shadow-sm dark:border-neutral-800 dark:bg-neutral-950/60">
              <Search size={14} className="text-slate-500 dark:text-white/55" />
              <TextInput
                value={apiKeyFilterInput}
                onChange={(event) => setApiKeyFilterInput(event.target.value)}
                variant="ghost"
                className="w-44"
                placeholder="API Key 过滤"
              />
            </div>
            <Button variant="secondary" size="sm" onClick={applyFilter}>
              应用过滤
            </Button>
            <Button variant="secondary" size="sm" onClick={() => void refreshAll()} disabled={isPending}>
              <RefreshCw size={14} className={isPending ? "animate-spin" : ""} />
              刷新
            </Button>
          </div>
        </div>
      </section>

      <Tabs value={tab} onValueChange={(value) => setTab(value as MemoryTab)}>
        <TabsList>
          <TabsTrigger value="entries">记忆条目</TabsTrigger>
          <TabsTrigger value="applications">命中记录</TabsTrigger>
          <TabsTrigger value="turns">最近轮次</TabsTrigger>
        </TabsList>

        <TabsContent value="entries" className="mt-4">
          <section className="grid gap-4 xl:grid-cols-[380px_minmax(0,1fr)]">
            <Card
              title="新建记忆"
              description="支持全局或 API Key 作用域。priority 仅影响排序，不会替代匹配条件。"
              actions={<StatChip label="最近轮次" value={latestTurnAt ? formatDateTime(latestTurnAt) : "--"} tone="slate" />}
            >
              <div className="space-y-4">
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-slate-600 dark:text-white/65">作用域</label>
                    <Select
                      value={draft.scopeType}
                      onChange={(value) =>
                        setDraft((prev) => ({
                          ...prev,
                          scopeType: value as MemoryDraft["scopeType"],
                          scopeValue: value === "global" ? "" : prev.scopeValue,
                        }))
                      }
                      options={createScopeOptions.map((item) => ({ value: item.value, label: item.label }))}
                      aria-label="新建记忆作用域"
                      className="w-full justify-between"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-slate-600 dark:text-white/65">类型</label>
                    <Select
                      value={draft.kind}
                      onChange={(value) => setDraft((prev) => ({ ...prev, kind: value }))}
                      options={kindOptions.map((item) => ({ value: item.value, label: item.label }))}
                      aria-label="记忆类型"
                      className="w-full justify-between"
                    />
                  </div>
                </div>

                {draft.scopeType === "api_key" ? (
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-slate-600 dark:text-white/65">Scope Value</label>
                    <TextInput
                      value={draft.scopeValue}
                      onChange={(event) => setDraft((prev) => ({ ...prev, scopeValue: event.target.value }))}
                      placeholder="填写完整 API Key"
                    />
                  </div>
                ) : null}

                <div className="space-y-1.5">
                  <label className="text-xs font-medium text-slate-600 dark:text-white/65">内容</label>
                  <textarea
                    value={draft.content}
                    onChange={(event) => setDraft((prev) => ({ ...prev, content: event.target.value }))}
                    rows={6}
                    placeholder="例如：用户偏好简洁回答；当前项目用 CliRelay 作为网关；不要改动生产配置。"
                    className="w-full rounded-2xl border border-slate-200 bg-white px-3 py-2.5 text-sm text-slate-900 outline-none transition placeholder:text-slate-400 focus:border-slate-300 dark:border-neutral-800 dark:bg-neutral-900 dark:text-slate-100 dark:placeholder:text-neutral-500"
                  />
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-slate-600 dark:text-white/65">标签</label>
                    <TextInput
                      value={draft.tagsText}
                      onChange={(event) => setDraft((prev) => ({ ...prev, tagsText: event.target.value }))}
                      placeholder="memory, clirelay, frontend"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-slate-600 dark:text-white/65">来源</label>
                    <TextInput
                      value={draft.source}
                      onChange={(event) => setDraft((prev) => ({ ...prev, source: event.target.value }))}
                      placeholder="manual"
                    />
                  </div>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1.5">
                    <label className="text-xs font-medium text-slate-600 dark:text-white/65">Priority</label>
                    <TextInput
                      value={draft.priorityText}
                      onChange={(event) => setDraft((prev) => ({ ...prev, priorityText: event.target.value }))}
                      placeholder="0"
                    />
                  </div>
                  <div className="rounded-2xl border border-slate-200 bg-slate-50/70 px-3 py-2.5 text-xs text-slate-600 dark:border-neutral-800 dark:bg-neutral-900/60 dark:text-white/60">
                    <p>标签会参与匹配，priority 只影响排序。</p>
                    <p className="mt-1">`always_apply` 才代表无条件注入。</p>
                  </div>
                </div>

                <div className="space-y-3 rounded-2xl border border-slate-200 bg-slate-50/70 p-3 dark:border-neutral-800 dark:bg-neutral-900/50">
                  <ToggleSwitch
                    checked={draft.alwaysApply}
                    onCheckedChange={(next) => setDraft((prev) => ({ ...prev, alwaysApply: next }))}
                    label="Always Apply"
                    description="无论 query 是否匹配，都允许注入。"
                  />
                  <ToggleSwitch
                    checked={draft.active}
                    onCheckedChange={(next) => setDraft((prev) => ({ ...prev, active: next }))}
                    label="Active"
                    description="关闭后保留记录，但不再参与命中。"
                  />
                  <ToggleSwitch
                    checked={activeOnly}
                    onCheckedChange={setActiveOnly}
                    label="仅查看激活条目"
                    description="影响右侧列表查询，不影响创建。"
                  />
                </div>

                <Button onClick={() => void createMemory()} disabled={creating}>
                  <Sparkles size={14} />
                  {creating ? "创建中…" : "创建记忆"}
                </Button>
              </div>
            </Card>

            <Card
              title="已存记忆"
              description="按优先级和更新时间排序。当前列表受顶部范围/API Key 过滤影响。"
              loading={entriesLoading}
            >
              {entries.length === 0 ? (
                <EmptyState title="暂无记忆条目" description="先在左侧创建一条记忆，或调整筛选条件。" />
              ) : (
                <div className="space-y-3">
                  {entries.map((entry) => (
                    <article
                      key={entry.id}
                      className="rounded-2xl border border-slate-200 bg-slate-50/75 p-4 dark:border-neutral-800 dark:bg-neutral-900/50"
                    >
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="space-y-2">
                          <div className="flex flex-wrap items-center gap-2">
                            <StatChip label="ID" value={entry.id} />
                            <StatChip label="范围" value={scopeLabel(entry)} tone="blue" />
                            <StatChip label="类型" value={entry.kind || "--"} tone="amber" />
                            <StatChip label="优先级" value={entry.priority} />
                            {entry.always_apply ? <StatChip label="模式" value="Always Apply" tone="emerald" /> : null}
                            {!entry.active ? <StatChip label="状态" value="Inactive" tone="amber" /> : null}
                          </div>
                          <p className="text-sm leading-6 text-slate-800 dark:text-slate-100">{entry.content}</p>
                          {entry.tags?.length ? (
                            <div className="flex flex-wrap gap-2">
                              {entry.tags.map((tag) => (
                                <span
                                  key={`${entry.id}-${tag}`}
                                  className="rounded-full border border-slate-200 bg-white px-2 py-1 text-xs text-slate-600 dark:border-neutral-700 dark:bg-neutral-950/60 dark:text-white/60"
                                >
                                  #{tag}
                                </span>
                              ))}
                            </div>
                          ) : null}
                        </div>
                        <div className="min-w-[160px] space-y-1 text-right text-xs text-slate-500 dark:text-white/55">
                          <p>来源：{entry.source || "--"}</p>
                          <p>创建：{formatDateTime(entry.created_at)}</p>
                          <p>更新：{formatDateTime(entry.updated_at)}</p>
                        </div>
                      </div>
                    </article>
                  ))}
                </div>
              )}
            </Card>
          </section>
        </TabsContent>

        <TabsContent value="applications" className="mt-4">
          <Card
            title="命中记录"
            description="显示最近哪些记忆被注入到了请求里。API Key 过滤为空时展示全量记录。"
            loading={applicationsLoading}
          >
            {applications.length === 0 ? (
              <EmptyState title="暂无命中记录" description="等实际请求命中记忆后，这里会显示注入轨迹。" />
            ) : (
              <div className="space-y-3">
                {applications.map((item) => (
                  <article
                    key={item.id}
                    className="rounded-2xl border border-slate-200 bg-white/80 p-4 dark:border-neutral-800 dark:bg-neutral-900/45"
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="space-y-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <StatChip label="Entry" value={item.memory_entry_id || "--"} tone="blue" />
                          <StatChip label="原因" value={reasonLabel(item.match_reason)} tone="emerald" />
                          <StatChip label="路径" value={item.request_path || "--"} />
                        </div>
                        <div className="space-y-1">
                          <p className="text-xs font-medium uppercase tracking-[0.16em] text-slate-500 dark:text-white/45">
                            Query
                          </p>
                          <p className="text-sm leading-6 text-slate-800 dark:text-slate-100">
                            {truncate(item.query_text, 220)}
                          </p>
                        </div>
                        <div className="space-y-1">
                          <p className="text-xs font-medium uppercase tracking-[0.16em] text-slate-500 dark:text-white/45">
                            Injected
                          </p>
                          <pre className="overflow-x-auto whitespace-pre-wrap rounded-2xl bg-slate-950 px-3 py-3 text-xs leading-5 text-slate-100 dark:bg-black/70">
                            {truncate(item.injected_text, 480)}
                          </pre>
                        </div>
                      </div>
                      <div className="min-w-[160px] space-y-1 text-right text-xs text-slate-500 dark:text-white/55">
                        <p>时间：{formatDateTime(item.timestamp)}</p>
                        <p>API Key：{truncate(item.api_key, 20)}</p>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </Card>
        </TabsContent>

        <TabsContent value="turns" className="mt-4">
          <Card
            title="最近轮次"
            description="读取后端为指定 API Key 自动记录的用户与助手对话。这里必须提供 API Key 过滤。"
            loading={turnsLoading}
          >
            {!apiKeyFilter ? (
              <EmptyState
                title="请先填写 API Key"
                description="顶部过滤器中的 API Key 应用后，这里才会拉取最近轮次。"
              />
            ) : turns.length === 0 ? (
              <EmptyState title="暂无最近轮次" description="当前 API Key 还没有被记录的用户请求。" />
            ) : (
              <div className="space-y-3">
                {turns.map((turn) => (
                  <article
                    key={turn.id}
                    className="rounded-2xl border border-slate-200 bg-slate-50/80 p-4 dark:border-neutral-800 dark:bg-neutral-900/45"
                  >
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="space-y-2">
                        <div className="flex flex-wrap items-center gap-2">
                          <StatChip label="模型" value={turn.model || "--"} tone="blue" />
                          <StatChip label="路径" value={turn.request_path || "--"} />
                        </div>
                        <div className="space-y-2 rounded-2xl border border-slate-200 bg-white/70 p-3 dark:border-neutral-800 dark:bg-neutral-950/40">
                          <div className="space-y-1">
                            <p className="text-xs font-medium uppercase tracking-[0.16em] text-slate-500 dark:text-white/45">
                              User
                            </p>
                            <p className="text-sm leading-6 text-slate-800 dark:text-slate-100">
                              {turn.user_text || "--"}
                            </p>
                          </div>
                          <div className="space-y-1">
                            <p className="text-xs font-medium uppercase tracking-[0.16em] text-slate-500 dark:text-white/45">
                              Assistant
                            </p>
                            <p className="text-sm leading-6 text-slate-700 dark:text-white/80">
                              {turn.assistant_text?.trim() ? turn.assistant_text : "--"}
                            </p>
                          </div>
                        </div>
                      </div>
                      <div className="min-w-[160px] space-y-1 text-right text-xs text-slate-500 dark:text-white/55">
                        <p>时间：{formatDateTime(turn.timestamp)}</p>
                        <p>API Key：{truncate(turn.api_key, 20)}</p>
                      </div>
                    </div>
                  </article>
                ))}
              </div>
            )}
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  );
}
