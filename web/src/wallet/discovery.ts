export interface EIP1193Provider {
  request(args: { method: string; params?: readonly unknown[] | object }): Promise<unknown>;
  on(event: string, listener: (...args: unknown[]) => void): unknown;
  removeListener(event: string, listener: (...args: unknown[]) => void): unknown;
}

interface WalletInfo {
  readonly uuid: string;
  readonly name: string;
  readonly rdns: string;
  readonly icon?: string;
}

export interface DiscoveredWallet {
  readonly id: string;
  readonly provider: EIP1193Provider;
  readonly info?: WalletInfo;
}

interface Snapshot {
  readonly wallets: readonly DiscoveredWallet[];
  readonly selectedId: string | null;
}

function isProvider(value: unknown): value is EIP1193Provider {
  if (!value || typeof value !== 'object') return false;
  const provider = value as Partial<EIP1193Provider>;
  return typeof provider.request === 'function' && typeof provider.on === 'function' && typeof provider.removeListener === 'function';
}

function readInfo(value: unknown): WalletInfo | undefined {
  if (!value || typeof value !== 'object') return;
  const info = value as Record<string, unknown>;
  if (typeof info.uuid !== 'string' || !/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(info.uuid) ||
      typeof info.name !== 'string' || !info.name.trim() || typeof info.rdns !== 'string' || !info.rdns.trim() || typeof info.icon !== 'string') return;
  return {
    uuid: info.uuid.toLowerCase(), name: info.name, rdns: info.rdns,
    // An unusable icon must not hide an otherwise usable provider.
    icon: /^data:image\/(?:png|jpeg|gif|webp|avif|svg\+xml)(?:;[^,]*)?,/i.test(info.icon) ? info.icon : undefined,
  };
}

export class WalletDiscovery {
  private snapshot: Snapshot = { wallets: [], selectedId: null };
  private listeners = new Set<() => void>();
  private started = false;
  private nextId = 0;

  constructor(private target: Window) {}

  getSnapshot = (): Snapshot => this.snapshot;

  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  private update(snapshot: Snapshot) {
    this.snapshot = snapshot;
    this.listeners.forEach((listener) => listener());
  }

  private add(provider: EIP1193Provider, info?: WalletInfo) {
    const existing = this.snapshot.wallets.find((wallet) => wallet.provider === provider);
    if (existing && (existing.info || !info)) return;
    // Upgrade a legacy entry in place. UUID and rdns never select the instance.
    const wallet = { id: existing?.id ?? `wallet-${++this.nextId}`, provider, info };
    const wallets = existing
      ? this.snapshot.wallets.map((value) => value === existing ? wallet : value)
      : [...this.snapshot.wallets, wallet];
    this.update({ ...this.snapshot, wallets });
  }

  private announce = (event: Event) => {
    try {
      if (!(event instanceof CustomEvent)) return;
      const detail: unknown = event.detail;
      if (!detail || typeof detail !== 'object') return;
      const { info: value, provider } = detail as { info?: unknown; provider?: unknown };
      const info = readInfo(value);
      if (info && isProvider(provider)) this.add(provider, info);
    } catch {
      // A malformed extension announcement must not break other wallets.
    }
  };

  refresh = () => {
    if (!this.started) return;
    this.target.dispatchEvent(new Event('eip6963:requestProvider'));
    try {
      const provider = (this.target as Window & { ethereum?: unknown }).ethereum;
      if (isProvider(provider)) this.add(provider);
    } catch {
      // A throwing legacy getter must not break standard discovery.
    }
  };

  clearSelection = () => { this.update({ ...this.snapshot, selectedId: null }); };

  select = (id: string) => {
    if (this.snapshot.selectedId === id || !this.snapshot.wallets.some((wallet) => wallet.id === id)) return;
    this.update({ ...this.snapshot, selectedId: id });
  };

  start = () => {
    if (this.started) return;
    this.started = true;
    this.target.addEventListener('eip6963:announceProvider', this.announce);
    this.target.addEventListener('focus', this.refresh);
    this.refresh();
  };

  stop = () => {
    this.started = false;
    this.target.removeEventListener('eip6963:announceProvider', this.announce);
    this.target.removeEventListener('focus', this.refresh);
  };
}
