import type { Page } from '@playwright/test';
import { account, to } from './transaction-fixture';

interface WalletFixture {
  accounts: string[];
  chain: string;
  calls: string[];
  event(name: string): void;
  duplicate(): void;
  replaceLegacy(): void;
  delayAccountRead(): void;
  finishAccountRead(): void;
  switchMode: 'approve' | 'reject' | 'hold';
  finishSwitch(): void;
  sends: unknown[];
  finishSend(result: { hash: string } | { code: number }): void;
}
declare global { interface Window { reviewWallet: WalletFixture } }

export async function install(page: Page, legacy = false) {
  await page.addInitScript(({ account, to, legacy }) => {
    const events = new EventTarget();
    let delayed = false;
    let finish = () => {};
    let releaseSwitch = () => {};
    let settleSend: WalletFixture['finishSend'] = () => {};
    const fixture: WalletFixture = {
      accounts: [to, account], chain: '0x047', calls: [],
      event: name => events.dispatchEvent(new Event(name)),
      duplicate: () => announce('测试钱包', 'test.wallet', other),
      replaceLegacy: () => { Object.defineProperty(window, 'ethereum', { configurable: true, value: other }); },
      delayAccountRead: () => { delayed = true; },
      finishAccountRead: () => finish(),
      switchMode: 'approve', finishSwitch: () => releaseSwitch(),
      sends: [], finishSend: result => settleSend(result),
    };
    function makeProvider(label: string) {
      return {
        on: (name: string, fn: EventListener) => events.addEventListener(name, fn),
        removeListener: (name: string, fn: EventListener) => events.removeEventListener(name, fn),
        request: async ({ method, params }: { method: string; params?: { chainId: string }[] }) => {
          fixture.calls.push(label + '/' + method);
          if (method === 'eth_sendTransaction') {
            fixture.sends.push(params);
            return new Promise<string>((resolve, reject) => {
              settleSend = result => 'hash' in result ? resolve(result.hash) : reject(result);
            });
          }
          if (method === 'eth_chainId') return fixture.chain;
          if (method === 'eth_requestAccounts') { fixture.accounts = [account]; return fixture.accounts; }
          if (method === 'wallet_switchEthereumChain') {
            if (fixture.switchMode === 'reject') throw { code: 4001 };
            if (fixture.switchMode === 'hold') await new Promise<void>(resolve => { releaseSwitch = resolve; });
            fixture.chain = params![0].chainId;
            return null;
          }
          if (method !== 'eth_accounts') throw Error('Unexpected wallet method');
          const accounts = [...fixture.accounts];
          if (delayed) {
            delayed = false;
            await new Promise<void>(resolve => { finish = resolve; });
          }
          return accounts;
        },
      };
    }
    function announce(name: string, rdns: string, instance: ReturnType<typeof makeProvider>) {
      window.dispatchEvent(new CustomEvent('eip6963:announceProvider', { detail: {
        provider: instance, info: { name, rdns, uuid: crypto.randomUUID(), icon: 'data:image/png;base64,' },
      } }));
    }
    const provider = makeProvider('fixed'), other = makeProvider('other');
    const unrelated = makeProvider('unrelated');
    if (legacy) Object.defineProperty(window, 'ethereum', { configurable: true, value: provider });
    else window.addEventListener('eip6963:requestProvider', () => {
      announce('测试钱包', 'other.wallet', unrelated);
      announce('测试钱包', 'test.wallet', provider);
    });
    window.reviewWallet = fixture;
  }, { account, to, legacy });
}
