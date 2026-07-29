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

// DEFAULT_HOST and DEFAULT_PORT seed a new profile's listen address. The server
// fills in nothing — a profile must name a valid port — so a create form that
// omits them is rejected outright.
export const DEFAULT_HOST = '127.0.0.1';
export const DEFAULT_PORT = 8080;

export function templateProfile(
  name: string,
  backend: string,
  kind: ProfileTemplate,
  params: BackendParameter[],
  listen: { modelSource: string; modelReference?: string; host?: string; port?: number }
): Profile {
  return create(ProfileSchema, {
    name,
    backend,
    modelSource: listen.modelSource,
    modelReference: listen.modelReference || '',
    host: listen.host || DEFAULT_HOST,
    port: listen.port || DEFAULT_PORT,
    engineArgs: engineArgsFromRows(templateRows(kind, params))
  });
}

// nextFreePort keeps a new profile off a port another profile already claims,
// which is the difference between "created" and "created and unstartable".
export function nextFreePort(existing: { port?: number }[], start = DEFAULT_PORT) {
  const used = new Set(existing.map((item) => item.port).filter(Boolean));
  for (let port = start; port <= 65535; port += 1) {
    if (!used.has(port)) return port;
  }
  return start;
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
  port = DEFAULT_PORT
): Profile {
  return create(ProfileSchema, {
    name,
    backend,
    // A local model is addressed by its own path. There is no separate
    // reference: the file on disk IS the artifact, and a placeholder source
    // with the path in reference resolves to a fetch with no host.
    modelSource: reference,
    host: DEFAULT_HOST,
    port,
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
