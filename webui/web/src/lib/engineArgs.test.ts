import { describe, expect, it } from 'vitest';
import { defaultRowsFor, engineArgsFromRows, kindForParameterType, rowsFromEngineArgs } from './engineArgs';
import { ParameterType, type BackendParameter } from './gen/inferencerig/control/v1/control_pb';

describe('rowsFromEngineArgs', () => {
  // engine_args is a Struct, so the editor has to preserve the JSON type as
  // well as the text: MLX renders a false bool as --no-key and a list as a
  // repeated flag, and neither survives being flattened to a string.
  it('recovers the JSON type of every value', () => {
    expect(
      rowsFromEngineArgs({ 'ctx-size': 4096, trust: false, stop: ['</s>', 'END'], name: 'qwen' })
    ).toEqual([
      { key: 'ctx-size', value: '4096', kind: 'int' },
      { key: 'name', value: 'qwen', kind: 'string' },
      { key: 'stop', value: '</s>, END', kind: 'list' },
      { key: 'trust', value: 'false', kind: 'bool' }
    ]);
  });

  it('returns no rows for a profile with no engine args', () => {
    expect(rowsFromEngineArgs(undefined)).toEqual([]);
  });
});

describe('engineArgsFromRows', () => {
  it('round-trips back into typed Struct values', () => {
    const args = { 'ctx-size': 4096, trust: false, stop: ['</s>', 'END'], name: 'qwen' };
    expect(engineArgsFromRows(rowsFromEngineArgs(args))).toEqual(args);
  });

  it('keeps a false bool as a bool rather than the string "false"', () => {
    expect(engineArgsFromRows([{ key: 'trust', value: 'false', kind: 'bool' }])).toEqual({ trust: false });
    expect(engineArgsFromRows([{ key: 'trust', value: 'true', kind: 'bool' }])).toEqual({ trust: true });
  });

  it('drops unnamed rows the user has not finished typing', () => {
    expect(engineArgsFromRows([{ key: '  ', value: 'x', kind: 'string' }])).toEqual({});
  });

  it('keeps an unparseable number as text rather than emitting NaN, which Struct cannot hold', () => {
    expect(engineArgsFromRows([{ key: 'ctx-size', value: '12e', kind: 'int' }])).toEqual({ 'ctx-size': '12e' });
  });

  it('produces an empty list, not [""], for an empty list field', () => {
    expect(engineArgsFromRows([{ key: 'stop', value: '', kind: 'list' }])).toEqual({ stop: [] });
  });
});

describe('kindForParameterType', () => {
  it('maps the proto parameter types onto editor kinds', () => {
    expect(kindForParameterType(ParameterType.INT)).toBe('int');
    expect(kindForParameterType(ParameterType.BOOL)).toBe('bool');
    expect(kindForParameterType(ParameterType.LIST)).toBe('list');
    expect(kindForParameterType(ParameterType.UNSPECIFIED)).toBe('string');
    expect(kindForParameterType(undefined)).toBe('string');
  });
});

describe('defaultRowsFor', () => {
  // Names are namespaced as the backends report them; rows hold the bare key.
  it('seeds required engine arguments and those carrying a default', () => {
    const params: BackendParameter[] = [
      {
        $typeName: 'inferencerig.control.v1.BackendParameter',
        name: 'engine_args.adapter',
        description: '',
        required: true,
        aliases: [],
        valueHint: '',
        defaultValue: '',
        type: ParameterType.STRING
      },
      {
        $typeName: 'inferencerig.control.v1.BackendParameter',
        name: 'engine_args.max-tokens',
        description: '',
        required: false,
        aliases: [],
        valueHint: '',
        defaultValue: '512',
        type: ParameterType.INT
      },
      {
        $typeName: 'inferencerig.control.v1.BackendParameter',
        name: 'engine_args.optional',
        description: '',
        required: false,
        aliases: [],
        valueHint: '',
        defaultValue: '',
        type: ParameterType.STRING
      }
    ];
    expect(defaultRowsFor(params)).toEqual([
      { key: 'adapter', value: '', kind: 'string' },
      { key: 'max-tokens', value: '512', kind: 'int' }
    ]);
  });
});
