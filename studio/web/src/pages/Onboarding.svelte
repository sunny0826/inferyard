<script lang="ts">
  import { onMount } from 'svelte';
  import { api, mockMode } from '../api';
  import { failure } from '../api/client';
  import type { ApiError } from '../api/types';
  import type { Readiness, WorkspaceProgress, Startup, ProcessCandidate } from '../api/onboarding-schema';
  import type { RunsStore } from '../runs/store.svelte';
  import { terminal, taskLabel } from '../runs/model';
  import { quote, setArgument, actionLabels } from '../onboarding/commands';
  import ReadinessCard from '../components/ReadinessCard.svelte';
  import AcquireStep from '../components/AcquireStep.svelte';
  import ErrorNotice from '../components/ErrorNotice.svelte';
  let { store }: { store: RunsStore } = $props();
  let readiness = $state<Readiness | null>(null); let progress = $state<WorkspaceProgress | null>(null);
  let error = $state<ApiError | null>(null); let pending = $state(false); let message = $state('');
  let command = $state(''); let startup = $state<Startup | null>(null); let processes = $state<ProcessCandidate[]>([]);
  let engine = $state(''); let pid = $state(''); let endpoint = $state('http://127.0.0.1:48857');
  let revision = 0; let disposed = false;
  let active = $derived(progress?.bundles.find(bundle => bundle.phase !== 'run_complete'));
  let activeTasks = $derived(store.tasks.filter(task => !terminal(task)));
  let workspace = $derived(readiness?.workspace.path ?? '');
  async function refresh() {
    const version = ++revision;
    try {
      const ready = await api.getReadiness(); if (disposed || version !== revision) return;
      readiness = ready;
      if (ready.cli.status !== 'ok') { progress = null; return; }
      const response = await api.getProgress(); if (disposed || version !== revision) return;
      progress = response.progress; startup = null;
      command = progress?.next_command ?? (progress ? '' : `inferyard init --out ${quote(ready.workspace.exists ? `${ready.workspace.path}/workspace-${Date.now()}` : ready.workspace.path ?? '')}`);
      if (progress?.next_action === 'device_check' && ready.models.path) command = setArgument(command, '--models-root', ready.models.path);
      const candidate = progress?.bundles.find(bundle => bundle.phase === 'bound_missing')?.candidate_path;
      if (candidate) {
        const [frozen, candidates] = await Promise.all([api.getStartup(candidate), api.getProcesses()]);
        if (!disposed && version === revision) {
          startup = frozen; processes = candidates; pid = '';
          const host = frozen.startup_args[frozen.startup_args.indexOf('--host') + 1];
          const port = frozen.startup_args[frozen.startup_args.indexOf('--port') + 1];
          if (frozen.startup_args.includes('--host') && frozen.startup_args.includes('--port') && host && port) {
            endpoint = `http://${host.includes(':') ? `[${host}]` : host}:${port}`;
            command = setArgument(command, '--endpoint', endpoint);
          }
        }
      }
    } catch (cause) { if (!disposed && version === revision) error = failure(cause); }
  }
  async function provision() {
    pending = true; error = null;
    try { await api.provision(); await refresh(); } catch (cause) { error = failure(cause); } finally { pending = false; }
  }
  async function execute(value: string, reportVerify = false) {
    if (pending) return; pending = true; error = null; message = '';
    try {
      const result = await api.executeWizard(value, reportVerify);
      store.addGroup(result.tasks);
      message = result.tasks.length ? '已加入运行队列，可查看进度或取消。' : `执行状态：${result.status}（退出码 ${result.exit_code}）`;
      if (result.exit_code === 0) await refresh();
    } catch (cause) { error = failure(cause); } finally { pending = false; }
  }
  async function cancel(id: string) { try { store.state((await api.cancelTask(id)).task); } catch (cause) { error = failure(cause); } }
  async function copyStartup() {
    if (!startup) return;
    try { await navigator.clipboard.writeText([startup.path, ...startup.startup_args].map(quote).join(' ')); message = '启动命令已复制，请在外部终端执行。'; }
    catch { message = '复制失败，请手动选择下方命令。'; }
  }
  onMount(() => {
    const close = api.subscribe({ deviceUpdated() {}, connection() {}, error(value) { error = value; }, reset() { if (!pending) void refresh(); }, runState(event) { if (terminal(event.task)) void refresh(); } });
    void refresh(); return () => { disposed = true; revision++; close(); };
  });
</script>
<section class="onboarding" aria-label="首次上手向导">
  <header><div><h2>首次上手向导</h2><p>{mockMode ? '模拟演示，不下载、不发送真实模型请求。' : '从本地准备到两题包报告，进度由工作区证据决定。'}</p></div><button onclick={refresh} disabled={pending}>刷新向导</button></header>
  <ErrorNotice {error} /><ReadinessCard {readiness} {provision} {pending} />
  <p role="status">{message}</p>
  {#if readiness}
    {#if readiness.models.path}<AcquireStep kind="model" root={readiness.models.path} {execute} {pending} />{:else}<p>1 · 下载模型：请先准备目录，或在 Settings 指定模型目录。</p>{/if}
    <AcquireStep kind="engine" root={`${workspace}/engines`} {execute} {pending} />
  {/if}
  {#if progress}
    <ol>{#each progress.bundles as bundle}<li><strong>{bundle.bundle}</strong> · {bundle.phase}{#if bundle.blocked_reason}<p>{bundle.blocked_reason}</p>{/if}</li>{/each}</ol>
    {#if active?.bundle === 'zh-svg-pelican'}<h3>8 · 第二题包</h3><p>按该题包重新创建候选、启动服务、绑定并运行。</p>{/if}
    {#if startup}
      <section><h3>5 · 在外部终端启动服务</h3><p>以下命令来自候选冻结的 startup_args。请由操作者启动并确认服务；Studio 不代启或代停。</p>
        <pre>{[startup.path, ...startup.startup_args].map(quote).join(' ')}</pre><button onclick={copyStartup}>复制启动命令</button>
      </section>
      <label for="wizard-pid">本机进程</label><select id="wizard-pid" bind:value={pid} onchange={() => { if (pid) command = setArgument(command, '--pid', pid); }}><option value="">选择已启动的进程</option>{#each processes as process}<option value={String(process.pid)}>{process.pid} · {process.name}</option>{/each}</select>
      <label for="wizard-endpoint">endpoint</label><input id="wizard-endpoint" bind:value={endpoint} oninput={event => { command = setArgument(command, '--endpoint', event.currentTarget.value); }} />
      <small>仅列出进程名；绑定时由 CLI 校验 PID、启动时间与实际参数。</small>
    {/if}
    {#if progress.next_action === 'prepare_candidate'}<label for="wizard-engine">引擎文件绝对路径</label><input id="wizard-engine" bind:value={engine} oninput={event => { if (event.currentTarget.value) command = setArgument(command, '--engine', event.currentTarget.value); }} />{/if}
    {#if progress.default_scope_complete}<h3>两题包报告已验证</h3><a href="#/reports">查看报告</a>{/if}
  {:else}<p>先初始化工作区。init 需要新目录；准备目录后会建议其下的新工作区，成功后自动更新设置。</p>{/if}
  {#if command}
    <label for="wizard-command">下一步命令（参数可编辑）</label><textarea id="wizard-command" rows="4" bind:value={command}></textarea>
    <button disabled={pending || activeTasks.length > 0} onclick={() => execute(command, progress?.next_action === 'report' || progress?.next_action === 'verify')}>{pending ? '执行中…' : progress ? actionLabels[active?.next_action === 'resume' ? 'resume' : progress.next_action] : '初始化工作区'}</button>
    {#if progress?.next_action === 'run'}<small>将执行当前明确显示的题包；完成后向导继续第二题包。实际请求前请核对设备、预算和停止条件。</small>{/if}
  {/if}
  {#each activeTasks as task}<p><a href={`#/runs/${encodeURIComponent(task.task_id)}`}>{task.bundle} · {taskLabel(task)} · 查看进度</a> <button onclick={() => cancel(task.task_id)} disabled={task.state === 'cancelling'}>取消运行</button></p>{/each}
</section>
<style>
  .onboarding { margin-bottom: 1.5rem; }
  pre { white-space: pre-wrap; overflow-wrap: anywhere; }
  textarea, select { width: 100%; max-width: 100%; } small { display: block; margin: .5rem 0; }
</style>
