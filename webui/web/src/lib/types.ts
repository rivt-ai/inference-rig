// View-model types only. Everything that travels on the wire is a generated
// Protobuf-ES message from lib/gen, so there are no hand-written mirrors of
// server shapes here and no snake_case translation layer to keep in sync.

// EngineArgRow is one editable row in the engine_args editor. engine_args is a
// google.protobuf.Struct — arbitrary JSON — so a row keeps the raw text the
// user typed plus the declared type used to parse it back into JSON.
export type EngineArgKind = 'string' | 'int' | 'bool' | 'list';

export type EngineArgRow = {
  key: string;
  value: string;
  kind: EngineArgKind;
};

export type ProfileApplyPreview = {
  original: string;
  updated: string;
};

export type Section = {
  id: string;
  label: string;
};

export type RuntimeSummary = {
  status: string;
  detail: string;
  checkedAt: string;
};
