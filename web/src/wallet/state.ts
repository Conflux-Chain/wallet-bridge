import type { EIP1193Provider } from './discovery';

export async function walletState(provider: EIP1193Provider) {
  const [accounts, chain] = await Promise.all([
    provider.request({ method: 'eth_accounts' }), provider.request({ method: 'eth_chainId' }),
  ]);
  if (!Array.isArray(accounts) || !accounts.every((a): a is string => typeof a === 'string' && /^0x[0-9a-f]{40}$/i.test(a)) ||
      typeof chain !== 'string' || !/^0x[0-9a-f]+$/i.test(chain)) {
    throw new Error('钱包返回的账户或网络格式无效。');
  }
  return { accounts: Array.from(new Set(accounts.map((a) => a.toLowerCase()))), chain: '0x' + BigInt(chain).toString(16) };
}
