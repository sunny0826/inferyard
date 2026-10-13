<script lang="ts">
  import type { RunHistoryResponse, RunSummary, RunTask } from '../api/types';
  import { aggregateStatus, historyLabels } from '../runs/model';
  let { history, tasks, resume, denied }: { history: RunHistoryResponse | null; tasks: RunTask[]; resume: (run: RunSummary) => void; denied: string[] } = $props();
</script>
{#snippet row(run: RunSummary)}
  <article class="history-run" data-testid={`history-${run.run_id}`}>
    <div class="row"><h3>{run.run_id}</h3><span class="badge">{historyLabels[run.status]} · {run.status}</span></div>
    <p>{run.model_name ?? '模型未知'} · {run.bundle ?? '题包未知'} · {run.engine ?? '引擎未知'}</p>
    <p class="path">{run.path}</p>
    <small>封存：{run.sealed ? '是' : '否'} · 完整度：{run.completeness ?? '未知'} · 停止原因：{run.stop_reason ?? '无'}</small>
    <small>得分：{run.score === null ? '证据不足' : `${run.score.passed}/${run.score.valid} · ${run.score.rate === null ? '无有效题' : `${(run.score.rate * 100).toFixed(1)}%`}`}</small>
    <small>关系：{run.relation ?? '未知'} · 来源：{run.parent_run_id ?? '无'} · 开始：{run.started_at ?? '未知'}</small>
    <div class="actions"><button disabled={run.batch_id === null || !run.resumable || denied.includes(run.path)} onclick={() => resume(run)}>续测 {run.run_id}</button>
    {#each tasks.filter(task => task.run_path === run.path) as task}<a href={`#/runs/${encodeURIComponent(task.task_id)}`}>查看实时详情</a>{/each}</div>
    {#if run.batch_id === null}<small>独立运行不支持续测。</small>{:else if !run.resumable || denied.includes(run.path)}<small>当前来源不可续测（not_resumable）。</small>{/if}
  </article>
{/snippet}
<section><h2>运行历史</h2>
{#if history === null}<p>正在读取历史…</p>{:else}
  <p class="path">结果目录：{history.results_root ?? '尚未配置'}</p>
  {#if !history.batches.length && !history.standalone_runs.length}<p>暂无运行记录。</p>{/if}
  {#each history.batches as batch}
    {@const status = aggregateStatus(batch.runs.map(run => run.status))}
    <section class="batch" data-testid={`batch-${batch.batch_id}`}><h3>批次 {batch.batch_id}</h3>
    {#if status}<span class="badge">{historyLabels[status]} · {status}</span>{/if}<p class="path">{batch.path}</p>
    {#each batch.runs as run}{@render row(run)}{/each}</section>
  {/each}
  {#if history.standalone_runs.length}<h3>独立运行</h3>{#each history.standalone_runs as run}{@render row(run)}{/each}{/if}
{/if}
</section>
