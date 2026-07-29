import { describe, expect, it } from 'vitest';
import { parseLogText } from './logs';

describe('parseLogText', () => {
  it('classifies zap JSON lines as daemon entries by level', () => {
    const text = [
      '{"level":"info","ts":1782571471.83,"caller":"cmd/serve.go:48","msg":"starting control rpc","socket":"/run/control.sock"}',
      '{"level":"warn","ts":1782571472,"msg":"listen address is remote-capable","auth_token_env":"INFERENCERIG_CONTROL_TOKEN"}',
      '{"level":"error","ts":1782571473,"msg":"stop runtime","error":"stop timed out","stacktrace":"inferencerig/cmd.shutdown\\n\\tmore"}'
    ].join('\n');

    const { daemon, raw } = parseLogText(text);
    expect(raw).toHaveLength(0);
    expect(daemon.map((entry) => entry.level)).toEqual(['info', 'warn', 'error']);
    expect(daemon[0].msg).toBe('starting control rpc');
    expect(daemon[0].fields).toEqual({ socket: '/run/control.sock' });
    expect(daemon[2].stacktrace).toContain('inferencerig/cmd.shutdown');
  });

  // llamarig classified non-JSON lines by llama.cpp's own `\s([IWE])\s` prefix.
  // The log service is a free-form name now and MLX writes nothing of the sort,
  // so guessing a severity from one backend's format would mislabel the rest.
  it('keeps non-JSON runtime output verbatim without inventing a severity', () => {
    const text = [
      '[53069] 0.09.354 I srv  llama_server: model loaded',
      '[53069] 0.09.355 W srv  update_slots: slot context shift',
      'INFO:     Uvicorn running on http://127.0.0.1:8080'
    ].join('\n');

    const { daemon, raw } = parseLogText(text);
    expect(daemon).toHaveLength(0);
    expect(raw.map((line) => line.text)).toEqual(text.split('\n'));
    expect(raw[0]).not.toHaveProperty('severity');
  });

  it('ignores blank lines and lets a malformed JSON line fall through as raw', () => {
    const { daemon, raw } = parseLogText('\n{"level":}\n\n');
    expect(daemon).toHaveLength(0);
    expect(raw).toHaveLength(1);
  });

  it('rejects JSON without a level, which is not a structured entry', () => {
    const { daemon, raw } = parseLogText('{"msg":"hello"}');
    expect(daemon).toHaveLength(0);
    expect(raw).toHaveLength(1);
  });
});
