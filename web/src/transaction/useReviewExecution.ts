import { useEffect, useRef, useState } from 'react';
import { newID } from '../api/connections';
import { changeReview, interruptReview, observeReview, postReviewCommand, ReviewCommandError, type ReviewProgress } from '../api/transaction-execution';
import type { TransactionReview } from '../api/transactions';
import { retainReport } from './WalletReport';

interface Owner { id: string; lastCall?: string }
type Operation = { kind: 'prepare' | 'recover' | 'send' };

export function useReviewExecution(transaction: string, watch: 'pending' | 'terminal' | undefined) {
  const owner = useRef<Owner | undefined>(undefined);
  const live = useRef(false);
  const working = useRef<Operation | undefined>(undefined);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [progress, setProgress] = useState<ReviewProgress>();
  const [revision, setRevision] = useState(0);
  const [recoveredId, setRecoveredId] = useState('');
  useEffect(() => {
    live.current = true;
    function leave() {
      live.current = false;
      if (owner.current) interruptReview(transaction, owner.current.id);
      owner.current = undefined;
    }
    function restore(event: PageTransitionEvent) { if (event.persisted) location.reload(); }
    window.addEventListener('pagehide', leave);
    window.addEventListener('pageshow', restore);
    const timer = window.setInterval(async () => {
      const current = owner.current;
      if (!current) return;
      try { await changeReview(transaction, { action: 'heartbeat', id: current.id }); }
      catch (error) {
        if (current === owner.current && error instanceof ReviewCommandError) {
          owner.current = undefined;
          setRevision(n => n + 1);
        }
      }
    }, 10_000);
    return () => {
      leave();
      window.clearInterval(timer);
      window.removeEventListener('pagehide', leave);
      window.removeEventListener('pageshow', restore);
    };
  }, [transaction]);

  useEffect(() => {
    if (!watch) { setProgress(undefined); return; }
    let active = true;
    let timer: number;
    async function poll() {
      const next = await observeReview(transaction).catch(() => undefined);
      if (!active) return;
      setProgress(next);
      if (watch === 'pending' || !next || next.phase === 'calling' || next.phase === 'interrupted_calling') {
        timer = window.setTimeout(poll, 2500);
      }
    }
    void poll();
    return () => { active = false; window.clearTimeout(timer); };
  }, [transaction, watch, revision]);

  async function run(work: (operation: Operation) => Promise<string>, kind: Operation['kind'] = 'prepare') {
    if (!live.current || (working.current && (kind !== 'recover' || working.current.kind === 'recover'))) return;
    const operation = { kind };
    working.current = operation;
    setBusy(true);
    setMessage('');
    try {
      const result = await work(operation);
      if (live.current && working.current === operation) setMessage(result);
    } catch (error) {
      if (working.current !== operation) return;
      if (owner.current) interruptReview(transaction, owner.current.id);
      owner.current = undefined;
      if (live.current) {
        setMessage(error instanceof ReviewCommandError && error.message === 'wallet_busy'
          ? '钱包正在处理其他请求，请稍后重试。' : '审阅状态已改变，请核对原交易的最新进度。');
      }
    } finally {
      if (working.current === operation) {
        working.current = undefined;
        if (live.current) { setBusy(false); setRevision(n => n + 1); }
      }
    }
  }

  async function acquire(operation: Operation, previousId?: string) {
    const lease = previousId ? { id: newID() } : owner.current ?? { id: newID() };
    const result = await changeReview(transaction, { action: previousId ? 'recover' : 'claim', id: lease.id, previousId })
      .catch(error => { if (!(error instanceof ReviewCommandError)) interruptReview(transaction, lease.id); throw error; });
    if (!live.current || working.current !== operation) {
      interruptReview(transaction, lease.id);
      throw new Error('Review interrupted');
    }
    setProgress(result);
    owner.current = result.phase === 'ready' ? lease : undefined;
    return owner.current;
  }

  const available = !busy && watch === 'pending' &&
    (progress?.phase === 'unclaimed' || (progress?.phase === 'ready' && !!owner.current));

  function prepare(walletCall: () => Promise<string>) {
    if (!available) return;
    return run(async operation => {
      const lease = await acquire(operation);
      if (!lease) throw new Error('Review unavailable');
      const callId = newID();
      const permit = await changeReview(transaction, { action: 'begin_preparation', id: lease.id, previousId: lease.lastCall, callId });
      lease.lastCall = callId;
      try {
        if (!live.current || working.current !== operation || owner.current !== lease || !permit.invoke || permit.phase !== 'calling') {
          return '钱包操作未执行，请重新核对后重试。';
        }
        return await walletCall();
      } finally {
        // The original popup can settle after cancellation, navigation or recovery.
        await changeReview(transaction, { action: 'return_preparation', id: lease.id, callId });
      }
    });
  }

  function recover() {
    const previous = progress?.recoverFromId;
    if (previous) return run(async operation => {
      const lease = await acquire(operation, previous);
      if (lease) setRecoveredId(lease.id);
      return lease ? '已恢复审阅，请重新核对钱包。' : '已解除原钱包操作的占用。';
    }, 'recover');
  }

  function acknowledge() {
    return run(async () => {
      await postReviewCommand('browser/transactions/' + transaction + '/acknowledge', {});
      return '已解除原发送弹窗的占用。';
    }, 'recover');
  }

  function send(review: TransactionReview, checkWallet: () => Promise<(() => boolean) | undefined>,
    invoke: (payload: TransactionReview['transaction']) => Promise<unknown>) {
    if (!available) return;
    return run(async operation => {
      const lease = await acquire(operation);
      if (!lease) throw new Error('Review unavailable');
      const ownsReview = () => live.current && working.current === operation && owner.current === lease;
      if (!await checkWallet() || !ownsReview()) return '钱包状态已改变，请重新核对后发送。';
      const callId = newID();
      const { from: account, chainId } = review.transaction;
      const permit = await postReviewCommand<{ invoke: boolean; transaction?: TransactionReview['transaction'] }>(
        'browser/transactions/' + transaction + '/send',
        { id: lease.id, previousCallId: lease.lastCall, callId, wallet: review.wallet, account, chainId });
      let result: { txHash: string } | { errorCode: number } = { errorCode: 0 };
      if (permit.invoke === true && permit.transaction && ownsReview()) {
        const stillCurrent = await checkWallet();
        const confirmed = stillCurrent && await changeReview(transaction, { action: 'heartbeat', id: lease.id }).catch(() => undefined);
        if (confirmed?.phase === 'calling' && stillCurrent?.() && ownsReview()) {
          try {
            const hash = await invoke(permit.transaction);
            if (typeof hash === 'string' && /^0x[0-9a-fA-F]{64}$/.test(hash)) result = { txHash: hash };
          } catch (error) {
            const code = (error as { code?: number } | null)?.code;
            if (typeof code === 'number' && Number.isSafeInteger(code)) result = { errorCode: code };
          }
        }
      }
      retainReport(transaction, lease.id, callId, result);
      if (owner.current === lease) owner.current = undefined;
      return '';
    }, 'send');
  }
  return { progress, owned: !!owner.current, busy, recovering: working.current?.kind === 'recover',
    sending: working.current?.kind === 'send', recoveredId, available, message, prepare, recover, send, acknowledge };
}

export type ReviewExecution = ReturnType<typeof useReviewExecution>;
