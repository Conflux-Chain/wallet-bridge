import { useEffect, useState } from 'react';
import { postReviewCommand } from '../api/transaction-execution';

type Outcome = { txHash: string } | { errorCode: number };
type Report = { id: string; callId: string } & Outcome;
const pending = new Map<string, Report>();
const changes = new EventTarget();
const storageKey = (request: string) => 'wallet-bridge.result.' + request;

export function retainReport(request: string, id: string, callId: string, outcome: Outcome) {
  const report = { id, callId, ...outcome };
  pending.set(request, report);
  try { sessionStorage.setItem(storageKey(request), JSON.stringify(report)); } catch { /* Keep the in-memory copy. */ }
  changes.dispatchEvent(new Event(request));
}

function pendingReport(request: string): Report | undefined {
  if (pending.has(request)) return pending.get(request);
  try { return JSON.parse(sessionStorage.getItem(storageKey(request)) ?? 'null') ?? undefined; }
  catch { return undefined; }
}

export function WalletReport({ request, grant, refresh }: { request: string; grant: string; refresh: () => void }) {
  const [message, setMessage] = useState('');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let active = true;
    async function deliver() {
      const report = pendingReport(request);
      if (!report) return;
      setMessage('');
      try {
        await postReviewCommand('browser/transactions/' + request + '/result', report);
        pending.delete(request);
        try { sessionStorage.removeItem(storageKey(request)); } catch { /* Service has the result. */ }
        if (active) refresh();
      } catch {
        if (active) setMessage('钱包结果待补交。请保留此标签页，检查本机服务与浏览器授权后重试。');
      }
    }
    changes.addEventListener(request, deliver);
    void deliver();
    return () => { active = false; changes.removeEventListener(request, deliver); };
  }, [request, grant, retry, refresh]);
  return message && <section aria-label="钱包结果补交"><p>{message}</p>
    <button type="button" onClick={() => setRetry(n => n + 1)}>补交钱包结果</button>
  </section>;
}
