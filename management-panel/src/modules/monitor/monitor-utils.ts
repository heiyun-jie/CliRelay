import type { UsageData, UsageDetail } from "@/lib/http/types";

export interface KpiMetrics {
  requestCount: number;
  successCount: number;
  failedCount: number;
  successRate: number;
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  reasoningTokens: number;
  cachedTokens: number;
}

export interface UsageRecord extends UsageDetail {
  model: string;
}

export const filterUsageByDays = (data: UsageData, days: number, apiFilter: string): UsageData => {
  const now = Date.now();
  const cutoff = now - days * 24 * 60 * 60 * 1000;
  const normalizedFilter = apiFilter.trim().toLowerCase();

  const filtered: UsageData = { apis: {} };

  for (const [apiKey, apiData] of Object.entries(data.apis)) {
    if (normalizedFilter && !apiKey.toLowerCase().includes(normalizedFilter)) {
      continue;
    }

    const nextModels: UsageData["apis"][string]["models"] = {};

    for (const [modelName, modelData] of Object.entries(apiData.models)) {
      const details = modelData.details.filter((detail) => {
        const timestamp = new Date(detail.timestamp).getTime();
        return Number.isFinite(timestamp) ? timestamp >= cutoff : false;
      });

      if (details.length > 0) {
        nextModels[modelName] = { details };
      }
    }

    if (Object.keys(nextModels).length > 0) {
      filtered.apis[apiKey] = { models: nextModels };
    }
  }

  return filtered;
};

export const iterateUsageRecords = (data: UsageData): UsageRecord[] => {
  const details: UsageRecord[] = [];
  for (const apiData of Object.values(data.apis)) {
    for (const [model, modelData] of Object.entries(apiData.models)) {
      details.push(...modelData.details.map((detail) => ({ ...detail, model })));
    }
  }
  return details;
};

export const computeKpiMetrics = (data: UsageData): KpiMetrics => {
  const details = iterateUsageRecords(data);

  const requestCount = details.length;
  const failedCount = details.reduce((count, detail) => (detail.failed ? count + 1 : count), 0);
  const successCount = requestCount - failedCount;
  const successRate = requestCount === 0 ? 0 : (successCount / requestCount) * 100;

  const inputTokens = details.reduce((sum, detail) => sum + (detail.tokens?.input_tokens ?? 0), 0);
  const outputTokens = details.reduce(
    (sum, detail) => sum + (detail.tokens?.output_tokens ?? 0),
    0,
  );
  const reasoningTokens = details.reduce(
    (sum, detail) => sum + (detail.tokens?.reasoning_tokens ?? 0),
    0,
  );
  const cachedTokens = details.reduce(
    (sum, detail) => sum + (detail.tokens?.cached_tokens ?? 0),
    0,
  );
  const totalTokens = details.reduce((sum, detail) => sum + (detail.tokens?.total_tokens ?? 0), 0);

  return {
    requestCount,
    successCount,
    failedCount,
    successRate,
    totalTokens,
    inputTokens,
    outputTokens,
    reasoningTokens,
    cachedTokens,
  };
};

export const formatNumber = (value: number): string => {
  return new Intl.NumberFormat("zh-CN").format(Math.round(value));
};

export const formatRate = (value: number): string => {
  return `${value.toFixed(2)}%`;
};
