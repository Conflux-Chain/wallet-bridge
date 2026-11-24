import { execFile } from 'node:child_process';
import { readFile } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import type { Page } from '@playwright/test';

export const origin = 'http://127.0.0.1:18765';
export const root = dirname(dirname(dirname(dirname(fileURLToPath(import.meta.url)))));
const exec = promisify(execFile);

export async function cli(args: string[], directory = process.env.WALLET_BRIDGE_TEST_DATA!) {
  const binary = process.env.WALLET_BRIDGE_TEST_EXECUTABLE;
  if (!binary || !directory) throw new Error('Browser test environment has not been initialized');
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => key.toLowerCase() !== 'path'));
  env.PATH = '';
  try {
    const { stdout } = await exec(binary, [...args, '--data-dir', directory], {
      cwd: dirname(binary), env, windowsHide: true, timeout: 20_000, maxBuffer: 32 << 10,
    });
    return stdout.trim();
  } catch {
    throw new Error(`Test CLI operation failed: ${args[0]} ${args[1] ?? ''}`);
  }
}

export async function connectionClient(path: string, body?: object) {
  const key = await readFile(join(process.env.WALLET_BRIDGE_TEST_DATA!, 'client.key'), 'utf8');
  const response = await fetch(origin + '/api/v1/' + path, {
    method: body ? 'POST' : 'GET', redirect: 'error',
    headers: { Authorization: 'Bearer ' + key.trim(), 'Content-Type': 'application/json' },
    body: body && JSON.stringify(body),
  });
  if (response.status !== 200) throw new Error('Test client received HTTP ' + response.status);
  return response.json();
}

// Waiting commands print a receipt to stderr before the browser can complete the request.
export async function waitingCLI(args: string[]) {
  const child = execFile(process.env.WALLET_BRIDGE_TEST_EXECUTABLE!,
    [...args, '--data-dir', process.env.WALLET_BRIDGE_TEST_DATA!], { windowsHide: true, timeout: 25_000 });
  const done = new Promise<{code: number | null; stdout: string}>((resolve, reject) => {
    let stdout = '';
    child.stdout!.setEncoding('utf8').on('data', chunk => { stdout += chunk; });
    child.once('error', reject);
    child.once('close', code => resolve({code, stdout}));
  });
  const lines = createInterface({input: child.stderr!});
  for await (const line of lines) {
    return { receipt: JSON.parse(line), done };
  }
  await done;
  throw new Error('CLI exited before printing its pending request');
}

export async function link() { return cli(['ui', '--print-url']); }

export async function openLink(page: Page, url: string) {
  try { await page.goto(url); }
  catch { throw new Error('Browser authorization navigation failed'); }
}

export async function authorize(page: Page) { await openLink(page, await link()); }
