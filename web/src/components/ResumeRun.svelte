<script lang="ts">
  import { api } from '../api';
  import { failure } from '../api/client';
  import { resumeRequestSchema } from '../api/run-schema';
  import type { ApiError, RunSummary, RunTask } from '../api/types';
  import ErrorNotice from './ErrorNotice.svelte';
  let { run, resumed, close, rejected }: { run: RunSummary; resumed: (task: RunTask) => void; close: () => void; rejected: (path: string) => void } = $props();
  let endpoint = $state(''); let pid = $state(''); let pending = $state(false); let denied = $state(false);
  let error = $state<ApiError | null>(null);
  async function submit(event: SubmitEvent) {
    event.preventDefault(); error = null;
    const parsed = resumeRequestSchema.safeParse({ from_run: run.path, ...(endpoint.trim() ? { endpoint_url: endpoint.trim() } : {}), ...(pid.trim() ? { server_pid: Number(pid) } : {}) });
    if (!parsed.success) { error = { code: 'invalid_request', message: parsed.error.issues.map(issue => issue.message).join('；'), limitations: [], details: null }; return; }
    pending = true;
    try { const result = await api.resumeRun(parsed.data); resumed(result.task); }
    catch (cause) { error = failure(cause); if (error.details?.reason === 'not_resumable') { denied = true; rejected(run.path); } }
    finally { pending = false; }
  }
</script>
<section aria-label="续测设置"><h2>续测 {run.run_id}</h2><p class="path">{run.path}</p><ErrorNotice {error} />
<form onsubmit={submit}><fieldset disabled={pending || denied}>
  <label for="endpoint">endpoint_url（可选）</label><input id="endpoint" bind:value={endpoint} placeholder="http://127.0.0.1:8080" />
  <label for="server-pid">server_pid（与 endpoint 同给同省）</label><input id="server-pid" bind:value={pid} inputmode="numeric" />
  <small>不填写时沿用来源配置。来源是否可续测由 CLI 最终裁决。</small>
  <button disabled={pending || denied}>{pending ? '提交中…' : '确认续测'}</button>
</fieldset></form><button class="secondary" onclick={close}>关闭续测设置</button></section>
