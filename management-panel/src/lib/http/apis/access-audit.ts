import { apiClient } from "@/lib/http/client";

export interface AccessAuditItem {
  id: number;
  timestamp: string;
  client_ip: string;
  forwarded_for: string;
  user_agent: string;
  method: string;
  path: string;
  status_code: number;
  allowed: boolean;
  auth_subject: string;
}

export interface AccessAuditResponse {
  items: AccessAuditItem[];
  total: number;
  page: number;
  size: number;
}

export const accessAuditApi = {
  getAccessLogs(params: {
    page?: number;
    size?: number;
    days?: number;
    allowed?: "allowed" | "denied" | "";
    auth_subject?: string;
  }): Promise<AccessAuditResponse> {
    return apiClient.get<AccessAuditResponse>("/usage/access-logs", {
      params: {
        page: params.page,
        size: params.size,
        days: params.days,
        allowed: params.allowed || undefined,
        auth_subject: params.auth_subject || undefined,
      },
    });
  },
};
