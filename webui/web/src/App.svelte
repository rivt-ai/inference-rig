<script lang="ts">
  import { onDestroy, onMount } from 'svelte';
  import { ModeWatcher } from 'mode-watcher';
  import { Toaster } from '$lib/components/ui/sonner';
  import AppShell from './components/shell/AppShell.svelte';
  import { projectTitle } from './lib/project';
  import { createInferenceRigClient } from './lib/setup/createInferenceRigClient.svelte';

  const app = createInferenceRigClient();
  const state = app.state;

  onMount(app.mount);
  onDestroy(app.destroy);

  $effect(() => {
    document.title = app.hasDirtyEditors() ? `${projectTitle} *` : projectTitle;
  });
</script>

<ModeWatcher />
<Toaster />

<AppShell
  app={state}
  sections={app.sections}
  sectionTitle={app.sections.find((section) => section.id === state.activeSection)?.label}
  errorMessage={app.errorMessage}
  onSaveApiBase={app.saveApiBase}
  onSaveToken={app.saveToken}
  onTestConnection={app.testConnection}
>
  {#if state.activeSection === 'runtime'}
    {#await import('./panels/runtime/RuntimePanel.svelte') then panel}<panel.default {app} />{/await}
  {:else if state.activeSection === 'profiles'}
    {#await import('./panels/profiles/ProfilesPanel.svelte') then panel}<panel.default {app} />{/await}
  {:else if state.activeSection === 'models'}
    {#await import('./panels/models/ModelsPanel.svelte') then panel}<panel.default {app} />{/await}
  {:else if state.activeSection === 'logs'}
    {#await import('./panels/logs/LogsPanel.svelte') then panel}<panel.default {app} />{/await}
  {/if}
</AppShell>
