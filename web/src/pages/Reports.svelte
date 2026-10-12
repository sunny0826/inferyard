<script lang="ts">
  import { api } from '../api';
  import { failure, reportLink } from '../api/client';
  import type { ApiError } from '../api/types';
  import type { RunsStore } from '../runs/store.svelte';
  import type { AssetsStore } from '../assets-state/store.svelte';
  import ErrorNotice from '../components/ErrorNotice.svelte';
  let { store, runs }: { store: AssetsStore; runs: RunsStore } = $props();
  let selected = $state<string[]>([]);
  let pending = $state(false);
  let error = $state<ApiError | null>(null);
  let message = $state('');
  let history = $derived([...new Map([...(runs.history?.batches.flatMap(batch => batch.runs) ?? []), ...(runs.history?.standalone_runs ?? [])].map(run => [run.path, run])).values()]);
  async function generate() {
    if (pending || selected.length < 1 || selected.length > 100) return;
    pending = true; error = null; message = '';
    try { const value = await api.createReport({ run_paths: [...selected] }); message = `已生成 ${value.report.report_id}`; await store.refreshReports(); }
    catch (cause) { error = failure(cause); }
    finally { pending = false; }
  }
</script>
<header><div><h1>Reports</h1><p>选择运行记录，生成离线报告。</p></div><button onclick={() => { void store.refreshReports(); void runs.refreshHistory(); }}>刷新报告</button></header>
<div class="run-stack">
<section aria-label="生成报告"><h2>生成报告</h2><ErrorNotice {error} /><ErrorNotice error={runs.error} />
<form onsubmit={event => { event.preventDefault(); void generate(); }}>
<fieldset disabled={pending}><legend>选择来源 run（1–100 个）</legend>
{#each history as run (run.path)}<label class="check path"><input type="checkbox" value={run.path} bind:group={selected} disabled={selected.length >= 100 && !selected.includes(run.path)} />{run.run_id} · {run.path}</label>{/each}
{#if !history.length}<p>暂无运行记录。</p>{/if}</fieldset>
<p>已选 {selected.length} / 100</p><button disabled={pending || !selected.length || selected.length > 100}>{pending ? '生成中…' : '生成报告'}</button>
<p role="status">{pending ? '报告正在生成，请稍候。' : message}</p>
</form></section>
<section aria-label="报告列表"><h2>报告列表</h2><ErrorNotice error={store.reportError} />
{#if !store.reports}<p>正在读取报告…</p>{:else if !store.reports.reports.length}<p>暂无报告。</p>{/if}
{#each store.reports?.reports ?? [] as report (report.report_id)}
<article data-testid={`report-${report.report_id}`}><h3 class="path">{report.report_id}</h3><p>目录修改时间：{report.created_at ?? '未知'}</p>
<ul>{#each report.run_ids as id, index}<li class="path">{id} · {report.run_paths[index] ?? '来源不可达'}</li>{/each}</ul>
<a href={reportLink(report.report_id)} target="_blank" rel="noopener noreferrer">打开报告</a></article>
{/each}</section></div>
