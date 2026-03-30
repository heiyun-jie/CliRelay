import { apiClient } from "@/lib/http/client";
import type { UsageData } from "@/lib/http/types";

export interface UsageExportPayload {
  version?: number;
  exported_at?: string;
  usage?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface UsageImportResponse {
  added?: number;
  skipped?: number;
  total_requests?: number;
  failed_requests?: number;
  [key: string]: unknown;
}

const MONITOR_USAGE_DAYS = 30;
const MONITOR_USAGE_PAGE_SIZE = 1000;

const createEmptyUsage = (): UsageData => ({ apis: {} });

const hasLegacyUsageDetails = (payload: unknown): payload is UsageData => {
  if (!payload || typeof payload !== "object") return false;
  const apis = (payload as { apis?: unknown }).apis;
  if (!apis || typeof apis !== "object") return false;

  for (const apiData of Object.values(apis as Record<string, unknown>)) {
    if (!apiData || typeof apiData !== "object") continue;
    const models = (apiData as { models?: unknown }).models;
    if (!models || typeof models !== "object") continue;

    for (const modelData of Object.values(models as Record<string, unknown>)) {
      if (!modelData || typeof modelData !== "object") continue;
      if (Array.isArray((modelData as { details?: unknown }).details)) {
        return true;
      }
    }
  }

  return false;
};

const buildUsageFromLogs = (items: UsageLogItem[]): UsageData => {
  const apis: UsageData["apis"] = {};

  for (const item of items) {
    const apiKey = item.api_key?.trim();
    const model = item.model?.trim();
    if (!apiKey || !model) continue;

    const apiBucket = (apis[apiKey] ??= { models: {} });
    const modelBucket = (apiBucket.models[model] ??= { details: [] });

    modelBucket.details.push({
      timestamp: item.timestamp,
      failed: item.failed,
      source: item.source,
      auth_index: item.auth_index,
      latency_ms: item.latency_ms,
      tokens: {
        input_tokens: item.input_tokens,
        output_tokens: item.output_tokens,
        reasoning_tokens: item.reasoning_tokens,
        cached_tokens: item.cached_tokens,
        total_tokens: item.total_tokens,
      },
    });
  }

  return { apis };
};

export const usageApi = {
  async getUsage(): Promise<UsageData> {
    const response = await apiClient.get<Record<string, unknown>>("/usage");
    const candidate =
      response.usage && typeof response.usage === "object" ? response.usage : response;

    if (!candidate || typeof candidate !== "object") {
      return createEmptyUsage();
    }

    if (hasLegacyUsageDetails(candidate)) {
      return candidate;
    }

    const items: UsageLogItem[] = [];
    let page = 1;
    let total = 0;

    try {
      do {
        const logResponse = await this.getUsageLogs({
          page,
          size: MONITOR_USAGE_PAGE_SIZE,
          days: MONITOR_USAGE_DAYS,
        });

        const nextItems = logResponse.items ?? [];
        total = logResponse.total ?? nextItems.length;
        items.push(...nextItems);

        if (nextItems.length === 0 || items.length >= total) {
          break;
        }

        page += 1;
      } while (page <= 50);
    } catch {
      return createEmptyUsage();
    }

    return buildUsageFromLogs(items);
  },

  async getUsageLogs(params: {
    page?: number;
    size?: number;
    days?: number;
    api_key?: string;
    model?: string;
    status?: string;
  }): Promise<UsageLogsResponse> {
    const qs = new URLSearchParams();
    if (params.page) qs.set("page", String(params.page));
    if (params.size) qs.set("size", String(params.size));
    if (params.days) qs.set("days", String(params.days));
    if (params.api_key) qs.set("api_key", params.api_key);
    if (params.model) qs.set("model", params.model);
    if (params.status) qs.set("status", params.status);
    const query = qs.toString();
    return apiClient.get<UsageLogsResponse>(`/usage/logs${query ? `?${query}` : ""}`);
  },

  exportUsage(): Promise<UsageExportPayload> {
    return apiClient.get<UsageExportPayload>("/usage/export");
  },

  importUsage(payload: unknown): Promise<UsageImportResponse> {
    return apiClient.post<UsageImportResponse>("/usage/import", payload);
  },

  getDashboardSummary(days = 7): Promise<DashboardSummary> {
    return apiClient.get<DashboardSummary>(`/dashboard-summary?days=${days}`);
  },

  async getLogContent(id: number): Promise<LogContentResponse> {
    return apiClient.get<LogContentResponse>(`/usage/logs/${id}/content`);
  },
};

export interface DashboardSummary {
  kpi: {
    total_requests: number;
    success_requests: number;
    failed_requests: number;
    success_rate: number;
    input_tokens: number;
    output_tokens: number;
    reasoning_tokens: number;
    cached_tokens: number;
    total_tokens: number;
  };
  counts: {
    api_keys: number;
    providers_total: number;
    gemini_keys: number;
    claude_keys: number;
    codex_keys: number;
    vertex_keys: number;
    openai_providers: number;
    auth_files: number;
  };
  days: number;
}

export interface UsageLogItem {
  id: number;
  timestamp: string;
  api_key: string;
  api_key_name: string;
  model: string;
  source: string;
  channel_name: string;
  auth_index: string;
  failed: boolean;
  latency_ms: number;
  input_tokens: number;
  output_tokens: number;
  reasoning_tokens: number;
  cached_tokens: number;
  total_tokens: number;
  cost: number;
  has_content: boolean;
  client_ip: string;
  forwarded_for: string;
  user_agent: string;
  request_path: string;
}

export interface UsageLogsResponse {
  items: UsageLogItem[];
  total: number;
  page: number;
  size: number;
  filters: {
    api_keys: string[];
    api_key_names: Record<string, string>;
    models: string[];
  };
  stats: {
    total: number;
    success_rate: number;
    total_tokens: number;
  };
}

export interface LogContentResponse {
  id: number;
  input_content: string;
  output_content: string;
  model: string;
}
