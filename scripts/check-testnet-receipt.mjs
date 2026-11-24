// Read-only acceptance check for Conflux eSpace Testnet.
const rpcURL = 'https://evmtestnet.confluxrpc.com';

async function rpc(method, params) {
  const response = await fetch(rpcURL, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
    signal: AbortSignal.timeout(15_000),
  });
  if (!response.ok) throw new Error(`RPC HTTP ${response.status}`);
  const reply = await response.json();
  if (reply.error || !Object.hasOwn(reply, 'result')) throw new Error(reply.error?.message ?? 'Invalid RPC response');
  return reply.result;
}

async function check(hash) {
  if (process.argv.length !== 3 || !/^0x[0-9a-f]{64}$/i.test(hash)) {
    throw new Error('Usage: node scripts/check-testnet-receipt.mjs <transaction-hash>');
  }
  const chainId = await rpc('eth_chainId', []);
  if (chainId !== '0x47') throw new Error('RPC did not return Conflux eSpace Testnet (0x47)');
  const receipt = await rpc('eth_getTransactionReceipt', [hash]);
  if (receipt === null) throw new Error('No receipt yet. Query the same hash again later.');
  const { transactionHash, blockNumber, from, to, status } = receipt;
  if (transactionHash?.toLowerCase() !== hash.toLowerCase() || !blockNumber || !['0x0', '0x1'].includes(status)) {
    throw new Error('RPC returned an invalid or mismatched receipt');
  }
  console.log(JSON.stringify({ chainId, transactionHash, blockNumber, from, to, status }, null, 2));
  if (status === '0x0') process.exitCode = 1;
}

try {
  await check(process.argv[2]);
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
