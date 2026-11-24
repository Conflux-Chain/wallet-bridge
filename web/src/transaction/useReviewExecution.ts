import { useEffect, useRef, useState } from 'react';
import { newID } from '../api/connections';
import { changeReview, interruptReview, observeReview, ReviewCommandError, type ReviewProgress } from '../api/transaction-execution';

interface Owner { id: string; lastCall?: string }
interface Operation { recovery: boolean }

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

  async function run(work: (operation: Operation) => Promise<string>, recovery = false) {
    if (!live.current || (working.current && (!recovery || working.current.recovery))) return;
    const operation = { recovery };
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
    }, true);
  }
  return { progress, owned: !!owner.current, busy, recovering: !!working.current?.recovery, recoveredId, available, message, prepare, recover };
}

export type ReviewExecution = ReturnType<typeof useReviewExecution>;
