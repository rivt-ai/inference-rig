import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
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
});
