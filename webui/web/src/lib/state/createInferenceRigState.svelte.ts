import type {
  BackendInfo,
  BackendParameter,
  CatalogCacheState,
  CatalogModel,
  CommandResult,
  Disk,
  Event as ServerEvent,
  GetBackendInstallStatusResponse,
  LocalModel,
  LogArchive,
  MachineProfile,
  ModelDownload,
  Profile,
  ResolvedModel,
  ArtifactPlan,
  Signals
} from '../gen/inferencerig/control/v1/control_pb';
import { FitLevel } from '../gen/inferencerig/control/v1/control_pb';
import type { EngineArgRow, ProfileApplyPreview } from '../types';
import type { RuntimeHistorySample } from '../runtimeHistory';
import { CONTROL_LOG_SERVICE, ENGINE_LOG_SERVICE } from '../logs';

export function createInferenceRigState() {
  const state = $state({
    apiBase: '',
    token: '',
    busy: false,
    activeSection: 'runtime',

    // True only when the gateway serves unauthenticated AND this browser
    // reached it over the network. Loopback insecure mode is the ordinary
    // single-user case and must not nag; a gateway anyone on the network can
    // drive is something the user has to be told, every page, permanently.
    insecureExposed: false,

    // Backend awareness. selectedBackend drives every capability gate; it is
    // the axis llamarig did not have, because there was only ever llama.cpp.
    backends: [] as BackendInfo[],
    selectedBackend: '',
    backendParams: [] as BackendParameter[],
    backendInstall: null as GetBackendInstallStatusResponse | null,
    hostPlatform: '',

    profiles: [] as Profile[],
    // Distinguishes "no profiles exist" from "profiles have not loaded yet",
    // so the first-run banner cannot flash before the first ListProfiles.
    profilesLoaded: false,
    selectedProfileName: '',
    activeProfileNames: [] as string[],
    activeBackend: '',
    profileRuntimeStates: {} as Record<string, string>,
    autostartProfiles: [] as string[],
    defaultProfile: '',
    currentProfile: null as Profile | null,

    modelReference: '',
    modelResolution: null as ResolvedModel | null,
    modelPlan: null as ArtifactPlan | null,
    catalogQuery: {
      limit: 50,
      sort: 'downloads',
      query: '',
      minFit: FitLevel.FITS as FitLevel
    },
    catalogModels: [] as CatalogModel[],
    catalogMachine: null as MachineProfile | null,
    catalogCache: null as CatalogCacheState | null,
    catalogErrors: [] as string[],
    catalogLoading: false,
    selectedCatalogModelId: '',
    selectedVariantReference: '',

    signals: null as Signals | null,
    signalsLastError: '',
    runtimeHistory: [] as RuntimeHistorySample[],

    localModels: [] as LocalModel[],
    localModelsLoading: false,
    activeModelDownloadId: '',
    profileApplyPreview: null as ProfileApplyPreview | null,
    downloads: {} as Record<string, ModelDownload>,

    draftRows: [] as EngineArgRow[],
    originals: {
      rows: [] as EngineArgRow[]
    },
    dirty: {
      rows: false
    },

    logEntries: [] as string[],
    logText: '',
    // A free-form service name, matching the proto: which services exist
    // depends on which backends are installed, so this is not a closed union.
    logService: CONTROL_LOG_SERVICE,
    // The engine tab's service. Backends write their runtime output under this
    // name, kept apart from the control daemon's own structured log.
    engineLogService: ENGINE_LOG_SERVICE,
    engineLogText: '',
    logServices: [CONTROL_LOG_SERVICE, ENGINE_LOG_SERVICE] as string[],
    logLines: 500,
    logPaused: false,
    logArchives: [] as LogArchive[],
    selectedLogArchiveId: '',
    logArchiveText: '',

    serverEvents: [] as ServerEvent[],
    lastOperation: null as CommandResult | null,
    disks: [] as Disk[],
    runtimeStatus: {
      status: 'unknown',
      detail: '-',
      checkedAt: ''
    }
  });
  return state;
}

export type InferenceRigState = ReturnType<typeof createInferenceRigState>;
