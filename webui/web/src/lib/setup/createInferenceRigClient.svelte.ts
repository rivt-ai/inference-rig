import { create } from '@bufbuild/protobuf';
import { toast } from 'svelte-sonner';
import { apiUrl, createApiClient } from '../api';
import { isLoopbackHost, loadSession, saveApiBase, saveToken, takeLaunchToken } from '../session';
import { canApplyDownload, chooseProfileSelection, isTerminalDownloadState } from '../tasks';
import { capabilitiesFor, singleActiveProfileWarning } from '../backends';
import { engineArgsFromRows, rowsFromEngineArgs } from '../engineArgs';
import { modelProfile, nextFreePort, templateProfile, type ProfileTemplate } from '../profileTemplates';
import { createInferenceRigState } from '../state/createInferenceRigState.svelte';
import { isProfileActive as selectIsProfileActive } from '../state/selectors';
import { appendRuntimeSample } from '../runtimeHistory';
import { CONTROL_LOG_SERVICE, ENGINE_LOG_SERVICE } from '../logs';
import {
  ProfileSchema,
  type CatalogModel,
  type LocalModel,
  type ModelDownload,
  type ModelVariant,
  type Profile,
  type Signals
} from '../gen/inferencerig/control/v1/control_pb';

export function createInferenceRigClient() {
  const state = createInferenceRigState();
  const api = createApiClient(() => ({ apiBase: state.apiBase, token: state.token }));
  let errorMessage = $state('');
  let pollTimer: number | null = null;
  let refreshTimer: number | null = null;
  let catalogEvents: AbortController | null = null;

  const client = {
    state,
    api,
    sections: [
      { id: 'runtime', label: 'Dashboard' },
      { id: 'profiles', label: 'Profiles' },
      { id: 'models', label: 'Models' },
      { id: 'logs', label: 'Logs' }
    ],
    get errorMessage() {
      return errorMessage;
    },
    set errorMessage(value: string) {
      errorMessage = value;
    },
    mount,
    destroy,
    saveApiBase: () => saveApiBase(sessionStorage, state.apiBase),
    saveToken: () => saveToken(sessionStorage, state.token),
    hasDirtyEditors,
    beforeUnload,
    log,
    showError,
    runTask,
    testConnection,
    capabilities,
    selectBackend,
    loadBackends,
    loadBackendParams,
    loadBackendInstallStatus,
    installBackend,
    startWarning,
    refreshRuntimeStatus,
    refreshSignals,
    refreshEvents,
    refreshLogs,
    resumeLogs,
    loadLogArchives,
    selectLogArchive,
    deleteLogArchive,
    clearLogArchives,
    startSelectedProfile,
    startProfile,
    restartSelectedProfile,
    restartProfile,
    stopRuntime,
    stopProfile,
    loadProfiles,
    selectProfile,
    populateProfile,
    createProfile,
    createModelProfile,
    duplicateProfile,
    deleteProfile,
    cleanupProfile,
    toggleAutostart,
    deleteLocalModel,
    reloadSelectedProfile,
    saveProfile,
    afterProfileMutation,
    resolveModel,
    loadModelCatalog,
    loadLocalModels,
    refreshResourcesAndCatalog,
    connectCatalogEvents,
    closeCatalogEvents,
    useCatalogVariant,
    startDownload,
    cancelDownload,
    previewApplyToProfile,
    applyToProfile,
    startPolling,
    stopPolling,
    pollDownload,
    requireSelectedProfile,
    requireCurrentProfile,
    requireDownload,
    runtimeHint,
    isProfileActive,
    activeDownload,
    canApplyDownload,
    clearLogs: () => {
      state.logEntries = [];
    }
  };

  async function mount() {
    const session = loadSession(sessionStorage);
    state.apiBase = session.apiBase;
    // A `#token=…` launch URL is how `inferencerig web` hands the browser its
    // credential, so it wins over whatever this tab had stored.
    const launchToken = takeLaunchToken(window.location, window.history);
    state.token = launchToken || session.token;
    if (launchToken) saveToken(sessionStorage, launchToken);
    window.addEventListener('beforeunload', beforeUnload);
    await refreshAuthPosture();
    await runTask('initial load', async () => {
      await loadBackends();
      await refreshServerInfo();
      await refreshRuntimeStatus();
      await refreshSignals();
      await loadProfiles({ force: true });
      await loadLocalModels();
      await loadModelCatalog();
      await refreshEvents();
    });
    connectCatalogEvents();
    refreshTimer = window.setInterval(() => {
      refreshRuntimeStatus().catch(showError);
      refreshSignals().catch(() => undefined);
      refreshEvents().catch(() => undefined);
      if (state.activeSection === 'logs' && !state.logPaused) refreshLogs().catch(() => undefined);
    }, 5000);
  }

  function destroy() {
    window.removeEventListener('beforeunload', beforeUnload);
    if (refreshTimer) window.clearInterval(refreshTimer);
    closeCatalogEvents();
    stopPolling();
  }

  function beforeUnload(event: BeforeUnloadEvent) {
    if (!hasDirtyEditors()) return;
    event.preventDefault();
    event.returnValue = '';
  }

  function hasDirtyEditors() {
    return state.dirty.rows;
  }

  function log(message: string) {
    state.logEntries = [`${new Date().toLocaleTimeString()} ${message}`, ...state.logEntries].slice(0, 200);
  }

  function showError(err: unknown) {
    errorMessage = err instanceof Error ? err.message : String(err);
  }

  async function runTask(label: string, task: () => Promise<void>) {
    if (state.busy) return;
    state.busy = true;
    errorMessage = '';
    try {
      await task();
    } catch (err) {
      showError(err);
      log(`${label} failed: ${err instanceof Error ? err.message : err}`);
      toast.error(`${label} failed`, { description: err instanceof Error ? err.message : String(err) });
    } finally {
      state.busy = false;
    }
  }

  // The auth posture comes from the plain /health route rather than the Health
  // RPC because it must be readable before the UI holds a token — that is the
  // case the banner exists for. A gateway that cannot be reached at all is the
  // normal error path and says nothing about its posture, so it stays silent.
  async function refreshAuthPosture() {
    try {
      const response = await fetch(apiUrl('/health', state.apiBase));
      const health = await response.json();
      state.insecureExposed = health?.auth === 'disabled' && !isLoopbackHost(window.location.hostname);
    } catch {
      state.insecureExposed = false;
    }
  }

  async function testConnection() {
    await runTask('test connection', async () => {
      const health = await api.health();
      log(`Connection ok: ${health.service || 'control'}`);
      await refreshRuntimeStatus();
    });
  }

  function capabilities() {
    return capabilitiesFor(state.backends, state.selectedBackend);
  }

  async function loadBackends() {
    const data = await api.listBackends();
    state.backends = data.backends;
    if (!state.selectedBackend || !data.backends.some((backend) => backend.name === state.selectedBackend)) {
      state.selectedBackend = data.backends[0]?.name || '';
    }
    await loadBackendParams();
    await loadBackendInstallStatus();
  }

  async function selectBackend(name: string) {
    if (name === state.selectedBackend) return;
    state.selectedBackend = name;
    await runTask('switch backend', async () => {
      await loadBackendParams();
      await loadBackendInstallStatus();
      await loadLocalModels();
      await loadModelCatalog();
    });
  }

  // parameter_introspection off means the backend cannot enumerate its own
  // parameters, so the UI keeps an empty list and falls back to a free-text key
  // field instead of pretending an empty combobox means "no valid keys".
  async function loadBackendParams() {
    if (!state.selectedBackend || !capabilities().parameterIntrospection) {
      state.backendParams = [];
      return;
    }
    try {
      const data = await api.getBackendParams(state.selectedBackend);
      state.backendParams = data.params;
    } catch {
      state.backendParams = [];
    }
  }

  async function loadBackendInstallStatus() {
    if (!state.selectedBackend || !capabilities().managedInstall) {
      state.backendInstall = null;
      return;
    }
    try {
      state.backendInstall = await api.getBackendInstallStatus(state.selectedBackend);
    } catch (err) {
      state.backendInstall = null;
      showError(err);
    }
  }

  async function installBackend(options: { upgrade?: boolean } = {}) {
    await runTask(options.upgrade ? 'upgrade backend' : 'install backend', async () => {
      const result = await api.installBackend(state.selectedBackend, options);
      toast.success(result.changed ? `${state.selectedBackend} ${result.version}` : 'Already up to date', {
        description: result.message || result.path
      });
      await loadBackendInstallStatus();
      await loadBackendParams();
    });
  }

  // startWarning is what makes single_active_profile visible before the fact
  // rather than after: on MLX, starting B stops A, and the user is told so.
  function startWarning(name: string) {
    return singleActiveProfileWarning(capabilities(), state.activeProfileNames, name);
  }

  async function refreshServerInfo() {
    const info = await api.getInfo();
    state.autostartProfiles = info.autostartProfiles;
    state.defaultProfile = info.autostartProfiles[0] || '';
    return info;
  }

  async function refreshRuntimeStatus() {
    const status = await api.getRuntimeStatus();
    state.activeProfileNames = status.profiles
      .filter((profile) => profile.name && profile.status?.state !== 'stopped' && profile.status?.state !== 'failed')
      .map((profile) => profile.name);
    state.runtimeStatus.status = status.status?.state || 'stopped';
    state.runtimeStatus.detail = status.status?.detail || '-';
    state.runtimeStatus.checkedAt = status.status?.checkedAt || '';
    errorMessage = '';
  }

  async function refreshSignals() {
    try {
      const data = await api.getSignals();
      state.signals = data.signals || null;
      state.disks = data.signals?.disks || [];
      state.hostPlatform = data.signals?.host?.platform || data.signals?.host?.os || '';
      appendRuntimeHistory(state.signals);
      state.signalsLastError = '';
    } catch (err) {
      state.signalsLastError = err instanceof Error ? err.message : String(err);
    }
  }

  function appendRuntimeHistory(signals: Signals | null) {
    state.runtimeHistory = appendRuntimeSample(state.runtimeHistory, signals);
  }

  async function refreshEvents() {
    const data = await api.listEvents();
    state.serverEvents = data.events;
  }

  const activeLogRequests = new Set<string>();
  // Control and engine are separate files, so both are fetched. A service with
  // no log yet is a 404 from GetLogs; that is the normal state before a runtime
  // has ever started and must leave the other pane's content alone.
  async function refreshLogs() {
    await Promise.all([
      fetchLog(state.logService, (text) => (state.logText = text)),
      fetchLog(state.engineLogService, (text) => (state.engineLogText = text))
    ]);
  }

  async function fetchLog(service: string, assign: (text: string) => void) {
    if (!service || activeLogRequests.has(service)) return;
    activeLogRequests.add(service);
    try {
      const data = await api.getLogs(service, state.logLines);
      assign(data.text || '');
    } catch {
      assign('');
    } finally {
      activeLogRequests.delete(service);
    }
  }

  async function resumeLogs() {
    state.logPaused = false;
    try {
      await refreshLogs();
    } catch (err) {
      showError(err);
    }
  }

  async function loadLogArchives() {
    const data = await api.listLogArchives();
    state.logArchives = data.archives;
    // The archive list is the only place the set of live services is
    // discoverable, since the proto deliberately leaves it open-ended.
    const services = new Set([
      CONTROL_LOG_SERVICE,
      ENGINE_LOG_SERVICE,
      ...data.archives.map((archive) => archive.service).filter(Boolean)
    ]);
    state.logServices = Array.from(services);
  }

  async function selectLogArchive(id: string) {
    try {
      const data = await api.getLogArchive(id, state.logLines);
      state.selectedLogArchiveId = id;
      state.logArchiveText = data.text || '';
    } catch (err) {
      showError(err);
    }
  }

  async function deleteLogArchive(id: string) {
    await runTask('delete log archive', async () => {
      await api.deleteLogArchive(id);
      if (state.selectedLogArchiveId === id) {
        state.selectedLogArchiveId = '';
        state.logArchiveText = '';
      }
      await loadLogArchives();
    });
  }

  async function clearLogArchives() {
    await runTask('clear log archives', async () => {
      await api.clearLogArchives();
      state.selectedLogArchiveId = '';
      state.logArchiveText = '';
      await loadLogArchives();
    });
  }

  async function startSelectedProfile() {
    await startProfile(requireSelectedProfile());
  }

  async function startProfile(name: string) {
    await runTask('start profile', async () => {
      const response = await api.startRuntime(name);
      state.lastOperation = response.result || null;
      log(`Started ${name}.`);
      toast.success(`Started ${name}`);
      await refreshRuntimeStatus();
      await loadProfiles({ select: name, force: true });
    });
  }

  async function restartSelectedProfile() {
    await restartProfile(requireSelectedProfile());
  }

  async function restartProfile(name: string) {
    await runTask('restart profile', async () => {
      const response = await api.restartRuntime(name);
      state.lastOperation = response.started || null;
      log(`Restarted ${name}.`);
      toast.success(`Restarted ${name}`);
      await refreshRuntimeStatus();
      await loadProfiles({ select: name, force: true });
    });
  }

  async function stopRuntime() {
    await runTask('stop runtime', async () => {
      const response = await api.stopRuntime();
      state.lastOperation = response.result || null;
      log('Stopped active profiles.');
      toast.success('Stopped active profiles');
      await refreshRuntimeStatus();
      await loadProfiles({ force: true });
    });
  }

  async function stopProfile(name: string) {
    await runTask('stop profile', async () => {
      const response = await api.stopRuntime(name);
      state.lastOperation = response.result || null;
      log(`Stopped ${name}.`);
      toast.success(`Stopped ${name}`);
      await refreshRuntimeStatus();
      await loadProfiles({ select: name, force: true });
    });
  }

  async function loadProfiles(options: { select?: string; force?: boolean; preserveDirty?: boolean } = {}) {
    const task = async () => {
      const data = await api.listProfiles();
      state.profiles = data.profiles;
      state.profilesLoaded = true;
      const target = chooseProfileSelection(
        state.profiles,
        options.select || '',
        state.activeProfileNames,
        state.defaultProfile,
        state.selectedProfileName
      );
      const preserveCurrent = options.preserveDirty && state.dirty.rows && target === state.selectedProfileName;
      if (target && !preserveCurrent) {
        await selectProfile(target, { force: true, skipDirtyCheck: options.force });
      }
      log('Profiles loaded.');
    };
    if (options.force || state.busy) return task();
    return runTask('load profiles', task);
  }

  async function selectProfile(name: string, options: { force?: boolean; skipDirtyCheck?: boolean } = {}) {
    const load = async () => {
      const data = await api.getProfile(name);
      populateProfile(data.profile || null);
      state.selectedProfileName = name;
      state.profileApplyPreview = null;
    };
    if (options.force || state.busy) return load();
    return runTask('select profile', load);
  }

  function populateProfile(profile: Profile | null) {
    state.currentProfile = profile;
    const rows = rowsFromEngineArgs(profile?.engineArgs);
    state.originals.rows = rows.map((row) => ({ ...row }));
    state.draftRows = rows.map((row) => ({ ...row }));
    state.dirty.rows = false;
    // Switching to a profile switches the capability context with it: a profile
    // names its own backend, and every gate below reads selectedBackend.
    if (profile?.backend && profile.backend !== state.selectedBackend) {
      state.selectedBackend = profile.backend;
      void loadBackendParams();
      void loadBackendInstallStatus();
    }
  }

  async function createProfile(
    profileName: string,
    template: ProfileTemplate,
    model: { source: string; reference?: string; host?: string; port?: number }
  ) {
    await runTask('create profile', async () => {
      const name = profileName.trim();
      if (!name) throw new Error('profile name is required');
      const source = model.source.trim();
      // The server requires a model source and a valid port on every profile.
      // Failing here names the missing field; failing there returns a generic
      // invalid-profile error after a round trip.
      if (!source) throw new Error('model source is required');
      await api.putProfile(
        templateProfile(name, state.selectedBackend, template, state.backendParams, {
          modelSource: source,
          modelReference: model.reference?.trim(),
          host: model.host?.trim(),
          port: model.port || nextFreePort(state.profiles)
        }),
        true
      );
      log(`Created profile ${name}.`);
      toast.success(`Created ${name}`);
      await loadProfiles({ select: name, force: true });
      // A new profile changes which local models are in use, so the model list
      // is stale the moment this returns.
      await loadLocalModels();
    });
  }

  async function createModelProfile(profileName: string, reference: string) {
    await runTask('create model profile', async () => {
      const name = profileName.trim();
      if (!name) throw new Error('profile name is required');
      await api.putProfile(
        modelProfile(name, state.selectedBackend, reference, state.backendParams, nextFreePort(state.profiles)),
        true
      );
      log(`Created profile ${name}.`);
      toast.success(`Created ${name}`);
      await loadProfiles({ select: name, force: true });
      await loadLocalModels();
    });
  }

  async function duplicateProfile(profileName: string) {
    await runTask('duplicate profile', async () => {
      const current = requireCurrentProfile();
      const name = profileName.trim();
      if (!name) throw new Error('profile name is required');
      await api.putProfile(create(ProfileSchema, { ...current, name, profileYaml: '' }), true);
      log(`Duplicated ${current.name} to ${name}.`);
      toast.success(`Duplicated ${current.name}`);
      await loadProfiles({ select: name, force: true });
    });
  }

  async function deleteProfile() {
    await runTask('delete profile', async () => {
      const current = requireCurrentProfile();
      await api.deleteProfile(current.name);
      log(`Deleted profile ${current.name}.`);
      toast.success(`Deleted ${current.name}`);
      state.selectedProfileName = '';
      populateProfile(null);
      await loadProfiles({ force: true });
    });
  }

  async function toggleAutostart(name: string, enabled: boolean) {
    await runTask('set autostart', async () => {
      await api.setProfileAutostart(name, enabled);
      log(`Autostart ${enabled ? 'enabled' : 'disabled'} for ${name}.`);
      toast.success(`Autostart ${enabled ? 'enabled' : 'disabled'} for ${name}`);
      await refreshServerInfo();
      await loadProfiles({ force: true, preserveDirty: true });
    });
  }

  async function cleanupProfile() {
    await runTask('cleanup profile', async () => {
      const current = requireCurrentProfile();
      await api.cleanupProfile(current.name);
      log(`Cleaned up profile ${current.name}.`);
      toast.success(`Cleaned up ${current.name}`);
      state.selectedProfileName = '';
      populateProfile(null);
      await refreshServerInfo();
      await loadProfiles({ force: true });
      await refreshRuntimeStatus();
    });
  }

  async function deleteLocalModel(model: LocalModel) {
    await runTask('delete local model', async () => {
      await api.deleteLocalModel(state.selectedBackend, model.path, true);
      log(`Deleted local model ${model.filename || model.path}.`);
      toast.success(`Deleted ${model.filename || model.path}`);
      await loadLocalModels();
      await loadProfiles({ force: true });
      await refreshServerInfo();
    });
  }

  async function reloadSelectedProfile() {
    await selectProfile(requireSelectedProfile(), { force: true, skipDirtyCheck: true });
  }

  // Saving sends the structured Profile and leaves profile_yaml empty, so the
  // server owns serialization and the editor cannot drift from it.
  async function saveProfile() {
    await runTask('save profile', async () => {
      const current = requireCurrentProfile();
      // Model source, listen host and port are editable alongside the engine
      // arguments, so the same field checks the create form makes apply here:
      // the server rejects both, but only as a generic invalid-profile error.
      const modelSource = current.modelSource.trim();
      if (!modelSource) throw new Error('model source is required');
      if (!Number.isInteger(current.port) || current.port < 1 || current.port > 65535) {
        throw new Error(`invalid listen port ${current.port}`);
      }
      await api.putProfile(
        create(ProfileSchema, {
          ...current,
          modelSource,
          profileYaml: '',
          engineArgs: engineArgsFromRows(state.draftRows)
        })
      );
      log('Saved profile.');
      toast.success('Saved profile');
      await afterProfileMutation(current.name);
    });
  }

  async function afterProfileMutation(name: string) {
    await loadProfiles({ select: name, force: true });
    await refreshRuntimeStatus();
  }

  async function resolveModel() {
    await runTask('validate model', async () => {
      const reference = state.modelReference.trim();
      if (!reference) throw new Error('model reference is required');
      const response = await api.resolveModel(state.selectedBackend, reference, state.selectedVariantReference);
      state.modelResolution = response.model || null;
      state.modelPlan = response.plan || null;
      log(`Resolved ${response.model?.reference || reference}.`);
    });
  }

  async function loadModelCatalog() {
    state.catalogLoading = true;
    try {
      const data = await api.listModelCatalog({ ...state.catalogQuery, backend: state.selectedBackend });
      state.catalogModels = data.models;
      state.catalogMachine = data.machine || null;
      state.catalogCache = data.cache || null;
      state.catalogErrors = data.errors;
      if (!state.selectedCatalogModelId && state.catalogModels.length) {
        state.selectedCatalogModelId = state.catalogModels[0].id;
      }
      if (data.cache?.refreshing) connectCatalogEvents();
    } catch (error) {
      showError(error);
    } finally {
      state.catalogLoading = false;
    }
  }

  async function loadLocalModels() {
    state.localModelsLoading = true;
    try {
      const data = await api.listLocalModels(state.selectedBackend);
      state.localModels = data.models;
    } catch (error) {
      showError(error);
    } finally {
      state.localModelsLoading = false;
    }
  }

  async function refreshResourcesAndCatalog() {
    await refreshSignals();
    await loadModelCatalog();
  }

  function connectCatalogEvents() {
    if (catalogEvents) return;
    const controller = new AbortController();
    catalogEvents = controller;
    void watchCatalogEvents(controller);
  }

  async function watchCatalogEvents(controller: AbortController) {
    try {
      for await (const event of api.watchModelCatalog(controller.signal)) {
        if (event.error) {
          showError(new Error(event.error));
          continue;
        }
        await loadModelCatalog();
      }
    } catch (error) {
      if (!controller.signal.aborted) showError(error);
    } finally {
      if (catalogEvents === controller) catalogEvents = null;
    }
  }

  function closeCatalogEvents() {
    catalogEvents?.abort();
    catalogEvents = null;
  }

  // A catalog pick is a repository plus a file inside it. Collapsing the two
  // into one field loses the host, which is what left downloads fetching a bare
  // filename, so the repository stays in modelReference and the artifact in
  // selectedVariantReference.
  function useCatalogVariant(model: CatalogModel, variant: ModelVariant | undefined = model.bestVariant) {
    if (!variant) return;
    state.selectedCatalogModelId = model.id;
    state.modelReference = model.id;
    state.selectedVariantReference = variant.reference;
    log(`Selected ${model.id} ${variant.name || variant.reference}.`);
  }

  async function startDownload() {
    await runTask('download model', async () => {
      const reference = state.modelReference.trim();
      if (!reference) throw new Error('resolve or select a model first');
      const data = await api.startModelDownload({
        backend: state.selectedBackend,
        reference,
        variantReference: state.selectedVariantReference
      });
      const job = requireDownload(data.download);
      state.downloads[job.id] = job;
      state.activeModelDownloadId = job.id;
      log(`Started model download ${job.id}.`);
      startPolling(job.id);
    });
  }

  async function cancelDownload() {
    await runTask('cancel model download', async () => {
      if (!state.activeModelDownloadId) throw new Error('download a model first');
      const data = await api.cancelModelDownload(state.activeModelDownloadId);
      const job = requireDownload(data.download);
      state.downloads[job.id] = job;
      stopPolling();
      log(`Cancelled model download ${job.id}.`);
    });
  }

  async function applyToProfile() {
    await runTask('apply model', async () => {
      if (!state.activeModelDownloadId) throw new Error('download a model first');
      const name = requireSelectedProfile();
      await api.applyDownloadToProfile(state.activeModelDownloadId, name);
      log(`Applied model to ${name}.`);
      await loadProfiles({ select: name, force: true });
      state.profileApplyPreview = null;
    });
  }

  async function previewApplyToProfile() {
    await runTask('preview model apply', async () => {
      if (!state.activeModelDownloadId) throw new Error('download a model first');
      const name = requireSelectedProfile();
      const data = await api.applyDownloadToProfile(state.activeModelDownloadId, name, true);
      state.profileApplyPreview = {
        original: data.previewDiff?.original || '',
        updated: data.previewDiff?.updated || ''
      };
      log(`Previewed model apply to ${name}.`);
    });
  }

  function startPolling(id: string) {
    stopPolling();
    pollTimer = window.setInterval(() => pollDownload(id).catch(showError), 1000);
    pollDownload(id).catch(showError);
  }

  function stopPolling() {
    if (pollTimer) window.clearInterval(pollTimer);
    pollTimer = null;
  }

  async function pollDownload(id: string) {
    const data = await api.getModelDownload(id);
    const job = requireDownload(data.download);
    state.downloads[id] = job;
    if (isTerminalDownloadState(job.state)) stopPolling();
  }

  function requireSelectedProfile() {
    if (!state.selectedProfileName) throw new Error('select a profile first');
    return state.selectedProfileName;
  }

  function requireCurrentProfile() {
    if (!state.currentProfile) throw new Error('select a profile first');
    return state.currentProfile;
  }

  function requireDownload(job: ModelDownload | undefined) {
    if (!job) throw new Error('download response missing job');
    return job;
  }

  function runtimeHint() {
    const selected = state.selectedProfileName;
    if (!selected || state.activeProfileNames.length === 0) return '';
    if (!state.activeProfileNames.includes(selected)) {
      return `Restart will start ${selected}; active: ${state.activeProfileNames.join(', ')}.`;
    }
    return `Restart will reload ${selected}.`;
  }

  function isProfileActive(name: string) {
    return selectIsProfileActive(state.activeProfileNames, name);
  }

  function activeDownload() {
    return state.downloads[state.activeModelDownloadId] || null;
  }

  return client;
}

export type InferenceRigClient = ReturnType<typeof createInferenceRigClient>;
