<script lang="ts">
  import ChevronDown from '@lucide/svelte/icons/chevron-down';
  import Download from '@lucide/svelte/icons/download';
  import Info from '@lucide/svelte/icons/info';
  import Plus from '@lucide/svelte/icons/plus';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Search from '@lucide/svelte/icons/search';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
  import X from '@lucide/svelte/icons/x';
  import { formatBytes, formatDate } from '../../lib/formatting';
  import type { InferenceRigClient } from '../../lib/setup/createInferenceRigClient.svelte';
  import { FitLevel, type CatalogModel, type LocalModel, type ModelVariant } from '../../lib/gen/inferencerig/control/v1/control_pb';
  import { uniqueProfileName } from '../../lib/profileTemplates';
  import DiffPreview from '../../components/editor/DiffPreview.svelte';
  import BackendSelector from '../../components/backends/BackendSelector.svelte';
  import * as AlertDialog from '$lib/components/ui/alert-dialog';
  import { Badge } from '$lib/components/ui/badge';
  import { Button, buttonVariants } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import * as Collapsible from '$lib/components/ui/collapsible';
  import * as Dialog from '$lib/components/ui/dialog';
  import * as Empty from '$lib/components/ui/empty';
  import * as Field from '$lib/components/ui/field';
  import { Input } from '$lib/components/ui/input';
  import * as Item from '$lib/components/ui/item';
  import * as RadioGroup from '$lib/components/ui/radio-group';
  import { ScrollArea } from '$lib/components/ui/scroll-area';
  import { Separator } from '$lib/components/ui/separator';
  import * as Select from '$lib/components/ui/select';
  import * as Tabs from '$lib/components/ui/tabs';
  import * as Tooltip from '$lib/components/ui/tooltip';
  import {
    downloadStatusClass,
    downloadSummary,
    filterLocalModels,
    fitBadge,
    fitMeterColor,
    fitMeterWidths,
    localModelFilterCounts,
    modelMetadataChips,
    rankedResourceSummary,
    type LocalModelFilter
  } from './catalogPresentation';

  let { app }: { app: InferenceRigClient } = $props();
  const appState = $derived(app.state);
  const capabilities = $derived(app.capabilities());

  let activeTab = $state('mine');
  let localFilter = $state<LocalModelFilter>('all');
  let expandedModel = $state<string | null>(null);
  let createDialogOpen = $state(false);
  let selectedLocalModel = $state<LocalModel | null>(null);
  let draftName = $state('');

  const sortOptions = [
    { value: 'downloads', label: 'Downloads' },
    { value: 'likes', label: 'Likes' },
    { value: 'modified', label: 'Recently modified' },
    { value: 'fit', label: 'Best fit' }
  ];
  const fitOptions = [
    { value: FitLevel.FITS, label: 'Fits' },
    { value: FitLevel.MARGINAL, label: 'Fits or marginal' },
    { value: FitLevel.UNSPECIFIED, label: 'All' }
  ];
  const localFilterOptions: { value: LocalModelFilter; label: string }[] = [
    { value: 'all', label: 'All' },
    { value: 'serving', label: 'Serving' },
    { value: 'in_profile', label: 'In profile' },
    { value: 'unused', label: 'Unused' }
  ];

  const filteredLocalModels = $derived(filterLocalModels(appState.localModels, localFilter, appState.activeProfileNames));
  const localFilterCounts = $derived(localModelFilterCounts(appState.localModels, appState.activeProfileNames));
  const localDiskBytes = $derived(appState.localModels.reduce((sum, model) => sum + Number(model.sizeBytes), 0));

  function isServing(model: LocalModel) {
    return model.usedByProfiles.some((name) => appState.activeProfileNames.includes(name));
  }

  function openCreateProfile(model: LocalModel) {
    selectedLocalModel = model;
    draftName = uniqueProfileName(model.filename || model.path, appState.profiles);
    createDialogOpen = true;
  }

  function toggleDetails(model: LocalModel) {
    expandedModel = expandedModel === model.path ? null : model.path;
  }

  async function downloadCatalogModel(model: CatalogModel) {
    app.useCatalogVariant(model);
    await app.startDownload();
  }

  async function createLocalProfile(event: SubmitEvent) {
    event.preventDefault();
    if (!selectedLocalModel) return;
    await app.createModelProfile(draftName, selectedLocalModel.path);
    if (!app.errorMessage) {
      createDialogOpen = false;
      selectedLocalModel = null;
      draftName = '';
    }
  }

  function variantSummary(variant: ModelVariant | undefined) {
    if (!variant) return '';
    return [variant.name || variant.reference, variant.quant, formatBytes(Number(variant.sizeBytes)), variant.multiFile ? 'multi-file' : '']
      .filter(Boolean)
      .join(' · ');
  }
</script>

{#snippet fitMeter(needPct: number, level: FitLevel | undefined)}
  <div class="relative h-1.5 overflow-hidden rounded-full bg-muted">
    <div class={`h-full rounded-r-full ${fitMeterColor(level)}`} style={`width:${needPct}%`}></div>
  </div>
{/snippet}

{#snippet fitInfoTooltip()}
  <Tooltip.Provider>
    <Tooltip.Root>
      <Tooltip.Trigger class="text-muted-foreground"><Info class="size-3.5" /></Tooltip.Trigger>
      <Tooltip.Content class="max-w-64">
        Fit is estimated by the server for this backend's memory model, against this host.
      </Tooltip.Content>
    </Tooltip.Root>
  </Tooltip.Provider>
{/snippet}

<div class="space-y-4">
  <BackendSelector backends={appState.backends} value={appState.selectedBackend} disabled={appState.busy} onselect={app.selectBackend} />

  <Tabs.Root bind:value={activeTab} class="space-y-4">
    <Tabs.List>
      <Tabs.Trigger value="mine">My models <Badge variant="secondary">{appState.localModels.length}</Badge></Tabs.Trigger>
      <Tabs.Trigger value="catalog">Catalog</Tabs.Trigger>
    </Tabs.List>

    <Tabs.Content value="mine">
      <div class="space-y-4">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h2 class="text-2xl font-bold tracking-tight">My models</h2>
            <p class="mt-1 flex items-center gap-1.5 text-sm text-muted-foreground">
              {appState.localModels.length} downloaded · {formatBytes(localDiskBytes)} on disk · {rankedResourceSummary(appState.catalogMachine, appState.signals)}
              {@render fitInfoTooltip()}
            </p>
          </div>
          <div class="flex items-center gap-2">
            <Button size="sm" variant="outline" onclick={app.loadLocalModels} disabled={appState.localModelsLoading}><RefreshCw class={appState.localModelsLoading ? 'animate-spin' : ''} /> Refresh</Button>
            <Button size="sm" onclick={() => (activeTab = 'catalog')}><Plus /> Pull model</Button>
          </div>
        </div>

        <div class="flex flex-wrap gap-2">
          {#each localFilterOptions as option}
            <Button
              type="button"
              size="sm"
              variant={localFilter === option.value ? 'secondary' : 'ghost'}
              class={`rounded-full ${localFilter === option.value ? 'text-primary' : 'text-muted-foreground'}`}
              onclick={() => (localFilter = option.value)}
            >
              {option.label} · {localFilterCounts[option.value]}
            </Button>
          {/each}
        </div>

        <ScrollArea class="panel-scroll pr-3">
          <div class="space-y-3">
            {#each filteredLocalModels as model (model.path)}
              {@const profiles = model.usedByProfiles}
              {@const serving = isServing(model)}
              <div class="rounded-xl border p-4 transition-colors hover:border-primary/40">
                <div class="grid grid-cols-1 items-center gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,14rem)_minmax(0,14rem)]">
                  <div class="min-w-0">
                    <div class="flex flex-wrap items-center gap-2">
                      <span class="font-semibold">{model.filename || model.path}</span>
                      {#if serving}<Badge variant="outline" class="border-success/30 bg-success/15 text-success">Serving</Badge>{/if}
                    </div>
                    <div class="mt-1 truncate text-xs text-muted-foreground">{model.path}</div>
                  </div>
                  <div class="min-w-0 text-xs text-muted-foreground">{formatBytes(Number(model.sizeBytes))}</div>
                  <div class="flex min-w-0 flex-wrap justify-end gap-2">
                    <Button size="sm" variant="outline" onclick={() => toggleDetails(model)}>
                      Details <ChevronDown class={`size-3.5 transition-transform ${expandedModel === model.path ? 'rotate-180' : ''}`} />
                    </Button>
                    {#if !profiles.length}
                      <Button size="sm" onclick={() => openCreateProfile(model)}><Plus /> Create profile</Button>
                    {:else}
                      <Badge variant="secondary" class="max-w-40 truncate" title={`in: ${profiles.join(', ')}`}>in: {profiles.join(', ')}</Badge>
                    {/if}
                  </div>
                </div>
                {#if expandedModel === model.path}
                  <div class="mt-4 flex flex-wrap items-center justify-between gap-3 border-t pt-3 text-sm">
                    <dl class="grid grid-cols-2 gap-x-6 gap-y-1 text-xs sm:grid-cols-3">
                      <div><dt class="text-muted-foreground">Path</dt><dd class="truncate">{model.path}</dd></div>
                      <div><dt class="text-muted-foreground">Modified</dt><dd>{formatDate(model.modifiedAt)}</dd></div>
                      <div><dt class="text-muted-foreground">Size</dt><dd>{formatBytes(Number(model.sizeBytes))}</dd></div>
                    </dl>
                    <AlertDialog.Root>
                      <AlertDialog.Trigger class={buttonVariants({ variant: 'destructive', size: 'sm' })}><Trash2 /> Delete</AlertDialog.Trigger>
                      <AlertDialog.Content>
                        <AlertDialog.Header><AlertDialog.Title>Delete {model.filename || model.path}?</AlertDialog.Title><AlertDialog.Description>{#if profiles.length}This also deletes profiles {profiles.join(', ')} and clears their autostart references. Active profiles block deletion.{:else}This deletes the local model ({formatBytes(Number(model.sizeBytes))}) and cannot be undone.{/if}</AlertDialog.Description></AlertDialog.Header>
                        <AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={() => app.deleteLocalModel(model)}>Delete model</AlertDialog.Action></AlertDialog.Footer>
                      </AlertDialog.Content>
                    </AlertDialog.Root>
                  </div>
                {/if}
              </div>
            {:else}
              <Empty.Root><Empty.Header><Empty.Title>No models yet</Empty.Title><Empty.Description>Pull a model from the catalog to get started.</Empty.Description></Empty.Header></Empty.Root>
            {/each}
          </div>
        </ScrollArea>
      </div>
    </Tabs.Content>

    <Tabs.Content value="catalog">
      <div class="grid gap-4 lg:grid-cols-[1fr_minmax(20rem,24rem)]">
        <Card.Root>
          <Card.Header>
            <Card.Title>Recommended models</Card.Title>
            <Card.Description class="flex items-center gap-1.5">
              Ranked for {rankedResourceSummary(appState.catalogMachine, appState.signals)}
              {appState.catalogCache?.hit ? ` · cache ${appState.catalogCache.stale ? 'refreshing' : 'fresh'}` : ''}
              {@render fitInfoTooltip()}
            </Card.Description>
            <Card.Action><Button size="sm" variant="outline" onclick={app.refreshResourcesAndCatalog} disabled={appState.catalogLoading}><RefreshCw class={appState.catalogLoading ? 'animate-spin' : ''} /> Refresh</Button></Card.Action>
          </Card.Header>
          <Card.Content class="space-y-4">
            {#if appState.catalogErrors.length}
              <div role="alert" class="rounded-lg border border-warning/40 bg-warning/10 p-3 text-sm text-warning-foreground dark:text-warning">
                <p class="flex items-center gap-2 font-medium"><TriangleAlert class="size-4" /> {appState.catalogErrors.length} catalog model{appState.catalogErrors.length === 1 ? '' : 's'} could not be loaded.</p>
                <Collapsible.Root class="mt-2"><Collapsible.Trigger class={buttonVariants({ variant: 'ghost', size: 'sm' })}>Show details</Collapsible.Trigger><Collapsible.Content><ul class="mt-1 list-disc space-y-1 pl-5">{#each appState.catalogErrors as error}<li>{error}</li>{/each}</ul></Collapsible.Content></Collapsible.Root>
              </div>
            {/if}
            <div class="grid gap-3 md:grid-cols-[minmax(10rem,1fr)_11rem_11rem_auto]">
              <Field.Field><Field.Label for="catalog-search">Search</Field.Label><Input id="catalog-search" bind:value={appState.catalogQuery.query} placeholder="qwen coder" /></Field.Field>
              <Field.Field><Field.Label for="catalog-sort">Sort</Field.Label><Select.Root type="single" bind:value={appState.catalogQuery.sort}><Select.Trigger id="catalog-sort">{sortOptions.find((option) => option.value === appState.catalogQuery.sort)?.label}</Select.Trigger><Select.Content>{#each sortOptions as option}<Select.Item value={option.value}>{option.label}</Select.Item>{/each}</Select.Content></Select.Root></Field.Field>
              <Field.Field><Field.Label for="catalog-fit">Fit</Field.Label><Select.Root type="single" value={String(appState.catalogQuery.minFit)} onValueChange={(next: string) => (appState.catalogQuery.minFit = Number(next))}><Select.Trigger id="catalog-fit">{fitOptions.find((option) => option.value === appState.catalogQuery.minFit)?.label}</Select.Trigger><Select.Content>{#each fitOptions as option}<Select.Item value={String(option.value)}>{option.label}</Select.Item>{/each}</Select.Content></Select.Root></Field.Field>
              <div class="flex items-end"><Button onclick={app.loadModelCatalog} disabled={appState.catalogLoading}><Search /> Apply</Button></div>
            </div>

            <ScrollArea class="panel-scroll pr-3">
              <div class="space-y-3">
                {#each appState.catalogModels as model (model.id)}
                  {@const best = model.bestVariant}
                  {@const badge = fitBadge(best?.fit?.level)}
                  {@const metadata = modelMetadataChips(model)}
                  {@const widths = fitMeterWidths(best?.fit)}
                  <div class="rounded-xl border p-4 transition-colors hover:border-primary/40">
                    <div class="flex items-start justify-between gap-3">
                      <div class="min-w-0">
                        <a class="font-semibold hover:underline" href={model.url} target="_blank" rel="noreferrer">{model.id}</a>
                        <div class="mt-0.5 text-xs text-muted-foreground">{variantSummary(best)}</div>
                      </div>
                      {#if badge}<Badge variant="outline" class={`shrink-0 ${badge.class}`}>{badge.label}</Badge>{/if}
                    </div>
                    {#if widths}
                      <div class="mt-3">
                        <div class="flex justify-between text-xs text-muted-foreground">
                          <span>needs vs {formatBytes(widths.availableBytes)} available</span>
                          <span class="font-semibold">~{formatBytes(widths.requiredBytes)}</span>
                        </div>
                        {@render fitMeter(widths.needPct, best?.fit?.level)}
                        {#if best?.fit?.reason}<p class="mt-1 text-[11px] text-muted-foreground">{best.fit.reason}</p>{/if}
                      </div>
                    {/if}
                    <div class="mt-3 flex flex-wrap items-center gap-2">
                      {#each metadata.primary as chip}<Badge variant="secondary" class="text-[11px]">{chip}</Badge>{/each}
                      {#each metadata.capability as chip}<Badge variant="outline" class="text-[11px]">{chip}</Badge>{/each}
                      <div class="ml-auto flex gap-2">
                        <Button size="sm" variant="outline" onclick={() => app.useCatalogVariant(model)} disabled={!best}>Choose variant</Button>
                        <Button size="sm" onclick={() => downloadCatalogModel(model)} disabled={appState.busy || !best}><Download /> Download</Button>
                      </div>
                    </div>
                  </div>
                {:else}
                  <Empty.Root><Empty.Header><Empty.Title>No catalog models</Empty.Title><Empty.Description>Adjust filters or refresh machine resources.</Empty.Description></Empty.Header></Empty.Root>
                {/each}
              </div>
            </ScrollArea>
          </Card.Content>
        </Card.Root>

        <Card.Root class="self-start">
          <Card.Header><Card.Title>Manual model reference</Card.Title><Card.Description>Resolve a catalog reference or repository URL, choose a variant, then download and apply it.</Card.Description></Card.Header>
          <Card.Content class="space-y-4">
            <p class="text-xs font-medium uppercase tracking-wider text-muted-foreground">1 · Resolve reference</p>
            <Field.Field><Field.Label for="model-reference">Model reference</Field.Label><div class="flex gap-2"><Input id="model-reference" bind:value={appState.modelReference} placeholder="owner/repo" /><Button onclick={app.resolveModel} disabled={appState.busy}>Validate</Button></div></Field.Field>
            <dl class="grid gap-2 text-sm sm:grid-cols-2">
              <div><dt class="text-muted-foreground">Source</dt><dd class="truncate">{appState.modelResolution?.source || '-'}</dd></div>
              <div><dt class="text-muted-foreground">Layout</dt><dd>{appState.modelResolution ? (appState.modelResolution.multiFile ? 'multi-file' : 'single file') : '-'}</dd></div>
            </dl>

            <Separator />
            <p class="text-xs font-medium uppercase tracking-wider text-muted-foreground">2 · Choose artifact</p>
            {#if appState.modelPlan?.multiFile}
              <!-- multi_file_artifacts: MLX ships a directory of shards, config and
                   tokenizer, so there is no single filename to pick. The plan's
                   item count and target root are what describes the download. -->
              <div class="rounded-md border p-3 text-sm">
                <p class="font-medium">{appState.modelPlan.items.length} files</p>
                <p class="truncate text-xs text-muted-foreground" title={appState.modelPlan.targetRoot}>into {appState.modelPlan.targetRoot}</p>
                <p class="mt-1 text-xs text-muted-foreground">{formatBytes(Number(appState.modelPlan.totalBytes))} total</p>
              </div>
            {:else}
              <RadioGroup.Root bind:value={appState.selectedVariantReference} aria-label="Model artifact">
                {#each appState.modelResolution?.artifacts || [] as artifact (artifact.uri)}
                  <Field.Field orientation="horizontal" class="rounded-md border p-3">
                    <RadioGroup.Item id={`artifact-${artifact.name}`} value={artifact.uri} />
                    <Field.Content><Field.Label for={`artifact-${artifact.name}`}>{artifact.name}</Field.Label><Field.Description>{formatBytes(Number(artifact.sizeBytes))}</Field.Description></Field.Content>
                  </Field.Field>
                {:else}
                  <Empty.Root><Empty.Header><Empty.Title>No model resolved</Empty.Title><Empty.Description>Validate a model reference first.</Empty.Description></Empty.Header></Empty.Root>
                {/each}
              </RadioGroup.Root>
            {/if}

            <Separator />
            <p class="text-xs font-medium uppercase tracking-wider text-muted-foreground">3 · Download &amp; apply</p>
            <div class="flex flex-wrap items-center gap-3">
              <Button onclick={app.startDownload} disabled={appState.busy || !appState.modelReference.trim()}><Download /> Download</Button>
              <Button variant="outline" onclick={app.previewApplyToProfile} disabled={appState.busy || !appState.selectedProfileName || !app.canApplyDownload(app.activeDownload()?.state)}>Preview apply</Button>
              <Button variant="outline" onclick={app.applyToProfile} disabled={appState.busy || !appState.selectedProfileName || !app.canApplyDownload(app.activeDownload()?.state)}>Use in selected profile</Button>
            </div>
            {#if appState.profileApplyPreview}<DiffPreview original={appState.profileApplyPreview.original} current={appState.profileApplyPreview.updated} />{/if}
            {#if app.activeDownload()}
              {@const download = app.activeDownload()!}
              {@const summary = downloadSummary(download)}
              <div class="space-y-2">
                <div class="relative h-2 overflow-hidden rounded-full bg-muted"><div class="h-full bg-primary" style={`width:${download.percent}%`}></div></div>
                <Item.Root variant="outline">
                  <Item.Content>
                    <Item.Title>{summary.title}</Item.Title>
                    <Item.Description>{summary.detail} · {download.percent.toFixed(1)}%</Item.Description>
                  </Item.Content>
                  <Item.Actions>
                    {#if download.state === 'queued' || download.state === 'running'}<Button size="sm" variant="outline" onclick={app.cancelDownload} disabled={appState.busy}><X /> Cancel</Button>{/if}
                    <Badge variant="outline" class={downloadStatusClass(download.state, download.error)}>{download.state}</Badge>
                  </Item.Actions>
                </Item.Root>
                {#if download.error}<p class="text-sm text-destructive">{download.error}</p>{/if}
              </div>
            {/if}
          </Card.Content>
        </Card.Root>
      </div>
    </Tabs.Content>
  </Tabs.Root>
</div>

<Dialog.Root bind:open={createDialogOpen}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title>Create profile</Dialog.Title>
      <Dialog.Description>{selectedLocalModel?.path || 'Select an unconfigured model first.'}</Dialog.Description>
    </Dialog.Header>
    {#if selectedLocalModel}
      <form class="space-y-4" onsubmit={createLocalProfile}>
        <Field.Field><Field.Label for="local-profile-name">Profile name</Field.Label><Input id="local-profile-name" bind:value={draftName} autocomplete="off" /></Field.Field>
        <p class="text-sm text-muted-foreground">
          Engine arguments are seeded from {appState.selectedBackend}'s declared defaults; edit them in the Profiles panel after creating.
        </p>
        {#if !capabilities.parameterIntrospection}
          <p class="flex items-start gap-2 rounded-md border border-dashed p-3 text-sm text-muted-foreground">
            <Info class="mt-0.5 size-4 shrink-0" />
            <span>{appState.selectedBackend} does not expose a parameter list, so the profile starts with no engine arguments.</span>
          </p>
        {/if}
        <Dialog.Footer><Button type="button" variant="outline" onclick={() => (createDialogOpen = false)}>Cancel</Button><Button type="submit" disabled={appState.busy || !draftName.trim()}>Create profile</Button></Dialog.Footer>
      </form>
    {/if}
  </Dialog.Content>
</Dialog.Root>
