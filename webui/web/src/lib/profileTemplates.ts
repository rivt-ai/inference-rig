import { create } from '@bufbuild/protobuf';
import { ProfileSchema, type BackendParameter, type Profile } from './gen/inferencerig/control/v1/control_pb';
import { defaultRowsFor, engineArgsFromRows } from './engineArgs';
import type { EngineArgRow } from './types';

// Templates are derived from whatever the chosen backend declares, not from a
// hardcoded llama.cpp flag list. "blank" is the empty profile; "defaults" seeds
// every parameter the backend marks required or gives a default value.
export type ProfileTemplate = 'blank' | 'defaults';

export const profileTemplates: { value: ProfileTemplate; label: string }[] = [
  { value: 'defaults', label: 'Backend defaults' },
  { value: 'blank', label: 'Blank' }
];

export function templateRows(kind: ProfileTemplate, params: BackendParameter[]): EngineArgRow[] {
  if (kind === 'blank') return [];
  return defaultRowsFor(params);
}

export function templateProfile(
  name: string,
  backend: string,
  kind: ProfileTemplate,
  params: BackendParameter[]
): Profile {
  return create(ProfileSchema, {
    name,
    backend,
    engineArgs: engineArgsFromRows(templateRows(kind, params))
  });
}

// modelProfile builds a profile that serves one already-downloaded model:
// backend defaults plus the model reference the server resolved. The model
// itself lives in model_source/model_reference, not in engine_args, so this is
// backend-neutral — no `model` flag, no .gguf assumption.
export function modelProfile(
  name: string,
  backend: string,
  reference: string,
  params: BackendParameter[],
  source = 'local'
): Profile {
  return create(ProfileSchema, {
    name,
    backend,
    modelSource: source,
    modelReference: reference,
    engineArgs: engineArgsFromRows(defaultRowsFor(params))
  });
}

// uniqueProfileName derives a safe, unused profile name from a model reference.
// There is no extension stripping: a reference can be an MLX repo id with no
// filename at all, and stripping ".gguf" specifically only ever helped one
// backend.
export function uniqueProfileName(reference: string, existing: { name: string }[]) {
  const stem = reference.split(/[\\/]/).filter(Boolean).pop() || reference;
  const base = stem.replace(/[^A-Za-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '') || 'model';
  const used = new Set(existing.map((item) => item.name));
  if (!used.has(base)) return base;
  for (let suffix = 2; ; suffix += 1) {
    const candidate = `${base}-${suffix}`;
    if (!used.has(candidate)) return candidate;
  }
}
