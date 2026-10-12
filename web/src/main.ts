import './style.css';

const root = document.querySelector<HTMLDivElement>('#app');
if (!root) throw new Error('Missing app root');
root.innerHTML = `
  <main>
    <p class="eyebrow">INFERYARD STUDIO / M0</p>
    <h1>本地连接，实时可见。</h1>
    <p class="intro">桌面与浏览器，共享同一个计数。</p>
    <section aria-label="实时计数">
      <p id="status" role="status">正在连接…</p>
      <output id="count" aria-live="polite">—</output>
      <p>每 2 秒更新 · Server-Sent Events</p>
    </section>
    <footer>127.0.0.1 · 本机会话 · 内嵌前端</footer>
  </main>`;
const count = document.querySelector<HTMLOutputElement>('#count');
const status = document.querySelector<HTMLParagraphElement>('#status');
if (!count || !status) throw new Error('Missing counter elements');
const token = new URL(location.href).searchParams.get('token');
if (!token) {
  status.textContent = '缺少会话 token，请使用启动日志中的完整地址。';
} else {
  const stream = new EventSource(`/api/events?token=${encodeURIComponent(token)}`);
  stream.onopen = () => { status.textContent = '● 已连接'; };
  stream.onerror = () => { status.textContent = '连接中断，正在重连…'; };
  stream.addEventListener('count', (event: MessageEvent<string>) => {
    const value = Number(event.data);
    if (!Number.isSafeInteger(value) || value < 0) {
      status.textContent = '收到无效计数';
      return;
    }
    count.textContent = String(value);
  });
  window.addEventListener('pagehide', () => stream.close(), { once: true });
}
