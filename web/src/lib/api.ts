const BASE = '';

async function fetchJSON<T>(url: string, params?: Record<string, string>, method = 'GET'): Promise<T> {
  const searchParams = new URLSearchParams();
  if (params) {
    for (const [key, value] of Object.entries(params)) {
      if (value) searchParams.set(key, value);
    }
  }
  const query = searchParams.toString();
  const fullUrl = query ? `${BASE}${url}?${query}` : `${BASE}${url}`;
  const res = await fetch(fullUrl, { method });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(body.error || res.statusText);
  }
  return res.json();
}

// ─── Types ───────────────────────────────────────────────────────────

export interface LogEntry {
  id: string;
  timestamp: string;
  merchantKey: string;
  region: string;
  method: string;
  path: string;
  statusCode: number;
  cacheStatus: string;
  totalLatencyMs: number;
  upstreamLatencyMs: number;
  requestContentLength: number;
  responseContentLength: number;
  piiRedacted: boolean;
  amazonRequestId: string;
  errorReason?: string;
}

export interface LogDetail {
  id: string;
  timestamp: string;
  merchantKey: string;
  region: string;
  method: string;
  path: string;
  queryParams?: string;
  requestHeaders?: Record<string, string>;
  statusCode: number;
  responseHeaders?: Record<string, string>;
  cacheStatus: string;
  cachedFromId?: string;
  cachedFromTimestamp?: string;
  cachedFromStatus?: number;
  queued: boolean;
  queueWaitMs: number;
  upstreamLatencyMs: number;
  totalLatencyMs: number;
  requestContentLength: number;
  responseContentLength: number;
  piiRedacted: boolean;
  amazonRequestId?: string;
  errorReason?: string;
  hasBody: boolean;
  replayAvailable: boolean;
  replayUnavailableReason?: string;
}

export interface LogBody {
  requestBody: unknown;
  responseBody: unknown;
}

export interface ReplayResult {
  available: boolean;
  reason?: string;
  statusCode?: number;
  responseHeaders?: Record<string, string>;
  responseBody?: unknown;
  replayError?: string;
  bodyUnavailable?: boolean;
}

export interface AuditEvent {
  id: string;
  timestamp: string;
  eventType: string;
  source: string;
  message: string;
  metadata?: Record<string, unknown>;
}

// ─── API Functions ───────────────────────────────────────────────────

export function getLogs(params: {
  from: string;
  to: string;
  merchant?: string;
  region?: string;
  endpoint?: string;
  status?: string;
  cacheStatus?: string;
  method?: string;
  queued?: string;
  minLatency?: string;
  maxLatency?: string;
  limit?: string;
  offset?: string;
}) {
  return fetchJSON<{ rows: LogEntry[]; total: number }>('/api/v1/logs', params as Record<string, string>);
}

export function getLogDetail(id: string) {
  return fetchJSON<LogDetail>(`/api/v1/logs/${encodeURIComponent(id)}`);
}

export function getLogBody(id: string) {
  return fetchJSON<LogBody>(`/api/v1/logs/${encodeURIComponent(id)}/body`);
}

export function getAuditEvents(params: {
  from: string;
  to: string;
  eventType?: string;
  limit?: string;
  offset?: string;
}) {
  return fetchJSON<{ rows: AuditEvent[]; total: number }>('/api/v1/audit', params as Record<string, string>);
}

export function getMerchants(q?: string) {
  return fetchJSON<{ merchants: string[] }>('/api/v1/merchants', q ? { q } : undefined);
}

export function replayLog(id: string) {
  return fetchJSON<ReplayResult>(`/api/v1/logs/${encodeURIComponent(id)}/replay`, undefined, 'POST');
}

// ─── Helpers ─────────────────────────────────────────────────────────

export type TimeRange = '15m' | '1h' | '6h' | '24h' | '7d' | '30d' | 'custom';

const RANGE_MS: Record<string, number> = {
  '15m': 900_000,
  '1h': 3_600_000,
  '6h': 21_600_000,
  '24h': 86_400_000,
  '7d': 604_800_000,
  '30d': 2_592_000_000
};

const MAX_RANGE_MS = 90 * 86_400_000; // 90 days

export function rangeToISO(range: TimeRange, customFrom?: string, customTo?: string): { from: string; to: string } {
  const now = new Date();
  if (range === 'custom' && customFrom && customTo) {
    let fromDate = new Date(customFrom);
    let toDate = new Date(customTo);
    // Clamp "to" to now.
    if (toDate.getTime() > now.getTime()) toDate = now;
    // Ensure "from" is before "to".
    if (fromDate.getTime() > toDate.getTime()) fromDate = new Date(toDate.getTime() - 3_600_000);
    // Cap range at 90 days.
    if (toDate.getTime() - fromDate.getTime() > MAX_RANGE_MS) fromDate = new Date(toDate.getTime() - MAX_RANGE_MS);
    return { from: fromDate.toISOString(), to: toDate.toISOString() };
  }
  return {
    from: new Date(now.getTime() - (RANGE_MS[range] || 3_600_000)).toISOString(),
    to: now.toISOString()
  };
}

export function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export function formatLatency(ms: number): string {
  if (ms < 1) return '<1ms';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export function formatNumber(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
  if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
  return n.toLocaleString();
}

export function formatPercent(n: number, decimals = 1): string {
  return (n * 100).toFixed(decimals) + '%';
}
