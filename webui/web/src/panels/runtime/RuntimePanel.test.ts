import { cleanup, render, screen, within } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { tick } from 'svelte';
import { createInferenceRigState } from '../../lib/state/createInferenceRigState.svelte';
import { NO_CAPABILITIES } from '../../lib/backends';
import type { Accelerator, BackendCapabilities, Signals } from '../../lib/gen/inferencerig/control/v1/control_pb';
import RuntimePanel from './RuntimePanel.svelte';

function accelerator(overrides: Partial<Accelerator> = {}): Accelerator {
  return {
    $typeName: 'inferencerig.control.v1.Accelerator',
    name: 'RTX',
    unifiedMemory: false,
    totalMemoryBytes: 100n,
    usedMemoryBytes: 30n,
    utilizationPercent: 50,
    hasUtilization: true,
    temperatureCelsius: 64,
    hasTemperature: true,
    source: 'nvidia',
    ...overrides
  };
}

function dashboard(options: { accelerators?: Accelerator[]; capabilities?: Partial<BackendCapabilities> } = {}) {
  const state = createInferenceRigState();
  state.runtimeStatus = { status: 'running', detail: 'runtime ready', checkedAt: '2026-07-11T12:00:00Z' };
  state.activeProfileNames = ['qwen'];
  state.selectedBackend = 'llamacpp';
  state.signals = {
    capturedAt: '2026-07-11T12:00:00Z',
    logicalCpuCores: 16,
    cpuUsedPercent: 25,
    totalMemoryBytes: 100n,
    availableMemoryBytes: 40n,
    usedMemoryBytes: 60n,
    usedMemoryPercent: 60,
    warnings: [],
    runtime: [],
    accelerators: options.accelerators ?? [accelerator()],
    disks: []
  } as unknown as Signals;

  const app = {
    state,
    capabilities: () => ({ ...NO_CAPABILITIES, ...options.capabilities }),
    startWarning: () => null,
    runTask: vi.fn(async (_label: string, task: () => Promise<void>) => task()),
    refreshRuntimeStatus: vi.fn(),
    refreshSignals: vi.fn(),
    refreshEvents: vi.fn(),
    restartProfile: vi.fn(),
    stopProfile: vi.fn(),
    selectBackend: vi.fn(),
    installBackend: vi.fn()
  };
  render(RuntimePanel, { app: app as never });
  return { app, state };
}

describe('RuntimePanel dashboard', () => {
  afterEach(async () => {
    cleanup();
    await new Promise((resolve) => setTimeout(resolve, 50));
  });

  it('shows compact status and reported accelerator readings', () => {
    dashboard();
    expect(screen.getByText('System overview')).toBeInTheDocument();
    expect(screen.getByText('running')).toBeInTheDocument();
    expect(screen.getAllByText('RTX').length).toBeGreaterThan(0);
    expect(screen.getByText('64°C')).toBeInTheDocument();
  });

  it('confirms a targeted stop before invoking it', async () => {
    const user = userEvent.setup();
    const { app } = dashboard();
    await user.click(screen.getByRole('button', { name: 'Stop' }));
    expect(screen.getByText('Stop qwen?')).toBeInTheDocument();
    expect(app.stopProfile).not.toHaveBeenCalled();
    await user.click(screen.getByRole('button', { name: 'Stop profile' }));
    expect(app.stopProfile).toHaveBeenCalledWith('qwen');
  });

  // Confirming an action has to dismiss its own dialog: bits-ui's Action button
  // is styling only and never closes the dialog for us, so a restart used to
  // leave the operator staring at the confirmation they had already answered.
  it('closes the confirmation once the action is taken', async () => {
    const user = userEvent.setup();
    const { app } = dashboard();
    await user.click(screen.getByRole('button', { name: 'Restart' }));
    await user.click(screen.getByRole('button', { name: 'Restart profile' }));
    expect(app.restartProfile).toHaveBeenCalledWith('qwen');
    await tick();
    expect(screen.queryByText('Restart qwen?')).not.toBeInTheDocument();
  });

  it('retains metrics while marking a failed refresh stale', async () => {
    const { state } = dashboard();
    state.signalsLastError = 'collector unavailable';
    await tick();
    expect(screen.getByText('Stale')).toBeInTheDocument();
    expect(screen.getByText(/collector unavailable/)).toBeInTheDocument();
    expect(screen.getByText('25% · 16 cores')).toBeInTheDocument();
  });

  // The point of the unified-memory gate: on Apple silicon the accelerator's
  // bytes ARE system RAM, so a RAM meter plus a VRAM meter double-counts one
  // allocation and reads as twice the pressure the machine is actually under.
  // Scoped to the Resources region: each meter label is deliberately echoed by
  // its trend chart below, so an unscoped query matches both.
  it('collapses RAM and VRAM into one meter on a unified-memory host', async () => {
    dashboard({ accelerators: [accelerator({ name: 'Apple M3 Max', unifiedMemory: true, source: 'metal' })] });
    const meters = within(screen.getByRole('region', { name: 'Resources' }));
    expect(meters.getByText('Unified memory')).toBeInTheDocument();
    expect(meters.queryByText('VRAM')).not.toBeInTheDocument();
    expect(meters.queryByText('Memory')).not.toBeInTheDocument();
    expect(meters.getByText(/shares system memory/)).toBeInTheDocument();
  });

  it('keeps separate RAM and VRAM meters for a discrete accelerator', () => {
    dashboard();
    const meters = within(screen.getByRole('region', { name: 'Resources' }));
    expect(meters.getByText('Memory')).toBeInTheDocument();
    expect(meters.getByText('VRAM')).toBeInTheDocument();
    expect(meters.queryByText('Unified memory')).not.toBeInTheDocument();
  });

  // A zero reading is indistinguishable from idle, so the has_* flag is the
  // only honest signal to render on.
  it('says "not reported" instead of charting an unmeasured zero', () => {
    dashboard({ accelerators: [accelerator({ hasUtilization: false, hasTemperature: false })] });
    expect(screen.getAllByText('Not reported').length).toBe(2);
    expect(screen.queryByText('64°C')).not.toBeInTheDocument();
  });

  it('renders disk meters from the new Disk signals', async () => {
    const { state } = dashboard();
    state.disks = [
      {
        $typeName: 'inferencerig.control.v1.Disk',
        label: 'Models',
        path: '/models',
        totalBytes: 1000n,
        usedBytes: 400n,
        usedPercent: 40,
        freeBytes: 600n
      }
    ];
    await tick();
    expect(screen.getByText('Disks')).toBeInTheDocument();
    expect(screen.getByText('Models')).toBeInTheDocument();
  });

  it('offers install UI only for a managed_install backend', async () => {
    dashboard();
    expect(screen.queryByText('Backend install')).not.toBeInTheDocument();
    cleanup();
    dashboard({ capabilities: { managedInstall: true } });
    expect(screen.getByText('Backend install')).toBeInTheDocument();
  });
});
