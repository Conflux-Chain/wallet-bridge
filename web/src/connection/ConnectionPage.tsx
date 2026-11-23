import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react';
import { connectionAPI, ConnectionError, newID, type Connection, type Execution } from '../api/connections';
import type { WalletOperation } from '../api/browser';
import { WalletOperationNotice } from './WalletOperationNotice';
import { WalletDiscovery, type DiscoveredWallet } from '../wallet/discovery';
import { WalletPicker } from '../wallet/WalletPicker';
import { walletState } from '../wallet/state';

const terminal = { succeeded: '已同意连接', rejected: '已拒绝本次连接', canceled: '调用方已取消连接', expired: '连接请求已到期' };

const networkNames = new Map([
  ['0x406', 'Conflux eSpace'],
  ['0x47', 'Conflux eSpace Testnet'],
  ['0x1', 'Ethereum Mainnet'],
  ['0xaa36a7', 'Sepolia'],
  ['0xa', 'OP Mainnet'],
  ['0x38', 'BNB Smart Chain'],
  ['0x89', 'Polygon PoS'],
  ['0x2105', 'Base'],
  ['0xa4b1', 'Arbitrum One'],
]);

function targetNetwork({ chainId, chainName }: Connection) {
  const name = chainName?.trim() || networkNames.get(chainId);
  return name ? `${name} · ${chainId}` : chainId;
}

function errorMessage(error: unknown) {
  if (error instanceof ConnectionError) {
    const messages: Record<string, string> = {
      wallet_changed: '钱包状态已改变，请重新读取账户和网络。',
      execution_conflict: '执行状态已改变，请重新核对请求进度。',
      connection_conflict: '请求状态已改变，请核对最新结果。',
      connection_not_found: '找不到这个连接请求，请核对原链接。',
      unauthenticated: '浏览器授权已失效，请重新授权后打开原链接。',
    };
    return messages[error.code] ?? '服务未能完成操作，请重新检查请求。';
  }
  const code = (error as { code?: number } | null)?.code;
  if (code === 4001) return '已在钱包中拒绝。请求仍在原期限内，可重试或拒绝本次连接。';
  if (code === 4902 || code === 4200 || code === -32601) return '钱包无法自动切换，请在钱包内选择目标网络后重新读取。';
  return '暂时无法确认结果。请检查钱包和本机服务，再重新读取进度。';
}

interface ConnectionPageProps {
  id: string;
  operation?: WalletOperation;
  refreshStatus: () => Promise<void>;
}

export function ConnectionPage({ id, operation, refreshStatus }: ConnectionPageProps) {
  const [discovery] = useState(() => new WalletDiscovery(window));
  const { wallets, selectedId } = useSyncExternalStore(discovery.subscribe, discovery.getSnapshot);
  const wallet = wallets.find((item) => item.id === selectedId);
  const [request, setRequest] = useState<Connection>();
  const [execution, setExecution] = useState<Execution>();
  const [snapshot, setSnapshot] = useState<Awaited<ReturnType<typeof walletState>>>();
  const [account, setAccount] = useState('');
  const [message, setMessage] = useState('');
  const [busy, setBusy] = useState(false);
  const op = useRef(false);
  const live = useRef(false);
  const own = useRef('');
  const previousCall = useRef('');
  const revision = useRef(0);
  const generation = useRef(0);

  const refresh = useCallback(async () => {
    const stamp = ++generation.current;
    try {
      const next = await connectionAPI<Connection>(id);
      const current = await connectionAPI<Execution>(id, '/execution').catch((error) => {
        if (error instanceof ConnectionError && error.code === 'execution_not_found') return undefined;
        throw error;
      });
      if (live.current && stamp === generation.current) {
        setRequest(next);
        setExecution(current);
        if (next.state !== 'pending') own.current = '';
      }
    } catch (error) {
      if (live.current && stamp === generation.current) {
        setRequest(undefined);
        setMessage(errorMessage(error));
      }
    }
  }, [id]);

  useEffect(() => {
    live.current = true;
    void refresh();
    const poll = window.setInterval(refresh, 3500);
    const heartbeat = window.setInterval(() => {
      if (own.current) void connectionAPI<Execution>(id, '/execution/heartbeat', { id: own.current }).catch(() => void refresh());
    }, 10000);
    function leave() {
      live.current = false;
      if (own.current) void fetch('/api/v1/browser/connections/' + id + '/execution/interrupt', {
        method: 'POST', credentials: 'same-origin', mode: 'same-origin', redirect: 'error', keepalive: true,
        headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ id: own.current }),
      }).catch(() => {});
    }
    function restore(event: PageTransitionEvent) { if (event.persisted) location.reload(); }
    window.addEventListener('pagehide', leave);
    window.addEventListener('pageshow', restore);
    return () => {
      leave();
      window.clearInterval(poll);
      window.clearInterval(heartbeat);
      window.removeEventListener('pagehide', leave);
      window.removeEventListener('pageshow', restore);
    };
  }, [id, refresh]);

  useEffect(() => {
    function invalidate() {
      revision.current++;
      setSnapshot(undefined);
      setAccount('');
    }
    invalidate();
    const provider = wallet?.provider;
    const events = ['accountsChanged', 'chainChanged', 'disconnect'];
    for (const event of events) provider?.on(event, invalidate);
    return () => { for (const event of events) provider?.removeListener(event, invalidate); };
  }, [wallet?.provider]);

  async function perform(action: () => Promise<void>) {
    if (op.current) return;
    op.current = true;
    setBusy(true);
    setMessage('');
    try { await action(); }
    catch (error) {
      if (live.current) {
        if (error instanceof ConnectionError && error.code === 'wallet_busy') {
          own.current = '';
          await refreshStatus();
        } else {
          setMessage(errorMessage(error));
        }
      }
    }
    finally {
      if (live.current) await refresh();
      op.current = false;
      if (live.current) setBusy(false);
    }
  }

  async function claim() {
    if (!own.current) own.current = newID();
    const current = await connectionAPI<Execution>(id, '/execution/claim', { id: own.current });
    if (!live.current || current.phase !== 'ready') throw new ConnectionError('execution_conflict');
    previousCall.current = current.callId ?? '';
    setExecution(current);
    return current.id;
  }

  async function readWallet(selected: DiscoveredWallet) {
    const stamp = revision.current;
    const next = await walletState(selected.provider);
    if (!live.current || stamp !== revision.current) throw new ConnectionError('wallet_changed');
    setSnapshot(next);
    setAccount((value) => next.accounts.includes(value) ? value : '');
    return next;
  }

  async function invoke(selected: DiscoveredWallet, method: string) {
    const executionId = await claim();
    const callId = newID();
    const permit = await connectionAPI<{ invoke: boolean }>(id, '/execution/begin', {
      id: executionId, callId, previousCallId: previousCall.current || undefined,
    });
    if (!permit.invoke) throw new ConnectionError('execution_conflict');
    previousCall.current = callId;
    try {
      if (!live.current) return;
      await selected.provider.request(method === 'wallet_switchEthereumChain'
        ? { method, params: [{ chainId: request!.chainId }] } : { method });
    } finally {
      await connectionAPI(id, '/execution/return', { id: executionId, callId });
    }
    if (live.current) await readWallet(selected);
  }

  async function complete() {
    if (!wallet || !snapshot || !request || !account) return;
    const chosen = account;
    const stamp = revision.current;
    const executionId = await claim();
    const actual = await readWallet(wallet);
    if (!live.current || stamp !== revision.current || !actual.accounts.includes(chosen) || actual.chain !== request.chainId) {
      setMessage('账户或网络已改变，请重新核对后同意。');
      return;
    }
    const info = wallet.info;
    const result = await connectionAPI<Connection>(id, '/execution/complete', { id: executionId,
      wallet: info ? { kind: 'eip6963', name: info.name, rdns: info.rdns } : { kind: 'legacy', name: '浏览器注入钱包' },
      account: chosen, chainId: actual.chain,
    });
    own.current = '';
    setRequest(result);
  }

  async function resume() {
    if (!execution) return;
    // User confirmed the previous popup is closed; fence late results using a new ID.
    if (execution.id === own.current && execution.phase === 'calling') {
      await connectionAPI(id, '/execution/interrupt', { id: execution.id });
    }
    const replacement = newID();
    own.current = replacement; // A lost response can be reconciled by observing this ID.
    previousCall.current = '';
    setAccount('');
    setSnapshot(undefined);
    discovery.clearSelection();
    const next = await connectionAPI<Execution>(id, '/execution/recover', { id: replacement, previousId: execution.id });
    own.current = next.phase === 'ready' ? next.id : '';
    setExecution(next);
  }

  const pending = request?.state === 'pending' && Date.parse(request.expiresAt) > Date.now();
  const blocker = operation?.kind === 'connection' && operation.id === id ? undefined : operation;
  const available = !blocker && (!execution || execution.id === own.current && execution.phase === 'ready');
  const interrupted = execution?.phase === 'interrupted_ready' || execution?.phase === 'interrupted_calling';
  const canResume = interrupted || execution?.phase === 'calling' && execution.id === own.current;
  const recoveryAction = pending ? '已处理原钱包弹窗，继续连接' : '已处理原钱包弹窗，解除等待';
  return <main className="connection-page">
    <header><h1>连接钱包</h1></header>
    {request && <section aria-label="连接请求">
      <dl>
        <div><dt>调用方提供的名称</dt><dd>{request.clientName}</dd></div>
        <div><dt>目标网络</dt><dd>{targetNetwork(request)}</dd></div>
        {request.intent && <div><dt>调用方描述</dt><dd>{request.intent}</dd></div>}
        <div><dt>有效期至</dt><dd>{new Date(request.expiresAt).toLocaleTimeString()}</dd></div>
      </dl>
    </section>}
    {pending && blocker && <WalletOperationNotice operation={blocker} />}
    {pending && available && <WalletPicker discovery={discovery} disabled={busy} />}
    <section aria-label="连接操作" aria-busy={busy}>
      {!!request && !pending && <h2>{request.state === 'pending' ? '连接请求已到期' : terminal[request.state]}</h2>}
      {pending && available && <>
        {wallet && <button disabled={busy} onClick={() => void perform(async () => { await claim(); await readWallet(wallet); })}>读取账户和网络</button>}
        {wallet && snapshot && <>
          <p>钱包当前网络 <code>{snapshot.chain}</code></p>
          {snapshot.accounts.length === 0 && <button disabled={busy} onClick={() => void perform(() => invoke(wallet, 'eth_requestAccounts'))}>授权访问账户</button>}
          {snapshot.chain !== request.chainId && <button disabled={busy} onClick={() => void perform(() => invoke(wallet, 'wallet_switchEthereumChain'))}>切换到 {request.chainId}</button>}
          {snapshot.accounts.length > 0 && <label>本次连接账户
            <select value={account} disabled={busy} onChange={(e) => setAccount(e.target.value)}>
              <option value="">请选择账户</option>
              {snapshot.accounts.map((a) => <option key={a} value={a}>{a}</option>)}
            </select>
          </label>}
          <button disabled={busy || !account || snapshot.chain !== request.chainId} onClick={() => void perform(complete)}>同意连接</button>
        </>}
        <button className="secondary" disabled={busy} onClick={() => void perform(async () => {
          const executionId = await claim();
          const result = await connectionAPI<Connection>(id, '/execution/reject', { id: executionId });
          own.current = '';
          setRequest(result);
        })}>拒绝本次连接</button>
      </>}
      {request && canResume && !blocker && !busy && <button onClick={() => void perform(resume)}>
        {execution?.phase === 'interrupted_ready' ? '继续连接' : recoveryAction}
      </button>}
      {pending && !available && !blocker && <p>{execution?.phase.includes('calling') ? '正在等待钱包操作结束。' : '此请求已有连接操作，请在原页面核对进度。'}</p>}
      <p role="status">{message || (busy ? '正在处理…' : !request ? '正在读取请求…' : '')}</p>
      <button className="secondary" disabled={busy} onClick={() => { setMessage(''); void refreshStatus(); void refresh(); }}>重新检查请求</button>
    </section>
    <a href="/">浏览器授权管理</a>
  </main>;
}
