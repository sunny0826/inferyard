<script lang="ts">
  import { api } from '../api';
  import { failure } from '../api/client';
  import type { ApiError, SettingsResponse, UpdateSettingsRequest } from '../api/types';
  import ErrorNotice from '../components/ErrorNotice.svelte';
  import Fields from '../components/Fields.svelte';
  let { data, saved }: { data: SettingsResponse | null; saved: (value: SettingsResponse) => void } = $props();
  let inferyard = $state(''); let results = $state(''); let models = $state('');
  let saving = $state(false); let message = $state(''); let error = $state<ApiError | null>(null);
  $effect(() => { if (data) { inferyard = data.settings.inferyard_path ?? ''; results = data.settings.results_root ?? ''; models = data.settings.models_root ?? ''; } });
  async function save(event: SubmitEvent) {
    event.preventDefault(); if (!data || saving) return;
    const patch: UpdateSettingsRequest = {};
    if (inferyard !== (data.settings.inferyard_path ?? '')) patch.inferyard_path = inferyard || null;
    if (results !== (data.settings.results_root ?? '')) patch.results_root = results;
    if (models !== (data.settings.models_root ?? '')) patch.models_root = models;
    saving = true; message = ''; error = null;
    try { saved(await api.updateSettings(patch)); message = '设置已保存'; }
    catch (cause) { error = failure(cause); }
    finally { saving = false; }
  }
</script>
<header><div><h1>Settings</h1><p>配置 inferyard 与本地目录</p></div></header>
<ErrorNotice {error} />
{#if data}
  <div class="grid"><section><h2>路径设置</h2><form onsubmit={save}>
    <fieldset disabled={saving}><label for="inferyard">inferyard_path</label><input id="inferyard" bind:value={inferyard} placeholder="留空则自动探测 PATH" /><small>二进制绝对路径；留空使用 PATH。</small>
    <label for="results">results_root</label><input id="results" bind:value={results} placeholder="结果根目录的绝对路径" />
    <label for="models">models_root</label><input id="models" bind:value={models} placeholder="模型根目录的绝对路径" />
    <button type="submit">{saving ? '保存中…' : '保存设置'}</button></fieldset>
    <p role="status">{message}</p>
  </form></section><section><h2>生效值与探测状态</h2><Fields values={{ ...data.effective }} /></section></div>
{:else}<p role="status">正在读取设置…</p>{/if}
