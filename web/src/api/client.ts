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
  deviceUpdated: (event: DeviceUpdatedEvent) => void;
  runState?: (event: RunStateEvent) => void;
  runProgress?: (event: RunProgressEvent) => void;
  runResources?: (event: RunResourcesEvent) => void;
  reset: () => void;
  connection: (state: 'connected' | 'reconnecting') => void;
  error: (error: ApiError) => void;
}
export interface StudioApi {
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
async function request<T>(path: string, schema: z.ZodType<T>, method = 'GET', body?: UpdateSettingsRequest | StartRunsRequest | ResumeRunRequest): Promise<T> {
  const response = await fetch(`/api/${path}`, {
    method, headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data: unknown = await response.json();
  if (!response.ok) throw new ApiFailure(z.object({ error: errorSchema }).parse(data).error);
  return schema.parse(data);
}
export const realApi: StudioApi = {
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
    source.addEventListener('sync.reset', (event) => {
      try { resetSchema.parse(JSON.parse(event.data)); handlers.reset(); }
      catch (error) { handlers.error(failure(error)); }
    });
    return () => source.close();
  },
};
