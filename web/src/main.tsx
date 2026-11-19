import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import './style.css';

function App() {
  return (
    <main>
      <header>
        <p className="eyebrow">本地钱包桥接</p>
        <h1>Wallet Bridge</h1>
        <p>在浏览器钱包中确认来自 CLI 与 AI 工具的钱包请求。</p>
      </header>
      <section aria-labelledby="approval-title">
        <h2 id="approval-title">由你确认的钱包操作</h2>
        <p>连接账户、核对网络、检查交易内容，再由你确认签名与发送。</p>
      </section>
      <footer>私钥由浏览器钱包保管。</footer>
    </main>
  );
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
