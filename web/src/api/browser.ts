export const bridgeOrigin = 'http://127.0.0.1:18765';

export interface WalletOperation {
  kind: 'connection' | 'transaction';
  id: string;
  phase: 'ready' | 'calling' | 'interrupted_calling';
}

interface BrowserStatus {
  activeWalletRequest?: WalletOperation;
  startedAt: string;
  info: { version: string };
  browser: { id: string; issuedAt: string; expiresAt: string };
}

export type View =
  | { kind: 'authorized'; status: BrowserStatus }
  | { kind: 'checking' | 'unauthorized' | 'logged-out' | 'link-error' | 'exchange-uncertain' | 'logout-uncertain' | 'unavailable' | 'cookie-error' | 'preview' };

function request(path: string, body?: object) {
  return fetch(`/api/v1/${path}`, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin', mode: 'same-origin', redirect: 'error', cache: 'no-store',
    signal: AbortSignal.timeout(5000),
    ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  });
}

function timestamp(value: unknown): value is string {
  return typeof value === 'string' && Number.isFinite(Date.parse(value));
}

export async function checkStatus(): Promise<View> {
  if (location.origin !== bridgeOrigin) return { kind: 'preview' };
  try {
    const response = await request('status');
    if (response.status === 401) return { kind: 'unauthorized' };
    if (!response.ok) return { kind: 'unavailable' };
    const value = await response.json();
    if (value?.state !== 'running' || value?.info?.name !== 'wallet-bridge' || value?.info?.ready !== true ||
        value?.info?.apiVersion?.major !== 1 || !Array.isArray(value?.info?.capabilities) ||
        !value.info.capabilities.includes('browser.authorization') || typeof value.info.version !== 'string' ||
        !timestamp(value.startedAt) || !/^[0-9a-f]{32}$/.test(value?.browser?.id ?? '') ||
        !timestamp(value?.browser?.issuedAt) || !timestamp(value?.browser?.expiresAt) || value.browser.state !== 'active') {
      return { kind: 'unavailable' };
    }
    let operation = value.activeWalletRequest;
    if (operation === undefined && value.walletOperation !== undefined) {
      const legacy = value.walletOperation;
      operation = { kind: 'connection', id: legacy?.connectionId, phase: legacy?.phase };
    }
    if (operation !== undefined && (!['connection', 'transaction'].includes(operation?.kind) ||
        typeof operation?.id !== 'string' || !/^[0-9a-f]{32}$/.test(operation.id) ||
        !['ready', 'calling', 'interrupted_calling'].includes(operation.phase))) {
      return { kind: 'unavailable' };
    }
    return { kind: 'authorized', status: { ...value, activeWalletRequest: operation } };
  } catch {
    return { kind: 'unavailable' };
  }
}

// Capture and erase the secret synchronously, before any request or React effect.
// The entry point shares this one initialization promise across StrictMode mounts.
export async function initialize(): Promise<View> {
  const fragment = location.hash;
  if (fragment) {
    try {
      history.replaceState(history.state, '', location.pathname + location.search);
    } catch {
      return { kind: 'link-error' };
    }
  }
  if (location.origin !== bridgeOrigin) return { kind: 'preview' };
  if (!fragment) return checkStatus();
  const match = /^#authorize=([0-9a-f]{64})$/.exec(fragment);
  if (!match) return { kind: 'link-error' };
  try {
    const response = await request('browser/authorize', { ticket: match[1] });
    if (response.status === 401) return { kind: 'link-error' };
    if (!response.ok) return { kind: 'exchange-uncertain' };
  } catch {
    return { kind: 'exchange-uncertain' };
  }
  const view = await checkStatus();
  return view.kind === 'unauthorized' ? { kind: 'cookie-error' } : view;
}

export async function logout(): Promise<View> {
  if (location.origin !== bridgeOrigin) return { kind: 'preview' };
  try {
    const response = await request('browser/logout', {});
    if (response.status === 401) return { kind: 'unauthorized' };
    if (response.ok && (await response.json())?.state === 'revoked') return { kind: 'logged-out' };
  } catch {
    // A lost response cannot tell us whether revocation committed. Never replay.
  }
  return { kind: 'logout-uncertain' };
}
