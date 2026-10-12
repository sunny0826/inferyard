<script lang="ts">
  import { api, mockMode } from '../api';
  import { failure } from '../api/client';
  import { startRequestSchema } from '../api/run-schema';
  import type { ApiError, RunTask } from '../api/types';
  import ErrorNotice from './ErrorNotice.svelte';
  let { started }: { started: (tasks: RunTask[]) => void } = $props();
  let first = $state(mockMode ? '/mock/configs/zh-core' : '');
  let second = $state(mockMode ? '/mock/configs/zh-svg-pelican' : '');
  let single = $state(false); let confirmed = $state(false); let pending = $state(false);
  let error = $state<ApiError | null>(null);
  async function submit(event: SubmitEvent) {
    event.preventDefault(); error = null;
    const parsed = startRequestSchema.safeParse({ config_paths: single ? [first.trim()] : [first.trim(), second.trim()], ...(single && confirmed ? { allow_single_bundle: true } : {}) });
    if (!parsed.success) { error = { code: 'invalid_request', message: parsed.error.issues.map(issue => issue.message).join('；'), limitations: [], details: null }; return; }
    pending = true;
    try { const response = await api.startRuns(parsed.data); started(response.tasks); }
    catch (cause) { error = failure(cause); }
    finally { pending = false; }
  }
</script>
<section><h2>启动测评</h2><ErrorNotice {error} /><form onsubmit={submit}>
  <fieldset disabled={pending}>
    <label for="core-config">zh-core 已绑定配置路径</label><input id="core-config" bind:value={first} placeholder="/absolute/path/to/config.toml" required />
    <label class="check"><input type="checkbox" bind:checked={single} onchange={() => { confirmed = false; }} />仅运行单 bundle</label>
    {#if !single}<label for="svg-config">zh-svg-pelican 已绑定配置路径</label><input id="svg-config" bind:value={second} placeholder="/absolute/path/to/config.toml" required />
    {:else}<label class="check"><input type="checkbox" bind:checked={confirmed} />我确认只运行一个 bundle</label>{/if}
    <small>默认双 bundle，按配置顺序串行运行。配置须已绑定对应题包。</small>
    <button disabled={pending || (single && !confirmed)}>{pending ? '提交中…' : '启动运行'}</button>
  </fieldset>
</form></section>
