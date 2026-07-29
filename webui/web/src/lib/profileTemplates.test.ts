import { describe, expect, it } from 'vitest';
import { modelProfile, templateProfile, templateRows, uniqueProfileName } from './profileTemplates';
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
const llamacppParams = [
  param({ name: 'ctx-size', type: ParameterType.INT, defaultValue: '65536' }),
  param({ name: 'flash-attn', type: ParameterType.BOOL, defaultValue: 'true' }),
  param({ name: 'no-default', type: ParameterType.STRING })
];

const mlxParams = [
  param({ name: 'max-tokens', type: ParameterType.INT, defaultValue: '2048' }),
  param({ name: 'adapter-path', required: true })
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
    const profile = templateProfile('coder', 'llamacpp', 'defaults', llamacppParams);
    expect(profile.backend).toBe('llamacpp');
    expect(profile.engineArgs).toEqual({ 'ctx-size': 65536, 'flash-attn': true });
    // The server renders YAML, so the client never populates it.
    expect(profile.profileYaml).toBe('');
  });
});

describe('modelProfile', () => {
  it('puts the model in model_reference rather than an engine arg', () => {
    const profile = modelProfile('coder', 'mlx', 'mlx-community/Qwen3-8B-4bit', mlxParams);
    expect(profile.modelReference).toBe('mlx-community/Qwen3-8B-4bit');
    expect(profile.engineArgs).not.toHaveProperty('model');
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
