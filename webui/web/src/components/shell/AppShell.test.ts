import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { loadInsecureDismissed } from '$lib/session';
import { createInferenceRigState } from '../../lib/state/createInferenceRigState.svelte';
import Harness from '../../test/AppShellHarness.svelte';

const { setMode, resetMode } = vi.hoisted(() => ({ setMode: vi.fn(), resetMode: vi.fn() }));

vi.mock('mode-watcher', () => ({ setMode, resetMode }));

describe('AppShell', () => {
  beforeEach(() => {
    setMode.mockClear();
    resetMode.mockClear();
    localStorage.clear();
    document.documentElement.removeAttribute('style');
  });

  afterEach(async () => {
    cleanup();
    await new Promise((resolve) => setTimeout(resolve, 50));
  });

  it('navigates through shadcn sidebar controls', async () => {
    const app = createInferenceRigState();
    render(Harness, { app });

    await userEvent.click(screen.getByRole('button', { name: 'Profiles' }));

    expect(app.activeSection).toBe('profiles');
  });

  it('offers system, light, and dark theme choices', async () => {
    const app = createInferenceRigState();
    render(Harness, { app });

    await userEvent.click(screen.getByRole('button', { name: 'Choose color theme' }));
    await userEvent.click(await screen.findByText('Dark'));
    expect(setMode).toHaveBeenCalledWith('dark');

    const themeButton = screen.getByRole('button', { name: 'Choose color theme' });
    await waitFor(() => expect(getComputedStyle(themeButton).pointerEvents).not.toBe('none'));
    await userEvent.click(themeButton);
    await userEvent.click(await screen.findByText('System'));
    expect(resetMode).toHaveBeenCalledOnce();
  });

  it('persists separate light and dark primary overrides under the inferencerig namespace', async () => {
    const app = createInferenceRigState();
    render(Harness, { app });

    await userEvent.click(screen.getByRole('button', { name: 'Settings' }));
    await fireEvent.input(screen.getByLabelText('Light mode primary'), { target: { value: '#123456' } });
    await fireEvent.input(screen.getByLabelText('Dark mode primary'), { target: { value: '#abcdef' } });

    expect(localStorage.getItem('inferencerig.theme.primary.light')).toBe('#123456');
    expect(localStorage.getItem('inferencerig.theme.primary.dark')).toBe('#abcdef');
    expect(document.documentElement.style.getPropertyValue('--user-primary-light')).toBe('#123456');

    await userEvent.click(screen.getByRole('button', { name: 'Reset theme colors' }));
    expect(localStorage.getItem('inferencerig.theme.primary.light')).toBeNull();
  });

  // Someone running an exposed unauthenticated gateway must never be able to
  // believe they are protected, so the warning is a standing part of the page
  // rather than a toast that disappears on its own. It can be dismissed
  // deliberately, but only for the host it is warning about — see below.
  it('names what is exposed when the gateway serves unauthenticated over the network', () => {
    const app = createInferenceRigState();
    app.insecureExposed = true;
    render(Harness, { app });

    expect(screen.getByRole('alert').textContent).toMatch(/Authentication is disabled/);
  });

  it('does not nag when the gateway is authenticated', () => {
    const app = createInferenceRigState();
    render(Harness, { app });

    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('stays dismissed once the operator acknowledges it', async () => {
    const app = createInferenceRigState();
    app.insecureExposed = true;
    const first = render(Harness, { app });

    await userEvent.click(screen.getByRole('button', { name: /Dismiss this warning/ }));
    expect(screen.queryByRole('alert')).toBeNull();

    // The acknowledgement has to survive a reload, or it is not a dismissal.
    first.unmount();
    const next = createInferenceRigState();
    next.insecureExposed = true;
    render(Harness, { app: next });

    expect(screen.queryByRole('alert')).toBeNull();
  });

  // The warning is about *this* address being reachable without a credential,
  // so acknowledging it on one host says nothing about the next. A laptop that
  // moves networks has to be told again.
  it('warns again when reached at a different host', async () => {
    const app = createInferenceRigState();
    app.insecureExposed = true;
    render(Harness, { app });
    await userEvent.click(screen.getByRole('button', { name: /Dismiss this warning/ }));

    expect(loadInsecureDismissed(localStorage, window.location.host)).toBe(true);
    expect(loadInsecureDismissed(localStorage, 'somewhere-else.local:7000')).toBe(false);
  });
});
