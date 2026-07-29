import { cleanup, render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { createInferenceRigState } from '../../lib/state/createInferenceRigState.svelte';
import { NO_CAPABILITIES } from '../../lib/backends';
import type { BackendCapabilities, ModelDownload } from '../../lib/gen/inferencerig/control/v1/control_pb';
import ModelsPanel from './ModelsPanel.svelte';

function panel(options: { capabilities?: Partial<BackendCapabilities>; download?: ModelDownload | null } = {}) {
  const state = createInferenceRigState();
  state.selectedBackend = 'mlx';
  state.catalogErrors = ['owner/broken: temporary failure'];

  const app = {
    state,
    capabilities: () => ({ ...NO_CAPABILITIES, ...options.capabilities }),
    activeDownload: () => options.download ?? null,
    canApplyDownload: () => false,
    refreshResourcesAndCatalog: vi.fn(),
    loadModelCatalog: vi.fn(),
    loadLocalModels: vi.fn(),
    useCatalogVariant: vi.fn(),
    resolveModel: vi.fn(),
    startDownload: vi.fn(),
    cancelDownload: vi.fn(),
    applyToProfile: vi.fn(),
    previewApplyToProfile: vi.fn(),
    selectBackend: vi.fn(),
    deleteLocalModel: vi.fn(),
    createModelProfile: vi.fn()
  };
  render(ModelsPanel, { app: app as never });
  return { app, state };
}

function download(overrides: Partial<ModelDownload>): ModelDownload {
  return {
    $typeName: 'inferencerig.control.v1.ModelDownload',
    id: 'dl-1',
    state: 'running',
    multiFile: false,
    targetPath: '/models/qwen-q4.gguf',
    itemCount: 1,
    receivedBytes: 1024n,
    totalBytes: 2048n,
    percent: 50,
    error: '',
    startedAt: '',
    completedAt: '',
    backend: 'llamacpp',
    profile: '',
    ...overrides
  };
}

describe('ModelsPanel', () => {
  afterEach(async () => {
    cleanup();
    await new Promise((resolve) => setTimeout(resolve, 50));
  });

  it('shows partial catalog errors without hiding successful content', async () => {
    panel();
    await userEvent.click(screen.getByRole('tab', { name: 'Catalog' }));
    expect(screen.getByRole('alert')).toHaveTextContent('1 catalog model could not be loaded');
    await userEvent.click(screen.getByRole('button', { name: 'Show details' }));
    expect(screen.getByText('owner/broken: temporary failure')).toBeInTheDocument();
  });

  // multi_file_artifacts: an MLX model is a whole repository of shards, so
  // there is no single filename to show progress against.
  it('reports item count and target root for a multi-file download', async () => {
    panel({
      capabilities: { multiFileArtifacts: true },
      download: download({ multiFile: true, itemCount: 7, targetPath: '/models/mlx/Qwen3-8B-4bit' })
    });
    await userEvent.click(screen.getByRole('tab', { name: 'Catalog' }));
    expect(screen.getByText('7 files')).toBeInTheDocument();
    expect(screen.getByText(/\/models\/mlx\/Qwen3-8B-4bit/)).toBeInTheDocument();
  });

  it('reports a filename for a single-file download', async () => {
    panel({ capabilities: { singleFileArtifacts: true }, download: download({}) });
    await userEvent.click(screen.getByRole('tab', { name: 'Catalog' }));
    expect(screen.getByText('qwen-q4.gguf')).toBeInTheDocument();
  });
});
