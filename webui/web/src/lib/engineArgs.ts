import type { JsonObject, JsonValue } from '@bufbuild/protobuf';
import { ParameterType, type BackendParameter } from './gen/inferencerig/control/v1/control_pb';
import type { EngineArgKind, EngineArgRow } from './types';

// engine_args is a google.protobuf.Struct, so a value can be a string, a
// number, a bool, or a list. The editor keeps each row as text plus a declared
// kind and converts on save; the alternative — guessing the type from the text —
// cannot tell the string "true" from the bool true, and both backends care
// (MLX renders a false bool as --no-key and a list as a repeated flag).

export function kindForParameterType(type: ParameterType | undefined): EngineArgKind {
  switch (type) {
    case ParameterType.INT:
      return 'int';
    case ParameterType.BOOL:
      return 'bool';
    case ParameterType.LIST:
      return 'list';
    default:
      return 'string';
  }
}

function kindOfValue(value: JsonValue): EngineArgKind {
  if (typeof value === 'boolean') return 'bool';
  if (typeof value === 'number') return 'int';
  if (Array.isArray(value)) return 'list';
  return 'string';
}

function textOfValue(value: JsonValue): string {
  if (Array.isArray(value)) return value.map((item) => String(item)).join(', ');
  if (value === null) return '';
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

export function rowsFromEngineArgs(args: JsonObject | undefined): EngineArgRow[] {
  if (!args) return [];
  return Object.entries(args)
    .map(([key, value]) => ({ key, value: textOfValue(value as JsonValue), kind: kindOfValue(value as JsonValue) }))
    .sort((a, b) => a.key.localeCompare(b.key));
}

export function engineArgsFromRows(rows: EngineArgRow[]): JsonObject {
  const args: JsonObject = {};
  for (const row of rows) {
    const key = row.key.trim();
    if (!key) continue;
    args[key] = parseRowValue(row);
  }
  return args;
}

export function parseRowValue(row: EngineArgRow): JsonValue {
  const text = row.value.trim();
  switch (row.kind) {
    case 'bool':
      return text.toLowerCase() !== 'false' && text !== '' && text !== '0';
    case 'int': {
      const parsed = Number(text);
      // A number the user is mid-way through typing stays a string rather than
      // silently becoming NaN, which Struct cannot represent at all.
      return text !== '' && Number.isFinite(parsed) ? parsed : text;
    }
    case 'list':
      return text
        ? text
            .split(',')
            .map((item) => item.trim())
            .filter(Boolean)
        : [];
    default:
      return text;
  }
}

// defaultRowsFor builds a starter row set from a backend's own declared
// parameters: required ones first, then anything carrying a default. This is
// what replaces llamarig's hardcoded llama.cpp flag templates, so a new backend
// gets sensible templates without a UI change.
export function defaultRowsFor(params: BackendParameter[]): EngineArgRow[] {
  return params
    .filter((param) => param.required || param.defaultValue !== '')
    .map((param) => ({
      key: param.name,
      value: param.defaultValue,
      kind: kindForParameterType(param.type)
    }));
}
