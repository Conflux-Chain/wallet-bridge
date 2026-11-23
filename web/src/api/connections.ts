export interface Connection {
  id: string;
  clientName: string;
  intent: string;
  chainId: string;
  chainName?: string;
  state: 'pending' | 'succeeded' | 'rejected' | 'expired' | 'canceled';
  expiresAt: string;
}

export interface Execution {
  id: string;
  phase: 'ready' | 'calling' | 'interrupted_ready' | 'interrupted_calling' | 'finished';
  callId?: string;
}

export class ConnectionError extends Error {
  constructor(readonly code: string) { super(code); }
}

export async function connectionAPI<T>(id: string, suffix = '', body?: object): Promise<T> {
  const response = await fetch('/api/v1/browser/connections/' + id + suffix, {
    method: body ? 'POST' : 'GET', mode: 'same-origin', credentials: 'same-origin',
    ...(body ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) } : {}),
    redirect: 'error', cache: 'no-store', signal: AbortSignal.timeout(5000),
  });
  const result = await response.json();
  if (!response.ok) throw new ConnectionError(result.code);
  return result as T;
}

export function newID() { return crypto.randomUUID().replaceAll('-', ''); }
