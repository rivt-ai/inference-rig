import { formatBytes } from '../../lib/formatting';
import {
  FitLevel,
  type CatalogModel,
  type FitEstimate,
  type LocalModel,
  type MachineProfile,
  type ModelVariant,
  type Signals
} from '../../lib/gen/inferencerig/control/v1/control_pb';

export function modelMetadataChips(model: CatalogModel): { primary: string[]; capability: string[] } {
  const best = model.bestVariant;
  const primary = [best?.quant || '', best?.sizeBytes ? formatBytes(Number(best.sizeBytes)) : ''].filter(Boolean);
  const capability = new Set<string>();
  for (const tag of model.tags) {
    const value = tag.toLowerCase();
    if (value.includes('image') || value.includes('vision') || value.includes('multimodal')) capability.add('vision');
    if (value.includes('agent') || value.includes('tool') || value.includes('function-calling')) capability.add('agentic');
    if (value.includes('embedding') || value.includes('sentence-transformers')) capability.add('embedding');
  }
  return { primary, capability: Array.from(capability) };
}

// rankedResourceSummary describes the capacity the server ranked against. It
// reads unified_memory off MachineProfile rather than inferring a GPU from
// telemetry, because on a unified-memory host "RAM" and "VRAM" are one number
// and printing both would double-count it.
export function rankedResourceSummary(machine: MachineProfile | null, signals: Signals | null) {
  const total = Number(machine?.totalMemoryBytes || signals?.totalMemoryBytes || 0);
  const available = Number(machine?.availableMemoryBytes || signals?.availableMemoryBytes || 0);
  if (machine?.unifiedMemory) {
    return `${available ? formatBytes(available) : 'unknown'} of ${
      total ? formatBytes(total) : 'unknown'
    } unified memory available`;
  }
  const accelerator = Number(machine?.acceleratorMemoryBytes || 0);
  return `${available ? `${formatBytes(available)} RAM available` : 'RAM unknown'} / ${
    accelerator ? `${formatBytes(accelerator)} VRAM capacity` : 'RAM-only ranking'
  }`;
}

// fitOf returns the server's own FitEstimate for a variant. llamarig computed
// this in the browser as size × 1.1 against whichever number looked like GPU
// memory; that heuristic is wrong on unified memory (where the model and the
// OS share one pool) and wrong for MLX (whose weights are not GGUF-shaped).
// The server now estimates per variant, correctly for both memory models.
export function fitOf(variant: ModelVariant | undefined): FitEstimate | undefined {
  return variant?.fit;
}

export type FitBadge = { label: string; class: string };

export function fitBadge(level: FitLevel | undefined): FitBadge | null {
  switch (level) {
    case FitLevel.FITS:
      return { label: 'Fits', class: 'bg-success/15 text-success border-success/30' };
    case FitLevel.MARGINAL:
      return { label: 'Marginal', class: 'bg-warning/15 text-warning-foreground border-warning/30 dark:text-warning' };
    case FitLevel.TOO_LARGE:
      return { label: 'Too large', class: 'bg-destructive/15 text-destructive border-destructive/30' };
    default:
      // UNKNOWN / UNSPECIFIED get no badge rather than a misleading red one.
      return null;
  }
}

export function fitMeterColor(level: FitLevel | undefined): string {
  switch (level) {
    case FitLevel.FITS:
      return 'bg-success';
    case FitLevel.MARGINAL:
      return 'bg-warning';
    case FitLevel.TOO_LARGE:
      return 'bg-destructive';
    default:
      return 'bg-muted-foreground';
  }
}

// fitMeterWidths turns a server FitEstimate into the two bar widths the meter
// draws, so the UI renders the server's arithmetic instead of redoing it.
export function fitMeterWidths(fit: FitEstimate | undefined) {
  if (!fit) return null;
  const available = Number(fit.availableBytes);
  const required = Number(fit.requiredBytes);
  if (!available) return null;
  return {
    needPct: Math.min(100, (required / available) * 100),
    requiredBytes: required,
    availableBytes: available
  };
}

export type LocalModelFilter = 'all' | 'serving' | 'in_profile' | 'unused';

export function filterLocalModels(models: LocalModel[], filter: LocalModelFilter, activeProfileNames: string[]) {
  if (filter === 'all') return models;
  const active = new Set(activeProfileNames);
  return models.filter((model) => {
    const profiles = model.usedByProfiles;
    const serving = profiles.some((name) => active.has(name));
    if (filter === 'serving') return serving;
    if (filter === 'in_profile') return profiles.length > 0;
    return profiles.length === 0;
  });
}

export function localModelFilterCounts(models: LocalModel[], activeProfileNames: string[]) {
  const active = new Set(activeProfileNames);
  let serving = 0;
  let inProfile = 0;
  let unused = 0;
  for (const model of models) {
    const profiles = model.usedByProfiles;
    if (profiles.some((name) => active.has(name))) serving += 1;
    if (profiles.length > 0) inProfile += 1;
    else unused += 1;
  }
  return { all: models.length, serving, in_profile: inProfile, unused };
}

export function downloadStatusClass(state: string | undefined, error: string | undefined) {
  if (error || state === 'failed') return 'bg-destructive/15 text-destructive border-destructive/30';
  if (state === 'completed' || state === 'already_downloaded') return 'bg-success/15 text-success border-success/30';
  return undefined;
}

// downloadSummary describes progress in the terms the backend actually uses.
// A multi-file artifact (MLX ships a repo of shards, config and tokenizer) has
// no single filename to show, so it reports item count and target root instead.
export function downloadSummary(download: {
  multiFile: boolean;
  itemCount: number;
  targetPath: string;
  receivedBytes: bigint;
  totalBytes: bigint;
}) {
  const bytes = `${formatBytes(Number(download.receivedBytes))} / ${formatBytes(Number(download.totalBytes))}`;
  if (download.multiFile) {
    return {
      title: `${download.itemCount} file${download.itemCount === 1 ? '' : 's'}`,
      detail: `${download.targetPath || 'target root pending'} · ${bytes}`
    };
  }
  return {
    title: download.targetPath.split(/[\\/]/).filter(Boolean).pop() || 'download',
    detail: bytes
  };
}
