<script lang="ts">
  import { onMount } from 'svelte';
  import { api, mockMode } from '../api';
  import { failure } from '../api/client';
  import type { ApiError } from '../api/types';
  import type { Readiness, WorkspaceProgress } from '../api/onboarding-schema';
  import type { RunsStore } from '../runs/store.svelte';
  import { terminal, taskLabel } from '../runs/model';
  import { bundleName, nextStep, phaseLabels } from '../onboarding/summary';
  import ReadinessCard from '../components/ReadinessCard.svelte';
  import ErrorNotice from '../components/ErrorNotice.svelte';

  let { store }: { store: RunsStore } = $props();
  let readiness = $state<Readiness | null>(null);
  let progress = $state<WorkspaceProgress | null>(null);
  let error = $state<ApiError | null>(null);
  let pending = $state(false);
  let opening = $state(false);
  let loading = $state(true);
  let message = $state('');
  let revision = 0;
  let disposed = false;
  let activeTasks = $derived(store.tasks.filter(task => !terminal(task)));

  async function refresh() {
    const version = ++revision;
    loading = true; error = null;
    try {
      const ready = await api.getReadiness();
      if (disposed || version !== revision) return;
      readiness = ready;
      if (ready.cli.status !== 'ok') { progress = null; return; }
      const response = await api.getProgress();
      if (!disposed && version === revision) progress = response.progress;
    } catch (cause) {
      if (!disposed && version === revision) { error = failure(cause); progress = null; }
    } finally {
      if (!disposed && version === revision) loading = false;
    }
  }
  async function provision() {
    pending = true; error = null;
    try { await api.provision(); await refresh(); }
    catch (cause) { error = failure(cause); }
    finally { pending = false; }
  }
  async function open() {
    if (opening) return;
    opening = true; error = null; message = '';
    try {
      await api.openWizard();
      message = mockMode ? '模拟模式：已模拟打开向导，不会打开原生窗口。' : '已打开向导，请在向导窗口中继续。';
    } catch (cause) { error = failure(cause); }
    finally { opening = false; }
  }
  async function cancel(id: string) {
    try { store.state((await api.cancelTask(id)).task); }
    catch (cause) { error = failure(cause); }
  }
  onMount(() => {
    const close = api.subscribe({
      deviceUpdated() { void refresh(); }, connection() {}, error(value) { error = value; },
      reset() { if (!pending) void refresh(); },
      reportCreated() { void refresh(); },
      runState(event) { if (terminal(event.task)) void refresh(); },
    });
    const focus = () => { void refresh(); };
    window.addEventListener('focus', focus);
    void refresh();
    return () => { disposed = true; revision++; close(); window.removeEventListener('focus', focus); };
  });
</script>

<section class="onboarding" aria-label="首次上手向导">
  <header>
    <div><h2>首次上手向导</h2><p>{mockMode ? '模拟演示，不下载、不发送真实模型请求。' : '在独立向导窗口中完成准备、运行和报告验证。'}</p></div>
    <button class="secondary" onclick={refresh} disabled={pending || loading}>刷新向导</button>
  </header>
  <ErrorNotice {error} />
  <div class="entry-card">
    {#if readiness?.cli.status !== 'ok' && readiness}
      <h3>先安装 InferYard CLI</h3>
      <p>安装 CLI 后，在 Settings 配置路径。现在仍可打开向导或查看设备。</p>
      <a href="#/settings">前往 Settings</a> · <a href="#/device">查看设备</a>
    {:else if progress}
      <h3>{progress.default_scope_complete ? '两题包报告已验证' : '当前测评进度'}</h3>
      <ol class="bundle-progress" aria-label="题包进度">
        {#each progress.bundles as bundle}
          <li>
            <div><strong>{bundleName(bundle.bundle)}</strong><small>{bundle.bundle}</small></div>
            <span>{phaseLabels[bundle.phase]}</span>
            {#if bundle.blocked_reason}<p class="bundle-note">此题包需要处理准备问题，请在向导中查看。</p>{/if}
          </li>
        {/each}
      </ol>
      <p class="next-step">{nextStep(progress)}</p>
      {#if progress.default_scope_complete}<a href="#/reports">查看报告</a>{/if}
    {:else if loading}<p>正在读取工作区进度…</p>
    {:else if error}<p>暂时无法读取进度，可刷新重试或打开向导。</p>
    {:else if readiness?.workspace.initialized}
      <h3>暂时无法读取工作区进度</h3>
      <p>可刷新重试，或打开向导查看工作区状态。</p>
    {:else}
      <h3>{readiness?.first_run ? '开始首次测评' : '工作区尚未初始化'}</h3>
      <p>打开向导，初始化工作区并完成首次测评。</p>
      <p>模型与引擎准备、设备检查、两份题包运行和报告验证，都将在向导窗口中完成。</p>
    {/if}
    <div class="actions"><button onclick={open} disabled={opening}>{opening ? '正在打开…' : '打开向导'}</button></div>
    <p class="open-message" role="status">{message}</p>
  </div>
  <ReadinessCard {readiness} {provision} {pending} />
  {#if activeTasks.length}
    <div class="active-tasks" aria-label="运行中的任务">
      <h3>运行中的任务</h3>
      {#each activeTasks as task}
        <div class="task-row">
          <a href={`#/runs/${encodeURIComponent(task.task_id)}`}>{task.bundle} · {taskLabel(task)} · 查看进度</a>
          <button onclick={() => cancel(task.task_id)} disabled={task.state === 'cancelling'}>取消运行</button>
        </div>
      {/each}
    </div>
  {/if}
</section>

<style>
  .onboarding { margin-bottom: 1.5rem; }
  .entry-card { margin-bottom: 1.5rem; }
  h3 { margin-top: 0; font-size: 1.15rem; }
  .bundle-progress { list-style: none; padding: 0; display: grid; gap: 1rem; }
  .bundle-progress li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: .5rem 1rem; }
  .bundle-progress small { margin-top: .25rem; }
  .bundle-note { width: 100%; margin: 0; }
  .next-step { margin-top: 1.25rem; }
  .open-message:empty { display: none; }
  .active-tasks { margin-top: 1.5rem; }
  .task-row + .task-row { margin-top: 1rem; }
</style>
