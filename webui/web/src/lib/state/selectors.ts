import { basename } from '../formatting';
import type { Profile } from '../gen/inferencerig/control/v1/control_pb';

// profileTarget describes what a profile serves. The model is a first-class
// field now rather than a `model`/`models-dir` engine arg, so this no longer
// digs through backend-specific flag names.
// The reference is optional — it selects a file inside a repository — while the
// source is what every profile must have. Reading only the reference reported a
// fully configured profile as unconfigured, and applying a download (which
// clears the reference) made a working profile look broken.
export function profileTarget(profile: Profile) {
  const target = profile.modelReference || profile.modelSource || '';
  if (!target) return 'unconfigured';
  return basename(target) || target;
}

export function isProfileActive(activeProfileNames: string[], name: string) {
  return activeProfileNames.includes(name);
}
