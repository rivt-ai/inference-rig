import { cleanup, render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createInferenceRigState } from '../../lib/state/createInferenceRigState.svelte';
import { NO_CAPABILITIES, singleActiveProfileWarning } from '../../lib/backends';
import type { BackendCapabilities, Profile } from '../../lib/gen/inferencerig/control/v1/control_pb';
import type { InferenceRigClient } from '../../lib/setup/createInferenceRigClient.svelte';
import ProfilesPanel from './ProfilesPanel.svelte';

function profile(name: string, overrides: Partial<Profile> = {}): Profile {
  return {
    $typeName: 'inferencerig.control.v1.Profile',
    name,
    backend: 'mlx',
    profileYaml: '',
    modelSource: 'local',
    modelReference: `/models/${name}`,
    host: '127.0.0.1',
    port: 8080,
    engineArgs: {},
    ...overrides
  };
}

function panel(options: {
  capabilities?: Partial<BackendCapabilities>;
  activeProfileNames?: string[];
  activeBackend?: string;
  runtimeStates?: Record<string, string>;
  overrides?: Partial<Record<string, unknown>>;
} = {}) {
  const state = createInferenceRigState();
  state.selectedBackend = 'mlx';
  state.selectedProfileName = 'coder';
  state.currentProfile = profile('coder');
  state.profiles = [profile('coder'), profile('chat')];
  state.activeProfileNames = options.activeProfileNames ?? [];
  state.activeBackend = options.activeBackend ?? '';
  state.profileRuntimeStates = options.runtimeStates ?? {};
  const capabilities = { ...NO_CAPABILITIES, ...options.capabilities };

  const app = {
    state,
    errorMessage: '',
    capabilities: () => capabilities,
    startWarning: (name: string) => singleActiveProfileWarning(capabilities, state.activeProfileNames, name),
    isProfileActive: (name: string) => state.activeProfileNames.includes(name),
    loadProfiles: vi.fn(),
    selectProfile: vi.fn(),
    createProfile: vi.fn(),
    duplicateProfile: vi.fn(),
    deleteProfile: vi.fn(),
    cleanupProfile: vi.fn(),
    saveProfile: vi.fn(),
    reloadSelectedProfile: vi.fn(),
    startSelectedProfile: vi.fn(),
    resetRuntimes: vi.fn(),
    selectBackend: vi.fn(),
    ...options.overrides
  } as unknown as InferenceRigClient;

  render(ProfilesPanel, { app });
  return { app, state };
}

describe('ProfilesPanel', () => {
  afterEach(async () => {
    cleanup();
    await new Promise((resolve) => setTimeout(resolve, 50));
  });

  it('visually distinguishes selected and selectable profiles', () => {
    panel();
    const selected = screen.getByRole('button', { name: /coder/ });
    const option = screen.getByRole('button', { name: /chat/ });
    expect(selected).toHaveAttribute('aria-pressed', 'true');
    expect(selected.closest('[data-slot="item"]')).toHaveClass('border-primary/50', 'bg-primary/10');
    expect(option.closest('[data-slot="item"]')).toHaveClass('hover:bg-muted/50');
  });

  it('does not delete a profile before AlertDialog confirmation', async () => {
    const deleteProfile = vi.fn();
    panel({ overrides: { deleteProfile } });
    await userEvent.click(screen.getByRole('button', { name: 'Delete' }));
    expect(deleteProfile).not.toHaveBeenCalled();

    await userEvent.click(await screen.findByRole('button', { name: 'Delete profile' }));
    expect(deleteProfile).toHaveBeenCalledOnce();
  });

  // single_active_profile: on MLX, starting B stops A. The user has to be told
  // before the click, not shown a stopped profile afterwards. llama.cpp runs
  // profiles concurrently and must not show this at all.
  it('warns which profile a start will stop on a single_active_profile backend', async () => {
    const startSelectedProfile = vi.fn();
    panel({ capabilities: { singleActiveProfile: true }, activeProfileNames: ['chat'], overrides: { startSelectedProfile } });

    await userEvent.click(screen.getByRole('button', { name: 'Start' }));

    expect(screen.getByText('Start coder and stop the running profile?')).toBeInTheDocument();
    expect(screen.getByText(/chat will be stopped/)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: 'Start profile' }));
    expect(startSelectedProfile).toHaveBeenCalledWith(true);
  });

  it('keeps another backend profiles visible but offers a confirmed reset inline', async () => {
    const resetRuntimes = vi.fn();
    panel({ activeBackend: 'llamacpp', overrides: { resetRuntimes } });

    expect(screen.getByText('Active backend: llamacpp')).toBeInTheDocument();
    expect(screen.getByText('llamacpp is active — reset to start mlx profiles')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Start' })).toBeDisabled();
    expect(screen.getByRole('button', { name: /coder/ }).closest('[data-slot="item"]')).toHaveClass('opacity-50');

    await userEvent.click(screen.getByRole('button', { name: 'Reset runtime' }));
    expect(resetRuntimes).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole('button', { name: 'Reset and switch backend' }));
    expect(resetRuntimes).toHaveBeenCalledOnce();
  });

  it('shows the exact transitional state reported for a profile', () => {
    panel({ runtimeStates: { coder: 'activating' } });
    expect(screen.getByText('activating')).toBeInTheDocument();
  });

  it('starts without a stop warning on a backend that runs profiles concurrently', async () => {
    const startSelectedProfile = vi.fn();
    panel({ activeProfileNames: ['chat'], overrides: { startSelectedProfile } });

    await userEvent.click(screen.getByRole('button', { name: 'Start' }));

    expect(screen.queryByText(/will be stopped/)).not.toBeInTheDocument();
    expect(startSelectedProfile).toHaveBeenCalledOnce();
  });

  // parameter_introspection false: an empty combobox would imply no key is
  // valid, so the field degrades to free text and validation stands down.
  it('falls back to a free-text key field without parameter introspection', () => {
    panel();
    expect(screen.getByText(/does not expose a parameter list/)).toBeInTheDocument();
  });

  it('edits engine args as typed rows rather than opaque text', async () => {
    const { state } = panel({
      capabilities: { parameterIntrospection: true },
      overrides: {}
    });
    state.draftRows = [{ key: 'max-tokens', value: '512', kind: 'int' }];
    await userEvent.click(screen.getByRole('button', { name: /Add argument/ }));
    expect(state.draftRows.length).toBe(2);
    expect(state.dirty.rows).toBe(true);
  });

  // Model, host and port are the fields a profile is retargeted with; before
  // they were read-only text and only engine args could be edited.
  it('edits the model, listen host and port of the selected profile', async () => {
    const { state } = panel();

    await userEvent.clear(screen.getByLabelText('Listen host'));
    await userEvent.type(screen.getByLabelText('Listen host'), '0.0.0.0');
    await userEvent.clear(screen.getByLabelText('Port'));
    await userEvent.type(screen.getByLabelText('Port'), '9090');
    await userEvent.clear(screen.getByLabelText('Model'));
    await userEvent.type(screen.getByLabelText('Model'), '/models/other.gguf');

    expect(state.currentProfile?.host).toBe('0.0.0.0');
    expect(state.currentProfile?.port).toBe(9090);
    expect(state.currentProfile?.modelSource).toBe('/models/other.gguf');
    expect(state.dirty.rows).toBe(true);
  });

  it('offers the downloaded models as model completions', () => {
    const { state } = panel();
    state.localModels = [
      {
        $typeName: 'inferencerig.control.v1.LocalModel',
        path: '/models/Qwen3-0.6B.Q4_K_M.gguf',
        filename: 'Qwen3-0.6B.Q4_K_M.gguf',
        sizeBytes: 0n,
        modifiedAt: '',
        usedByProfiles: []
      }
    ];
    expect(screen.getByLabelText('Model')).toHaveAttribute('list', 'profile-model-options');
  });
});
