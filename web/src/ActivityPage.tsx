import { useEffect, useRef, useState } from 'react';
import { browserRequest, type BrowserStatus } from './api/browser';
import { requestStates } from './requestStates';
import { networks } from './wallet/networks';

interface Activity {
  kind: 'connection' | 'transaction';
  id: string;
  clientName: string;
  chainId: string;
  createdAt: string;
  state: keyof typeof requestStates;
  sessionId?: string;
  txHash?: string;
  removable?: boolean;
}

interface ActivityPageData { activities: Activity[]; nextAfter?: string }

function validActivity(row: Partial<Activity> | null): row is Activity {
  return (row?.kind === 'connection' || row?.kind === 'transaction') &&
    typeof row.id === 'string' && /^[0-9a-f]{32}$/.test(row.id) &&
    typeof row.clientName === 'string' && typeof row.chainId === 'string' &&
    typeof row.createdAt === 'string' && Number.isFinite(Date.parse(row.createdAt)) &&
    typeof row.state === 'string' && Object.hasOwn(requestStates, row.state) &&
    (row.sessionId === undefined || typeof row.sessionId === 'string') &&
    (row.txHash === undefined || typeof row.txHash === 'string');
}

export function ActivityPage({ status, after, onUnauthorized }: {
  status: BrowserStatus; after: string | null; onUnauthorized: () => void;
}) {
  const [data, setData] = useState<ActivityPageData | 'loading' | 'error'>('loading');
  const [refresh, setRefresh] = useState(0);
  const [removing, setRemoving] = useState(false);
  const [notice, setNotice] = useState('');
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; };
  }, []);

  async function remove(row: Activity) {
    setRemoving(true);
    setNotice('');
    try {
      const response = await browserRequest('browser/activity/remove', { kind: row.kind, id: row.id });
      if (!mounted.current) return;
      if (response.status === 401) { onUnauthorized(); return; }
      if (!response.ok) throw new Error();
      setNotice('已从活动列表移除，仍可按原请求 ID 查询。');
    } catch {
      if (mounted.current) setNotice('暂时无法确认移除结果，请核对刷新后的列表。');
    } finally {
      if (mounted.current) {
        setRefresh(value => value + 1);
        setRemoving(false);
      }
    }
  }
  useEffect(() => {
    let current = true;
    async function read() {
      try {
        const query = new URLSearchParams({ limit: '20' });
        if (after !== null) query.set('after', after);
        const response = await browserRequest('browser/activity?' + query);
        if (response.status === 401) {
          if (current) onUnauthorized();
          return;
        }
        if (!response.ok) throw new Error();
        const page = await response.json();
        if (!Array.isArray(page?.activities) || !page.activities.every(validActivity) ||
          (page.nextAfter !== undefined && typeof page.nextAfter !== 'string')) throw new Error();
        if (current) setData(page);
      } catch {
        if (current) setData('error');
      }
    }
    void read();
    return () => { current = false; };
  }, [status, after, onUnauthorized, refresh]);

  return <main className="activity-page">
    <a href="/">返回管理首页</a>
    <h1>活动记录</h1>
    <section aria-label="请求活动">
      <div className="activity-heading">
        <h2>{after === null ? '最近活动' : '更早的活动'}</h2>
        <button type="button" className="secondary" onClick={() => setRefresh(value => value + 1)}>刷新记录</button>
      </div>
      {data === 'loading' && <p role="status">正在读取活动记录…</p>}
      {data === 'error' && <p role="alert">暂时无法读取活动记录。请点击「刷新记录」重试。</p>}
      {notice && <p role="status">{notice}</p>}
      {typeof data === 'object' && (data.activities.length === 0 ? <p>此页没有活动记录。</p> : <ol>
        {data.activities.map(row => {
          const network = networks.get(row.chainId);
          return <li key={row.kind + row.id}>
            <div className="activity-heading">
              <a href={`/?${row.kind}=${row.id}`}>{row.kind === 'connection' ? '连接' : '交易'} · <bdi>{row.clientName}</bdi></a>
              <strong className={'activity-state ' + row.state}>{requestStates[row.state]}</strong>
            </div>
            <p><time dateTime={row.createdAt}>{new Date(row.createdAt).toLocaleString()}</time>{' · '}{network && network.name + ' · '}{row.chainId}</p>
            <div>请求 ID <code>{row.id}</code></div>
            {row.sessionId && <div>Session ID <code>{row.sessionId}</code></div>}
            {row.txHash && <div>交易哈希 <code>{row.txHash}</code></div>}
            {row.removable === true && status.info.capabilities.includes('browser.activity.remove') &&
              <button type="button" className="secondary" disabled={removing} onClick={() => void remove(row)}>从活动列表移除</button>}
          </li>;
        })}
      </ol>)}
      <nav aria-label="活动记录分页">
        {after !== null && <a href="/?view=activity">返回最新记录</a>}
        {typeof data === 'object' && data.nextAfter &&
          <a href={'/?view=activity&after=' + encodeURIComponent(data.nextAfter)}>查看更早记录</a>}
      </nav>
    </section>
  </main>;
}
