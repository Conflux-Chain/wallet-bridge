import { useCallback, useEffect, useState } from 'react';
import { readTransaction, rejectTransaction, TransactionReadError, type TransactionReview } from '../api/transactions';
import { networks } from '../wallet/networks';
import { TransactionWallet } from './TransactionWallet';
import { WalletReport } from './WalletReport';
import type { WalletOperation } from '../api/browser';
import { WalletOperationNotice } from '../connection/WalletOperationNotice';
import { useReviewExecution, type ReviewExecution } from './useReviewExecution';
import { requestStates } from '../requestStates';
import './review.css';

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

function ReviewDetails({ review, execution, blocker, canSend }: {
  review: TransactionReview; execution?: ReviewExecution; blocker?: WalletOperation; canSend: boolean;
}) {
  const tx = review.transaction;
  const network = networks.get(tx.chainId);
  const units = BigInt(tx.value).toString();
  const bytes = (tx.data.length - 2) / 2;
  return <>
    <section className="review-summary" aria-label="请求与金额">
      <div className="review-context"><h2>原生币金额</h2><span className={'review-state ' + review.state} role="status">{requestStates[review.state]}</span></div>
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
    {blocker && review.state === 'pending' && <WalletOperationNotice operation={blocker} />}
    {execution && !review.handedOffAt && <ExecutionNotice execution={execution} pending={review.state === 'pending'} blocked={!!blocker} />}
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
    {(review.state === 'pending' || execution?.sending) && <div hidden={review.state !== 'pending'}>
      <TransactionWallet key={execution?.recoveredId} review={review} execution={blocker ? undefined : execution} canSend={canSend} />
    </div>}
    <section aria-labelledby="request-record"><h2 id="request-record">请求记录</h2>
      <dl className="review-fields"><div><dt>请求 ID</dt><dd><code>{review.id}</code></dd></div>
        {review.txHash && <div><dt>交易 hash</dt><dd><CopyValue value={review.txHash} label="交易 hash" /></dd></div>}
      </dl>
      {review.state === 'uncertain' && <p>暂未确认是否已广播。可在原链接查看后续钱包结果。</p>}
      {review.state === 'awaiting_wallet' && <p>请保留原交易标签页，在钱包中核对交易并确认或拒绝。</p>}
      {canSend && review.state === 'uncertain' && review.handedOffAt && execution?.progress?.phase === 'interrupted_calling' && <>
        <p>请先处理原钱包弹窗，再解除占用。原钱包仍可回报这笔交易的结果。</p>
        <button type="button" disabled={execution.recovering} onClick={execution.acknowledge}>已处理钱包弹窗，解除占用</button>
        {execution.message && <p aria-live="polite">{execution.message}</p>}
      </>}
      {review.state === 'sent' && <p>钱包已返回交易 hash，可在目标网络查询链上执行结果。</p>}
    </section>
  </>;
}

function ExecutionNotice({ execution, pending, blocked }: { execution: ReviewExecution; pending: boolean; blocked: boolean }) {
  const { progress, busy, owned, message } = execution;
  if (progress?.recoverFromId) {
    const popup = progress.phase === 'interrupted_calling';
    if (!pending && !popup) return null;
    return <section aria-label="恢复交易审阅">
      <p>{popup ? '原钱包操作已中断。请先处理原钱包弹窗，再继续。' : '原交易页已中断，可在本页继续核对。'}</p>
      <button type="button" disabled={execution.recovering || (pending && blocked)} onClick={execution.recover}>
        {popup ? (pending ? '已处理钱包弹窗，继续审阅' : '已处理钱包弹窗，解除占用') : '继续审阅'}
      </button>
    </section>;
  }
  if (!pending) return null;
  let hint = message || (!progress ? '正在核对审阅进度…' : '');
  if (busy) hint = '正在处理钱包操作…';
  else if (!owned && (progress?.phase === 'ready' || progress?.phase === 'calling')) hint = '此交易正在其他页面审阅，请在原页面操作钱包。';
  else if (owned && !hint) hint = '本页正在审阅交易。';
  return hint ? <p aria-live="polite">{hint}</p> : null;
}

export function TransactionPage({ id, grant, canReject, canCoordinate, operation }: {
  id: string; grant: string; canReject: boolean; canCoordinate: boolean; operation?: WalletOperation;
}) {
  const [review, setReview] = useState<TransactionReview>();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);
  const [rejecting, setRejecting] = useState(false);
  const [rejectUnconfirmed, setRejectUnconfirmed] = useState(false);
  const refresh = useCallback(() => setAttempt(n => n + 1), []);
  const watch = canCoordinate && review ? (review.state === 'pending' ? 'pending' : 'terminal') : undefined;
  const execution = useReviewExecution(id, watch);
  const blocker = operation?.kind === 'transaction' && operation.id === id ? undefined : operation;
  useEffect(() => {
    if (rejecting) return;
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
  }, [id, attempt, rejecting]);

  async function reject() {
    if (!canReject || rejecting || review?.state !== 'pending') return;
    setRejecting(true);
    setRejectUnconfirmed(false);
    try {
      await rejectTransaction(id);
    } catch {
      setRejectUnconfirmed(true);
    } finally {
      // A lost response or a competing handoff requires a fresh read before another decision.
      setReview(undefined);
      setAttempt(n => n + 1);
      setRejecting(false);
    }
  }
  return <main className="transaction-page">
    <header><a className="review-brand" href="/"><span className="brand-mark" aria-hidden="true">W</span>Wallet Bridge</a>
      <div className="review-title"><h1>交易审阅</h1><div className="actions">
        <button type="button" className="secondary" disabled={rejecting} onClick={() => setAttempt(n => n + 1)}>刷新状态</button>
        {canReject && review?.state === 'pending' &&
          <button type="button" className="secondary" disabled={rejecting} onClick={reject}>{rejecting ? '正在提交拒绝决定…' : '拒绝这笔交易'}</button>}
      </div></div>
    </header>
    {rejectUnconfirmed && review?.state === 'pending' && <p role="status">请求仍待审批，可以再次拒绝。</p>}
    {canReject && <WalletReport request={id} grant={grant} refresh={refresh} />}
    {error ? <p role="alert">{error}</p> : review ? <ReviewDetails review={review} execution={watch ? execution : undefined} blocker={blocker} canSend={canReject && canCoordinate} /> :
      <p role="status">{rejectUnconfirmed ? '正在核对拒绝结果…' : '正在读取交易…'}</p>}
  </main>;
}
