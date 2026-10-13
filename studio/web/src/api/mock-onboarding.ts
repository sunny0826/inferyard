import { ApiFailure, type StudioApi } from './client';
import { workspaceProgressSchema, type WorkspaceProgress } from './onboarding-schema';
import type { SettingsResponse } from './types';
import { quote } from '../onboarding/commands';
import type { EventHandlers } from './client';

type RunApi = Pick<StudioApi, 'getTasks' | 'startRuns' | 'resumeRun'>;
export function createMockOnboarding(getSettings: () => SettingsResponse, save: (settings: SettingsResponse) => void, runs: RunApi, listeners: Set<EventHandlers>): Pick<StudioApi, 'getReadiness' | 'provision' | 'getProgress' | 'executeWizard' | 'getStartup' | 'getProcesses'> {
  let initialized = false; let provisioned = false; let preflight = false;
  const phases = new Map<string, 'candidate_missing' | 'bound_missing' | 'run_missing'>([['zh-core', 'candidate_missing'], ['zh-svg-pelican', 'candidate_missing']]);
  let report: 'missing' | 'verified' = 'missing';
  const workspace = () => getSettings().effective.workspace_root ?? '/mock/workspace';
  const reset = () => { for (const listener of listeners) listener.reset(); };
  async function progress(): Promise<WorkspaceProgress> {
    const w = workspace(); const tasks = (await runs.getTasks()).tasks;
    const bundles = ['zh-core', 'zh-svg-pelican'].map(bundle => {
      const complete = tasks.find(task => task.bundle === bundle && task.exit?.exit_code === 0);
      const phase = complete ? 'run_complete' : !preflight ? 'preflight_missing' : phases.get(bundle) ?? 'candidate_missing';
      const action = phase === 'run_complete' ? 'none' : phase === 'preflight_missing' ? 'device_check' : phase === 'candidate_missing' ? 'prepare_candidate' : phase === 'bound_missing' ? 'bind' : 'run';
      const command = action === 'none' ? null : action === 'device_check' ? `inferyard device-check --models-root '/mock/models' --out ${quote(`${w}/preflight/check-1`)}` : action === 'prepare_candidate' ? `inferyard config create --workspace ${quote(w)} --bundle ${bundle} --engine '/mock/engine' --out ${quote(`${w}/candidates/${bundle}-1`)}` : action === 'bind' ? `inferyard config bind --candidate ${quote(`${w}/candidates/${bundle}-1/candidate.toml`)} --pid 123 --endpoint 'http://127.0.0.1:48857' --out ${quote(`${w}/bound/${bundle}-1`)}` : `inferyard run --config ${quote(`${w}/bound/${bundle}-1/config.toml`)}`;
      return { bundle, phase, candidate_path: phase === 'bound_missing' || phase === 'run_missing' || phase === 'run_complete' ? `${w}/candidates/${bundle}-1` : null, bound_path: phase === 'run_missing' || phase === 'run_complete' ? `${w}/bound/${bundle}-1` : null, runs: complete ? [{ path: complete.run_path ?? '', complete: true, sealed: true }] : [], next_action: action, next_command: command, blocked_reason: null };
    });
    const first = bundles.find(bundle => bundle.phase !== 'run_complete');
    return workspaceProgressSchema.parse({ kind: 'progress', progress_version: 1, workspace: w, device_preflight: { status: preflight ? 'ok' : 'missing', path: preflight ? `${w}/preflight/check-1` : null }, bundles,
      report: { status: report, path: report === 'verified' ? `${w}/reports/summary-1` : null, runs: bundles.flatMap(bundle => bundle.runs.map(run => run.path)) },
      next_action: first?.next_action ?? (report === 'verified' ? 'done' : 'report'), next_command: first?.next_command ?? (report === 'verified' ? null : `inferyard report --runs ${bundles.flatMap(bundle => bundle.runs.map(run => quote(run.path))).join(' ')} --out ${quote(`${w}/reports/summary-1`)}`), default_scope_complete: !first && report === 'verified' });
  }
  return {
    async getReadiness() {
      const s = getSettings(); const dir = (path: string | null, exists: boolean) => ({ path, configured: path !== null, exists, initialized: false });
      return { cli: { path: s.effective.inferyard_path, version: s.effective.inferyard_version, status: s.effective.inferyard_status }, workspace: { ...dir(workspace(), provisioned || initialized), configured: s.settings.workspace_root !== null, initialized }, results: dir(s.settings.results_root, s.settings.results_root !== null), models: dir(s.settings.models_root, s.settings.models_root !== null), first_run: !initialized && !provisioned };
    },
    async provision() { const s = getSettings(); s.settings.workspace_root = workspace(); s.effective.workspace_root = workspace(); provisioned = true; save(s); reset(); return s; },
    async getProgress() { return { status: initialized ? 'complete' : 'blocked', exit_code: initialized ? 0 : 2, progress: initialized ? await progress() : null }; },
    async getStartup() { return { path: '/mock/llama-server', startup_args: ['-m', '/mock/models/demo.gguf', '--host', '127.0.0.1', '--port', '48857'] }; },
    async getProcesses() { return [{ pid: 123, name: 'llama-server (mock)' }]; },
    async executeWizard(command, reportVerify = false) {
      if (/--token(?:\s|=)|--api-key|https?:\/\/[^/\s]*@/.test(command)) throw new ApiFailure({ code: 'invalid_request', message: '只接受凭据环境变量名。', limitations: [], details: null });
      const result = { status: 'complete' as const, exit_code: 0, tasks: [] };
      if (command.startsWith('inferyard init ')) { initialized = true; return result; }
      if (command.startsWith('inferyard model acquire ') || command.startsWith('inferyard engine acquire ')) return result;
      if (command.startsWith('inferyard device-check ')) { preflight = true; return result; }
      const p = await progress(); const b = p.bundles.find(bundle => bundle.phase !== 'run_complete');
      if (command.startsWith('inferyard config create ') && b) phases.set(b.bundle, 'bound_missing');
      else if (command.startsWith('inferyard config bind ') && b) phases.set(b.bundle, 'run_missing');
      else if (command.startsWith('inferyard run ') && b) return { ...result, tasks: (await runs.startRuns({ config_paths: [`${workspace()}/bound/${b.bundle}-1/config.toml`], allow_single_bundle: true })).tasks };
      else if (command.startsWith('inferyard report ') && reportVerify && !b) report = 'verified';
      else throw new ApiFailure({ code: 'invalid_request', message: '模拟命令与当前步骤不符。', limitations: [], details: null });
      return result;
    },
  };
}
