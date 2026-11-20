import { execFile } from 'node:child_process';
import { dirname } from 'node:path';
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

export async function link() { return cli(['ui', '--print-url']); }

export async function openLink(page: Page, url: string) {
  try { await page.goto(url); }
  catch { throw new Error('Browser authorization navigation failed'); }
}

export async function authorize(page: Page) { await openLink(page, await link()); }
