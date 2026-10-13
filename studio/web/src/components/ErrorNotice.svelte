<script lang="ts">
  import type { ApiError } from '../api/types';
  let { error }: { error: ApiError | null } = $props();
  const reasons: Record<string, string> = {
    queue_duplicate: '同一配置已有未结束任务，请等待或取消。',
    invalid_state: '任务已结束，无法再取消。',
    output_exists: '输出目录已存在，请更换目录名。',
    not_resumable: '该来源不可续测；仅支持批次内最新未完成轮次。',
    refresh_in_progress: '刷新正在进行。',
    settings_incomplete: '请先补全必要的目录设置。',
    output_root_outside_results: '配置的输出目录不在结果根目录内。',
  };
  let reason = $derived(typeof error?.details?.reason === 'string' ? error.details.reason : null);
</script>
{#if error}
  <div class="error" role="alert"><strong>{error.message}</strong>
    <small>{error.code}</small>
    {#if reason}<p>{reasons[reason] ?? '服务端返回以下原因'} · {reason}</p>{/if}
    {#if error.limitations.length}<ul>{#each error.limitations as reason}<li>{reason}</li>{/each}</ul>{/if}
  </div>
{/if}
