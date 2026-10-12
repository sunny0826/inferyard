import { api } from '../api';
import { failure } from '../api/client';
import type { ApiError, RunHistoryResponse, RunTask, RunTaskDetail, RunProgressEvent, RunResourcesEvent } from '../api/types';
import { appendResources, terminal } from './model';

type Update = { kind: 'state'; task: RunTask } | { kind: 'progress'; event: RunProgressEvent } | { kind: 'resources'; event: RunResourcesEvent };
export class RunsStore {
  history = $state<RunHistoryResponse | null>(null);
  tasks = $state<RunTask[]>([]);
  detail = $state<RunTaskDetail | null>(null);
  groups = $state<string[][]>([]);
  error = $state<ApiError | null>(null);
  detailError = $state<ApiError | null>(null);
  selected = $state<string | null>(null);
  private historyVersion = 0;
  private listVersion = 0;
  private detailVersion = 0;
  private listUpdates: RunTask[] | null = null;
  private detailUpdates: Update[] | null = null;
  async refreshHistory() {
    const version = ++this.historyVersion;
    this.error = null;
    try { const value = await api.getRuns(); if (version === this.historyVersion) { this.history = value; } }
    catch (error) { if (version === this.historyVersion) this.error = failure(error); }
  }
  async rebuild() {
    const version = ++this.listVersion;
    this.listUpdates = [];
    const list = api.getTasks().then(value => {
      if (version !== this.listVersion) return;
      this.tasks = value.tasks;
      for (const task of this.listUpdates ?? []) this.putTask(task);
      this.listUpdates = null;
    }).catch(error => { if (version === this.listVersion) { this.error = failure(error); this.listUpdates = null; } });
    await Promise.all([this.refreshHistory(), list, this.select(this.selected)]);
  }
  async select(id: string | null) {
    const version = ++this.detailVersion;
    this.selected = id; this.detail = null; this.detailError = null; this.detailUpdates = null;
    if (id === null) return;
    this.detailUpdates = [];
    try {
      const value = await api.getTask(id);
      if (version !== this.detailVersion) return;
      this.detail = value;
      for (const update of this.detailUpdates ?? []) this.applyDetail(update);
    } catch (error) { if (version === this.detailVersion) this.detailError = failure(error); }
    finally { if (version === this.detailVersion) this.detailUpdates = null; }
  }
  private putTask(task: RunTask) {
    this.tasks = [task, ...this.tasks.filter(value => value.task_id !== task.task_id)].sort((a, b) => b.created_at.localeCompare(a.created_at));
  }
  state(task: RunTask) {
    this.putTask(task); this.listUpdates?.push(task);
    this.update({ kind: 'state', task });
    if (terminal(task)) void this.refreshHistory();
  }
  progress(event: RunProgressEvent) { this.update({ kind: 'progress', event }); }
  resources(event: RunResourcesEvent) { this.update({ kind: 'resources', event }); }
  addGroup(tasks: RunTask[]) {
    this.groups = [...this.groups, tasks.map(task => task.task_id)];
    for (const task of tasks) {
      // A run.state event can arrive before the POST response; keep the newer stream state.
      if (!this.tasks.some(existing => existing.task_id === task.task_id)) this.state(task);
    }
  }
  private update(update: Update) {
    const id = update.kind === 'state' ? update.task.task_id : update.event.task_id;
    if (id !== this.selected) return;
    this.detailUpdates?.push(update);
    this.applyDetail(update);
  }
  private applyDetail(update: Update) {
    if (!this.detail) return;
    switch (update.kind) {
      case 'state': this.detail = { ...this.detail, task: update.task }; break;
      case 'progress':
        if (update.event.progress.last_seq >= (this.detail.progress?.last_seq ?? 0)) this.detail = { ...this.detail, progress: update.event.progress };
        break;
      case 'resources': this.detail = { ...this.detail, resources: appendResources(this.detail.resources, update.event.points) }; break;
      default: { const unreachable: never = update; return unreachable; }
    }
  }
}
