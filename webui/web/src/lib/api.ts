import { createClient, type Interceptor } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-web';
import { ControlService, type FitLevel, type Profile } from './gen/inferencerig/control/v1/control_pb';
import type { SessionState } from './session';

export type InferenceRigApi = ReturnType<typeof createApiClient>;

export function apiUrl(path: string, apiBase: string, locationProtocol = window.location.protocol) {
  const base = apiBase.trim().replace(/\/$/, '');
  if (!base) return path;
  const normalized = /^https?:\/\//i.test(base) ? base : `${locationProtocol}//${base}`;
  return new URL(path, normalized).toString();
}

export type CatalogQuery = {
  backend?: string;
  query?: string;
  limit?: number;
  sort?: string;
  minFit?: FitLevel;
};

// createApiClient exposes the whole control surface over Connect. Unlike the
// llamarig client there is no REST half: logs, archives and everything else are
// RPCs, so there is one transport, one auth path, and one error shape.
export function createApiClient(getSession: () => SessionState, fetcher: typeof fetch = fetch) {
  const auth: Interceptor = (next) => (request) => {
    const token = getSession().token.trim();
    if (token) request.header.set('Authorization', `Bearer ${token}`);
    return next(request);
  };
  const control = () =>
    createClient(
      ControlService,
      createConnectTransport({
        baseUrl: apiUrl('/', getSession().apiBase).replace(/\/$/, '') || '/',
        fetch: fetcher,
        interceptors: [auth]
      })
    );

  return {
    health: () => control().health({}),
    getInfo: () => control().getInfo({}),
    listBackends: () => control().listBackends({}),
    getBackendParams: (backend: string) => control().getBackendParams({ backend }),
    getBackendInstallStatus: (backend: string) => control().getBackendInstallStatus({ backend }),
    installBackend: (backend: string, options: { version?: string; upgrade?: boolean; force?: boolean } = {}) =>
      control().installBackend({ backend, ...options }),

    getRuntimeStatus: (profile = '') => control().getRuntimeStatus({ profile }),
    startRuntime: (profile: string) => control().startRuntime({ profile }),
    stopRuntime: (profile = '') => control().stopRuntime({ profile }),
    restartRuntime: (profile: string) => control().restartRuntime({ profile }),

    listProfiles: () => control().listProfiles({}),
    getProfile: (name: string) => control().getProfile({ name }),
    // profileYaml is deliberately left empty: the server renders YAML from the
    // structured Profile, so the editor never needs a YAML writer of its own
    // and cannot disagree with the server about how a profile serializes.
    putProfile: (profile: Profile, createOnly = false) =>
      control().putProfile({ name: profile.name, profile, profileYaml: '', createOnly }),
    deleteProfile: (name: string) => control().deleteProfile({ name }),
    cleanupProfile: (name: string) => control().cleanupProfile({ name }),
    setProfileAutostart: (name: string, enabled: boolean) => control().setProfileAutostart({ name, enabled }),

    getSignals: () => control().getSignals({}),
    listEvents: () => control().listEvents({}),
    watchEvents: (signal: AbortSignal) => control().watchEvents({}, { signal }),

    listModelCatalog: (params: CatalogQuery = {}) =>
      control().listModelCatalog({
        backend: params.backend,
        query: params.query,
        limit: params.limit,
        sort: params.sort,
        minFit: params.minFit
      }),
    watchModelCatalog: (signal: AbortSignal) => control().watchModelCatalog({}, { signal }),
    estimateFit: (backend: string, sizeBytes: bigint) => control().estimateFit({ backend, sizeBytes }),
    resolveModel: (backend: string, reference: string, variantReference = '') =>
      control().resolveModel({ backend, reference, variantReference }),
    resolveProfileModel: (profile: string) => control().resolveProfileModel({ profile }),
    listLocalModels: (backend = '') => control().listLocalModels({ backend }),
    deleteLocalModel: (backend: string, path: string, cascadeProfiles = false) =>
      control().deleteLocalModel({ backend, path, cascadeProfiles }),

    startModelDownload: (request: {
      backend?: string;
      reference?: string;
      variantReference?: string;
      profile?: string;
      force?: boolean;
    }) =>
      control().startModelDownload(request),
    getModelDownload: (id: string) => control().getModelDownload({ id }),
    cancelModelDownload: (id: string) => control().cancelModelDownload({ id }),
    applyDownloadToProfile: (id: string, profile: string, preview = false) =>
      control().applyDownloadToProfile({ id, profile, preview }),

    getLogs: (service: string, lines: number) => control().getLogs({ service, lines }),
    watchLogs: (service: string, signal: AbortSignal) => control().watchLogs({ service }, { signal }),
    listLogArchives: () => control().listLogArchives({}),
    getLogArchive: (id: string, lines: number) => control().getLogArchive({ id, lines }),
    deleteLogArchive: (id: string) => control().deleteLogArchive({ id }),
    clearLogArchives: () => control().clearLogArchives({})
  };
}
