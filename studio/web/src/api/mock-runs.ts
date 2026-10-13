import { ApiFailure, type EventHandlers, type StudioApi } from './client';
import { detailSchema, historySchema, progressEventSchema, resourcesEventSchema, startRequestSchema, resumeRequestSchema, taskResponseSchema } from './run-schema';
import type { RunHistoryResponse, RunSummary, RunTask, RunTaskDetail, RunHistoryStatus, CaseProgress } from './types';
import { terminal } from '../runs/model';
const now = () => new Date().toISOString();
function reject(reason: string, status: 'conflict' | 'invalid_request' = 'conflict'): never {
  throw new ApiFailure({ code: status, message: status === 'conflict' ? '运行请求与当前状态冲突（mock）。' : '运行请求参数无效（mock）。', limitations: [], details: { reason } });
}
function summary(id: string, status: RunHistoryStatus, batch: string | null): RunSummary {
  return { run_id: id, path: batch ? `/mock/results/${batch}/runs/${id}` : `/mock/results/${id}`, batch_id: batch, status, sealed: status === 'complete', completeness: status === 'complete' ? 'complete' : 'incomplete', stop_reason: status === 'cancelled' ? 'user_cancelled' : status === 'dirty' || status === 'partial' ? 'duration_window_interrupted' : null,
    model_name: 'Demo GGUF', model_path: '/mock/models/demo.gguf', engine: 'mock', bundle: 'zh-core', score: status === 'complete' ? { passed: 2, valid: 2, rate: 1 } : null, relation: 'initial', parent_run_id: null, started_at: '2026-10-12T10:00:00Z', finished_at: '2026-10-12T10:01:00Z', resumable: batch !== null && status !== 'complete' };
}
export function createMockRuns(listeners: Set<EventHandlers>): Pick<StudioApi, 'getRuns' | 'getTasks' | 'getTask' | 'startRuns' | 'resumeRun' | 'cancelTask'> {
  const history: RunHistoryResponse = {
    results_root: '/mock/results', scanned_at: now(),
    batches: [
      { batch_id: 'batch-resume', path: '/mock/results/batch-resume', runs: [summary('resume-source', 'partial', 'batch-resume')] },
      { batch_id: 'batch-stale', path: '/mock/results/batch-stale', runs: [summary('stale-source', 'partial', 'batch-stale')] },
      { batch_id: 'batch-priority', path: '/mock/results/batch-priority', runs: [summary('old-complete', 'complete', 'batch-priority'), { ...summary('old-partial', 'partial', 'batch-priority'), resumable: false }, { ...summary('old-dirty', 'dirty', 'batch-priority'), resumable: false }, summary('old-cancelled', 'cancelled', 'batch-priority')] },
    ], standalone_runs: [summary('standalone-partial', 'partial', null)],
  };
  const details = new Map<string, RunTaskDetail>();
  const scenarios = new Map<string, string>();
  let sequence = 0;
  const copy = <T>(value: T): T => structuredClone(value);
  function publish(task: RunTask) {
    const event = taskResponseSchema.parse(copy({ task }));
    for (const listener of listeners) listener.runState?.(event);
  }
  function make(config: string | null, from: string | null, scenario: string): RunTask {
    const task: RunTask = { task_id: `mock-${++sequence}`, kind: from ? 'resume' : 'run', state: 'starting', queued: true, config_path: config, resume_from: from, bundle: config?.includes('svg') ? 'zh-svg-pelican' : 'zh-core', run_id: null, run_path: null, created_at: now(), started_at: null, finished_at: null, exit: null, error: null };
    details.set(task.task_id, { task, progress: null, resources: [] }); scenarios.set(task.task_id, scenario); publish(task); return task;
  }
  function record(detail: RunTaskDetail, code: number) {
    const task = detail.task;
    if (!task.run_id || !task.run_path) return;
    const status: RunHistoryStatus = code === 130 ? 'cancelled' : scenarios.get(task.task_id) === 'dirty' ? 'dirty' : code === 0 ? 'complete' : 'partial';
    const source = history.batches.find(batch => batch.runs.some(run => run.path === task.resume_from));
    const run = { ...summary(task.run_id, status, source?.batch_id ?? null), path: task.run_path, bundle: task.bundle, started_at: task.started_at, finished_at: task.finished_at, stop_reason: task.exit?.stop_reason ?? null, relation: task.kind === 'resume' ? 'resume' as const : 'initial' as const, parent_run_id: source?.runs.find(run => run.path === task.resume_from)?.run_id ?? null };
    if (source) { source.runs = source.runs.map(item => ({ ...item, resumable: false })); source.runs.unshift(run); }
    else history.standalone_runs.unshift(run);
    history.scanned_at = now();
  }
  function finish(detail: RunTaskDetail, code: number) {
    const task = detail.task;
    task.state = code === 130 ? 'cancelled' : code === 2 || code === 4 ? 'failed' : 'done'; task.finished_at = now();
    const limitation = code === 2 ? 'device_preflight_blocked' : code === 4 ? 'evidence_invalid' : code === 3 ? (scenarios.get(task.task_id) === 'dirty' ? 'engine_not_drained' : 'duration_requires_new_window') : null;
    task.exit = { exit_code: code, cli_status: code === 3 ? 'duration_requires_new_window' : code === 130 ? 'interrupted' : code === 0 ? 'finished' : code === 2 ? 'blocked' : 'error', completeness: code === 0 ? 'complete' : 'incomplete', stop_reason: code === 130 ? 'user_cancelled' : code === 3 ? 'duration_window_interrupted' : code === 4 ? 'tool_interrupted' : null, limitations: limitation ? [limitation] : [] };
    if (code === 2 || code === 4) task.error = { code: code === 2 ? 'cli_blocked' : 'cli_error', message: code === 2 ? '输入或预检阻断（mock）。' : '工具或证据错误（mock）。', limitations: limitation ? [limitation] : [], details: null };
    record(detail, code); publish(task); pump();
  }
  function pump() {
    if ([...details.values()].some(detail => !detail.task.queued && !terminal(detail.task))) return;
    const detail = [...details.values()].find(value => value.task.queued && !terminal(value.task));
    if (!detail) return;
    const task = detail.task;
    task.queued = false; task.state = 'running'; task.started_at = now(); task.run_id = `run-${task.task_id}`;
    task.run_path = task.resume_from ? `${task.resume_from.slice(0, task.resume_from.lastIndexOf('/'))}/${task.run_id}` : `/mock/results/${task.run_id}`;
    const count = task.kind === 'resume' ? 1 : 2;
    const cases: CaseProgress[] = Array.from({ length: count }, (_, index) => ({ case_id: `case-${index + 1}`, plan_index: index, state: 'pending', category: 'qa', quality_state: null, started_at: null, finished_at: null, duration_ms: null, error_category: null }));
    detail.progress = { phase: 'probe', done: 0, total: null, current_case_id: null, last_seq: 0, cases };
    publish(task);
    let tick = 0;
    const timer = setInterval(() => {
      if (terminal(task)) { clearInterval(timer); return; }
      tick++;
      const scenario = scenarios.get(task.task_id);
      const code = scenario === 'blocked' ? 2 : scenario === 'error' ? 4 : scenario === 'partial' || scenario === 'dirty' ? 3 : 0;
      const stopping = task.state === 'cancelling';
      const end = stopping || (scenario !== 'slow' && tick >= 8);
      const progress = detail.progress;
      if (!progress) return;
      progress.total = count; progress.last_seq = tick;
      progress.phase = end ? 'finalizing' : tick < 3 ? 'warmup' : 'formal';
      const current = cases[tick < 4 ? 0 : count - 1];
      if (current) { current.state = 'running'; current.started_at ??= now(); }
      if (tick >= 4 && cases[0]) { cases[0].state = 'completed'; cases[0].quality_state = 'pass'; cases[0].finished_at ??= now(); cases[0].duration_ms = 1500; }
      if (end) for (const item of cases) {
        if (item.state !== 'completed') { item.state = stopping ? 'cancelled' : code === 4 ? 'failed' : code === 2 ? 'invalid' : code === 3 ? 'pending' : 'completed'; item.finished_at = item.state === 'pending' ? null : now(); item.quality_state = item.state === 'completed' ? 'pass' : null; item.error_category = code === 4 ? 'evidence_invalid' : null; }
      }
      progress.done = cases.filter(item => item.state !== 'pending' && item.state !== 'running').length;
      progress.current_case_id = cases.find(item => item.state === 'running')?.case_id ?? null;
      const event = progressEventSchema.parse(copy({ task_id: task.task_id, run_id: task.run_id, progress }));
      for (const listener of listeners) listener.runProgress?.(event);
      const point = { utc: now(), mem_available_bytes: tick === 3 ? null : Math.round((8 - tick / 20) * 1024 ** 3), service_rss_bytes: tick === 3 ? null : Math.round((1 + tick / 50) * 1024 ** 3), service_cpu_percent: tick === 1 || tick === 3 ? null : 20 + tick * 2, temperature_celsius: tick === 3 ? null : 40 + tick };
      detail.resources = [...detail.resources, point].slice(-600);
      const resourceEvent = resourcesEventSchema.parse({ task_id: task.task_id, points: [point] });
      for (const listener of listeners) listener.runResources?.(resourceEvent);
      if (end) { clearInterval(timer); finish(detail, stopping ? 130 : code); }
    }, 500);
  }
  return {
    async getRuns() { return historySchema.parse(copy(history)); },
    async getTasks() { return { tasks: copy([...details.values()].map(value => value.task).reverse()) }; },
    async getTask(id) { const detail = details.get(id); if (!detail) throw new ApiFailure({ code: 'not_found', message: '任务不存在（mock）。', limitations: [], details: null }); return detailSchema.parse(copy(detail)); },
    async startRuns(body) {
      if (!startRequestSchema.safeParse(body).success) reject('invalid_config', 'invalid_request');
      if (body.config_paths.some(path => path.includes('/invalid'))) reject('invalid_config', 'invalid_request');
      if (body.config_paths.some(path => path.includes('/outside'))) reject('output_root_outside_results');
      if (new Set(body.config_paths).size !== body.config_paths.length || body.config_paths.some(path => [...details.values()].some(detail => detail.task.config_path === path && !terminal(detail.task)))) reject('queue_duplicate');
      const tasks = body.config_paths.map(path => make(path, null, ['slow', 'blocked', 'error', 'partial', 'dirty'].find(mode => path.includes(`/${mode}`)) ?? 'complete'));
      const response = copy({ tasks }); setTimeout(pump, 100); return response;
    },
    async resumeRun(body) {
      if (!resumeRequestSchema.safeParse(body).success) reject('invalid_resume', 'invalid_request');
      const source = history.batches.flatMap(batch => batch.runs).find(run => run.path === body.from_run);
      if (!source || !source.resumable || source.run_id === 'stale-source') reject('not_resumable');
      const task = make(null, source.path, body.endpoint_url?.includes('invalid') ? 'partial' : 'complete');
      const response = copy({ task }); setTimeout(pump, 100); return response;
    },
    async cancelTask(id) {
      const detail = details.get(id); if (!detail) throw new ApiFailure({ code: 'not_found', message: '任务不存在（mock）。', limitations: [], details: null });
      const task = detail.task;
      if (terminal(task)) reject('invalid_state');
      if (task.queued) { task.state = 'cancelled'; task.queued = false; task.finished_at = now(); publish(task); }
      else { task.state = 'cancelling'; publish(task); }
      return copy({ task });
    },
  };
}
