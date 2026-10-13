<script lang="ts">
  import type { Readiness } from '../api/onboarding-schema';
  let { readiness, provision, pending }: { readiness: Readiness | null; provision: () => void; pending: boolean } = $props();
  let dismissed = $state(localStorage.getItem('inferyard.readiness.dismissed') === 'true');
  const dismiss = () => { dismissed = true; localStorage.setItem('inferyard.readiness.dismissed', 'true'); };
  let missing = $derived(readiness && (readiness.cli.status !== 'ok' || !readiness.workspace.initialized || !readiness.results.exists || !readiness.models.exists));
</script>
{#if readiness && (missing || !dismissed)}
<section aria-label="上手体检">
  {#if readiness.first_run && !dismissed}
    <h2>欢迎使用 InferYard</h2><p>先准备本地目录，再按向导完成两份题包。缺少配置时仍可浏览所有页面。</p>
    <button onclick={dismiss}>收起引导</button>
  {:else}<h2>环境准备</h2>{/if}
  <details open={readiness.first_run && !dismissed}><summary>{missing ? '有待准备的项目' : '目录与 CLI 已就绪'}</summary>
    <ul>
      <li>CLI：{readiness.cli.status === 'ok' ? readiness.cli.version : '请安装 CLI 或在 Settings 配置路径'}</li>
      <li>工作区：{readiness.workspace.path ?? '未配置'} · {readiness.workspace.initialized ? '已初始化' : readiness.workspace.configured ? '已配置，待初始化' : '待配置'}</li>
      <li>结果目录：{readiness.results.exists ? '可用' : '待创建'}</li>
      <li>模型目录：{readiness.models.exists ? '可用' : '待创建'}</li>
    </ul>
    <button onclick={provision} disabled={pending}>准备目录</button> <a href="#/settings">修改设置</a> · <a href="#/device">查看设备</a>
  </details>
</section>
{/if}
