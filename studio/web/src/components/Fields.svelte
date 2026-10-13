<script lang="ts">
  let { values }: { values: Record<string, unknown> } = $props();
  function display(value: unknown): string {
    if (value === null || value === undefined) return '未知 / 未配置';
    if (typeof value === 'boolean') return value ? '是' : '否';
    return typeof value === 'object' ? JSON.stringify(value) : String(value);
  }
</script>
<dl>{#each Object.entries(values) as [key, value]}<div><dt>{key}</dt><dd>{display(value)}</dd></div>{/each}</dl>
