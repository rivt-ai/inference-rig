import { describe, expect, it } from 'vitest';
import { appendRuntimeSample, type RuntimeHistorySample } from './runtimeHistory';
import type { Accelerator, Signals } from './gen/inferencerig/control/v1/control_pb';

function accelerator(overrides: Partial<Accelerator> = {}): Accelerator {
  return {
    $typeName: 'inferencerig.control.v1.Accelerator',
    name: 'A',
    unifiedMemory: false,
    totalMemoryBytes: 0n,
    usedMemoryBytes: 0n,
    utilizationPercent: 0,
    hasUtilization: false,
    temperatureCelsius: 0,
    hasTemperature: false,
    source: 'nvidia',
    ...overrides
  };
}

function signals(overrides: Partial<Signals> = {}): Signals {
  return { accelerators: [], ...overrides } as unknown as Signals;
}

describe('appendRuntimeSample', () => {
  it('keeps timestamps, clamps outliers, and samples each accelerator independently', () => {
    const history = appendRuntimeSample(
      [],
      signals({
        capturedAt: '2026-07-11T12:00:00Z',
        cpuUsedPercent: 125,
        accelerators: [
          accelerator({
            name: 'A',
            totalMemoryBytes: 100n,
            usedMemoryBytes: 25n,
            utilizationPercent: 40,
            hasUtilization: true,
            temperatureCelsius: 63,
            hasTemperature: true
          }),
          accelerator({ name: 'B' })
        ]
      })
    );

    expect(history[0]).toMatchObject({ capturedAt: '2026-07-11T12:00:00Z', cpu: 100, memory: null });
    expect(history[0].accelerators[0]).toMatchObject({ key: 'nvidia:A:0', utilization: 40, memory: 25, temperature: 63 });
    expect(history[0].accelerators[1]).toMatchObject({ utilization: null, memory: null, temperature: null });
  });

  // A zero utilisation reading with has_utilization false is "not measured",
  // not "idle"; charting it as zero permanently flattens the trend line.
  it('records an unreported reading as a gap, not a zero', () => {
    const history = appendRuntimeSample(
      [],
      signals({
        capturedAt: 't',
        accelerators: [accelerator({ utilizationPercent: 0, hasUtilization: false, temperatureCelsius: 0, hasTemperature: false })]
      })
    );
    expect(history[0].accelerators[0].utilization).toBeNull();
    expect(history[0].accelerators[0].temperature).toBeNull();
  });

  it('deduplicates captures and caps history at 60 samples', () => {
    let history: RuntimeHistorySample[] = [];
    for (let index = 0; index < 61; index++) {
      history = appendRuntimeSample(history, signals({ capturedAt: String(index), cpuUsedPercent: index }));
    }
    expect(history).toHaveLength(60);
    expect(history[0].capturedAt).toBe('1');
    expect(appendRuntimeSample(history, signals({ capturedAt: '60' }))).toBe(history);
  });

  it('ignores a null snapshot', () => {
    expect(appendRuntimeSample([], null)).toEqual([]);
  });
});
