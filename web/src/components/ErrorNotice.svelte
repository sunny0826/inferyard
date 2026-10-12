<script lang="ts">
  import type { ApiError } from '../api/types';
  let { error }: { error: ApiError | null } = $props();
</script>
{#if error}
  <div class="error" role="alert"><strong>{error.message}</strong>
    {#if error.limitations.length}<ul>{#each error.limitations as reason}<li>{reason}</li>{/each}</ul>{/if}
  </div>
{/if}
