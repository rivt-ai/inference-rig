import { describe, expect, it, vi } from 'vitest';
import { apiUrl, createApiClient } from './api';
import { FitLevel } from './gen/inferencerig/control/v1/control_pb';

const connectResponse = (body = '{}', status = 200) =>
  new Response(body, { status, headers: { 'Content-Type': 'application/json' } });

describe('apiUrl', () => {
  it('uses same-origin paths for empty base', () => {
    expect(apiUrl('/health', '')).toBe('/health');
  });

  it('normalizes hosts without protocol', () => {
    expect(apiUrl('/health', '127.0.0.1:7000', 'http:')).toBe('http://127.0.0.1:7000/health');
  });
});

describe('createApiClient', () => {
  it('adds bearer token only when present', async () => {
    let captured: RequestInit | undefined;
    const fetcher: typeof fetch = async (_input, init) => {
      captured = init;
      return connectResponse();
    };
    const api = createApiClient(() => ({ apiBase: '', token: ' secret ' }), fetcher);

    await api.getInfo();

    expect((captured?.headers as Headers).get('Authorization')).toBe('Bearer secret');
  });

  it('omits the header entirely when no token is configured', async () => {
    let captured: RequestInit | undefined;
    const fetcher: typeof fetch = async (_input, init) => {
      captured = init;
      return connectResponse();
    };
    const api = createApiClient(() => ({ apiBase: '', token: '   ' }), fetcher);

    await api.getInfo();

    expect((captured?.headers as Headers).get('Authorization')).toBeNull();
  });

  it('includes Connect error code and message', async () => {
    const fetcher = vi.fn(async () => connectResponse(JSON.stringify({ code: 'invalid_argument', message: 'nope' }), 400));
    const api = createApiClient(() => ({ apiBase: '', token: '' }), fetcher as unknown as typeof fetch);

    await expect(api.getInfo()).rejects.toThrow('[invalid_argument] nope');
  });

  it('routes every call at the InferenceRig control service', async () => {
    const calls: string[] = [];
    const fetcher: typeof fetch = async (input) => {
      calls.push(String(input));
      return connectResponse();
    };
    const api = createApiClient(() => ({ apiBase: '', token: '' }), fetcher);

    await api.getSignals();
    await api.listBackends();

    expect(calls).toEqual([
      '/inferencerig.control.v1.ControlService/GetSignals',
      '/inferencerig.control.v1.ControlService/ListBackends'
    ]);
  });

  it('encodes confirmed replacement and reset runtime calls', async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    const fetcher: typeof fetch = async (input, init) => {
      const url = String(input);
      calls.push({ url, body: JSON.parse(await new Request(new URL(url, 'http://localhost'), init).text()) });
      return connectResponse();
    };
    const api = createApiClient(() => ({ apiBase: '', token: '' }), fetcher);

    await api.startRuntime('coder', true);
    await api.resetRuntimes();

    expect(calls).toEqual([
      { url: '/inferencerig.control.v1.ControlService/StartRuntime', body: { profile: 'coder', replace: true } },
      { url: '/inferencerig.control.v1.ControlService/ResetRuntimes', body: {} }
    ]);
  });

  it('sends typed catalog query fields', async () => {
    let captured = '';
    let body = '';
    const fetcher: typeof fetch = async (input, init) => {
      captured = String(input);
      body = await new Request(new URL(captured, 'http://localhost'), init).text();
      return connectResponse();
    };
    const api = createApiClient(() => ({ apiBase: '', token: '' }), fetcher);

    await api.listModelCatalog({ backend: 'mlx', limit: 25, sort: 'fit', query: 'qwen coder', minFit: FitLevel.MARGINAL });

    expect(captured).toBe('/inferencerig.control.v1.ControlService/ListModelCatalog');
    expect(JSON.parse(body)).toEqual({
      backend: 'mlx',
      limit: 25,
      sort: 'fit',
      query: 'qwen coder',
      minFit: 'FIT_LEVEL_MARGINAL'
    });
  });

  // Logs used to be five hand-rolled REST endpoints alongside the Connect
  // client. They are RPCs now, so they carry the same auth and error handling
  // as everything else instead of a parallel implementation of both.
  it('issues log and archive calls over Connect, not REST', async () => {
    const calls: Array<{ url: string; body: unknown }> = [];
    const fetcher: typeof fetch = async (input, init) => {
      const url = String(input);
      calls.push({ url, body: JSON.parse(await new Request(new URL(url, 'http://localhost'), init).text()) });
      return connectResponse();
    };
    const api = createApiClient(() => ({ apiBase: '', token: '' }), fetcher);

    await api.getLogs('mlx-runtime', 2000);
    await api.getLogArchive('archive-1', 200);
    await api.clearLogArchives();

    expect(calls).toEqual([
      { url: '/inferencerig.control.v1.ControlService/GetLogs', body: { service: 'mlx-runtime', lines: 2000 } },
      { url: '/inferencerig.control.v1.ControlService/GetLogArchive', body: { id: 'archive-1', lines: 200 } },
      { url: '/inferencerig.control.v1.ControlService/ClearLogArchives', body: {} }
    ]);
  });

  // The server renders the YAML from the structured profile, so profile_yaml
  // must go out empty; sending both would let the editor disagree with it.
  it('sends the structured profile and an empty profile_yaml', async () => {
    let body: Record<string, unknown> = {};
    const fetcher: typeof fetch = async (input, init) => {
      body = JSON.parse(await new Request(new URL(String(input), 'http://localhost'), init).text());
      return connectResponse();
    };
    const api = createApiClient(() => ({ apiBase: '', token: '' }), fetcher);

    await api.putProfile({
      $typeName: 'inferencerig.control.v1.Profile',
      name: 'coder',
      backend: 'mlx',
      profileYaml: 'stale: true',
      modelSource: 'local',
      modelReference: '/models/coder',
      host: '127.0.0.1',
      port: 8080,
      engineArgs: { 'max-tokens': 512, trust: true }
    });

    expect(body.profileYaml).toBeUndefined();
    expect(body.profile).toMatchObject({
      name: 'coder',
      backend: 'mlx',
      engineArgs: { 'max-tokens': 512, trust: true }
    });
  });
});
