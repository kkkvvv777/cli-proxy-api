import { apiClient } from './client';
import type { Config } from '@/types';

export interface Employee {
  id: string;
  name: string;
  keyPrefix: string;
  enabled: boolean;
}

interface RawEmployee {
  id: string;
  name: string;
  key_prefix: string;
  enabled: boolean;
}

export interface AuditEntry {
  kind: 'request' | 'attempt';
  timestamp: string;
  userId: string;
  userName: string;
  requestedModel: string;
  model: string;
  provider: string;
  statusCode: number;
  failed: boolean;
  inputTokens: number;
  outputTokens: number;
  totalTokens: number;
  latencyMs: number;
  stream: boolean;
  requestId: string;
}

interface RawAuditEntry {
  kind: 'request' | 'attempt';
  timestamp: string;
  user_id: string;
  user_name: string;
  requested_model?: string;
  model: string;
  response_model?: string;
  provider: string;
  status_code: number;
  failed: boolean;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  latency_ms: number;
  stream: boolean;
  request_id: string;
}

export function isCompanyMode(config: Config | null): boolean {
  const value = config?.raw?.['company-gateway'];
  return (
    typeof value === 'object' && value !== null && 'enabled' in value && value.enabled === true
  );
}

const employee = (value: RawEmployee): Employee => ({
  id: value.id,
  name: value.name,
  keyPrefix: value.key_prefix,
  enabled: value.enabled,
});

const secret = (value: { user: RawEmployee; api_key: string }) => ({
  user: employee(value.user),
  apiKey: value.api_key,
});

export const companyApi = {
  async list(signal?: AbortSignal) {
    const data = await apiClient.get<{ users: RawEmployee[] }>('/company/users', { signal });
    return data.users.map(employee);
  },
  async create(id: string, name: string) {
    return secret(await apiClient.post('/company/users', { id, name }));
  },
  async update(id: string, changes: { name?: string; enabled?: boolean }) {
    await apiClient.patch(`/company/users/${encodeURIComponent(id)}`, changes);
  },
  async rotate(id: string) {
    return secret(await apiClient.post(`/company/users/${encodeURIComponent(id)}/rotate`));
  },
  async remove(id: string) {
    await apiClient.delete(`/company/users/${encodeURIComponent(id)}`);
  },
  async audit(userId: string, limit: number, signal?: AbortSignal) {
    const data = await apiClient.get<{ entries: RawAuditEntry[]; healthy: boolean }>(
      '/company/audit',
      { params: { user_id: userId.trim(), limit }, signal }
    );
    const entries: AuditEntry[] = data.entries.map((entry) => ({
      kind: entry.kind,
      timestamp: entry.timestamp,
      userId: entry.user_id,
      userName: entry.user_name,
      requestedModel: entry.requested_model || '',
      model: entry.response_model || entry.model,
      provider: entry.provider,
      statusCode: entry.status_code,
      failed: entry.failed,
      inputTokens: entry.input_tokens,
      outputTokens: entry.output_tokens,
      totalTokens: entry.total_tokens,
      latencyMs: entry.latency_ms,
      stream: entry.stream,
      requestId: entry.request_id,
    }));
    return { entries, healthy: data.healthy };
  },
};
