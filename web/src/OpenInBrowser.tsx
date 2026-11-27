import { useRef, useState } from 'react';
import { bridgeOrigin, browserRequest } from './api/browser';

export function OpenInBrowser() {
  const [state, setState] = useState<'idle' | 'opening' | 'opened' | 'error'>('idle');
  const inFlight = useRef(false);
  const params = new URLSearchParams(location.search);
  const [kind, id] = params.entries().next().value ?? [];
  if (location.origin !== bridgeOrigin || params.size !== 1 ||
      (kind !== 'connection' && kind !== 'transaction') || !id || !/^[0-9a-f]{32}$/.test(id)) return null;

  async function open() {
    if (inFlight.current) return;
    inFlight.current = true;
    setState('opening');
    try {
      const response = await browserRequest('browser/open-request', { kind, id });
      setState(response.status === 204 ? 'opened' : 'error');
    } catch {
      setState('error');
    } finally {
      inFlight.current = false;
    }
  }

  return <div>
    <button type="button" className="secondary" disabled={state === 'opening'} onClick={() => void open()}>
      在系统默认浏览器继续
    </button>
    <div aria-live="polite">
      {state === 'opened' && <p>已请求系统打开原审批链接。请在装有钱包的浏览器中继续。</p>}
      {state === 'error' && <p>暂时无法确认浏览器是否打开。你也可以把原审批链接复制到装有钱包的浏览器中。</p>}
    </div>
  </div>;
}
