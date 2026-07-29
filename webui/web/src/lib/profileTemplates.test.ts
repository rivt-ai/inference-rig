import { describe, expect, it } from 'vitest';
import { modelProfile, nextFreePort, templateProfile, templateRows, uniqueProfileName } from './profileTemplates';
import { defaultRowsFor } from './engineArgs';
import { ParameterType, type BackendParameter } from './gen/inferencerig/control/v1/control_pb';

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

// Templates are per-backend now: they are built from the backend's own
// declared defaults instead of the literal llama.cpp flag list llamarig
// shipped, which was meaningless for MLX.
//
// Parameter names are namespaced exactly as the backends report them
// (`engine_args.ctx-size`, `model.source`). Fixtures that dropped the namespace
// let a change through that wrote profile-level fields into engine_args.
const llamacppParams = [
  param({ name: 'model.source', required: true }),
  param({ name: 'listen.port', required: true }),
  param({ name: 'engine_args.*' }),
  param({ name: 'engine_args.ctx-size', type: ParameterType.INT, defaultValue: '65536' }),
  param({ name: 'engine_args.flash-attn', type: ParameterType.BOOL, defaultValue: 'true' }),
  param({ name: 'engine_args.no-default', type: ParameterType.STRING })
];

const mlxParams = [
  param({ name: 'engine_args.max-tokens', type: ParameterType.INT, defaultValue: '2048' }),
  param({ name: 'engine_args.adapter-path', required: true })
];

describe('templateRows', () => {
  it('seeds llama.cpp defaults from what llama.cpp itself declares', () => {
    const rows = templateRows('defaults', llamacppParams);
    expect(rows).toEqual([
      { key: 'ctx-size', value: '65536', kind: 'int' },
      { key: 'flash-attn', value: 'true', kind: 'bool' }
    ]);
  });

  it('seeds a different backend from its own parameters, sharing no flag names', () => {
    const rows = templateRows('defaults', mlxParams);
    expect(rows.map((row) => row.key)).toEqual(['max-tokens', 'adapter-path']);
    expect(rows.some((row) => row.key === 'ctx-size')).toBe(false);
  });

  it('creates no rows for a blank template', () => {
    expect(templateRows('blank', llamacppParams)).toEqual([]);
  });

  it('creates no rows when the backend cannot enumerate its parameters', () => {
    expect(templateRows('defaults', [])).toEqual([]);
  });
});

describe('templateProfile', () => {
  it('carries the backend and typed engine args into the structured profile', () => {
    const profile = templateProfile('coder', 'llamacpp', 'defaults', llamacppParams, {
      modelSource: 'owner/repo'
    });
    expect(profile.backend).toBe('llamacpp');
    expect(profile.engineArgs).toEqual({ 'ctx-size': 65536, 'flash-attn': true });
    // The server renders YAML, so the client never populates it.
    expect(profile.profileYaml).toBe('');
  });

  // The server fills in nothing: a profile without a model source or a valid
  // port is rejected, which is what made every "New profile" fail.
  it('always carries a model source and a listen port', () => {
    const profile = templateProfile('coder', 'llamacpp', 'blank', [], {
      modelSource: 'owner/repo',
      modelReference: 'model.gguf'
    });
    expect(profile.modelSource).toBe('owner/repo');
    expect(profile.modelReference).toBe('model.gguf');
    expect(profile.host).toBe('127.0.0.1');
    expect(profile.port).toBeGreaterThan(0);
  });
});

describe('modelProfile', () => {
  // A local model is addressed by its own path: a placeholder source with the
  // path in reference resolves to a fetch URI with no host.
  it('puts the local path in model_source rather than an engine arg', () => {
    const profile = modelProfile('coder', 'mlx', '/models/Qwen3-8B-4bit', mlxParams);
    expect(profile.modelSource).toBe('/models/Qwen3-8B-4bit');
    expect(profile.port).toBeGreaterThan(0);
    expect(profile.engineArgs).not.toHaveProperty('model');
  });
});

describe('nextFreePort', () => {
  it('skips ports existing profiles already claim', () => {
    expect(nextFreePort([{ port: 8080 }, { port: 8081 }])).toBe(8082);
    expect(nextFreePort([])).toBe(8080);
  });
});

describe('uniqueProfileName', () => {
  it('derives a safe unique name and disambiguates collisions', () => {
    expect(uniqueProfileName('Qwen 2.5 (Q4)', [{ name: 'Qwen-2.5-Q4' }])).toBe('Qwen-2.5-Q4-2');
  });

  // No .gguf stripping: an MLX reference is a repo id with no filename, and
  // the special case only ever helped one backend.
  it('uses the last path segment of a multi-file reference', () => {
    expect(uniqueProfileName('mlx-community/Qwen3-8B-4bit', [])).toBe('Qwen3-8B-4bit');
  });
});

describe('defaultRowsFor', () => {
  // The parameter list describes the whole profile, not just engine args. Rows
  // carrying model.source or listen.port write them into engine_args, where the
  // backend ignores them while the real fields stay empty.
  it('offers only bare engine arguments', () => {
    expect(defaultRowsFor(llamacppParams).map((row) => row.key)).toEqual(['ctx-size', 'flash-attn']);
  });
});
