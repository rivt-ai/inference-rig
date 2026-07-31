import { cleanup, render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createInferenceRigState } from '../../lib/state/createInferenceRigState.svelte';
import type { InferenceRigClient } from '../../lib/setup/createInferenceRigClient.svelte';
import LogsPanel from './LogsPanel.svelte';

describe('LogsPanel', () => {
  afterEach(async () => {
    cleanup();
    await new Promise((resolve) => setTimeout(resolve, 50));
  });

  it('switches services, pauses live tail, and lists archives', async () => {
    const state = createInferenceRigState();
    // The log service is a free-form name in the proto, not a control/gateway
    // union, so a backend runtime's own log is selectable like any other.
    state.logServices = ['inferencerig', 'engine', 'mlx-runtime'];
    state.logArchives = [
      {
        $typeName: 'inferencerig.control.v1.LogArchive',
        id: 'mlx-runtime-20260703T120000Z.log',
        service: 'mlx-runtime',
        sizeBytes: 42n,
        archivedAt: '2026-07-03T12:00:00Z'
      }
    ];
    const app = {
      state,
      refreshLogs: vi.fn(async () => undefined),
      refreshEvents: vi.fn(async () => undefined),
      resumeLogs: vi.fn(async () => undefined),
      loadLogArchives: vi.fn(async () => undefined),
      selectLogArchive: vi.fn(async () => undefined),
      deleteLogArchive: vi.fn(async () => undefined),
      clearLogArchives: vi.fn(async () => undefined),
      showError: vi.fn()
    } as unknown as InferenceRigClient;

    render(LogsPanel, { app });

    // Service selection lives on the Control tab; the Engine tab is pinned to
    // the engine log, which is now its own file rather than lines filtered out
    // of the control log.
    await userEvent.click(screen.getByRole('tab', { name: /Control/ }));
    await userEvent.click(screen.getByRole('button', { name: 'mlx-runtime' }));
    expect(state.logService).toBe('mlx-runtime');
    expect(screen.queryByRole('button', { name: 'engine' })).toBeNull();

    await userEvent.click(screen.getByRole('button', { name: 'Pause' }));
    expect(state.logPaused).toBe(true);

    await userEvent.click(screen.getByRole('tab', { name: /Archives/ }));
    expect(screen.getByText(/42 bytes/)).toBeInTheDocument();
  });
});
