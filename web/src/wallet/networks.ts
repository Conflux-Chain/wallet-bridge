// Native currencies of the EVM networks named by the Bridge UI.
export const networks = new Map<string, { name: string; symbol: string; decimals: number }>([
  ['0x406', { name: 'Conflux eSpace', symbol: 'CFX', decimals: 18 }],
  ['0x47', { name: 'Conflux eSpace Testnet', symbol: 'CFX', decimals: 18 }],
  ['0x1', { name: 'Ethereum Mainnet', symbol: 'ETH', decimals: 18 }],
  ['0xaa36a7', { name: 'Sepolia', symbol: 'ETH', decimals: 18 }],
  ['0xa', { name: 'OP Mainnet', symbol: 'ETH', decimals: 18 }],
  ['0x38', { name: 'BNB Smart Chain', symbol: 'BNB', decimals: 18 }],
  ['0x89', { name: 'Polygon PoS', symbol: 'POL', decimals: 18 }],
  ['0x2105', { name: 'Base', symbol: 'ETH', decimals: 18 }],
  ['0xa4b1', { name: 'Arbitrum One', symbol: 'ETH', decimals: 18 }],
]);
