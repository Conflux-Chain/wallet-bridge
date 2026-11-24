import { useCallback, useEffect, useRef, useState } from 'react';
import { checkStatus, logout } from './api/browser';
import type { View } from './api/browser';
import { WalletOperationNotice } from './connection/WalletOperationNotice';
import { ConnectionPage } from './connection/ConnectionPage';
import { WalletPicker } from './wallet/WalletPicker';
import { TransactionPage } from './transaction/TransactionPage';
import { PendingRequests } from './PendingRequests';

const messages = {
  checking: ['正在检查浏览器授权', '正在连接本机 Wallet Bridge。'],
  unauthorized: ['需要浏览器授权', '当前浏览器尚未授权，或授权已到期、已被撤销。请在本机终端运行以下命令。'],
  'logged-out': ['已退出当前浏览器', '当前授权已撤销。需要再次访问时，请在本机终端重新授权。'],
  'link-error': ['授权链接已失效', '链接格式不正确、已过期或已被使用。请重新运行以下命令获取新链接。'],
  'exchange-uncertain': ['暂时无法确认授权结果', '请先重新检查状态；若仍未授权，请获取新链接。此页面不会重试旧链接。'],
  'logout-uncertain': ['暂时无法确认退出结果', '请重新检查状态，确认当前授权是否已撤销。'],
  unavailable: ['暂时无法确认服务状态', '请确认本机服务正在运行，然后重新检查。需要启动服务时运行 wallet-bridge service ensure。'],
  'cookie-error': ['无法保存浏览器授权', '请确认当前浏览器允许本机网站保存 cookie，然后重新获取授权链接。'],
  preview: ['开发预览', '当前地址仅用于预览页面。完整授权流程请运行 wallet-bridge ui，并访问本地服务提供的页面。'],
} satisfies Record<Exclude<View['kind'], 'authorized'>, [string, string]>;

function date(value: string) { return new Date(value).toLocaleString(); }

export function App({ initial }: { initial: Promise<View> }) {
  const [view, setView] = useState<View>({ kind: 'checking' });
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);
  const mounted = useRef(true);
  const authorizationLost = useCallback(() => setView({ kind: 'unauthorized' }), []);

  useEffect(() => {
    let active = true;
    mounted.current = true;
    void initial.then((result) => { if (active) setView(result); });
    return () => { active = false; mounted.current = false; };
  }, [initial]);

  const perform = useCallback(async (operation: () => Promise<View>) => {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    try {
      const result = await operation();
      if (mounted.current) setView(result);
    } finally {
      inFlight.current = false;
      if (mounted.current) setBusy(false);
    }
  }, []);

  const expiresAt = view.kind === 'authorized' ? view.status.browser.expiresAt : undefined;
  useEffect(() => {
    if (!expiresAt) return;
    const refresh = () => { if (document.visibilityState === 'visible') void perform(checkStatus); };
    const interval = window.setInterval(refresh, 10_000);
    const expiration = window.setTimeout(refresh, Math.min(2_147_483_647, Math.max(0, Date.parse(expiresAt) - Date.now())));
    window.addEventListener('focus', refresh);
    document.addEventListener('visibilitychange', refresh);
    return () => {
      window.clearInterval(interval);
      window.clearTimeout(expiration);
      window.removeEventListener('focus', refresh);
      document.removeEventListener('visibilitychange', refresh);
    };
  }, [expiresAt, perform]);

  const authorized = view.kind === 'authorized';
  const [title, description] = authorized
    ? ['浏览器已授权', '此浏览器可以访问本机 Wallet Bridge。钱包操作仍需由你确认。']
    : messages[view.kind];
  const showCommand = ['unauthorized', 'logged-out', 'link-error', 'exchange-uncertain', 'cookie-error'].includes(view.kind);

  const params = new URLSearchParams(location.search);
  const connection = params.get('connection');
  const transactions = params.getAll('transaction');
  if (authorized && transactions.length) {
    const id = transactions[0];
    if (connection !== null || transactions.length !== 1 || !/^[0-9a-f]{32}$/.test(id)) {
      return <main><h1>交易链接无效</h1><p>请从调用方重新打开交易请求链接。</p></main>;
    }
    if (!view.status.info.capabilities.includes('browser.transactions.read')) {
      return <main><h1>当前服务不支持交易审阅</h1><p>请使用支持交易读取的 Wallet Bridge 服务打开原链接。</p></main>;
    }
    return <TransactionPage key={id} id={id} grant={view.status.browser.id} canReject={view.status.info.capabilities.includes('browser.transactions.decisions')}
      canCoordinate={view.status.info.capabilities.includes('browser.transactions.coordination')} operation={view.status.activeWalletRequest} />;
  }
  if (authorized && connection !== null) {
    return /^[0-9a-f]{32}$/.test(connection) ? <ConnectionPage key={connection} id={connection} operation={view.status.activeWalletRequest} refreshStatus={() => perform(checkStatus)} />
      : <main><h1>连接链接无效</h1><p>请从调用方重新打开请求链接。</p></main>;
  }
  return (
    <main>
      <header>
        <p className="eyebrow"><span className="brand-mark" aria-hidden="true">W</span> 本地钱包桥接</p>
        <h1>Wallet Bridge</h1>
        <p className="intro">连接你的工具，保留你的确认。</p>
      </header>
      {authorized && view.status.activeWalletRequest && <WalletOperationNotice operation={view.status.activeWalletRequest} />}
      {authorized && view.status.info.capabilities.includes('browser.pending-requests') &&
        <PendingRequests key={view.status.browser.id} status={view.status} onUnauthorized={authorizationLost} />}
      <section aria-labelledby="authorization-title" aria-busy={busy || view.kind === 'checking'}>
        <div className="state-heading">
          <span className={`state-dot ${authorized ? 'active' : ''}`} aria-hidden="true" />
          <h2 id="authorization-title" role="status" aria-live="polite">{title}</h2>
        </div>
        <p className="description">{description}</p>
        {showCommand && <pre className="command"><code>wallet-bridge ui</code></pre>}
        {showCommand && transactions.length === 1 && <p>请保留原交易标签页，在同一浏览器配置的新标签页完成授权，再回到此处点击「重新检查」。</p>}
        {view.kind === 'authorized' && (
          <dl>
            <div><dt>本地服务</dt><dd><span className="running">运行中</span></dd></div>
            <div><dt>服务版本</dt><dd>{view.status.info.version}</dd></div>
            <div><dt>服务启动时间</dt><dd>{date(view.status.startedAt)}</dd></div>
            <div><dt>授权有效期至</dt><dd>{date(view.status.browser.expiresAt)}</dd></div>
            <div className="grant-id"><dt>当前授权 ID</dt><dd><code>{view.status.browser.id}</code></dd></div>
          </dl>
        )}
        {view.kind !== 'preview' && (
          <div className="actions">
            <button type="button" disabled={busy || view.kind === 'checking'} onClick={() => { void perform(checkStatus); }}>
              {busy ? '正在检查…' : '重新检查'}
            </button>
            {authorized && <button className="secondary" type="button" disabled={busy} onClick={() => { void perform(logout); }}>退出当前浏览器</button>}
          </div>
        )}
      </section>
      {(authorized || view.kind === 'preview') && <WalletPicker />}
      <footer>
        <p>私钥由浏览器钱包保管。</p>
        <p>打开调用方提供的连接链接可核对请求并连接钱包。</p>
      </footer>
    </main>
  );
}
