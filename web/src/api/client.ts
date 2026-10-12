import type { ReportListResponse, CreateReportRequest, CreateReportResponse, DownloadListResponse, CreateDownloadRequest, CreateDownloadResponse, CancelDownloadResponse, ReportCreatedEvent, DownloadStateEvent, DownloadProgressEvent } from './types';
import { reportListSchema, reportResponseSchema, reportRequestSchema, downloadListSchema, downloadResponseSchema, downloadRequestSchema, downloadProgressSchema } from './assets-schema';
import { z } from 'zod';
import type { RunHistoryResponse, RunTaskListResponse, RunTaskDetail, StartRunsRequest, StartRunsResponse, ResumeRunRequest, ResumeRunResponse, CancelTaskResponse, RunStateEvent, RunProgressEvent, RunResourcesEvent } from './types';
import { historySchema, taskListSchema, taskResponseSchema, detailSchema, progressEventSchema, resourcesEventSchema, startRequestSchema, resumeRequestSchema } from './run-schema';
import type { ApiError, DeviceResponse, DeviceUpdatedEvent, SettingsResponse, UpdateSettingsRequest } from './types';
import { deviceSchema, deviceEventSchema, errorSchema, refreshSchema, resetSchema, settingsSchema } from './schema';

const token = new URLSearchParams(window.location.search).get('token') ?? '';
export class ApiFailure extends Error {
  constructor(public readonly error: ApiError) { super(error.message); }
}
export function failure(error: unknown): ApiError {
  return error instanceof ApiFailure ? error.error : { code: 'internal_error', message: '无法读取服务响应，请重试。', limitations: [], details: null };
}
export interface EventHandlers {
  reportCreated?: (event: ReportCreatedEvent) => void;
  downloadState?: (event: DownloadStateEvent) => void;
  downloadProgress?: (event: DownloadProgressEvent) => void;
  deviceUpdated: (event: DeviceUpdatedEvent) => void;
  runState?: (event: RunStateEvent) => void;
  runProgress?: (event: RunProgressEvent) => void;
  runResources?: (event: RunResourcesEvent) => void;
  reset: () => void;
  connection: (state: 'connected' | 'reconnecting') => void;
  error: (error: ApiError) => void;
}
export interface StudioApi {
  getReports(): Promise<ReportListResponse>;
  createReport(body: CreateReportRequest): Promise<CreateReportResponse>;
  getDownloads(): Promise<DownloadListResponse>;
  createDownload(body: CreateDownloadRequest): Promise<CreateDownloadResponse>;
  cancelDownload(id: string): Promise<CancelDownloadResponse>;
  getRuns(): Promise<RunHistoryResponse>;
  getTasks(): Promise<RunTaskListResponse>;
  getTask(id: string): Promise<RunTaskDetail>;
  startRuns(body: StartRunsRequest): Promise<StartRunsResponse>;
  resumeRun(body: ResumeRunRequest): Promise<ResumeRunResponse>;
  cancelTask(id: string): Promise<CancelTaskResponse>;
  getDevice(): Promise<DeviceResponse>;
  refreshDevice(): Promise<{ refreshing: true }>;
  getSettings(): Promise<SettingsResponse>;
  updateSettings(settings: UpdateSettingsRequest): Promise<SettingsResponse>;
  subscribe(handlers: EventHandlers): () => void;
}
async function request<T>(path: string, schema: z.ZodType<T>, method = 'GET', body?: UpdateSettingsRequest | StartRunsRequest | ResumeRunRequest | CreateReportRequest | CreateDownloadRequest, timeoutMs?: number): Promise<T> {
  const response = await fetch(`/api/${path}`, {
    signal: timeoutMs === undefined ? undefined : AbortSignal.timeout(timeoutMs),
    method, headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data: unknown = await response.json();
  if (!response.ok) throw new ApiFailure(z.object({ error: errorSchema }).parse(data).error);
  return schema.parse(data);
}
export function reportLink(id: string): string {
  return `/reports/${encodeURIComponent(id)}/report.html?token=${encodeURIComponent(token)}`;
}
export const realApi: StudioApi = {
  getReports: () => request('reports', reportListSchema),
  createReport: body => request('reports', reportResponseSchema, 'POST', reportRequestSchema.parse(body), 180000),
  getDownloads: () => request('downloads', downloadListSchema),
  createDownload: body => request('downloads', downloadResponseSchema, 'POST', downloadRequestSchema.parse(body)),
  cancelDownload: id => request(`downloads/${encodeURIComponent(id)}/cancel`, downloadResponseSchema, 'POST'),
  getRuns: () => request('runs', historySchema),
  getTasks: () => request('run-tasks', taskListSchema),
  getTask: id => request(`run-tasks/${encodeURIComponent(id)}`, detailSchema),
  startRuns: body => request('runs', taskListSchema, 'POST', startRequestSchema.parse(body)),
  resumeRun: body => request('runs/resume', taskResponseSchema, 'POST', resumeRequestSchema.parse(body)),
  cancelTask: id => request(`run-tasks/${encodeURIComponent(id)}/cancel`, taskResponseSchema, 'POST'),
  getDevice: () => request('device', deviceSchema),
  refreshDevice: () => request('device/refresh', refreshSchema, 'POST'),
  getSettings: () => request('settings', settingsSchema),
  updateSettings: (body) => request('settings', settingsSchema, 'PUT', body),
  subscribe(handlers) {
    // Keep this instance alive: native EventSource retains and sends Last-Event-ID on reconnect.
    const source = new EventSource(`/api/events?token=${encodeURIComponent(token)}`);
    source.onopen = () => handlers.connection('connected');
    source.onerror = () => handlers.connection('reconnecting');
    source.addEventListener('device.updated', (event) => {
      try { handlers.deviceUpdated(deviceEventSchema.parse(JSON.parse(event.data))); }
      catch (error) { handlers.error(failure(error)); }
    });
    source.addEventListener('run.state', event => {
      try { handlers.runState?.(taskResponseSchema.parse(JSON.parse(event.data))); }
      catch (error) { handlers.error(failure(error)); }
    });
    source.addEventListener('run.progress', event => {
      try { handlers.runProgress?.(progressEventSchema.parse(JSON.parse(event.data))); }
      catch (error) { handlers.error(failure(error)); }
    });
    source.addEventListener('run.resources', event => {
      try { handlers.runResources?.(resourcesEventSchema.parse(JSON.parse(event.data))); }
      catch (error) { handlers.error(failure(error)); }
    });
    source.addEventListener('report.created', event => {
      try { handlers.reportCreated?.(reportResponseSchema.parse(JSON.parse(event.data))); }
      catch (error) { handlers.error(failure(error)); }
    });
    source.addEventListener('download.state', event => {
      try { handlers.downloadState?.(downloadResponseSchema.parse(JSON.parse(event.data))); }
      catch (error) { handlers.error(failure(error)); }
    });
    source.addEventListener('download.progress', event => {
      try { handlers.downloadProgress?.(downloadProgressSchema.parse(JSON.parse(event.data))); }
      catch (error) { handlers.error(failure(error)); }
    });
    source.addEventListener('sync.reset', (event) => {
      try { resetSchema.parse(JSON.parse(event.data)); handlers.reset(); }
      catch (error) { handlers.error(failure(error)); }
    });
    return () => source.close();
  },
};
