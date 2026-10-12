import { z } from 'zod';
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
  reset: () => void;
  connection: (state: 'connected' | 'reconnecting') => void;
  error: (error: ApiError) => void;
}
export interface StudioApi {
  getDevice(): Promise<DeviceResponse>;
  refreshDevice(): Promise<{ refreshing: true }>;
  getSettings(): Promise<SettingsResponse>;
  updateSettings(settings: UpdateSettingsRequest): Promise<SettingsResponse>;
  subscribe(handlers: EventHandlers): () => void;
}
async function request<T>(path: string, schema: z.ZodType<T>, method = 'GET', body?: UpdateSettingsRequest): Promise<T> {
  const response = await fetch(`/api/${path}`, {
    method, headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data: unknown = await response.json();
  if (!response.ok) throw new ApiFailure(z.object({ error: errorSchema }).parse(data).error);
  return schema.parse(data);
}
export const realApi: StudioApi = {
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
    source.addEventListener('sync.reset', (event) => {
      try { resetSchema.parse(JSON.parse(event.data)); handlers.reset(); }
      catch (error) { handlers.error(failure(error)); }
    });
    return () => source.close();
  },
};
