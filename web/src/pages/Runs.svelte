<script lang="ts">
  import type { RunsStore } from '../runs/store.svelte';
  import type { RunSummary, RunTask } from '../api/types';
  import { taskLabel } from '../runs/model';
  import StartRuns from '../components/StartRuns.svelte';
  import ResumeRun from '../components/ResumeRun.svelte';
  import RunHistory from '../components/RunHistory.svelte';
  import ErrorNotice from '../components/ErrorNotice.svelte';
  let { store, dashboard = false }: { store: RunsStore; dashboard?: boolean } = $props();
  let source = $state<RunSummary | null>(null); let denied = $state<string[]>([]);
  let ungrouped = $derived(store.tasks.filter(task => !store.groups.some(group => group.includes(task.task_id))));
  function started(tasks: RunTask[]) { store.addGroup(tasks); }
</script>
{#snippet taskRow(task: RunTask)}
  <article class="task-row" data-testid={`task-${task.task_id}`}><a href={`#/runs/${encodeURIComponent(task.task_id)}`}>{task.bundle ?? task.kind} · {task.task_id}</a><span>{taskLabel(task)}</span></article>
{/snippet}
<header><div><h1>{dashboard ? 'Dashboard' : 'Runs'}</h1><p>启动、跟踪与续测。历史状态以证据索引为准。</p></div><button onclick={() => store.rebuild()}>刷新运行</button></header>
<ErrorNotice error={store.error} />
<div class="run-stack"><StartRuns {started} />
{#if source}{#key source.path}<ResumeRun run={source} close={() => { source = null; }} rejected={path => { denied = [...denied, path]; }} resumed={task => { store.addGroup([task]); source = null; location.hash = `/runs/${encodeURIComponent(task.task_id)}`; }} />{/key}{/if}
<section><h2>任务队列</h2><small>同次启动的任务成组显示；分组保留于当前页面会话。</small>
{#each store.groups as group, index}<section class="task-group" data-testid="task-group"><h3>启动组 {index + 1} · {group.length} 个任务</h3>{#each group as id}{@const task = store.tasks.find(value => value.task_id === id)}{#if task}{@render taskRow(task)}{:else}<p>{id} · 任务已不在服务端内存表中</p>{/if}{/each}</section>{/each}
{#each ungrouped as task}{@render taskRow(task)}{/each}
{#if !store.tasks.length}<p>暂无任务。</p>{/if}</section>
<RunHistory history={store.history} tasks={store.tasks} resume={run => { source = run; }} {denied} />
</div>
