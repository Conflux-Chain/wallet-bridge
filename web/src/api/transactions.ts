import { browserRequest } from './browser';

export interface TransactionReview {
  id: string;
  clientName: string;
  intent: string;
  state: 'pending' | 'awaiting_wallet' | 'uncertain' | 'sent' | 'rejected' | 'canceled' | 'expired';
  expiresAt: string;
  handedOffAt?: string;
  txHash?: string;
  wallet: { name: string; rdns?: string };
  transaction: {
    from: string; to: string; value: string; chainId: string; data: string;
    gas?: string; gasPrice?: string; maxFeePerGas?: string; maxPriorityFeePerGas?: string;
  };
}

export class TransactionReadError extends Error {}

export async function rejectTransaction(id: string) {
  const response = await browserRequest('browser/transactions/' + id + '/reject', {});
  if (!response.ok) throw new Error('拒绝结果尚未确认');
}

export async function readTransaction(id: string): Promise<TransactionReview> {
  const response = await browserRequest('browser/transactions/' + id);
  if (response.status === 401) throw new TransactionReadError('浏览器授权已失效。请运行 wallet-bridge ui 重新授权，再打开原链接。');
  if (response.status === 404) throw new TransactionReadError('找不到这笔交易，请核对调用方提供的原链接。');
  if (!response.ok) throw new TransactionReadError('暂时无法读取交易，请检查本机服务。');
  const review: TransactionReview = await response.json();
  if (review.id !== id) throw new TransactionReadError('响应的交易请求 ID 与链接不一致。');
  return review;
}
