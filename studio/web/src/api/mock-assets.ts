import { ApiFailure, type EventHandlers, type StudioApi } from './client';
import { reportRequestSchema, downloadRequestSchema } from './assets-schema';
import type { DownloadTask, ReportSummary } from './types';
export function createMockAssets(listeners: Set<EventHandlers>): Pick<StudioApi, 'getReports' | 'createReport' | 'getDownloads' | 'createDownload' | 'cancelDownload'> {
  const reports: ReportSummary[] = [{ report_id: 'report-20261012T080000Z', path: '/mock/reports/report-20261012T080000Z', created_at: null, run_ids: ['old-complete', 'removed-run'], run_paths: ['/mock/results/batch-priority/runs/old-complete', null], html_url: '/reports/report-20261012T080000Z/report.html' }];
  const tasks = new Map<string, DownloadTask>(); let sequence = 0;
  function publish(task: DownloadTask) { for (const listener of listeners) listener.downloadState?.({ task: structuredClone(task) }); }
  return {
    async getReports() { return structuredClone({ reports_root: '/mock/reports', reports }); },
    async createReport(body) {
      reportRequestSchema.parse(body);
      await new Promise(resolve => setTimeout(resolve, 650));
      if (body.run_paths.some(path => path.includes('old-dirty'))) throw new ApiFailure({ code: 'cli_blocked', message: 'Mock report blocked', limitations: ['unsupported_format'], details: null });
      const id = `report-${new Date().toISOString().replace(/[-:]/g, '').slice(0, 15)}Z-${++sequence}`;
      const report: ReportSummary = { report_id: id, path: `/mock/reports/${id}`, created_at: new Date().toISOString(), run_ids: body.run_paths.map(path => path.split('/').pop() ?? path), run_paths: [...body.run_paths], html_url: `/reports/${id}/report.html` };
      reports.unshift(report); for (const listener of listeners) listener.reportCreated?.({ report: structuredClone(report) });
      return structuredClone({ report });
    },
    async getDownloads() { return { tasks: structuredClone([...tasks.values()]) }; },
    async createDownload(body) {
      downloadRequestSchema.parse(body);
      if ([...tasks.values()].some(task => task.out_name === body.out_name)) throw new ApiFailure({ code: 'conflict', message: '输出目录已占用（含失败保留目录）。', limitations: ['mock_output_exists'], details: { reason: 'output_exists' } });
      const task: DownloadTask = { task_id: `download-${++sequence}`, state: 'downloading', source_url: body.source_url, out_name: body.out_name, out_dir: `/mock/models/${body.out_name}`, file_name: null, downloaded_bytes: 0, bytes_per_second: null, started_at: new Date().toISOString(), finished_at: null, result: null, error: null };
      tasks.set(task.task_id, task); publish(task);
      let ticks = 0;
      const timer = setInterval(() => {
        if (task.state !== 'downloading') { clearInterval(timer); return; }
        ticks++; task.downloaded_bytes += 1048576; task.bytes_per_second = 2097152; task.file_name = 'model.gguf.part';
        for (const listener of listeners) listener.downloadProgress?.({ task_id: task.task_id, downloaded_bytes: task.downloaded_bytes, bytes_per_second: task.bytes_per_second, utc: new Date().toISOString() });
        if (ticks >= 4 && !body.out_name.includes('slow')) {
          task.state = body.out_name.includes('fail') ? 'failed' : 'done'; task.finished_at = new Date().toISOString(); task.bytes_per_second = null;
          if (task.state === 'failed') task.error = { code: 'cli_error', message: 'Mock download failed', limitations: ['mock_network_failure'], details: null };
          else { task.result = { bytes: task.downloaded_bytes, sha256: 'a'.repeat(64) }; task.file_name = 'model.gguf'; }
          publish(task); clearInterval(timer);
        }
      }, 500);
      return structuredClone({ task });
    },
    async cancelDownload(id) {
      const task = tasks.get(id);
      if (!task || task.state !== 'downloading') throw new ApiFailure({ code: 'conflict', message: '下载已结束。', limitations: [], details: { reason: 'invalid_state' } });
      task.state = 'cancelled'; task.finished_at = new Date().toISOString(); task.bytes_per_second = null; publish(task);
      return structuredClone({ task });
    },
  };
}
