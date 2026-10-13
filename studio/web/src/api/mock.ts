import { createMockOnboarding } from './mock-onboarding';
import { createMockAssets } from './mock-assets';
import { createMockRuns } from './mock-runs';
import { ApiFailure, type StudioApi, type EventHandlers } from './client';
import type { DevicePreflight, DeviceResponse, SettingsResponse } from './types';
const GiB = 1024 ** 3;
const fixture: DevicePreflight = {
  kind: 'device_preflight.v1',
  hardware: { platform: 'darwin', architecture: 'arm64', cpu_model: 'Apple M2 (mock)', logical_cpus: 8, memory_total_bytes: 16 * GiB, memory_available_bytes: 9 * GiB, memory_available_is_estimate: true, disk_free_bytes: 120 * GiB, disk_path: '/mock', gpu: { status: 'unavailable', devices: [], reason: 'gpu_memory_unavailable', source: 'mock' }, sources: { cpu_model: 'mock fixture', memory_available_bytes: 'mock estimate' }, missing: { gpu_memory: 'gpu_memory_unavailable' } },
  models: [{ status: 'available', path: '/mock/models/demo.gguf', name: 'Demo GGUF', size_bytes: 2 * GiB, architecture: 'llama', context_length: 4096, file_type: 15, chat_template_available: true }, { status: 'unavailable', path: '/mock/models/unreadable.gguf', reason: 'gguf_header_invalid' }],
  recommendation: { status: 'blocked', selected: null, reason: 'engine_not_configured', candidates: [{ model_path: '/mock/models/demo.gguf', model_name: 'Demo GGUF', mode: 'cpu', gpu_index: null, estimated_gpu_memory_bytes: null, min_available_memory_bytes: 4 * GiB, min_disk_bytes: GiB, context_size: 4096, threads: 4, reason: 'cpu_candidate' }], limitations: ['engine_not_configured'] },
  model_requests_sent: 0, platform_capabilities: { device_inspection: true, live_single_run: true, live_experiments: false, metal_backend_verified: false }, ready_to_run: false, next_steps: [{ action: 'configure_engine', reason: 'engine_not_configured' }],
};
export function createMockApi(): StudioApi {
  let settings: SettingsResponse = { settings: { workspace_root: null, inferyard_path: null, results_root: '/mock/results', models_root: '/mock/models' }, effective: { workspace_root: '/mock/workspace', inferyard_path: '/mock/bin/inferyard', inferyard_version: 'mock-1.0', inferyard_status: 'ok', results_root: '/mock/results', models_root: '/mock/models', reports_root: '/mock/reports' } };
  let device: DeviceResponse = { snapshot: null, captured_at: null, refreshing: false, last_error: null };
  const listeners = new Set<EventHandlers>();
  const runs = createMockRuns(listeners);
  return {
    ...runs,
    ...createMockOnboarding(() => structuredClone(settings), value => { settings = value; }, runs, listeners),
    ...createMockAssets(listeners),
    async getDevice() { return structuredClone(device); },
    async getSettings() { return structuredClone(settings); },
    async updateSettings(patch) {
      for (const value of Object.values(patch)) {
        if (value !== null && !/^(\/|[A-Za-z]:[\\/]|\\\\)/.test(value)) throw new ApiFailure({ code: 'invalid_request', message: '路径必须是非空绝对路径。', limitations: [], details: null });
      }
      if (patch.inferyard_path === '/mock/blocked') throw new ApiFailure({ code: 'cli_blocked', message: 'inferyard 探测被阻断（mock）。', limitations: ['mock_probe_blocked'], details: null });
      const next = { ...settings.settings, ...patch };
      settings = { settings: next, effective: { ...next, inferyard_path: next.inferyard_path ?? '/mock/bin/inferyard', inferyard_version: 'mock-1.0', inferyard_status: 'ok', reports_root: next.results_root?.replace(/[\\/][^\\/]+[\\/]?$/, '/reports') ?? null } };
      return structuredClone(settings);
    },
    async refreshDevice() {
      if (device.refreshing) throw new ApiFailure({ code: 'conflict', message: '设备刷新进行中。', limitations: [], details: { reason: 'refresh_in_progress' } });
      device.refreshing = true;
      setTimeout(() => {
        const snapshot = structuredClone(fixture);
        const root = settings.settings.models_root ?? '/mock/models';
        snapshot.models = snapshot.models.map(model => ({ ...model, path: model.path.replace('/mock/models', root) }));
        device = { snapshot, captured_at: new Date().toISOString(), refreshing: false, last_error: null };
        for (const listener of listeners) listener.deviceUpdated({ snapshot, captured_at: device.captured_at, error: null });
      }, 650);
      return { refreshing: true };
    },
    subscribe(handlers) { listeners.add(handlers); handlers.connection('connected'); return () => { listeners.delete(handlers); }; },
  };
}
