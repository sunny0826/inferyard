import { z } from 'zod';
import { taskListSchema } from './run-schema';
const text = z.string().nullable();
const directory = z.object({ path: text, configured: z.boolean(), exists: z.boolean(), initialized: z.boolean() });
export const readinessSchema = z.object({
  cli: z.object({ path: text, version: text, status: z.enum(['ok', 'not_found', 'not_executable', 'version_failed']) }),
  workspace: directory, results: directory, models: directory, first_run: z.boolean(),
});
export const workspaceProgressSchema = z.object({
  kind: z.literal('progress'), progress_version: z.literal(1), workspace: z.string(),
  device_preflight: z.object({ status: z.enum(['ok', 'missing']), path: text }),
  bundles: z.array(z.object({
    bundle: z.string(), phase: z.enum(['preflight_missing', 'candidate_missing', 'bound_missing', 'run_missing', 'run_incomplete', 'run_complete']),
    candidate_path: text, bound_path: text,
    runs: z.array(z.object({ path: z.string(), complete: z.boolean(), sealed: z.boolean() })),
    next_action: z.enum(['device_check', 'prepare_candidate', 'bind', 'run', 'resume', 'none']), next_command: text, blocked_reason: text,
  })),
  report: z.object({ status: z.enum(['missing', 'present', 'verified']), path: text, runs: z.array(z.string()) }),
  next_action: z.enum(['device_check', 'prepare_candidate', 'bind', 'run', 'report', 'verify', 'done']),
  next_command: text, default_scope_complete: z.boolean(),
});
const exitStatus = z.enum(['complete', 'blocked', 'incomplete', 'error', 'cancelled', 'unknown']);
export const workspaceProgressResponseSchema = z.object({ status: exitStatus, exit_code: z.number().int(), progress: workspaceProgressSchema.nullable() });
export const wizardResultSchema = z.object({ status: exitStatus, exit_code: z.number().int(), tasks: taskListSchema.shape.tasks });
export const startupSchema = z.object({ path: z.string(), startup_args: z.array(z.string()) });
export const processesSchema = z.array(z.object({ pid: z.number().int().positive(), name: z.string() }));
export type Readiness = z.infer<typeof readinessSchema>;
export type WorkspaceProgress = z.infer<typeof workspaceProgressSchema>;
export type WorkspaceProgressResponse = z.infer<typeof workspaceProgressResponseSchema>;
export type WizardResult = z.infer<typeof wizardResultSchema>;
export type Startup = z.infer<typeof startupSchema>;
export type ProcessCandidate = z.infer<typeof processesSchema>[number];
