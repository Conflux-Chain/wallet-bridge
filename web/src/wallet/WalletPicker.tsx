import { useEffect, useState, useSyncExternalStore } from 'react';
import { WalletDiscovery } from './discovery';

export function WalletPicker({ discovery: shared, disabled = false }: { discovery?: WalletDiscovery; disabled?: boolean }) {
  const [discovery] = useState(() => shared ?? new WalletDiscovery(window));
  const { wallets, selectedId } = useSyncExternalStore(discovery.subscribe, discovery.getSnapshot);
  useEffect(() => {
    discovery.start();
    return discovery.stop;
  }, [discovery]);
  const selected = wallets.find((wallet) => wallet.id === selectedId);

  return (
    <section aria-labelledby="wallet-title">
      <h2 id="wallet-title">选择浏览器钱包</h2>
      <p className="description">{shared ? '选择本次连接使用的钱包。' : '选择要查看的浏览器钱包。'}</p>
      {wallets.length === 0 ? (
        <p className="wallet-empty">尚未发现钱包。请确认当前浏览器配置已安装并启用钱包扩展，允许其访问此页面，然后重新检测或刷新页面。</p>
      ) : (
        <fieldset className="wallet-options" disabled={disabled}>
          <legend>已发现的钱包</legend>
          {wallets.map((wallet) => {
            const uuid = wallet.info?.uuid;
            const conflict = uuid && wallets.some((other) => other.id !== wallet.id && other.info?.uuid === uuid);
            return (
              <label className="wallet-option" key={wallet.id}>
                <input type="radio" name="wallet" value={wallet.id} checked={selectedId === wallet.id} onChange={() => discovery.select(wallet.id)} />
                <span className="wallet-icon" aria-hidden="true">
                  {wallet.info?.icon ? <img src={wallet.info.icon} alt="" onError={(event) => { event.currentTarget.hidden = true; }} /> : 'W'}
                </span>
                <span className="wallet-detail">
                  <span className="wallet-name">{wallet.info?.name ?? '浏览器注入钱包（兼容入口）'}</span>
                  <span className="wallet-source">{wallet.info?.rdns ?? '旧式入口无法可靠识别钱包名称，可能无法列出所有扩展。'}</span>
                  {conflict && <span className="wallet-conflict">此钱包的标识与其他钱包重复，请核对名称与来源。</span>}
                </span>
              </label>
            );
          })}
        </fieldset>
      )}
      <p className="wallet-selection" aria-live="polite">{selected ? `当前选择：${selected.info?.name ?? '浏览器注入钱包（兼容入口）'}` : '请选择一个钱包。'}</p>
      <p className="wallet-note">钱包名称与来源由扩展提供。选择或重新检测不会请求账户授权、切换网络或发送交易。</p>
      <button type="button" className="secondary" onClick={discovery.refresh}>重新检测钱包</button>
    </section>
  );
}
