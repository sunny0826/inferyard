<script lang="ts">
  import { onMount } from 'svelte';
  import uPlot from 'uplot';
  import 'uplot/dist/uPlot.min.css';
  import type { ResourceSample } from '../api/types';
  import { chartData } from '../runs/model';
  let { points }: { points: ResourceSample[] } = $props();
  let host: HTMLDivElement;
  let chart: uPlot | undefined;
  let ready = $state(false);
  onMount(() => {
    chart = new uPlot({ width: Math.max(host.clientWidth, 200), height: 290,
      scales: { x: { time: true }, memory: { auto: true }, cpu: { auto: true }, temperature: { auto: true } },
      series: [{}, { label: '可用内存 GiB', scale: 'memory', stroke: '#8bbfff', spanGaps: false }, { label: '服务 RSS GiB', scale: 'memory', stroke: '#b8a2ff', spanGaps: false }, { label: '服务 CPU %', scale: 'cpu', stroke: '#f0c479', spanGaps: false }, { label: '温度 °C', scale: 'temperature', stroke: '#ed96a3', spanGaps: false }],
      axes: [{ stroke: '#9eafc3' }, { scale: 'memory', label: 'GiB', stroke: '#8bbfff', size: 48 }, { scale: 'cpu', label: '%', side: 1, stroke: '#f0c479', size: 38 }, { scale: 'temperature', show: false }],
    }, chartData(points), host);
    ready = true;
    let frame = 0;
    const observer = new ResizeObserver(() => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const width = Math.max(host.clientWidth, 200);
        if (chart && chart.width !== width) chart.setSize({ width, height: 290 });
      });
    });
    observer.observe(host);
    return () => { observer.disconnect(); cancelAnimationFrame(frame); chart?.destroy(); chart = undefined; };
  });
  $effect(() => { const data = chartData(points); if (ready) chart?.setData(data); });
</script>
<section class="resource-chart" aria-label="资源曲线">
  <h2>资源曲线</h2>
  <p>内存由 bytes 换算为 GiB（1 GiB = 1,073,741,824 bytes）。CPU 为服务端 ticks 差分百分比；温度为 °C，独立刻度，通过图例读取。缺测保留断点。</p>
  <div class="plot" bind:this={host}></div>
  <small data-testid="sample-count">采样点：{points.length}</small>
  {#if points.length === 0}<p>等待资源采样，缺测不补零。</p>{/if}
  <details><summary>采样数值</summary><div class="table-scroll"><table><thead><tr><th>UTC</th><th>可用内存 bytes</th><th>RSS bytes</th><th>CPU %</th><th>温度 °C</th></tr></thead><tbody>{#each points as point}<tr><td>{point.utc}</td><td>{point.mem_available_bytes ?? '缺测'}</td><td>{point.service_rss_bytes ?? '缺测'}</td><td>{point.service_cpu_percent ?? '缺测'}</td><td>{point.temperature_celsius ?? '缺测'}</td></tr>{/each}</tbody></table></div></details>
</section>
