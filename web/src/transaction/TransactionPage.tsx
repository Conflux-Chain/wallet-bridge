import { useEffect, useState } from 'react';
import { readTransaction, TransactionReadError, type TransactionReview } from '../api/transactions';
import { networks } from '../wallet/networks';
import './review.css';

const states: Record<TransactionReview['state'], string> = {
  pending: '待审批', awaiting_wallet: '等待钱包', uncertain: '结果未确定', sent: '已发送',
  rejected: '用户已拒绝', canceled: '已取消', expired: '已到期',
};

function nativeAmount(units: string, decimals: number) {
  const digits = units.padStart(decimals + 1, '0');
  const fraction = digits.slice(-decimals).replace(/0+$/, '');
  return digits.slice(0, -decimals) + (fraction ? '.' + fraction : '');
}

function CopyValue({ value, label }: { value: string; label: string }) {
  const [feedback, setFeedback] = useState('');
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
      setFeedback('已复制');
    } catch {
      setFeedback('请选中原文复制');
    }
  }
  return <div className="copy-value">
    <code dir="ltr">{value}</code>
    <button type="button" className="secondary copy" aria-label={'复制' + label} onClick={copy}>复制</button>
    {feedback && <small role="status">{feedback}</small>}
  </div>;
}

function Fee({ label, value, unit }: { label: string; value?: string; unit: string }) {
  return <div><dt>{label}</dt><dd>{value === undefined ? '由钱包估算' : <>
    {BigInt(value).toString()} {unit}<small>调用方提供 · {value}</small>
  </>}</dd></div>;
}

function ReviewDetails({ review }: { review: TransactionReview }) {
  const tx = review.transaction;
  const network = networks.get(tx.chainId);
  const units = BigInt(tx.value).toString();
  const bytes = (tx.data.length - 2) / 2;
  return <>
    <section className="review-summary" aria-label="请求与金额">
      <div className="review-context"><h2>原生币金额</h2><span className={'review-state ' + review.state} role="status">{states[review.state]}</span></div>
      <p className="review-amount" dir="ltr">{network ? nativeAmount(units, network.decimals) : units}
        <span>{network?.symbol ?? '最小单位'}</span></p>
      {network && <p className="review-units">{units} 最小单位</p>}
      <dl>
        <div><dt>目标网络</dt><dd>{network?.name ?? '未知网络'}<small>chainId {BigInt(tx.chainId).toString()} · {tx.chainId}</small></dd></div>
        <div><dt>Session 固定钱包</dt><dd><bdi>{review.wallet.name}</bdi>{review.wallet.rdns && <small>{review.wallet.rdns}</small>}</dd></div>
        <div><dt>请求来源（调用方提供）</dt><dd><bdi>{review.clientName}</bdi></dd></div>
        <div><dt>审批截止时间</dt><dd><time dateTime={review.expiresAt}>{new Date(review.expiresAt).toLocaleString()}</time></dd></div>
        <div className="wide"><dt>调用方意图说明</dt><dd className="intent"><bdi>{review.intent.trim() ? review.intent : '调用方未提供说明'}</bdi></dd></div>
      </dl>
    </section>
    <section aria-labelledby="actual-tx"><h2 id="actual-tx">实际交易参数</h2>
      <dl className="review-fields">
        <div><dt>发送地址 · from</dt><dd><CopyValue value={tx.from} label="发送地址" /></dd></div>
        <div><dt>目标地址 · to</dt><dd><CopyValue value={tx.to} label="目标地址" /></dd></div>
      </dl>
      <details><summary>原始 data · {bytes} 字节{bytes > 0 && ' · 未解码'}</summary>
        <CopyValue value={tx.data} label="完整 data" />
      </details>
    </section>
    <section aria-labelledby="fees"><h2 id="fees">Gas 与费用</h2><dl>
      <Fee label="Gas 上限 · gas" value={tx.gas} unit="gas" />
      {tx.maxFeePerGas === undefined
        ? <Fee label={tx.gasPrice === undefined ? '费率' : 'Gas 单价 · gasPrice'} value={tx.gasPrice} unit="最小单位 / gas" />
        : <><Fee label="最高费率 · maxFeePerGas" value={tx.maxFeePerGas} unit="最小单位 / gas" />
          <Fee label="最高优先费 · maxPriorityFeePerGas" value={tx.maxPriorityFeePerGas} unit="最小单位 / gas" /></>}
    </dl></section>
    <section aria-labelledby="request-record"><h2 id="request-record">请求记录</h2>
      <dl className="review-fields"><div><dt>请求 ID</dt><dd><code>{review.id}</code></dd></div>
        {review.txHash && <div><dt>交易 hash</dt><dd><CopyValue value={review.txHash} label="交易 hash" /></dd></div>}
      </dl>
      {review.state === 'uncertain' && <p>暂未确认是否已广播。可在原链接查看后续钱包结果。</p>}
      {review.state === 'sent' && <p>钱包已返回交易 hash，可在目标网络查询链上执行结果。</p>}
    </section>
  </>;
}

export function TransactionPage({ id }: { id: string }) {
  const [review, setReview] = useState<TransactionReview>();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    let timer: number | undefined;
    async function refresh() {
      try {
        const next = await readTransaction(id);
        if (!live) return;
        setReview(next);
        setError('');
        if (['pending', 'awaiting_wallet', 'uncertain'].includes(next.state)) timer = window.setTimeout(refresh, 3500);
      } catch (cause) {
        if (!live) return;
        setReview(undefined);
        setError(cause instanceof TransactionReadError ? cause.message : '暂时无法读取交易，请检查本机服务后刷新。');
      }
    }
    void refresh();
    return () => { live = false; window.clearTimeout(timer); };
  }, [id, attempt]);
  return <main className="transaction-page">
    <header><a className="review-brand" href="/"><span className="brand-mark" aria-hidden="true">W</span>Wallet Bridge</a>
      <div className="review-title"><h1>交易审阅</h1><button type="button" className="secondary" onClick={() => setAttempt(n => n + 1)}>刷新状态</button></div>
    </header>
    {error ? <p role="alert">{error}</p> : review ? <ReviewDetails review={review} /> : <p role="status">正在读取交易…</p>}
  </main>;
}
