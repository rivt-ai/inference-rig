import type { Signals } from './gen/inferencerig/control/v1/control_pb';

export type RuntimeHistorySample = {
  capturedAt: string;
  cpu: number | null;
  memory: number | null;
  accelerators: Array<{
    key: string;
    utilization: number | null;
    memory: number | null;
    temperature: number | null;
  }>;
};

export function appendRuntimeSample(history: RuntimeHistorySample[], signals: Signals | null, limit = 60) {
  if (!signals) return history;
  const capturedAt = signals.capturedAt || new Date().toISOString();
  if (history[history.length - 1]?.capturedAt === capturedAt) return history;
  return [
    ...history,
    {
      capturedAt,
      cpu: value(signals.cpuUsedPercent),
      memory: value(signals.usedMemoryPercent || percentOf(signals.usedMemoryBytes, signals.totalMemoryBytes)),
      // Utilisation and temperature are recorded only when the collector says
      // it actually read them; a zero is otherwise charted as a real idle
      // sample and permanently flattens the trend line.
      accelerators: signals.accelerators.map((accelerator, index) => ({
        key: acceleratorKey(accelerator.source, accelerator.name, index),
        utilization: accelerator.hasUtilization ? value(accelerator.utilizationPercent) : null,
        memory: value(percentOf(accelerator.usedMemoryBytes, accelerator.totalMemoryBytes)),
        temperature: accelerator.hasTemperature ? value(accelerator.temperatureCelsius, false) : null
      }))
    }
  ].slice(-limit);
}

export function acceleratorKey(source: string | undefined, name: string | undefined, index: number) {
  return `${source || 'accelerator'}:${name || 'device'}:${index}`;
}

function percentOf(used: bigint | number | undefined, total: bigint | number | undefined): number | null {
  const totalValue = Number(total || 0);
  if (!totalValue) return null;
  return (Number(used || 0) / totalValue) * 100;
}

function value(input: number | null | undefined, clamp = true) {
  if (input == null || !Number.isFinite(input)) return null;
  return clamp ? Math.max(0, Math.min(100, input)) : input;
}
