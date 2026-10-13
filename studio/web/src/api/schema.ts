import { z } from 'zod';
import type { ApiError, DeviceResponse, DeviceUpdatedEvent, SettingsResponse } from './types';
const text = z.string().nullable();
const number = z.number().nullable();
const probe = z.enum(['ok', 'not_found', 'not_executable', 'version_failed']);
export const errorSchema: z.ZodType<ApiError> = z.object({
  code: z.enum(['invalid_request', 'unauthorized', 'not_found', 'conflict', 'cli_blocked', 'internal_error', 'cli_error', 'cli_unavailable']),
  message: z.string(), limitations: z.array(z.string()), details: z.record(z.string(), z.unknown()).nullable(),
});
export const settingsSchema: z.ZodType<SettingsResponse> = z.object({
  settings: z.object({ workspace_root: text.default(null), inferyard_path: text, results_root: text, models_root: text }),
  effective: z.object({ workspace_root: text.default(null), inferyard_path: text, inferyard_version: text, inferyard_status: probe, results_root: text, models_root: text, reports_root: text }),
});
const candidate = z.looseObject({ model_path: z.string(), model_name: z.string(), mode: z.enum(['cpu', 'cuda', 'metal', 'blocked']), gpu_index: number, estimated_gpu_memory_bytes: number, min_available_memory_bytes: z.number(), min_disk_bytes: z.number(), context_size: z.number(), threads: z.number(), reason: z.string() });
const snapshot = z.looseObject({
  kind: z.literal('device_preflight.v1'),
  hardware: z.looseObject({
    platform: text.optional(), architecture: text.optional(), cpu_model: text.optional(), logical_cpus: number.optional(), memory_total_bytes: number.optional(), memory_available_bytes: number.optional(), memory_available_is_estimate: z.boolean().optional(), disk_free_bytes: number.optional(), disk_path: text.optional(),
    gpu: z.looseObject({ status: z.enum(['observed', 'unavailable']), source: text.optional(), reason: text.optional(), devices: z.array(z.looseObject({ index: z.number().optional(), name: text.optional(), memory_total_bytes: number.optional(), memory_free_bytes: number.optional() })) }).nullable().optional(),
    sources: z.record(z.string(), text).optional(), missing: z.record(z.string(), z.string()).optional(),
  }),
  models: z.array(z.discriminatedUnion('status', [
    z.looseObject({ status: z.literal('available'), path: z.string(), name: z.string(), size_bytes: z.number(), architecture: text, context_length: number, file_type: number, chat_template_available: z.boolean() }),
    z.looseObject({ status: z.literal('unavailable'), path: z.string(), reason: z.string() }),
  ])),
  recommendation: z.object({ status: z.enum(['recommended', 'blocked']), selected: candidate.nullable(), reason: text, candidates: z.array(candidate), limitations: z.array(z.string()) }),
  model_requests_sent: z.number(), platform_capabilities: z.object({ device_inspection: z.boolean(), live_single_run: z.boolean(), live_experiments: z.boolean(), metal_backend_verified: z.boolean() }),
  ready_to_run: z.boolean(), next_steps: z.array(z.looseObject({ action: z.string(), reason: z.string().optional(), command: z.array(z.string()).optional() })),
}).nullable();
export const deviceSchema: z.ZodType<DeviceResponse> = z.object({ snapshot, captured_at: text, refreshing: z.boolean(), last_error: errorSchema.nullable() });
export const deviceEventSchema: z.ZodType<DeviceUpdatedEvent> = z.object({ snapshot, captured_at: text, error: errorSchema.nullable() });
export const resetSchema = z.object({ reason: z.enum(['buffer_gap', 'settings_changed', 'workspace_changed']) });
export const refreshSchema = z.object({ refreshing: z.literal(true) });
