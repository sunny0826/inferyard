import { z } from 'zod';
import { errorSchema } from './schema';
import type { RunProgressEvent, RunHistoryResponse, RunTask, RunProgress, ResourceSample, StartRunsRequest, ResumeRunRequest } from './types';
const text = z.string().nullable();
const number = z.number().nullable();
const completeness = z.enum(['complete', 'incomplete']).nullable();
export const historyStatusSchema = z.enum(['complete', 'partial', 'cancelled', 'dirty']);
const summary = z.object({
  run_id: z.string(), path: z.string(), batch_id: text, status: historyStatusSchema,
  sealed: z.boolean(), completeness, stop_reason: text, model_name: text, model_path: text,
  engine: text, bundle: text, score: z.object({ passed: z.number(), valid: z.number(), rate: number }).nullable(),
  relation: z.enum(['initial', 'repeat', 'resume', 'rerun', 'derived']).nullable(), parent_run_id: text,
  started_at: text, finished_at: text, resumable: z.boolean(),
});
export const historySchema: z.ZodType<RunHistoryResponse> = z.object({
  results_root: text, scanned_at: z.string(), batches: z.array(z.object({ batch_id: z.string(), path: z.string(), runs: z.array(summary) })), standalone_runs: z.array(summary),
});
export const taskSchema: z.ZodType<RunTask> = z.object({
  task_id: z.string(), kind: z.enum(['run', 'resume']), state: z.enum(['starting', 'running', 'cancelling', 'done', 'cancelled', 'failed']), queued: z.boolean(),
  config_path: text, resume_from: text, bundle: text, run_id: text, run_path: text,
  created_at: z.string(), started_at: text, finished_at: text,
  exit: z.object({ exit_code: z.number().int(), cli_status: text, completeness: text, stop_reason: text, limitations: z.array(z.string()) }).nullable(), error: errorSchema.nullable(),
});
export const progressSchema: z.ZodType<RunProgress> = z.object({
  phase: z.enum(['probe', 'warmup', 'baseline', 'formal', 'scoring', 'residual', 'finalizing']).nullable(),
  done: z.number(), total: number, current_case_id: text, last_seq: z.number(),
  cases: z.array(z.object({ case_id: z.string(), plan_index: number,
    state: z.enum(['pending', 'running', 'completed', 'failed', 'cancelled', 'invalid']),
    category: z.enum(['instruction', 'extraction', 'qa', 'math', 'classification', 'structured', 'performance', 'svg']).nullable(),
    quality_state: z.enum(['pass', 'fail', 'unscorable', 'not_applicable']).nullable(),
    started_at: text, finished_at: text, duration_ms: number, error_category: text,
  })),
});
export const resourceSchema: z.ZodType<ResourceSample> = z.object({
  utc: z.string(), mem_available_bytes: number, service_rss_bytes: number, service_cpu_percent: number, temperature_celsius: number,
});
export const taskListSchema = z.object({ tasks: z.array(taskSchema) });
export const taskResponseSchema = z.object({ task: taskSchema });
export const detailSchema = z.object({ task: taskSchema, progress: progressSchema.nullable(), resources: z.array(resourceSchema) });
export const progressEventSchema: z.ZodType<RunProgressEvent> = z.object({ task_id: z.string(), run_id: text, progress: progressSchema });
export const resourcesEventSchema = z.object({ task_id: z.string(), points: z.array(resourceSchema) });
const absolutePath = z.string().min(1).regex(/^(\/|[A-Za-z]:[\\/]|\\\\)/, '配置必须是绝对路径');
export const startRequestSchema: z.ZodType<StartRunsRequest> = z.object({ config_paths: z.array(absolutePath).min(1).max(2), allow_single_bundle: z.boolean().optional() })
  .refine(value => value.config_paths.length !== 1 || value.allow_single_bundle === true, '单 bundle 须显式确认');
export const resumeRequestSchema: z.ZodType<ResumeRunRequest> = z.object({ from_run: z.string().min(1), endpoint_url: z.url().optional(), server_pid: z.number().int().positive().optional() })
  .refine(value => (value.endpoint_url === undefined) === (value.server_pid === undefined), 'endpoint_url 与 server_pid 必须同给同省');
