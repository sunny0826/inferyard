<script lang="ts">
  import { quote } from '../onboarding/commands';
  let { kind, root, execute, pending }: { kind: 'model' | 'engine'; root: string; execute: (command: string) => Promise<void>; pending: boolean } = $props();
  let source = $state(''); let name = $state(''); let tokenEnv = $state('');
  let command = $derived(`inferyard ${kind} acquire --source ${quote(source)} --out ${quote(`${root}/${name}`)}${kind === 'model' && tokenEnv ? ` --token-env ${quote(tokenEnv)}` : ''}`);
</script>
<details><summary>{kind === 'model' ? '1 · 下载模型' : '2 · 下载引擎（macOS）'}</summary>
  <form onsubmit={event => { event.preventDefault(); void execute(command); }}>
    <label for={`${kind}-url`}>{kind === 'model' ? '模型 URL' : '引擎 URL'}</label><input id={`${kind}-url`} type="url" bind:value={source} required placeholder="https://…" />
    <label for={`${kind}-name`}>新目录名</label><input id={`${kind}-name`} bind:value={name} required pattern="[A-Za-z0-9][A-Za-z0-9._-]*" />
    {#if kind === 'model'}<label for="token-env">凭据环境变量名（可选）</label><input id="token-env" bind:value={tokenEnv} pattern="[A-Za-z_][A-Za-z0-9_]*" placeholder="HF_TOKEN" /><small>只填写变量名；凭据由启动 Studio 的环境提供。</small>{/if}
    <pre>{command}</pre><button disabled={pending}>下载{kind === 'model' ? '模型' : '引擎'}</button>
  </form>
</details>

<style>details { padding: 1rem 0; } pre { white-space: pre-wrap; overflow-wrap: anywhere; } form { min-width: 0; }</style>
