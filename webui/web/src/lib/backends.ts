import type { Accelerator, BackendCapabilities, BackendInfo, Profile, Signals } from './gen/inferencerig/control/v1/control_pb';

// NO_CAPABILITIES is the conservative default used before ListBackends has
// answered, or for a backend the server did not describe. Every flag off means
// every panel falls back to its least-assuming form rather than guessing that
// whatever the previous UI assumed (llama.cpp) is still true.
export const NO_CAPABILITIES: BackendCapabilities = {
  $typeName: 'inferencerig.control.v1.BackendCapabilities',
  singleFileArtifacts: false,
  multiFileArtifacts: false,
  discreteVram: false,
  unifiedMemory: false,
  managedInstall: false,
  singleActiveProfile: false,
  parameterIntrospection: false
};

export function capabilitiesFor(backends: BackendInfo[], name: string): BackendCapabilities {
  return backends.find((backend) => backend.name === name)?.capabilities ?? NO_CAPABILITIES;
}

// usesUnifiedMemory decides whether RAM and VRAM are the same bytes. It is
// driven by the host's own accelerator reading first and the backend's declared
// capability second — never by a platform sniff, which would be wrong for a
// discrete GPU on a Mac and for an Apple-silicon host running llama.cpp.
export function usesUnifiedMemory(signals: Signals | null, capabilities: BackendCapabilities): boolean {
  const accelerators = signals?.accelerators ?? [];
  if (accelerators.length) return accelerators.every((accelerator) => accelerator.unifiedMemory);
  return capabilities.unifiedMemory;
}

// showsUtilization / showsTemperature gate optional accelerator readings on the
// has_* companion booleans. A zero utilisation reading is indistinguishable
// from an idle GPU, so the presence flag is the only honest signal to render on.
export function showsUtilization(accelerator: Accelerator): boolean {
  return accelerator.hasUtilization;
}

export function showsTemperature(accelerator: Accelerator): boolean {
  return accelerator.hasTemperature;
}

// singleActiveProfileWarning explains, before the fact, that starting a profile
// on a single-active-profile backend (MLX) stops whatever is already running.
// Returning null means there is nothing to warn about: either the backend
// permits concurrent profiles, or nothing else is running.
export function singleActiveProfileWarning(
  capabilities: BackendCapabilities,
  activeProfileNames: string[],
  target: string
): string | null {
  if (!capabilities.singleActiveProfile) return null;
  const displaced = activeProfileNames.filter((name) => name !== target);
  if (!displaced.length) return null;
  return `${displaced.join(', ')} will be stopped: this backend runs one profile at a time.`;
}

// runtimeReplacementWarning also covers router processes: one process binds
// the address of the profile that started it, so activating a profile on a
// different address requires replacing that process even when the backend can
// otherwise hold several profiles.
export function runtimeReplacementWarning(
  capabilities: BackendCapabilities,
  profiles: Profile[],
  activeProfileNames: string[],
  target: string
): string | null {
  const exclusive = singleActiveProfileWarning(capabilities, activeProfileNames, target);
  if (exclusive) return exclusive;
  const targetProfile = profiles.find((profile) => profile.name === target);
  if (!targetProfile) return null;
  const displaced = profiles.filter(
    (profile) =>
      activeProfileNames.includes(profile.name) &&
      profile.name !== target &&
      profile.backend === targetProfile.backend &&
      (profile.host !== targetProfile.host || profile.port !== targetProfile.port)
  );
  if (!displaced.length) return null;
  return `${displaced.map((profile) => profile.name).join(', ')} will be stopped: the target uses a different listen address.`;
}

// installUnavailableReason explains a managed installer that cannot run here,
// instead of letting the user press a button that is designed to fail. MLX's
// installer is macOS/arm64-only by construction.
export function installUnavailableReason(backend: string, platform: string | undefined): string | null {
  if (backend !== 'mlx') return null;
  const host = (platform || '').toLowerCase();
  if (!host) return null;
  if (host.includes('darwin') || host.includes('mac')) return null;
  return `The MLX installer only runs on macOS on Apple silicon; this host reports ${platform}.`;
}
