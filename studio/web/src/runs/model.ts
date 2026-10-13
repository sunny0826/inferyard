import type { CaseState, RunHistoryStatus, RunTask, RunPhase, ResourceSample } from '../api/types';
export const historyLabels = { cancelled: '已取消', dirty: '未排空', partial: '不完整', complete: '完整' } satisfies Record<RunHistoryStatus, string>;
export const caseLabels = { pending: '待执行（聚合态）', running: '执行中（聚合态）', completed: '已完成', failed: '失败', cancelled: '已取消', invalid: '无效' } satisfies Record<CaseState, string>;
export const phaseLabels = { probe: '探测', warmup: '预热', baseline: '基线', formal: '正式运行', scoring: '评分', residual: '残留检测', finalizing: '收尾' } satisfies Record<RunPhase, string>;
const priority = { cancelled: 4, dirty: 3, partial: 2, complete: 1 } satisfies Record<RunHistoryStatus, number>;
export function aggregateStatus(statuses: RunHistoryStatus[]): RunHistoryStatus | null {
  return statuses.reduce<RunHistoryStatus | null>((current, status) => current === null || priority[status] > priority[current] ? status : current, null);
}
export function terminal(task: RunTask): boolean {
  switch (task.state) {
    case 'starting': case 'running': case 'cancelling': return false;
    case 'done': case 'cancelled': case 'failed': return true;
    default: return exhaustive(task.state);
  }
}
export function taskLabel(task: RunTask): string {
  if (task.exit) {
    switch (task.exit.exit_code) {
      case 0: return '成功 · done';
      case 2: return '输入 / 预检阻断 · failed';
      case 3: return '运行不完整 · done · partial';
      case 4: return '工具 / 证据错误 · failed';
      case 130: return '已取消 · cancelled';
    }
  }
  switch (task.state) {
    case 'starting': return task.queued ? '排队中 · starting' : '启动中 · starting';
    case 'running': return '运行中 · running';
    case 'cancelling': return '取消中 · cancelling';
    case 'done': return '已结束 · done';
    case 'failed': return '失败 · failed';
    case 'cancelled': return '已取消 · cancelled';
    default: return exhaustive(task.state);
  }
}
function exhaustive(value: never): never { throw new Error(`Unsupported state: ${value}`); }
export function appendResources(previous: ResourceSample[], incoming: ResourceSample[]): ResourceSample[] {
  const byTime = new Map(previous.map(point => [point.utc, point]));
  for (const point of incoming) byTime.set(point.utc, point);
  return [...byTime.values()].sort((a, b) => Date.parse(a.utc) - Date.parse(b.utc)).slice(-600);
}
export function chartData(points: ResourceSample[]): [number[], (number | null)[], (number | null)[], (number | null)[], (number | null)[]] {
  return [points.map(p => Date.parse(p.utc) / 1000), points.map(p => p.mem_available_bytes === null ? null : p.mem_available_bytes / 1024 ** 3), points.map(p => p.service_rss_bytes === null ? null : p.service_rss_bytes / 1024 ** 3), points.map(p => p.service_cpu_percent), points.map(p => p.temperature_celsius)];
}
