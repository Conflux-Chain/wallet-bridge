import type { WalletOperation } from '../api/browser';

const descriptions = {
  ready: '已有连接正在核对账户和网络。完成该请求后可以处理下一项。',
  calling: '已有连接正在等待钱包返回。可以查看该请求的进度。',
  interrupted_calling: '原连接已中断。请先处理原钱包弹窗，再打开该请求手动继续或解除等待。',
};

export function WalletOperationNotice({ operation }: { operation: WalletOperation }) {
  return <section aria-label="钱包占用情况">
    <h2>钱包正在处理其他连接</h2>
    <p>{descriptions[operation.phase]}</p>
    <a href={'/?connection=' + operation.connectionId} target="_blank" rel="noopener">在新标签页查看占用钱包的请求</a>
  </section>;
}
