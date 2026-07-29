import { describe, expect, it } from 'vitest';
import { isKnownParamKey, missingRequiredParams, unknownParamKeys } from './profileValidation';
import { ParameterType, type BackendParameter } from './gen/inferencerig/control/v1/control_pb';
import type { EngineArgRow } from './types';

function param(overrides: Partial<BackendParameter> & { name: string }): BackendParameter {
  return {
    $typeName: 'inferencerig.control.v1.BackendParameter',
    description: '',
    required: false,
    aliases: [],
    valueHint: '',
    defaultValue: '',
    type: ParameterType.STRING,
    ...overrides
  };
}

// Validation is now against whatever the backend declares, not a compiled-in
// llama.cpp flag table, so a backend the UI has never heard of validates
// correctly with no UI change.
//
// Names are namespaced exactly as GetBackendParams returns them. An editor row
// holds the bare key, so the two forms have to reconcile; a fixture of bare
// names would pass while the real server response failed.
const params = [
  param({ name: 'engine_args.ctx-size', aliases: ['c'], type: ParameterType.INT, defaultValue: '4096' }),
  param({ name: 'engine_args.threads', aliases: ['t'], type: ParameterType.INT }),
  param({ name: 'engine_args.model-path', required: true }),
  param({ name: 'model.source', required: true }),
  param({ name: 'listen.port', required: true, type: ParameterType.INT })
];

function row(key: string, value = ''): EngineArgRow {
  return { key, value, kind: 'string' };
}

describe('profileValidation', () => {
  it('accepts parameters the backend declares', () => {
    expect(isKnownParamKey('ctx-size', params)).toBe(true);
    expect(isKnownParamKey('threads', params)).toBe(true);
  });

  it('accepts the aliases the backend reports alongside each name', () => {
    expect(isKnownParamKey('c', params)).toBe(true);
    expect(isKnownParamKey('t', params)).toBe(true);
  });

  it('rejects a key the backend does not declare', () => {
    expect(isKnownParamKey('priority', params)).toBe(false);
    expect(isKnownParamKey('--ctx-size', params)).toBe(false);
  });

  it('treats an empty key as valid, since the row is not filled in yet', () => {
    expect(isKnownParamKey('', params)).toBe(true);
    expect(isKnownParamKey('   ', params)).toBe(true);
  });

  // parameter_introspection false means there is nothing to validate against.
  // Inventing a verdict from an empty list would reject every legitimate key.
  it('accepts any key when the backend exposes no parameter list', () => {
    expect(isKnownParamKey('anything-at-all', [])).toBe(true);
    expect(unknownParamKeys([row('anything-at-all')], [])).toEqual([]);
  });

  it('collects unique unknown keys from a draft', () => {
    const rows = [row('ctx-size', '4096'), row('priority', '2'), row('priority', '2'), row('bogus'), row('')];
    expect(unknownParamKeys(rows, params)).toEqual(['priority', 'bogus']);
  });

  it('reports required parameters the draft is missing', () => {
    expect(missingRequiredParams([row('ctx-size')], params)).toEqual(['engine_args.model-path']);
    expect(missingRequiredParams([row('model-path', '/m')], params)).toEqual([]);
  });

  // model.source and listen.port have their own inputs above the argument
  // rows. Measuring them against the rows reported every profile as missing
  // them no matter how completely it was filled in.
  it('does not report profile-level required fields as missing arguments', () => {
    const missing = missingRequiredParams([row('model-path', '/m')], params);
    expect(missing).not.toContain('model.source');
    expect(missing).not.toContain('listen.port');
  });

  // A row holds `ctx-size`; the backend declares `engine_args.ctx-size`.
  it('resolves a bare row key against its namespaced parameter', () => {
    expect(isKnownParamKey('ctx-size', params)).toBe(true);
    expect(isKnownParamKey('engine_args.ctx-size', params)).toBe(true);
    expect(unknownParamKeys([row('ctx-size'), row('threads')], params)).toEqual([]);
  });

  // engine_args.* means the backend forwards arbitrary arguments, so the
  // client has no basis to reject anything.
  it('accepts any key when the backend declares the engine_args wildcard', () => {
    const open = [...params, param({ name: 'engine_args.*' })];
    expect(isKnownParamKey('some-new-engine-flag', open)).toBe(true);
    expect(unknownParamKeys([row('totally-unheard-of')], open)).toEqual([]);
  });
});
