// Parses a service log tail into the two views the UI shows: structured
// entries and everything else, verbatim.
//
// The control daemon writes slog's text format (`time=… level=… msg=…`), not
// JSON, so that is the primary shape parsed here; JSON records are still
// accepted because the handler is a one-line change away and older archives
// hold them.
//
// llamarig also classified unstructured lines by a `\s([IWE])\s` regex, which
// is llama.cpp's own log prefix. MLX writes nothing of the sort, and the log
// service is a free-form name now, so a severity guessed from one backend's
// format would mislabel every other backend's output. Raw lines stay raw.

// The two service logs the UI always shows a tab for. They mirror
// config.LogServiceControl and config.LogServiceEngine on the Go side: a
// service log is named after the process that writes it, and the control
// daemon is detached under the project name.
export const CONTROL_LOG_SERVICE = 'inferencerig';
export const ENGINE_LOG_SERVICE = 'engine';

export type ZapEntry = {
  level: string;
  ts: number;
  msg: string;
  caller: string;
  stacktrace: string;
  fields: Record<string, unknown>;
};

export type RawLine = {
  text: string;
};

const reserved = new Set(['level', 'ts', 'msg', 'caller', 'stacktrace']);

export function parseLogText(text: string): { daemon: ZapEntry[]; raw: RawLine[] } {
  const daemon: ZapEntry[] = [];
  const raw: RawLine[] = [];
  for (const line of text.split('\n')) {
    if (!line.trim()) continue;
    const entry = parseZapLine(line) ?? parseSlogLine(line);
    if (entry) daemon.push(entry);
    else raw.push({ text: line });
  }
  return { daemon, raw };
}

// Splits a slog text line into key=value pairs, honouring the double quotes
// slog adds around any value containing whitespace or an equals sign.
function slogPairs(line: string): Map<string, string> | null {
  const pairs = new Map<string, string>();
  let i = 0;
  while (i < line.length) {
    while (i < line.length && line[i] === ' ') i++;
    if (i >= line.length) break;
    const eq = line.indexOf('=', i);
    if (eq < 0) return null;
    const key = line.slice(i, eq);
    // A key never contains a space or a quote; anything else means this is not
    // a slog line and must not be shown as structured.
    if (!key || /[\s"]/.test(key)) return null;
    i = eq + 1;
    let value: string;
    if (line[i] === '"') {
      let j = i + 1;
      let out = '';
      while (j < line.length && line[j] !== '"') {
        if (line[j] === '\\' && j + 1 < line.length) j++;
        out += line[j];
        j++;
      }
      if (j >= line.length) return null;
      value = out;
      i = j + 1;
    } else {
      let j = i;
      while (j < line.length && line[j] !== ' ') j++;
      value = line.slice(i, j);
      i = j;
    }
    pairs.set(key, value);
  }
  return pairs.size ? pairs : null;
}

function parseSlogLine(line: string): ZapEntry | null {
  const pairs = slogPairs(line);
  if (!pairs) return null;
  const level = pairs.get('level');
  // time and level are what slog always emits first; without them this is some
  // engine's own output that merely happens to contain an equals sign.
  if (!level || !pairs.has('time')) return null;
  const parsedTime = Date.parse(pairs.get('time') ?? '');
  const fields: Record<string, unknown> = {};
  for (const [key, value] of pairs) {
    if (key !== 'time' && key !== 'level' && key !== 'msg' && key !== 'source') fields[key] = value;
  }
  return {
    level: level.toLowerCase(),
    ts: Number.isNaN(parsedTime) ? 0 : parsedTime / 1000,
    msg: pairs.get('msg') ?? '',
    caller: pairs.get('source') ?? '',
    stacktrace: '',
    fields
  };
}

function parseZapLine(line: string): ZapEntry | null {
  let parsed: Record<string, unknown>;
  try {
    parsed = JSON.parse(line);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== 'object' || typeof parsed.level !== 'string' || !parsed.level) return null;
  const fields: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(parsed)) {
    if (!reserved.has(key)) fields[key] = value;
  }
  return {
    level: parsed.level,
    ts: typeof parsed.ts === 'number' ? parsed.ts : 0,
    msg: typeof parsed.msg === 'string' ? parsed.msg : '',
    caller: typeof parsed.caller === 'string' ? parsed.caller : '',
    stacktrace: typeof parsed.stacktrace === 'string' ? parsed.stacktrace : '',
    fields
  };
}
