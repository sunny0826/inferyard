<script lang="ts">
  import { onMount } from 'svelte';
  import { api, mockMode } from './api';
  import { failure } from './api/client';
  import type { ApiError, DeviceResponse, SettingsResponse } from './api/types';
  import Reports from './pages/Reports.svelte';
  import Models from './pages/Models.svelte';
  import { AssetsStore } from './assets-state/store.svelte';
  const assets = new AssetsStore();
  import Runs from './pages/Runs.svelte';
  import RunDetail from './pages/RunDetail.svelte';
  import { RunsStore } from './runs/store.svelte';
  const runs = new RunsStore();
  import Device from './pages/Device.svelte';
  import Settings from './pages/Settings.svelte';
  import ErrorNotice from './components/ErrorNotice.svelte';
  const routes = ['dashboard', 'runs', 'reports', 'models', 'device', 'settings'];
  let route = $state(location.hash.slice(2) || 'dashboard');
  let device = $state<DeviceResponse | null>(null);
  let settings = $state<SettingsResponse | null>(null);
  let error = $state<ApiError | null>(null);
  let connection = $state('reconnecting'); let pending = $state(false);
  let deviceVersion = 0; let settingsVersion = 0;
  $effect(() => { void runs.select(route.startsWith('runs/') ? decodeURIComponent(route.slice(5)) : null); });
  async function rebuild() {
    error = null;
    const dv = ++deviceVersion; const sv = ++settingsVersion;
    await Promise.all([
      runs.rebuild(), assets.rebuild(),
      api.getDevice().then(value => { if (dv === deviceVersion) device = value; }).catch(cause => { error = failure(cause); }),
      api.getSettings().then(value => { if (sv === settingsVersion) settings = value; }).catch(cause => { error = failure(cause); }),
    ]);
  }
  async function refresh() {
    pending = true; error = null; const version = ++deviceVersion;
    try { await api.refreshDevice(); if (version === deviceVersion && device) device = { ...device, refreshing: true }; }
    catch (cause) { error = failure(cause); }
    finally { pending = false; }
  }
  onMount(() => {
    const hash = () => { route = location.hash.slice(2) || 'dashboard'; };
    window.addEventListener('hashchange', hash);
    const close = api.subscribe({
      deviceUpdated(event) { deviceVersion++; device = { snapshot: event.snapshot, captured_at: event.captured_at, last_error: event.error, refreshing: false }; },
      runState(event) { runs.state(event.task); }, runProgress(event) { runs.progress(event); }, runResources(event) { runs.resources(event); },
      reportCreated() { void assets.refreshReports(); },
      downloadState(event) { assets.state(event.task); }, downloadProgress(event) { assets.progress(event); },
      reset() { void rebuild(); }, connection(state) { connection = state; }, error(value) { error = value; },
    });
    void rebuild();
    return () => { close(); window.removeEventListener('hashchange', hash); };
  });
</script>
<div class="shell"><aside><a class="brand" href="#/dashboard">InferYard <span>Studio</span></a><nav aria-label="主导航">{#each routes as name}<a href={`#/${name}`} class:active={route === name || (name === 'runs' && route.startsWith('runs/'))} aria-current={route === name ? 'page' : undefined}>{name[0]?.toUpperCase()}{name.slice(1)}</a>{/each}</nav><div class="connection">{mockMode ? 'Mock · 内存数据' : '本地会话'}<br />{connection === 'connected' ? '事件已连接' : '事件连接中 / 正在重连'}</div></aside>
<main><ErrorNotice {error} />{#if error}<button class="retry" onclick={rebuild}>重新读取</button>{/if}
{#if route === 'device'}<Device {device} {refresh} {pending} />
{:else if route === 'dashboard' || route === 'runs'}<Runs store={runs} dashboard={route === 'dashboard'} />
{:else if route.startsWith('runs/')} {#key route}<RunDetail store={runs} />{/key}
{:else if route === 'reports'}<Reports store={assets} {runs} />
{:else if route === 'models'}<Models store={assets} />
{:else if route === 'settings'}<Settings data={settings} saved={value => { settingsVersion++; settings = value; }} />
{:else}<header><h1>{routes.includes(route) ? route : '页面不存在'}</h1></header><section class="empty"><p>此页面尚未实现。</p><a href="#/device">查看本机设备</a></section>{/if}
</main></div>
