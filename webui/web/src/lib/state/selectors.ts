import { basename } from '../formatting';
import type { Profile } from '../gen/inferencerig/control/v1/control_pb';

// profileTarget describes what a profile serves. The model is a first-class
// field now rather than a `model`/`models-dir` engine arg, so this no longer
// digs through backend-specific flag names.
export function profileTarget(profile: Profile) {
  const reference = profile.modelReference || '';
  if (!reference) return 'unconfigured';
  return basename(reference) || reference;
}

export function isProfileActive(activeProfileNames: string[], name: string) {
  return activeProfileNames.includes(name);
}
