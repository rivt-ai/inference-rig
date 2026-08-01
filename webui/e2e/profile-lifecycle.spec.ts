import { expect, test } from '@playwright/test';

import { sessionTokenKey } from '../web/src/lib/project';

// One browser scenario, deliberately. Cross-browser and per-panel coverage is
// not justified until a browser-specific defect appears; what is justified is
// proving that the built Svelte app, the Connect transport, the public HTTP
// server, the control daemon, a real llama.cpp process, and a real GGUF model
// work together for the workflow a user performs first.
//
// Nothing here is mocked. The Go process harness supplies the URL, token,
// model path, and a free port.

const token = process.env.INFERENCERIG_E2E_TOKEN ?? '';
const modelSource = process.env.INFERENCERIG_E2E_MODEL_SOURCE ?? '';
const listenPort = process.env.INFERENCERIG_E2E_PROFILE_PORT ?? '';
const profileName = 'browser';

test.beforeEach(async ({ context }) => {
  if (!token || !modelSource || !listenPort) {
    throw new Error(
      'INFERENCERIG_E2E_TOKEN, INFERENCERIG_E2E_MODEL_SOURCE, and INFERENCERIG_E2E_PROFILE_PORT are required; run this through `make e2e-browser`'
    );
  }
  // The app reads its gateway token from sessionStorage, so seeding it is the
  // browser equivalent of pasting the token the gateway printed at startup.
  await context.addInitScript(
    ([key, value]) => window.sessionStorage.setItem(key, value),
    [sessionTokenKey, token]
  );
});

test('create, start, stop, and delete a profile against a real engine', async ({ page }) => {
  const errors: string[] = [];
  page.on('pageerror', (error) => errors.push(error.message));
  // A failed RPC otherwise surfaces only as a dialog that did not close, which
  // says nothing about why.
  page.on('response', async (response) => {
    if (response.url().includes('ControlService/') && !response.ok()) {
      console.error(`[rpc ${response.status()}] ${response.url()} ${await response.text().catch(() => '')}`);
    }
  });

  await page.goto('/');
  await page.getByRole('button', { name: 'Profiles', exact: true }).click();

  // 1. Create the profile, pointing it at the provisioned GGUF and a port the
  // harness reserved.
  await page.getByRole('button', { name: 'New' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog.getByText('Create profile')).toBeVisible();
  await dialog.getByLabel('Name').fill(profileName);
  await dialog.getByLabel('Model source').fill(modelSource);
  await dialog.getByLabel('Listen port').fill(listenPort);
  await dialog.getByRole('button', { name: 'Create' }).click();
  await expect(dialog).toBeHidden();

  // 2. Start it. This launches a real llama-server that loads a real model, so
  // the wait is for the engine, not for an animation.
  await page.getByRole('button', { name: 'Start', exact: true }).click();
  await confirmIfPresent(page, 'Start profile');

  // 3. The dashboard is where a user confirms the profile is actually serving.
  await page.getByRole('button', { name: 'Dashboard', exact: true }).click();
  const active = page.getByText(profileName, { exact: true }).first();
  await expect(active).toBeVisible({ timeout: 90_000 });
  await expect(page.getByText('Running').first()).toBeVisible({ timeout: 90_000 });

  // 4. Stopping is destructive to in-flight requests, so it is confirmed.
  await page.getByRole('button', { name: 'Stop', exact: true }).click();
  await page.getByRole('button', { name: 'Stop profile' }).click();
  await expect(page.getByText('No profiles configured').or(page.getByText('Choose a profile'))).toBeVisible({
    timeout: 60_000
  });

  // 5. Deletion must be confirmed too; a single click may not destroy a profile.
  await page.getByRole('button', { name: 'Profiles', exact: true }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByText(`Delete ${profileName}?`)).toBeVisible();
  await page.getByRole('button', { name: 'Delete profile' }).click();
  await expect(page.getByRole('button', { name: profileName })).toHaveCount(0, { timeout: 30_000 });

  expect(errors, 'the app logged uncaught errors').toEqual([]);
});

// confirmIfPresent clicks a confirmation only when the app decided one was
// warranted. The Start button confirms on a single-active-profile backend or
// with unsaved edits, and skipping the click when there is no dialog keeps the
// test from depending on which of those applied.
async function confirmIfPresent(page: import('@playwright/test').Page, name: string) {
  const button = page.getByRole('button', { name });
  if (await button.isVisible().catch(() => false)) {
    await button.click();
  }
}
