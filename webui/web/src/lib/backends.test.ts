import { describe, expect, it } from 'vitest';
import {
  NO_CAPABILITIES,
  capabilitiesFor,
  installUnavailableReason,
  showsTemperature,
  showsUtilization,
  singleActiveProfileWarning,
  usesUnifiedMemory
} from './backends';
import type { Accelerator, BackendInfo, Signals } from './gen/inferencerig/control/v1/control_pb';

function capabilities(overrides: Partial<typeof NO_CAPABILITIES> = {}) {
  return { ...NO_CAPABILITIES, ...overrides };
}

function backend(name: string, overrides: Partial<typeof NO_CAPABILITIES> = {}): BackendInfo {
  return { $typeName: 'inferencerig.control.v1.BackendInfo', name, capabilities: capabilities(overrides) };
}

function accelerator(overrides: Partial<Accelerator> = {}): Accelerator {
  return {
    $typeName: 'inferencerig.control.v1.Accelerator',
    name: 'device',
    unifiedMemory: false,
    totalMemoryBytes: 0n,
    usedMemoryBytes: 0n,
    utilizationPercent: 0,
    hasUtilization: false,
    temperatureCelsius: 0,
    hasTemperature: false,
    source: 'test',
    ...overrides
  };
}

function signals(accelerators: Accelerator[]): Signals {
  return { accelerators } as unknown as Signals;
}

describe('capabilitiesFor', () => {
  it('falls back to all-off for an unknown backend rather than assuming llama.cpp', () => {
    expect(capabilitiesFor([backend('mlx', { unifiedMemory: true })], 'nope')).toEqual(NO_CAPABILITIES);
  });

  it('returns the named backend capabilities', () => {
    const backends = [backend('llamacpp', { discreteVram: true }), backend('mlx', { unifiedMemory: true })];
    expect(capabilitiesFor(backends, 'mlx').unifiedMemory).toBe(true);
    expect(capabilitiesFor(backends, 'llamacpp').unifiedMemory).toBe(false);
  });
});

// The meter collapse is a correctness fix, not cosmetics: on Apple silicon the
// GPU's bytes ARE system RAM, so two meters report one allocation twice.
describe('usesUnifiedMemory', () => {
  it('collapses the meters when every accelerator reports unified memory', () => {
    expect(usesUnifiedMemory(signals([accelerator({ unifiedMemory: true })]), capabilities())).toBe(true);
  });

  it('keeps separate meters for a discrete device even on a unified-memory backend', () => {
    expect(usesUnifiedMemory(signals([accelerator({ unifiedMemory: false })]), capabilities({ unifiedMemory: true }))).toBe(false);
  });

  it('keeps separate meters when a host mixes unified and discrete devices', () => {
    const mixed = signals([accelerator({ unifiedMemory: true }), accelerator({ unifiedMemory: false })]);
    expect(usesUnifiedMemory(mixed, capabilities({ unifiedMemory: true }))).toBe(false);
  });

  it('falls back to the backend capability when the host reports no accelerator', () => {
    expect(usesUnifiedMemory(signals([]), capabilities({ unifiedMemory: true }))).toBe(true);
    expect(usesUnifiedMemory(null, capabilities())).toBe(false);
  });
});

describe('optional accelerator readings', () => {
  it('renders only on the has_* companion flag, since zero reads as idle', () => {
    expect(showsUtilization(accelerator({ utilizationPercent: 0, hasUtilization: true }))).toBe(true);
    expect(showsUtilization(accelerator({ utilizationPercent: 0, hasUtilization: false }))).toBe(false);
    expect(showsTemperature(accelerator({ temperatureCelsius: 0, hasTemperature: true }))).toBe(true);
    expect(showsTemperature(accelerator({ temperatureCelsius: 62, hasTemperature: false }))).toBe(false);
  });
});

describe('singleActiveProfileWarning', () => {
  it('warns which profile a start will displace', () => {
    expect(singleActiveProfileWarning(capabilities({ singleActiveProfile: true }), ['alpha'], 'beta')).toContain('alpha');
  });

  it('stays silent for a backend that runs profiles concurrently', () => {
    expect(singleActiveProfileWarning(capabilities(), ['alpha'], 'beta')).toBeNull();
  });

  it('stays silent when nothing else is running or the target is already active', () => {
    const single = capabilities({ singleActiveProfile: true });
    expect(singleActiveProfileWarning(single, [], 'beta')).toBeNull();
    expect(singleActiveProfileWarning(single, ['beta'], 'beta')).toBeNull();
  });
});

describe('installUnavailableReason', () => {
  it('explains the MLX installer up front on an unsupported host', () => {
    expect(installUnavailableReason('mlx', 'linux')).toContain('macOS');
  });

  it('permits install on macOS and for other backends', () => {
    expect(installUnavailableReason('mlx', 'darwin')).toBeNull();
    expect(installUnavailableReason('llamacpp', 'linux')).toBeNull();
  });
});
