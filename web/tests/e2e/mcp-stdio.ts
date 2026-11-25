import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { once } from 'node:events';
import { createInterface } from 'node:readline';

type Options = {
  binary: string;
  env?: NodeJS.ProcessEnv;
  args?: string[];
  clientInfo: { name: string; title?: string; version: string };
};

type Request = (method: string, params: object) => Promise<ReturnType<typeof JSON.parse>>;

// Drive a packaged MCP server as a sequential STDIO client in integration tests.
export async function withMCP({ binary, env, args = [], clientInfo }: Options,
  run: (init: ReturnType<typeof JSON.parse>, request: Request) => Promise<void>) {
  const child = spawn(binary, ['mcp', ...args], {
    env, stdio: ['pipe', 'pipe', 'inherit'], windowsHide: true, timeout: 45_000, killSignal: 'SIGKILL',
  });
  const closed = once(child, 'close');
  const lines = createInterface({ input: child.stdout })[Symbol.asyncIterator]();
  const send = (message: object) => child.stdin.write(JSON.stringify({ jsonrpc: '2.0', ...message }) + '\n');
  let sequence = 0;
  async function request(method: string, params: object) {
    const id = ++sequence;
    send({ id, method, params });
    const line = await lines.next();
    assert.equal(line.done, false, 'MCP server closed before replying');
    const response = JSON.parse(line.value);
    assert.equal(response.jsonrpc, '2.0');
    assert.equal(response.id, id);
    assert.ok(response.result, line.value);
    return response.result;
  }
  try {
    const protocolVersion = '2025-11-25';
    const init = await request('initialize', { protocolVersion, capabilities: {}, clientInfo });
    assert.equal(init.protocolVersion, protocolVersion);
    send({ method: 'notifications/initialized' });
    await run(init, request);
  } finally {
    child.stdin.end();
    assert.deepEqual(await closed, [0, null], 'MCP server must exit cleanly on EOF');
  }
}
