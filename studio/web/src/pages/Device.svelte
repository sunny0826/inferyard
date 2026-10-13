<script lang="ts">
  import type { DeviceResponse } from '../api/types';
  import Fields from '../components/Fields.svelte';
  import ErrorNotice from '../components/ErrorNotice.svelte';
  let { device, refresh, pending }: { device: DeviceResponse | null; refresh: () => void; pending: boolean } = $props();
</script>
<header><div><h1>Device</h1><p>本机硬件、模型与运行准备情况</p></div><button onclick={refresh} disabled={pending || device?.refreshing}>{pending || device?.refreshing ? '刷新中…' : '刷新设备'}</button></header>
{#if device}
  <ErrorNotice error={device.last_error} />
  <p role="status">{device.refreshing || pending ? '正在采集设备信息…' : '设备快照'} · {device.captured_at ?? '尚未采集'}</p>
  {#if device.snapshot}
    {@const s = device.snapshot}
    <div class="grid">
      <section><h2>硬件</h2><Fields values={Object.fromEntries(Object.entries(s.hardware).filter(([key]) => !['gpu', 'sources', 'missing'].includes(key)))} />
        <h3>GPU</h3>
        {#if s.hardware.gpu}<Fields values={{ status: s.hardware.gpu.status, source: s.hardware.gpu.source, reason: s.hardware.gpu.reason }} />
          {#each s.hardware.gpu.devices as gpu}<Fields values={gpu} />{/each}
        {:else}<p>未提供 GPU 信息。</p>{/if}
      </section>
      <section><h2>推荐配置</h2><p class="badge">{s.recommendation.status === 'blocked' ? '已阻断' : '有推荐配置'} · {s.ready_to_run ? '可运行' : '尚未就绪'}</p>
        {#if s.recommendation.reason}<p>{s.recommendation.reason}</p>{/if}
        <ul>{#each s.recommendation.limitations as reason}<li>{reason}</li>{/each}</ul>
        {#if s.recommendation.selected}<h3>已选配置</h3><Fields values={s.recommendation.selected} />{/if}
        {#each s.recommendation.candidates as candidate}<h3>{candidate.model_name} · {candidate.mode}</h3><Fields values={candidate} />{/each}
      </section>
      <section class="wide"><h2>本地 GGUF 模型</h2>
        {#each s.models as model}<article><h3>{model.status === 'available' ? model.name : '不可用模型'}</h3><p class="path">{model.path}</p>
          {#if model.status === 'available'}<Fields values={{ size_bytes: model.size_bytes, architecture: model.architecture, context_length: model.context_length, file_type: model.file_type, chat_template_available: model.chat_template_available }} />
          {:else}<p class="error">{model.reason}</p>{/if}</article>
        {:else}<p>没有发现本地 GGUF 模型。</p>{/each}
      </section>
      <section><h2>平台能力</h2><Fields values={{ ...s.platform_capabilities }} /></section>
      <section><h2>缺测与来源</h2><h3>缺测原因</h3><Fields values={s.hardware.missing ?? { status: '未提供缺测说明' }} /><h3>采集来源</h3><Fields values={s.hardware.sources ?? { status: '未提供来源说明' }} />
        <p>可用内存{s.hardware.memory_available_is_estimate ? '为估算值' : '未标记为估算值'}。模型请求数：{s.model_requests_sent}</p>
      </section>
      <section class="wide"><h2>下一步</h2>{#each s.next_steps as step}<Fields values={step} />{:else}<p>无后续操作。</p>{/each}</section>
    </div>
  {:else}<section class="empty"><h2>尚无设备快照</h2><p>刷新设备以查看硬件与本地模型。请先在 Settings 配置模型目录。</p></section>{/if}
{:else}<p role="status">正在读取设备信息…</p>{/if}
