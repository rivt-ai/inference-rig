import { describe, expect, it } from 'vitest';
import {
  downloadStatusClass,
  downloadSummary,
  filterLocalModels,
  fitBadge,
  fitMeterWidths,
  localModelFilterCounts,
  modelMetadataChips,
  rankedResourceSummary
} from './catalogPresentation';
import { FitLevel, type CatalogModel, type LocalModel, type MachineProfile } from '../../lib/gen/inferencerig/control/v1/control_pb';

function localModel(path: string, usedByProfiles: string[] = []): LocalModel {
  return {
    $typeName: 'inferencerig.control.v1.LocalModel',
    path,
    filename: path.split('/').pop() || path,
    sizeBytes: 1n,
    modifiedAt: '',
    usedByProfiles
  };
}

function machine(overrides: Partial<MachineProfile>): MachineProfile {
  return {
    $typeName: 'inferencerig.control.v1.MachineProfile',
    totalMemoryBytes: 0n,
    availableMemoryBytes: 0n,
    acceleratorName: '',
    unifiedMemory: false,
    acceleratorMemoryBytes: 0n,
    ...overrides
  };
}

describe('modelMetadataChips', () => {
  it('builds chips from the best variant and tags', () => {
    const model = {
      $typeName: 'inferencerig.control.v1.CatalogModel',
      id: 'a/b',
      url: '',
      downloads: 0n,
      likes: 0n,
      lastModified: '',
      tags: ['text-generation', 'function-calling', 'vision'],
      variants: [],
      bestVariant: {
        $typeName: 'inferencerig.control.v1.ModelVariant',
        name: '4bit',
        reference: 'a/b-4bit',
        sizeBytes: 2048n,
        multiFile: true,
        quant: '4bit'
      }
    } as unknown as CatalogModel;

    expect(modelMetadataChips(model)).toEqual({
      primary: ['4bit', '2.0 KB'],
      capability: ['agentic', 'vision']
    });
  });
});

describe('rankedResourceSummary', () => {
  // On unified memory there is one pool, so reporting "RAM available / VRAM
  // capacity" would quote the same bytes twice under two names.
  it('reports one pool for a unified-memory host', () => {
    const summary = rankedResourceSummary(
      machine({ unifiedMemory: true, totalMemoryBytes: 4096n, availableMemoryBytes: 2048n }),
      null
    );
    expect(summary).toBe('2.0 KB of 4.0 KB unified memory available');
    expect(summary).not.toContain('VRAM');
  });

  it('reports RAM and VRAM separately for a discrete host', () => {
    expect(rankedResourceSummary(machine({ availableMemoryBytes: 1024n, acceleratorMemoryBytes: 2048n }), null)).toContain(
      '2.0 KB VRAM capacity'
    );
  });
});

// Fit comes from the server per variant now. llamarig multiplied file size by
// 1.1 in the browser and compared it to whatever looked like GPU memory, which
// is wrong on unified memory and meaningless for non-GGUF weights.
describe('server fit estimates', () => {
  it('maps every fit level and hides the unknown ones', () => {
    expect(fitBadge(FitLevel.FITS)?.label).toBe('Fits');
    expect(fitBadge(FitLevel.MARGINAL)?.label).toBe('Marginal');
    expect(fitBadge(FitLevel.TOO_LARGE)?.label).toBe('Too large');
    expect(fitBadge(FitLevel.UNKNOWN)).toBeNull();
    expect(fitBadge(FitLevel.UNSPECIFIED)).toBeNull();
    expect(fitBadge(undefined)).toBeNull();
  });

  it('renders the meter from the server numbers without re-deriving them', () => {
    const widths = fitMeterWidths({
      $typeName: 'inferencerig.control.v1.FitEstimate',
      level: FitLevel.FITS,
      reason: 'fits with headroom',
      requiredBytes: 8_000_000_000n,
      availableBytes: 16_000_000_000n
    });
    expect(widths).toEqual({ needPct: 50, requiredBytes: 8_000_000_000, availableBytes: 16_000_000_000 });
  });

  it('caps the bar rather than overflowing when a model exceeds capacity', () => {
    const widths = fitMeterWidths({
      $typeName: 'inferencerig.control.v1.FitEstimate',
      level: FitLevel.TOO_LARGE,
      reason: '',
      requiredBytes: 60_000_000_000n,
      availableBytes: 16_000_000_000n
    });
    expect(widths?.needPct).toBe(100);
  });

  it('renders nothing when the server reported no estimate', () => {
    expect(fitMeterWidths(undefined)).toBeNull();
  });
});

describe('local model filters', () => {
  const models = [localModel('/m/a', ['alpha']), localModel('/m/b', []), localModel('/m/c', ['beta'])];

  it('filters by serving, in-profile and unused', () => {
    expect(filterLocalModels(models, 'serving', ['alpha']).map((m) => m.path)).toEqual(['/m/a']);
    expect(filterLocalModels(models, 'in_profile', ['alpha']).map((m) => m.path)).toEqual(['/m/a', '/m/c']);
    expect(filterLocalModels(models, 'unused', ['alpha']).map((m) => m.path)).toEqual(['/m/b']);
    expect(filterLocalModels(models, 'all', []).length).toBe(3);
  });

  it('counts each filter bucket', () => {
    expect(localModelFilterCounts(models, ['alpha'])).toEqual({ all: 3, serving: 1, in_profile: 2, unused: 1 });
  });
});

describe('downloadStatusClass', () => {
  it('maps download states emitted by the API', () => {
    expect(downloadStatusClass('completed', undefined)).toContain('success');
    expect(downloadStatusClass('already_downloaded', undefined)).toContain('success');
    expect(downloadStatusClass('failed', undefined)).toContain('destructive');
    expect(downloadStatusClass('cancelled', undefined)).toBeUndefined();
  });
});

// multi_file_artifacts: an MLX model is a directory of shards plus config and
// tokenizer, so there is no single filename to report progress against.
describe('downloadSummary', () => {
  it('reports item count and target root for a multi-file artifact', () => {
    const summary = downloadSummary({
      multiFile: true,
      itemCount: 7,
      targetPath: '/models/mlx/Qwen3-8B-4bit',
      receivedBytes: 1024n,
      totalBytes: 2048n
    });
    expect(summary.title).toBe('7 files');
    expect(summary.detail).toContain('/models/mlx/Qwen3-8B-4bit');
  });

  it('reports the filename for a single-file artifact', () => {
    const summary = downloadSummary({
      multiFile: false,
      itemCount: 1,
      targetPath: '/models/gguf/qwen-q4.gguf',
      receivedBytes: 1024n,
      totalBytes: 2048n
    });
    expect(summary.title).toBe('qwen-q4.gguf');
  });
});
