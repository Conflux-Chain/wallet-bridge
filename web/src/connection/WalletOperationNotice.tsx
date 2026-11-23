import type { WalletOperation } from '../api/browser';

const descriptions = {
  ready: '已有连接正在核对账户和网络。完成该请求后可以处理下一项。',
  calling: '已有连接正在等待钱包返回。可以查看该请求的进度。',
  interrupted_calling: '原连接已中断。请先处理原钱包弹窗，再打开该请求手动继续或解除等待。',
};

const transactionDescriptions = {
  ready: '这笔交易正在核对钱包、账户和网络。',
  calling: '请等待钱包返回操作结果。',
  interrupted_calling: '原钱包调用已中断。请先检查钱包中是否还有待处理的弹窗。',
};

export function WalletOperationNotice({ operation }: { operation: WalletOperation }) {
  if (operation.kind === 'transaction') {
    return <section aria-label="钱包占用情况">
      <h2>钱包正在处理交易</h2>
      <p>{transactionDescriptions[operation.phase]}</p>
      <p>交易请求 ID <code>{operation.id}</code></p>
    </section>;
  }
  return <section aria-label="钱包占用情况">
    <h2>钱包正在处理其他连接</h2>
    <p>{descriptions[operation.phase]}</p>
    <a href={'/?connection=' + operation.id} target="_blank" rel="noopener">在新标签页查看占用钱包的请求</a>
  </section>;
}
