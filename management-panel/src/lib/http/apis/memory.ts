import { apiClient } from "@/lib/http/client";
import type { ConversationTurn, MemoryApplicationLog, MemoryEntry } from "@/lib/http/types";

type MemoryEntryPayload = {
  items?: MemoryEntry[];
};

type MemoryApplicationPayload = {
  items?: MemoryApplicationLog[];
};

type ConversationTurnPayload = {
  items?: ConversationTurn[];
};

export const memoryApi = {
  async listEntries(input?: {
    scopeType?: string;
    apiKey?: string;
    activeOnly?: boolean;
    limit?: number;
  }) {
    const params: Record<string, string | number | boolean | null | undefined> = {
      limit: input?.limit,
      active_only: input?.activeOnly,
    };
    if (input?.scopeType && input.scopeType !== "all") {
      params.scope_type = input.scopeType;
    }
    if (input?.apiKey?.trim()) {
      params.api_key = input.apiKey.trim();
    }
    const response = await apiClient.get<MemoryEntryPayload>("/memory", { params });
    return Array.isArray(response?.items) ? response.items : [];
  },

  createEntry(input: {
    scopeType: string;
    scopeValue?: string;
    kind: string;
    content: string;
    tags?: string[];
    source?: string;
    priority?: number;
    alwaysApply?: boolean;
    active?: boolean;
  }) {
    return apiClient.post<MemoryEntry>("/memory", {
      scope_type: input.scopeType,
      scope_value: input.scopeValue?.trim() ?? "",
      kind: input.kind,
      content: input.content,
      tags: input.tags ?? [],
      source: input.source?.trim() ?? "",
      priority: input.priority ?? 0,
      always_apply: Boolean(input.alwaysApply),
      active: input.active ?? true,
    });
  },

  async listApplications(input?: { apiKey?: string; limit?: number }) {
    const response = await apiClient.get<MemoryApplicationPayload>("/memory/applications", {
      params: {
        api_key: input?.apiKey?.trim() || undefined,
        limit: input?.limit,
      },
    });
    return Array.isArray(response?.items) ? response.items : [];
  },

  async listTurns(input: { apiKey: string; limit?: number }) {
    const response = await apiClient.get<ConversationTurnPayload>("/memory/turns", {
      params: {
        api_key: input.apiKey.trim(),
        limit: input.limit,
      },
    });
    return Array.isArray(response?.items) ? response.items : [];
  },
};
