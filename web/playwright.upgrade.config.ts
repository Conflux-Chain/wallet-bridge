import { defineConfig } from '@playwright/test';
import base from './playwright.config';

// The Go test owns both packaged programs and the service data directory.
export default defineConfig({ ...base, testDir: './tests/upgrade', globalSetup: undefined });
