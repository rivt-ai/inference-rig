// Parses a service log tail into the two views the UI shows: structured zap
// entries and everything else, verbatim.
//
// llamarig also classified non-JSON lines by a `\s([IWE])\s` regex, which is
// llama.cpp's own log prefix. MLX writes nothing of the sort, and the log
// service is a free-form name now, so a severity guessed from one backend's
// format would mislabel every other backend's output. Raw lines stay raw.

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
    const entry = parseZapLine(line);
    if (entry) daemon.push(entry);
    else raw.push({ text: line });
  }
  return { daemon, raw };
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
