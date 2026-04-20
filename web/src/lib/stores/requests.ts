import { writable, get } from 'svelte/store';
import { getLogs, getLogDetail, getLogBody, type LogEntry, type LogDetail, type LogBody } from '$lib/api';

export const logs = writable<LogEntry[]>([]);
export const totalLogs = writable(0);
export const selectedLog = writable<LogDetail | null>(null);
export const logBody = writable<LogBody | null>(null);
export const loading = writable(false);
export const detailLoading = writable(false);
export const currentPage = writable(0);
export const newCount = writable(0);

const PAGE_SIZE = 50;

export interface LogFilters {
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
}

export async function fetchLogs(filters: LogFilters, page = 0) {
  loading.set(true);
  currentPage.set(page);
  newCount.set(0);
  try {
    const data = await getLogs({
      ...filters,
      limit: String(PAGE_SIZE),
      offset: String(page * PAGE_SIZE)
    });
    logs.set(data.rows || []);
    totalLogs.set(data.total);
  } catch {
    logs.set([]);
    totalLogs.set(0);
  } finally {
    loading.set(false);
  }
}

export async function pollNewLogs(filters: LogFilters) {
  if (get(currentPage) !== 0) return;

  const currentLogs = get(logs);
  if (currentLogs.length === 0) {
    await fetchLogs(filters, 0);
    return;
  }

  const newestTimestamp = currentLogs[0].timestamp;
  try {
    const data = await getLogs({
      ...filters,
      from: newestTimestamp,
      limit: String(PAGE_SIZE),
      offset: '0'
    });

    const newRows = (data.rows || []).filter(
      (r: LogEntry) => !currentLogs.some(existing => existing.id === r.id)
    );

    if (newRows.length > 0) {
      const merged = [...newRows, ...currentLogs].slice(0, PAGE_SIZE);
      logs.set(merged);
      totalLogs.update(t => t + newRows.length);
      newCount.update(n => n + newRows.length);
    }
  } catch {
    // Silently ignore poll errors
  }
}

export async function fetchLogDetail(id: string) {
  detailLoading.set(true);
  logBody.set(null);
  try {
    const detail = await getLogDetail(id);
    selectedLog.set(detail);
  } catch {
    selectedLog.set(null);
  } finally {
    detailLoading.set(false);
  }
}

export async function fetchLogBody(id: string) {
  try {
    const body = await getLogBody(id);
    logBody.set(body);
  } catch {
    logBody.set(null);
  }
}

export function clearSelection() {
  selectedLog.set(null);
  logBody.set(null);
}
