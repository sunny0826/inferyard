import { api } from '../api';
import { failure } from '../api/client';
import type { ApiError, ReportListResponse, DownloadTask, DownloadProgressEvent } from '../api/types';
type Update = { kind: 'state'; task: DownloadTask } | { kind: 'progress'; event: DownloadProgressEvent };
export class AssetsStore {
  reports = $state<ReportListResponse | null>(null);
  downloads = $state<DownloadTask[]>([]);
  reportError = $state<ApiError | null>(null);
  downloadError = $state<ApiError | null>(null);
  private reportVersion = 0;
  private downloadVersion = 0;
  private updates: Update[] | null = null;
  async refreshReports() {
    const version = ++this.reportVersion; this.reportError = null;
    try { const value = await api.getReports(); if (version === this.reportVersion) this.reports = value; }
    catch (error) { if (version === this.reportVersion) this.reportError = failure(error); }
  }
  async refreshDownloads() {
    const version = ++this.downloadVersion; this.downloadError = null; this.updates = [];
    try {
      const value = await api.getDownloads();
      if (version !== this.downloadVersion) return;
      this.downloads = value.tasks;
      for (const update of this.updates ?? []) this.apply(update);
    } catch (error) { if (version === this.downloadVersion) this.downloadError = failure(error); }
    finally { if (version === this.downloadVersion) this.updates = null; }
  }
  async rebuild() { await Promise.all([this.refreshReports(), this.refreshDownloads()]); }
  state(task: DownloadTask) { this.update({ kind: 'state', task }); }
  progress(event: DownloadProgressEvent) { this.update({ kind: 'progress', event }); }
  cancelled(task: DownloadTask) {
    const current = this.downloads.find(item => item.task_id === task.task_id);
    if (!current || current.state === 'downloading') this.state(task);
  }
  created(task: DownloadTask) {
    if (!this.downloads.some(item => item.task_id === task.task_id)) this.state(task);
  }
  private update(update: Update) { this.updates?.push(update); this.apply(update); }
  private apply(update: Update) {
    switch (update.kind) {
      case 'state': this.downloads = [update.task, ...this.downloads.filter(item => item.task_id !== update.task.task_id)]; break;
      case 'progress': this.downloads = this.downloads.map(task => task.task_id === update.event.task_id && task.state === 'downloading' ? { ...task, downloaded_bytes: update.event.downloaded_bytes, bytes_per_second: update.event.bytes_per_second } : task); break;
      default: { const unreachable: never = update; return unreachable; }
    }
  }
}
