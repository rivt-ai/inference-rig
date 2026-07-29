import type { BackendParameter } from './gen/inferencerig/control/v1/control_pb';
import type { EngineArgRow } from './types';

// Validation is now entirely server-described: a key is legitimate if the
// backend's own GetBackendParams response names it, directly or as an alias.
// llamarig validated against a 128-flag llama.cpp table compiled into the
// bundle plus a ROUTER_PRESET_KEYS allowlist and an LLAMA_ARG_* env-var form;
// none of that generalises past one backend, and all of it went stale whenever
// llama.cpp changed. The proto now carries aliases, so the client does not need
// its own copy of any backend's vocabulary.

// Backends namespace their parameters (`engine_args.ctx-size`, `listen.port`)
// but an editor row holds the bare engine-arg key the user types (`ctx-size`),
// so every lookup has to try the namespaced form too.
export const ENGINE_ARG_PREFIX = 'engine_args.';
const ENGINE_ARG_WILDCARD = `${ENGINE_ARG_PREFIX}*`;

export function createParamLookup(params: BackendParameter[]) {
  const index = new Map<string, BackendParameter>();
  for (const param of params) {
    index.set(param.name, param);
    // Index the bare form as well so a row key resolves without the namespace.
    if (param.name.startsWith(ENGINE_ARG_PREFIX)) {
      index.set(param.name.slice(ENGINE_ARG_PREFIX.length), param);
    }
    for (const alias of param.aliases) index.set(alias, param);
  }
  return (key: string) => index.get(key.trim());
}

// A backend that declares `engine_args.*` is telling us it forwards arbitrary
// arguments to its engine, so the client has no basis to reject any key.
export function acceptsAnyEngineArg(params: BackendParameter[]): boolean {
  return params.some((param) => param.name === ENGINE_ARG_WILDCARD);
}

export function isKnownParamKey(key: string, params: BackendParameter[]): boolean {
  const trimmed = key.trim();
  // An empty key is a row the user has not filled in yet, not an error.
  if (!trimmed) return true;
  // With no introspection available there is nothing to check against, so the
  // client must not invent a verdict; free-text keys are accepted as typed.
  if (!params.length) return true;
  if (acceptsAnyEngineArg(params)) return true;
  return Boolean(createParamLookup(params)(trimmed));
}

export function unknownParamKeys(rows: EngineArgRow[], params: BackendParameter[]): string[] {
  const unknown: string[] = [];
  for (const row of rows) {
    const trimmed = row.key.trim();
    if (!trimmed) continue;
    if (!isKnownParamKey(trimmed, params) && !unknown.includes(trimmed)) unknown.push(trimmed);
  }
  return unknown;
}

// missingRequiredParams reports declared-required parameters the draft omits.
// The old UI had no equivalent: llama.cpp's table carried no required flag.
//
// Only engine_args parameters are checked here. Required profile-level fields
// such as model.source and listen.port live in their own inputs, not in these
// rows, so measuring them against the rows reports every profile as missing
// them however well configured it is.
export function missingRequiredParams(rows: EngineArgRow[], params: BackendParameter[]): string[] {
  const present = new Set(rows.map((row) => row.key.trim()).filter(Boolean));
  return params
    .filter(
      (param) =>
        param.required &&
        param.name.startsWith(ENGINE_ARG_PREFIX) &&
        param.name !== ENGINE_ARG_WILDCARD &&
        !present.has(param.name) &&
        !present.has(param.name.slice(ENGINE_ARG_PREFIX.length))
    )
    .map((param) => param.name);
}
