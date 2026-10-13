<script lang="ts">
  import { api } from '../api';
  import { failure } from '../api/client';
  import type { ApiError } from '../api/types';
  import type { RunsStore } from '../runs/store.svelte';
  import { caseLabels, phaseLabels, taskLabel, terminal } from '../runs/model';
  import ErrorNotice from '../components/ErrorNotice.svelte';
  import ResourceChart from '../components/ResourceChart.svelte';
  let { store }: { store: RunsStore } = $props();
  let pending = $state(false); let error = $state<ApiError | null>(null);
  async function cancel() {
    if (!store.detail) return;
    const previous = store.detail.task;
    pending = true; error = null;
    try {
      const result = await api.cancelTask(previous.task_id);
      // Do not roll back a terminal SSE state with an earlier cancelling acknowledgment.
      if (!store.tasks.some(task => task.task_id === previous.task_id && terminal(task))) store.state(result.task);
    } catch (cause) { error = failure(cause); }
    finally { pending = false; }
  }
</script>
<header><div><h1>RunDetail</h1><a href="#/runs">← 返回 Runs</a></div><button onclick={() => store.select(store.selected)}>刷新详情</button></header>
<ErrorNotice error={store.detailError} /><ErrorNotice {error} />
{#if store.detail}
  {@const task = store.detail.task}{@const progress = store.detail.progress}
  <div class="run-stack"><section><div class="row"><h2>{task.bundle ?? task.kind} · {task.task_id}</h2><span class="badge" data-testid="task-state">{taskLabel(task)}</span></div>
    <p class="path">{task.run_path ?? '等待侦测运行目录'}</p>
    <p>任务类型：{task.kind} · 运行 ID：{task.run_id ?? '未知'}</p>
    <button disabled={pending || terminal(task) || task.state === 'cancelling'} onclick={cancel}>{pending ? '提交取消…' : '取消任务'}</button>
    <ErrorNotice error={task.error} />
    {#if task.exit}<dl><div><dt>退出码</dt><dd>{task.exit.exit_code}</dd></div><div><dt>CLI 状态</dt><dd>{task.exit.cli_status ?? '未知'}</dd></div><div><dt>完整度</dt><dd>{task.exit.completeness ?? '未知'}</dd></div><div><dt>停止原因</dt><dd>{task.exit.stop_reason ?? '无'}</dd></div></dl>
    {#if task.exit.limitations.length}<h3>limitations</h3><ul>{#each task.exit.limitations as limitation}<li class="path">{limitation}</li>{/each}</ul>{/if}{/if}
  </section>
  <section><h2>实时进度</h2>
  {#if progress}<p data-testid="progress">{progress.phase === null ? '阶段未知' : `${phaseLabels[progress.phase]} · ${progress.phase}`} · {progress.done} / {progress.total ?? '未知'}</p>
    <small>{task.kind === 'resume' ? '分母为冻结续测子集' : '分母为冻结选中题集合'}；未读取时不猜测。当前题：{progress.current_case_id ?? '无'} · last_seq：{progress.last_seq}</small>
    <div class="table-scroll"><table><thead><tr><th>题目</th><th>状态</th><th>类别</th><th>质量</th><th>耗时 ms</th><th>错误类别</th></tr></thead><tbody>
    {#each progress.cases as item}<tr><td>{item.case_id}</td><td>{caseLabels[item.state]} · {item.state}</td><td>{item.category ?? '缺测'}</td><td>{item.quality_state ?? '缺测'}</td><td>{item.duration_ms ?? '缺测'}</td><td>{item.error_category ?? '无'}</td></tr>{/each}
    </tbody></table></div>
  {:else}<p>等待冻结题目与进度数据…</p>{/if}</section>
  <ResourceChart points={store.detail.resources} /></div>
{:else if !store.detailError}<p>正在读取任务详情…</p>{/if}
