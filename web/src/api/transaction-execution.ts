import { browserRequest } from './browser';

export interface ReviewProgress {
  phase: 'unclaimed' | 'ready' | 'calling' | 'interrupted_ready' | 'interrupted_calling' | 'finished';
  recoverFromId?: string;
}
type Command = {
  action: 'claim' | 'recover' | 'heartbeat' | 'begin_preparation' | 'return_preparation';
  id: string;
  previousId?: string;
  callId?: string;
};
export class ReviewCommandError extends Error {}

const path = (transaction: string) => 'browser/transactions/' + transaction + '/execution';

export async function observeReview(transaction: string): Promise<ReviewProgress> {
  const response = await browserRequest(path(transaction));
  if (!response.ok) throw new Error('无法读取审阅进度。');
  return response.json();
}

export async function changeReview(transaction: string, command: Command): Promise<ReviewProgress & { invoke: boolean }> {
  async function submit() {
    const response = await browserRequest(path(transaction), command);
    if (!response.ok) {
      if (response.status >= 500) throw new Error('服务暂时不可用。');
      throw new ReviewCommandError((await response.json()).code);
    }
    return response.json();
  }
  try { return await submit(); }
  catch (error) {
    if (error instanceof ReviewCommandError) throw error;
    // Replay only the idempotent service command, with exactly the same identifiers.
    return submit();
  }
}

export function interruptReview(transaction: string, id: string) {
  void fetch('/api/v1/' + path(transaction), {
    method: 'POST', credentials: 'same-origin', mode: 'same-origin', redirect: 'error', keepalive: true,
    headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'interrupt', id }),
  }).catch(() => {});
}
