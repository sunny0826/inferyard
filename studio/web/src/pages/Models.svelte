<script lang="ts">
  import { api } from '../api';
  import { failure } from '../api/client';
  import { outNameSchema } from '../api/assets-schema';
  import type { ApiError, DownloadState } from '../api/types';
  import type { AssetsStore } from '../assets-state/store.svelte';
  import ErrorNotice from '../components/ErrorNotice.svelte';
  let { store }: { store: AssetsStore } = $props();
  let url = $state(''); let outName = $state(''); let pending = $state(false);
  let cancelling = $state<string[]>([]); let error = $state<ApiError | null>(null);
  let validName = $derived(outNameSchema.safeParse(outName).success);
  const labels: Record<DownloadState, string> = { downloading: '下载中', done: '成功', failed: '失败', cancelled: '已取消' };
  function bytes(value: number) { return `${(value / 1024 ** 2).toFixed(2)} MiB（${value} bytes）`; }
  async function create() {
    if (!validName || !url.trim() || pending) return;
    pending = true; error = null;
    try { store.created((await api.createDownload({ source_url: url.trim(), out_name: outName })).task); }
    catch (cause) { error = failure(cause); }
    finally { pending = false; }
  }
  async function cancel(id: string) {
    cancelling = [...cancelling, id]; error = null;
    try { store.cancelled((await api.cancelDownload(id)).task); }
    catch (cause) { error = failure(cause); }
    finally { cancelling = cancelling.filter(value => value !== id); }
  }
</script>
<header><div><h1>Models / Downloads</h1><p>下载固定 revision 的 GGUF 模型。</p></div><button onclick={() => store.refreshDownloads()}>刷新下载</button></header>
<div class="run-stack"><section aria-label="创建下载"><h2>创建下载</h2><ErrorNotice {error} />
<form onsubmit={event => { event.preventDefault(); void create(); }}><fieldset disabled={pending}>
<label for="source-url">模型 URL</label><input id="source-url" type="url" bind:value={url} required placeholder="https://huggingface.co/…/blob/40位revision/model.gguf" />
<small>支持 Hugging Face / ModelScope blob URL，revision 必须为 40 位十六进制。</small>
<label for="out-name">out_name</label><input id="out-name" bind:value={outName} aria-invalid={outName.length > 0 && !validName} aria-describedby="name-help" />
<small id="name-help">仅允许英文字母、数字、点、下划线和连字符；不能全部为点。</small>
{#if outName && !validName}<p role="alert">目录名无效：不能使用路径分隔符、空格或纯点名称。</p>{/if}
</fieldset><button disabled={pending || !validName || !url.trim()}>{pending ? '创建中…' : '创建下载'}</button></form></section>
<section aria-label="下载列表"><h2>下载列表</h2><p>1 MiB = 1,048,576 bytes；速率单位 bytes/s。总量未知。</p><ErrorNotice error={store.downloadError} />
{#if !store.downloads.length}<p>暂无下载任务。</p>{/if}
{#each store.downloads as task (task.task_id)}<article data-testid={`download-${task.task_id}`}>
<div class="row"><h3 class="path">{task.out_name}</h3><span class="badge">{labels[task.state]} · {task.state}</span></div>
<dl><div><dt>输出目录</dt><dd>{task.out_dir}</dd></div><div><dt>文件</dt><dd>{task.file_name ?? '尚未侦测到文件'}</dd></div>
<div><dt>已下载</dt><dd>{bytes(task.downloaded_bytes)}</dd></div><div><dt>速率</dt><dd>{task.bytes_per_second === null ? '未知' : `${task.bytes_per_second} bytes/s`}</dd></div></dl>
{#if task.result}<p class="path">实测大小：{bytes(task.result.bytes)}<br />SHA-256：{task.result.sha256}</p>{/if}
{#if task.state === 'failed' || task.state === 'cancelled'}<p>输出目录保留；再次下载请更换目录名。</p>{/if}
<ErrorNotice error={task.error} />
{#if task.state === 'downloading'}<button class="secondary" disabled={cancelling.includes(task.task_id)} onclick={() => cancel(task.task_id)}>{cancelling.includes(task.task_id) ? '取消中…' : '取消下载'}</button>{/if}
</article>{/each}</section></div>
