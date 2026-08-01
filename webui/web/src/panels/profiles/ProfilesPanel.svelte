<script lang="ts">
  import Copy from '@lucide/svelte/icons/copy';
  import Plus from '@lucide/svelte/icons/plus';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Info from '@lucide/svelte/icons/info';
  import X from '@lucide/svelte/icons/x';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import { createParamLookup, missingRequiredParams, unknownParamKeys } from '../../lib/profileValidation';
  import { kindForParameterType } from '../../lib/engineArgs';
  import { DEFAULT_HOST, DEFAULT_PORT, nextFreePort, profileTemplates, type ProfileTemplate } from '../../lib/profileTemplates';
  import { profileTarget } from '../../lib/state/selectors';
  import type { EngineArgKind } from '../../lib/types';
  import ParamKeyCombobox from './ParamKeyCombobox.svelte';
  import BackendSelector from '../../components/backends/BackendSelector.svelte';
  import type { InferenceRigClient } from '../../lib/setup/createInferenceRigClient.svelte';
  import * as AlertDialog from '$lib/components/ui/alert-dialog';
  import { Badge } from '$lib/components/ui/badge';
  import { Button, buttonVariants } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import * as Dialog from '$lib/components/ui/dialog';
  import * as Empty from '$lib/components/ui/empty';
  import * as Field from '$lib/components/ui/field';
  import { Input } from '$lib/components/ui/input';
  import * as Item from '$lib/components/ui/item';
  import { ScrollArea } from '$lib/components/ui/scroll-area';
  import * as Select from '$lib/components/ui/select';
  import * as Tooltip from '$lib/components/ui/tooltip';

  let { app }: { app: InferenceRigClient } = $props();
  const appState = $derived(app.state);
  const capabilities = $derived(app.capabilities());

  let createOpen = $state(false);
  let duplicateOpen = $state(false);
  let deleteOpen = $state(false);
  let discardOpen = $state(false);
  let pendingProfile = $state('');
  let newName = $state('');
  let newTemplate = $state<ProfileTemplate>('defaults');
  let newModelSource = $state('');
  let newModelReference = $state('');
  let newHost = $state(DEFAULT_HOST);
  let newPort = $state(DEFAULT_PORT);
  let duplicateName = $state('');

  const params = $derived(appState.backendParams);
  const findParam = $derived(createParamLookup(params));
  const invalidKeys = $derived(unknownParamKeys(appState.draftRows, params));
  const missingRequired = $derived(missingRequiredParams(appState.draftRows, params));

  // The one warning that only exists because of capabilities: on a
  // single_active_profile backend, starting B stops A. Computed before the
  // click, so the confirmation names what is about to be stopped.
  const startWarning = $derived(app.startWarning(appState.selectedProfileName));
  const blockedReason = $derived(
    appState.currentProfile && appState.activeBackend && appState.activeBackend !== appState.currentProfile.backend
      ? `${appState.activeBackend} is active — reset to start ${appState.currentProfile.backend} profiles`
      : ''
  );

  const kindOptions: { value: EngineArgKind; label: string }[] = [
    { value: 'string', label: 'text' },
    { value: 'int', label: 'number' },
    { value: 'bool', label: 'bool' },
    { value: 'list', label: 'list' }
  ];

  function requestProfile(name: string) {
    if (!appState.dirty.rows) return app.selectProfile(name);
    pendingProfile = name;
    discardOpen = true;
  }

  async function createProfile(event: SubmitEvent) {
    event.preventDefault();
    await app.createProfile(newName, newTemplate, {
      source: newModelSource,
      reference: newModelReference,
      host: newHost,
      port: newPort
    });
    if (!app.errorMessage) {
      createOpen = false;
      newName = '';
      newModelSource = '';
      newModelReference = '';
    }
  }

  // The port defaults to the first one no existing profile claims, recomputed
  // when the dialog opens rather than once at load, so two profiles created in
  // one session do not collide.
  function openCreate() {
    newPort = nextFreePort(appState.profiles);
    createOpen = true;
  }

  async function duplicateProfile(event: SubmitEvent) {
    event.preventDefault();
    await app.duplicateProfile(duplicateName);
    if (!app.errorMessage) duplicateOpen = false;
  }

  async function deleteProfile() {
    await app.deleteProfile();
    if (!app.errorMessage) deleteOpen = false;
  }

  function removeRow(index: number) {
    appState.draftRows = appState.draftRows.filter((_, i) => i !== index);
    appState.dirty.rows = true;
  }

  function addRow() {
    appState.draftRows = [...appState.draftRows, { key: '', value: '', kind: 'string' }];
    appState.dirty.rows = true;
  }

  // Choosing a known key adopts the type the backend declares for it, so a
  // bool stays a bool in the Struct rather than becoming the string "true".
  function adoptKind(index: number) {
    const row = appState.draftRows[index];
    const param = findParam(row.key);
    if (param) row.kind = kindForParameterType(param.type);
    appState.dirty.rows = true;
  }

  function valueListId(index: number) {
    return `param-values-${index}`;
  }

  function valueSuggestions(key: string, kind: EngineArgKind): string[] | undefined {
    if (kind === 'bool') return ['true', 'false'];
    const param = findParam(key);
    if (param?.defaultValue) return [param.defaultValue];
    return undefined;
  }
</script>

<div class="space-y-4">
  <BackendSelector backends={appState.backends} value={appState.selectedBackend} disabled={appState.busy} onselect={app.selectBackend} />

  <div class="grid gap-4 xl:grid-cols-[22rem_minmax(0,1fr)]">
    <Card.Root>
      <Card.Header>
        <Card.Title>Profiles</Card.Title>
        <Card.Description>Select or manage a serving profile. <span>Active backend: {appState.activeBackend || 'none'}</span></Card.Description>
        <Card.Action><Button size="icon-sm" variant="outline" aria-label="Reload profiles" onclick={() => app.loadProfiles({ force: true })} disabled={appState.busy}><RefreshCw /></Button></Card.Action>
      </Card.Header>
      <Card.Content class="space-y-4">
        <div class="flex flex-wrap gap-2">
          <Dialog.Root bind:open={createOpen}>
            <Dialog.Trigger class={buttonVariants({ size: 'sm' })} onclick={openCreate}><Plus /> New</Dialog.Trigger>
            <Dialog.Content>
              <Dialog.Header><Dialog.Title>Create profile</Dialog.Title><Dialog.Description>Choose a name and starter configuration for {appState.selectedBackend || 'the selected backend'}.</Dialog.Description></Dialog.Header>
              <form class="space-y-4" onsubmit={createProfile}>
                <Field.Field><Field.Label for="profile-name">Name</Field.Label><Input id="profile-name" bind:value={newName} autocomplete="off" /></Field.Field>
                <Field.Field>
                  <Field.Label for="profile-template">Template</Field.Label>
                  <Select.Root type="single" bind:value={newTemplate}>
                    <Select.Trigger id="profile-template">{profileTemplates.find((option) => option.value === newTemplate)?.label}</Select.Trigger>
                    <Select.Content>{#each profileTemplates as option}<Select.Item value={option.value}>{option.label}</Select.Item>{/each}</Select.Content>
                  </Select.Root>
                  <Field.Description>Backend defaults are read from this backend's own parameter list.</Field.Description>
                </Field.Field>
                <Field.Field>
                  <Field.Label for="profile-model-source">Model source</Field.Label>
                  <Input id="profile-model-source" bind:value={newModelSource} autocomplete="off" placeholder="owner/repo, URL, or local path" />
                  <Field.Description>Required. Download a model from the Models tab and apply it here to change this later.</Field.Description>
                </Field.Field>
                <Field.Field>
                  <Field.Label for="profile-model-reference">Model reference</Field.Label>
                  <Input id="profile-model-reference" bind:value={newModelReference} autocomplete="off" placeholder="optional file within the repository" />
                </Field.Field>
                <div class="grid grid-cols-2 gap-3">
                  <Field.Field><Field.Label for="profile-host">Listen host</Field.Label><Input id="profile-host" bind:value={newHost} autocomplete="off" /></Field.Field>
                  <Field.Field><Field.Label for="profile-port">Listen port</Field.Label><Input id="profile-port" type="number" min="1" max="65535" bind:value={newPort} /></Field.Field>
                </div>
                <Dialog.Footer><Dialog.Close class={buttonVariants({ variant: 'outline' })}>Cancel</Dialog.Close><Button type="submit" disabled={appState.busy}>Create</Button></Dialog.Footer>
              </form>
            </Dialog.Content>
          </Dialog.Root>

          <Dialog.Root bind:open={duplicateOpen}>
            <Dialog.Trigger class={buttonVariants({ variant: 'outline', size: 'sm' })} disabled={!appState.currentProfile} onclick={() => (duplicateName = `${appState.currentProfile?.name || ''}-copy`)}><Copy /> Duplicate</Dialog.Trigger>
            <Dialog.Content>
              <Dialog.Header><Dialog.Title>Duplicate profile</Dialog.Title><Dialog.Description>Copy {appState.currentProfile?.name} into a new profile.</Dialog.Description></Dialog.Header>
              <form class="space-y-4" onsubmit={duplicateProfile}>
                <Field.Field><Field.Label for="duplicate-name">New name</Field.Label><Input id="duplicate-name" bind:value={duplicateName} autocomplete="off" /></Field.Field>
                <Dialog.Footer><Dialog.Close class={buttonVariants({ variant: 'outline' })}>Cancel</Dialog.Close><Button type="submit" disabled={appState.busy}>Duplicate</Button></Dialog.Footer>
              </form>
            </Dialog.Content>
          </Dialog.Root>

          <AlertDialog.Root bind:open={deleteOpen}>
            <AlertDialog.Trigger class={buttonVariants({ variant: 'destructive', size: 'sm' })} disabled={!appState.currentProfile}><Trash2 /> Delete</AlertDialog.Trigger>
            <AlertDialog.Content>
              <AlertDialog.Header><AlertDialog.Title>Delete {appState.currentProfile?.name}?</AlertDialog.Title><AlertDialog.Description>This removes the profile configuration and cannot be undone.</AlertDialog.Description></AlertDialog.Header>
              <AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={deleteProfile}>Delete profile</AlertDialog.Action></AlertDialog.Footer>
            </AlertDialog.Content>
          </AlertDialog.Root>
        </div>

        <ScrollArea class="panel-scroll pr-3">
          <Item.Group>
            {#each appState.profiles as profile (profile.name)}
              {@const runtimeState = appState.profileRuntimeStates[profile.name] || 'stopped'}
              {@const blocked = !!appState.activeBackend && appState.activeBackend !== profile.backend}
              <Item.Root
                variant={appState.selectedProfileName === profile.name ? 'muted' : 'default'}
                size="sm"
                class={`${appState.selectedProfileName === profile.name ? 'border-primary/50 bg-primary/10 shadow-sm' : 'hover:bg-muted/50'} ${blocked ? 'opacity-50' : ''}`}
              >
                <Item.Content>
                  <Button variant="ghost" class="h-auto w-full cursor-pointer justify-start px-0 text-left hover:bg-transparent" aria-pressed={appState.selectedProfileName === profile.name} onclick={() => requestProfile(profile.name)}>
                    <Item.Title>{profile.name}</Item.Title>
                    <Item.Description>{profile.backend} · {profileTarget(profile)}</Item.Description>
                  </Button>
                </Item.Content>
                {#if runtimeState !== 'stopped'}<Item.Actions><Badge class="bg-success/15 text-success border-success/30" variant="outline">{runtimeState}</Badge></Item.Actions>{/if}
                {#if appState.autostartProfiles.includes(profile.name)}<Item.Actions><Badge variant="secondary">autostart</Badge></Item.Actions>{/if}
              </Item.Root>
            {:else}
              <Empty.Root><Empty.Header><Empty.Title>No profiles</Empty.Title><Empty.Description>Create a serving profile to begin.</Empty.Description></Empty.Header></Empty.Root>
            {/each}
          </Item.Group>
        </ScrollArea>
      </Card.Content>
    </Card.Root>

    <Card.Root class="min-w-0">
      <Card.Header>
        <Card.Title>{appState.currentProfile?.name || 'No profile selected'}</Card.Title>
        <Card.Description>
          Engine arguments for this profile. Saved as structured data; {appState.currentProfile?.backend || 'the backend'} renders the YAML server-side.
        </Card.Description>
        <Card.Action>
          {#if appState.dirty.rows}
            <Badge variant="outline" class="bg-destructive/15 text-destructive border-destructive/30">Unsaved changes</Badge>
          {:else}
            <Badge variant="secondary">Saved</Badge>
          {/if}
        </Card.Action>
      </Card.Header>
      <Card.Content class="space-y-4">
        {#if appState.currentProfile}
          <div class="grid gap-3 sm:grid-cols-[auto_minmax(0,1fr)_10rem_7rem]">
            <div class="text-sm"><span class="text-muted-foreground">Backend</span><p class="pt-2 font-mono">{appState.currentProfile.backend}</p></div>
            <Field.Field class="min-w-0">
              <Field.Label for="profile-model">Model</Field.Label>
              <!-- A datalist, not a select: the source is a downloaded model's
                   path most of the time, but an unresolved repository id is
                   equally valid, and a closed list would reject it. -->
              <Input
                id="profile-model"
                class="font-mono text-sm"
                list="profile-model-options"
                bind:value={appState.currentProfile.modelSource}
                oninput={() => (appState.dirty.rows = true)}
                disabled={appState.busy}
              />
              <datalist id="profile-model-options">
                {#each appState.localModels as model (model.path)}<option value={model.path}>{model.filename}</option>{/each}
              </datalist>
            </Field.Field>
            <Field.Field>
              <Field.Label for="profile-listen-host">Listen host</Field.Label>
              <Input
                id="profile-listen-host"
                class="font-mono text-sm"
                list="profile-host-options"
                bind:value={appState.currentProfile.host}
                oninput={() => (appState.dirty.rows = true)}
                disabled={appState.busy}
              />
              <datalist id="profile-host-options"><option value="127.0.0.1">this machine only</option><option value="0.0.0.0">all interfaces</option></datalist>
            </Field.Field>
            <Field.Field>
              <Field.Label for="profile-listen-port">Port</Field.Label>
              <Input
                id="profile-listen-port"
                type="number"
                min="1"
                max="65535"
                class="font-mono text-sm"
                bind:value={appState.currentProfile.port}
                oninput={() => (appState.dirty.rows = true)}
                disabled={appState.busy}
              />
            </Field.Field>
          </div>

          {#if !capabilities.parameterIntrospection}
            <p class="flex items-start gap-2 rounded-md border border-dashed p-3 text-sm text-muted-foreground">
              <Info class="mt-0.5 size-4 shrink-0" />
              <span>{appState.currentProfile.backend} does not expose a parameter list, so keys are free text and are not validated here.</span>
            </p>
          {/if}

          <div class="space-y-2">
            {#each appState.draftRows as row, i (row)}
              {@const hint = findParam(row.key)}
              {@const valueOptions = valueSuggestions(row.key, row.kind)}
              {@const invalid = invalidKeys.includes(row.key.trim())}
              <div class="flex flex-wrap items-center gap-2">
                <div class={invalid ? 'rounded-md ring-1 ring-destructive' : ''}>
                  <ParamKeyCombobox
                    bind:value={row.key}
                    {params}
                    introspection={capabilities.parameterIntrospection}
                    disabled={appState.busy}
                    oninput={() => adoptKind(i)}
                  />
                </div>
                <Select.Root type="single" bind:value={row.kind} onValueChange={() => (appState.dirty.rows = true)}>
                  <Select.Trigger class="w-24" aria-label="Value type">{kindOptions.find((option) => option.value === row.kind)?.label}</Select.Trigger>
                  <Select.Content>{#each kindOptions as option}<Select.Item value={option.value}>{option.label}</Select.Item>{/each}</Select.Content>
                </Select.Root>
                <Input
                  class="min-w-40 flex-1 font-mono text-sm"
                  bind:value={row.value}
                  placeholder={row.kind === 'list' ? 'comma, separated' : 'value'}
                  list={valueOptions ? valueListId(i) : undefined}
                  oninput={() => (appState.dirty.rows = true)}
                  disabled={appState.busy}
                />
                {#if valueOptions}
                  <datalist id={valueListId(i)}>
                    {#each valueOptions as option}<option value={option}></option>{/each}
                  </datalist>
                {/if}
                {#if invalid}
                  <Tooltip.Provider>
                    <Tooltip.Root>
                      <Tooltip.Trigger><TriangleAlert class="size-4 shrink-0 text-destructive" /></Tooltip.Trigger>
                      <Tooltip.Content class="max-w-64"><p>{appState.currentProfile.backend} does not declare this parameter. Starting the profile will fail.</p></Tooltip.Content>
                    </Tooltip.Root>
                  </Tooltip.Provider>
                {:else if hint}
                  <Tooltip.Provider>
                    <Tooltip.Root>
                      <Tooltip.Trigger><Info class="size-4 shrink-0 text-muted-foreground" /></Tooltip.Trigger>
                      <Tooltip.Content class="max-w-64">
                        <p>{hint.description}</p>
                        {#if hint.valueHint}<p class="mt-1 font-mono text-xs">accepts: {hint.valueHint}</p>{/if}
                        {#if hint.defaultValue}<p class="mt-1 font-mono text-xs">default: {hint.defaultValue}</p>{/if}
                      </Tooltip.Content>
                    </Tooltip.Root>
                  </Tooltip.Provider>
                {:else}
                  <div class="size-4 shrink-0"></div>
                {/if}
                <Button size="icon-sm" variant="ghost" onclick={() => removeRow(i)} disabled={appState.busy} aria-label="Remove entry"><X /></Button>
              </div>
            {/each}
          </div>
          <Button size="sm" variant="outline" onclick={addRow} disabled={appState.busy}><Plus /> Add argument</Button>

          {#if invalidKeys.length}
            <p class="flex items-center gap-1.5 text-sm text-destructive"><TriangleAlert class="size-4 shrink-0" /> Unrecognized key{invalidKeys.length > 1 ? 's' : ''}: {invalidKeys.join(', ')}. Fix before saving.</p>
          {/if}
          {#if missingRequired.length}
            <p class="flex items-center gap-1.5 text-sm text-warning-foreground dark:text-warning"><TriangleAlert class="size-4 shrink-0" /> Required by {appState.currentProfile.backend}: {missingRequired.join(', ')}.</p>
          {/if}

          <div class="flex flex-wrap gap-2">
            {#if blockedReason}
              <div class="flex w-full flex-wrap items-center justify-between gap-3 rounded-md border border-warning/40 bg-warning/10 p-3">
                <p class="text-sm text-warning-foreground dark:text-warning">{blockedReason}</p>
                <AlertDialog.Root>
                  <AlertDialog.Trigger class={buttonVariants({ variant: 'destructive', size: 'sm' })} disabled={appState.busy}>Reset runtime</AlertDialog.Trigger>
                  <AlertDialog.Content>
                    <AlertDialog.Header><AlertDialog.Title>Reset the active backend?</AlertDialog.Title><AlertDialog.Description>This stops all active profiles before switching backends.</AlertDialog.Description></AlertDialog.Header>
                    <AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={app.resetRuntimes}>Reset and switch backend</AlertDialog.Action></AlertDialog.Footer>
                  </AlertDialog.Content>
                </AlertDialog.Root>
              </div>
            {/if}
            {#if appState.dirty.rows}
              <AlertDialog.Root>
                <AlertDialog.Trigger class={buttonVariants({ variant: 'outline', size: 'sm' })} disabled={appState.busy}>Reload</AlertDialog.Trigger>
                <AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>Discard unsaved changes?</AlertDialog.Title><AlertDialog.Description>Reloading replaces current arguments.</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>Keep editing</AlertDialog.Cancel><AlertDialog.Action onclick={app.reloadSelectedProfile}>Discard and reload</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content>
              </AlertDialog.Root>
            {:else}
              <Button size="sm" variant="outline" onclick={app.reloadSelectedProfile} disabled={appState.busy}>Reload</Button>
            {/if}
            <Button size="sm" onclick={app.saveProfile} disabled={appState.busy || invalidKeys.length > 0}>Save</Button>

            {#if startWarning || appState.dirty.rows}
              <AlertDialog.Root>
                <AlertDialog.Trigger class={buttonVariants({ variant: 'outline', size: 'sm' })} disabled={appState.busy || !!blockedReason || invalidKeys.length > 0 || app.isProfileActive(appState.selectedProfileName)}>Start</AlertDialog.Trigger>
                <AlertDialog.Content>
                  <AlertDialog.Header>
                    <AlertDialog.Title>{startWarning ? `Start ${appState.selectedProfileName} and stop the running profile?` : 'Start without saving?'}</AlertDialog.Title>
                    <AlertDialog.Description>
                      {#if startWarning}{startWarning}{/if}
                      {#if appState.dirty.rows}{' '}Runtime will use saved profile content, not current editor changes.{/if}
                    </AlertDialog.Description>
                  </AlertDialog.Header>
                  <AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={() => app.startSelectedProfile(!!startWarning)}>Start profile</AlertDialog.Action></AlertDialog.Footer>
                </AlertDialog.Content>
              </AlertDialog.Root>
            {:else}
              <Button size="sm" variant="outline" onclick={() => app.startSelectedProfile()} disabled={appState.busy || !!blockedReason || invalidKeys.length > 0 || app.isProfileActive(appState.selectedProfileName)}>Start</Button>
            {/if}

            <AlertDialog.Root>
              <AlertDialog.Trigger class={buttonVariants({ variant: 'ghost', size: 'sm' })} disabled={appState.busy}><Trash2 /> Cleanup</AlertDialog.Trigger>
              <AlertDialog.Content>
                <AlertDialog.Header><AlertDialog.Title>Cleanup {appState.currentProfile.name}?</AlertDialog.Title><AlertDialog.Description>This removes the profile and clears matching autostart references. This cannot be undone.</AlertDialog.Description></AlertDialog.Header>
                <AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={app.cleanupProfile}>Cleanup profile</AlertDialog.Action></AlertDialog.Footer>
              </AlertDialog.Content>
            </AlertDialog.Root>
          </div>
        {:else}
          <Empty.Root><Empty.Header><Empty.Title>No profile selected</Empty.Title><Empty.Description>Select a profile from the list to edit its engine arguments.</Empty.Description></Empty.Header></Empty.Root>
        {/if}
      </Card.Content>
    </Card.Root>
  </div>
</div>

<AlertDialog.Root bind:open={discardOpen}>
  <AlertDialog.Content>
    <AlertDialog.Header><AlertDialog.Title>Discard unsaved changes?</AlertDialog.Title><AlertDialog.Description>Switching profiles replaces current arguments.</AlertDialog.Description></AlertDialog.Header>
    <AlertDialog.Footer><AlertDialog.Cancel>Keep editing</AlertDialog.Cancel><AlertDialog.Action onclick={() => app.selectProfile(pendingProfile, { skipDirtyCheck: true })}>Discard and switch</AlertDialog.Action></AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
