import { useEffect, useRef, useState, useSyncExternalStore } from 'react';
import type { TransactionReview } from '../api/transactions';
import { WalletDiscovery, type DiscoveredWallet } from '../wallet/discovery';
import { networks } from '../wallet/networks';
import { walletState } from '../wallet/state';
import type { ReviewExecution } from './useReviewExecution';

function matchingWallets(discovery: WalletDiscovery, binding: TransactionReview['wallet']) {
  const { wallets, injectedProvider } = discovery.getSnapshot();
  if (binding.kind === 'legacy') return wallets.filter(wallet => !wallet.info && wallet.provider === injectedProvider);
  return wallets.filter(wallet => wallet.info && wallet.info.name === binding.name && wallet.info.rdns === binding.rdns);
}

export function TransactionWallet({ review, execution, canSend }: { review: TransactionReview; execution?: ReviewExecution; canSend: boolean }) {
  const [discovery] = useState(() => new WalletDiscovery(window));
  useSyncExternalStore(discovery.subscribe, discovery.getSnapshot);
  useEffect(() => {
    discovery.start();
    return discovery.stop;
  }, [discovery]);
  const matches = matchingWallets(discovery, review.wallet);
  return <section className="wallet-check" aria-labelledby="wallet-check">
    <h2 id="wallet-check">钱包状态核对</h2>
    {matches.length === 1
      ? <WalletReadings key={matches[0].id} wallet={matches[0]} review={review} discovery={discovery} execution={execution} canSend={canSend} />
      : <>
        <p>{matches.length > 1 ? '多个钱包使用相同的名称和 rdns。请在只启用目标钱包的浏览器配置中重新打开原交易。'
          : '未发现 Session 固定的钱包。请检查浏览器配置和扩展是否已启用。'}</p>
        <button type="button" className="secondary" onClick={discovery.refresh}>重新检测钱包</button>
      </>}
  </section>;
}

function networkLabel(chain: string) {
  return [networks.get(chain)?.name, chain].filter(Boolean).join(' · ');
}

function WalletReadings({ wallet, review, discovery, execution, canSend }: {
  wallet: DiscoveredWallet; review: TransactionReview; discovery: WalletDiscovery; execution?: ReviewExecution; canSend: boolean;
}) {
  const mounted = useRef(false);
  const generation = useRef(0);
  useEffect(() => { mounted.current = true; return () => { mounted.current = false; }; }, []);
  const [confirmed, setConfirmed] = useState(!!wallet.info);
  const [attempt, setAttempt] = useState(0);
  const [reading, setReading] = useState<Awaited<ReturnType<typeof walletState>>>();
  const [message, setMessage] = useState('正在读取钱包状态…');
  const { kind, name, rdns } = review.wallet;
  useEffect(() => {
    if (!confirmed) return;
    let revision = 0;
    let active = true;
    const { provider } = wallet;
    const current = () => {
      const matches = matchingWallets(discovery, { kind, name, rdns });
      return matches.length === 1 && matches[0] === wallet;
    };
    async function refresh() {
      if (!active) return;
      generation.current++;
      const version = ++revision;
      setReading(undefined);
      setMessage('正在读取钱包状态…');
      if (!current()) return;
      try {
        const next = await walletState(provider);
        if (version === revision && current()) setReading(next);
      } catch {
        if (version === revision) setMessage('无法读取钱包状态，请检查钱包后重新核对。');
      }
    }
    function disconnect() {
      if (!active) return;
      generation.current++;
      ++revision;
      setReading(undefined);
      setMessage('钱包已断开，请恢复连接后重新核对。');
    }
    const listeners: [string, () => void][] = [
      ['accountsChanged', refresh], ['chainChanged', refresh], ['connect', refresh], ['disconnect', disconnect],
    ];
    const subscribed: typeof listeners = [];
    try {
      for (const listener of listeners) {
        provider.on(...listener);
        subscribed.push(listener);
      }
      window.addEventListener('focus', refresh);
      void refresh();
    } catch {
      active = false;
      ++revision;
      setReading(undefined);
      setMessage('无法订阅钱包状态，请检查钱包后重新核对。');
    }
    return () => {
      active = false;
      ++revision;
      window.removeEventListener('focus', refresh);
      for (const listener of subscribed) {
        try { provider.removeListener(...listener); } catch { /* The extension may have disconnected. */ }
      }
    };
  }, [confirmed, attempt, wallet, discovery, kind, name, rdns]);

  function check() {
    discovery.refresh();
    setConfirmed(true);
    setAttempt(n => n + 1);
  }
  if (!confirmed) return <>
    <p>请核实当前浏览器注入的是建立 Session 时使用的钱包。</p>
    <button type="button" className="secondary" onClick={check}>使用当前注入钱包</button>
  </>;
  const tx = review.transaction;
  function isFixedWallet() {
    discovery.refresh();
    const matches = matchingWallets(discovery, review.wallet);
    return mounted.current && matches.length === 1 && matches[0] === wallet;
  }
  async function checkForSend() {
    if (!isFixedWallet()) return;
    const version = generation.current;
    try {
      const next = await walletState(wallet.provider);
      const stillCurrent = () => isFixedWallet() && version === generation.current;
      if (!stillCurrent()) return;
      setReading(next);
      if (next.accounts.includes(tx.from.toLowerCase()) && next.chain === tx.chainId) return stillCurrent;
    } catch {
      if (mounted.current) { setReading(undefined); setMessage('无法读取钱包状态，请重新核对。'); }
    }
  }
  function prepare(method: 'eth_requestAccounts' | 'wallet_switchEthereumChain') {
    void execution?.prepare(async () => {
      if (!isFixedWallet()) return '钱包已改变，请重新核对。';
      try {
        await wallet.provider.request(method === 'eth_requestAccounts' ? { method } : { method, params: [{ chainId: tx.chainId }] });
        return '钱包操作已返回，请核对当前账户和网络。';
      } catch (error) {
        if ((error as { code?: number } | null)?.code === 4001)
          return '已在钱包中拒绝此次操作。交易仍可在原期限内重试或拒绝。';
        return method === 'wallet_switchEthereumChain' ? '切链未完成，请在钱包中选择目标网络后重新核对。' : '授权未完成，请检查钱包的站点授权后重新核对。';
      } finally {
        if (mounted.current) setAttempt(n => n + 1);
      }
    });
  }
  return <>
    {reading ? <dl>
      <div><dt>交易要求网络</dt><dd>{networkLabel(tx.chainId)}</dd></div>
      <div><dt>钱包当前网络</dt><dd>{networkLabel(reading.chain)}
        <small>{reading.chain === tx.chainId ? '网络匹配' : '网络不匹配，请在钱包中切换到交易要求的网络。'}</small></dd></div>
      <div><dt>固定发送账户</dt><dd><code>{tx.from}</code></dd></div>
      <div><dt>钱包已授权账户</dt><dd>{reading.accounts.length ? reading.accounts.map(account =>
        <div key={account}><code>{account}</code></div>) : '尚未提供账户'}
        <small>{reading.accounts.includes(tx.from.toLowerCase()) ? '固定发送账户可用' : '请在钱包中切换或授权固定发送账户。'}</small></dd></div>
    </dl> : <p aria-live="polite">{message}</p>}
    {execution && reading && (!reading.accounts.length || reading.chain !== tx.chainId) && <div className="actions">
      {reading.accounts.length === 0 && <button type="button" disabled={!execution.available} onClick={() => prepare('eth_requestAccounts')}>授权访问账户</button>}
      {reading.chain !== tx.chainId && <button type="button" disabled={!execution.available} onClick={() => prepare('wallet_switchEthereumChain')}>切换到 {networkLabel(tx.chainId)}</button>}
    </div>}
    <div className="actions"><button type="button" className="secondary" onClick={check}>重新核对钱包</button>
    {canSend && <button type="button" disabled={!execution?.available || reading?.chain !== tx.chainId ||
      !reading.accounts.includes(tx.from.toLowerCase())}
      onClick={() => void execution?.send(review, checkForSend,
        payload => wallet.provider.request({ method: 'eth_sendTransaction', params: [payload] }))}>在钱包中确认并发送</button>}</div>
  </>;
}
