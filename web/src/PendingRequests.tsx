import { useEffect, useState } from 'react';
import { browserRequest, type BrowserStatus } from './api/browser';
import { networks } from './wallet/networks';

interface PendingRequest {
  kind: 'connection' | 'transaction';
  id: string;
  clientName: string;
  chainId: string;
  createdAt: string;
  expiresAt: string;
}

function validRequest(row: Partial<PendingRequest> | null): row is PendingRequest {
  return (row?.kind === 'connection' || row?.kind === 'transaction') &&
    typeof row.id === 'string' && /^[0-9a-f]{32}$/.test(row.id) &&
    typeof row.clientName === 'string' && typeof row.chainId === 'string' &&
    typeof row.createdAt === 'string' && Number.isFinite(Date.parse(row.createdAt)) &&
    typeof row.expiresAt === 'string' && Number.isFinite(Date.parse(row.expiresAt));
}

export function PendingRequests({ status, onUnauthorized }: { status: BrowserStatus; onUnauthorized: () => void }) {
  const [requests, setRequests] = useState<PendingRequest[] | 'loading' | 'error'>('loading');
  useEffect(() => {
    let current = true;
    async function refresh() {
      try {
        const response = await browserRequest('browser/pending-requests');
        if (response.status === 401) {
          if (current) onUnauthorized();
          return;
        }
        if (!response.ok) throw new Error();
        const { requests: rows } = await response.json();
        if (!Array.isArray(rows) || !rows.every(validRequest)) throw new Error();
        if (current) setRequests(rows);
      } catch {
        if (current) setRequests('error');
      }
    }
    void refresh();
    return () => { current = false; };
  }, [status, onUnauthorized]);

  return <section className="pending-requests" aria-labelledby="pending-title">
    <h2 id="pending-title">待审批请求</h2>
    {requests === 'loading' && <p>正在读取待审批请求…</p>}
    {requests === 'error' && <p>暂时无法读取列表。请点击「重新检查」重试。</p>}
    {Array.isArray(requests) && (requests.length === 0 ? <p>当前没有待审批请求。</p> : <ol>
      {requests.map(row => {
        const network = networks.get(row.chainId);
        const kind = row.kind === 'connection' ? '连接' : '交易';
        return <li key={row.kind + row.id}>
          <a href={`/?${row.kind}=${row.id}`}>审阅{kind} · {row.clientName}</a>
          <span>{network && network.name + ' · '}{row.chainId}</span>
          <span>请求 ID <code>{row.id}</code></span>
          <span>创建于 <time dateTime={row.createdAt}>{new Date(row.createdAt).toLocaleTimeString()}</time>
            {' · 到期于 '}<time dateTime={row.expiresAt}>{new Date(row.expiresAt).toLocaleTimeString()}</time></span>
        </li>;
      })}
    </ol>)}
  </section>;
}
