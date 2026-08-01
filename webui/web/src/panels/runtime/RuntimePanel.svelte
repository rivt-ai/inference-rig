<script lang="ts">
  import Activity from '@lucide/svelte/icons/activity';
  import Box from '@lucide/svelte/icons/box';
  import HardDrive from '@lucide/svelte/icons/hard-drive';
  import Radio from '@lucide/svelte/icons/radio';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Server from '@lucide/svelte/icons/server';
  import { formatBytes, formatDate } from '../../lib/formatting';
  import { acceleratorKey } from '../../lib/runtimeHistory';
  import { showsTemperature, showsUtilization, usesUnifiedMemory } from '../../lib/backends';
  import type { InferenceRigClient } from '../../lib/setup/createInferenceRigClient.svelte';
  import BackendInstallCard from '../../components/backends/BackendInstallCard.svelte';
  import BackendSelector from '../../components/backends/BackendSelector.svelte';
  import ResourceMeter from '../../components/metrics/ResourceMeter.svelte';
  import TrendChart from '../../components/metrics/TrendChart.svelte';
  import * as AlertDialog from '$lib/components/ui/alert-dialog';
  import { Badge } from '$lib/components/ui/badge';
  import { Button, buttonVariants } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';
  import * as Item from '$lib/components/ui/item';

  let { app }: { app: InferenceRigClient } = $props();
  const appState = $derived(app.state);
  const capabilities = $derived(app.capabilities());

  function statusDot(status: string) {
    if (status === 'running') return 'bg-success';
    if (status === 'failed') return 'bg-destructive';
    return 'bg-warning';
  }

  function percent(used: number, total: number) {
    return total ? (used / total) * 100 : 0;
  }

  const localModelBytes = $derived(appState.localModels.reduce((sum, model) => sum + Number(model.sizeBytes), 0));
  const capturedAt = $derived(appState.signals?.capturedAt || '');
  const stale = $derived(!!appState.signalsLastError);
  const accelerators = $derived(appState.signals?.accelerators || []);

  // On a unified-memory host the accelerator's bytes ARE system memory. Showing
  // a RAM meter and a VRAM meter side by side reports the same allocation
  // twice and makes a half-full machine look full. This collapses them into one
  // meter, driven by Accelerator.unified_memory rather than a platform sniff —
  // a discrete GPU in a Mac must still get its own meter.
  const unified = $derived(usesUnifiedMemory(appState.signals, capabilities));

  const memoryTotal = $derived(Number(appState.signals?.totalMemoryBytes || 0));
  const memoryUsed = $derived(Number(appState.signals?.usedMemoryBytes || 0));
  const memoryPercent = $derived(appState.signals?.usedMemoryPercent || percent(memoryUsed, memoryTotal));
  const memoryFree = $derived(Number(appState.signals?.availableMemoryBytes || 0));

  async function refreshDashboard() {
    await app.runTask('refresh dashboard', async () => {
      await Promise.all([app.refreshRuntimeStatus(), app.refreshSignals(), app.refreshEvents()]);
    });
  }
</script>

<div class="space-y-5">
  <div class="flex flex-wrap items-end justify-between gap-3 border-b pb-4">
    <div class="space-y-1"><h2 class="text-xl font-semibold tracking-tight">System overview</h2><p class="text-sm text-muted-foreground">Live status and five-minute resource trends.</p></div>
    <div class="flex flex-wrap items-center gap-3">
      <BackendSelector backends={appState.backends} value={appState.selectedBackend} disabled={appState.busy} onselect={app.selectBackend} />
      <Button size="sm" variant="outline" class="bg-background/60" onclick={refreshDashboard} disabled={appState.busy}><RefreshCw class={appState.busy ? 'animate-spin' : ''} /> Refresh</Button>
    </div>
  </div>

  <!-- Setup creates the config only, so a fresh install arrives here with no
       profile at all. Nothing can be started until one exists. -->
  {#if appState.profilesLoaded && !appState.profiles.length}
    <div
      class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-warning/40 bg-warning/10 p-4"
      role="status"
    >
      <div class="space-y-1">
        <p class="text-sm font-semibold">No profiles configured</p>
        <p class="text-sm text-muted-foreground">Create a serving profile and download a model for it to start serving.</p>
      </div>
      <Button size="sm" onclick={() => (appState.activeSection = 'profiles')}>Create a profile</Button>
    </div>
  {/if}

  <section class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4" aria-label="Dashboard summary">
    {#snippet statIcon(Icon: typeof Activity)}
      <span class="flex size-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"><Icon class="size-4" /></span>
    {/snippet}
    <Card.Root size="sm" class="bg-card/70 shadow-sm">
      <Card.Content class="flex items-start justify-between gap-3">
        <div class="min-w-0 space-y-1.5">
          <p class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Runtime</p>
          <p class="flex items-center gap-2 text-2xl font-semibold capitalize"><span class={`size-2.5 shrink-0 rounded-full ${statusDot(appState.runtimeStatus.status)}`}></span>{appState.runtimeStatus.status}</p>
          <p class="truncate text-sm text-muted-foreground" title={appState.runtimeStatus.detail}>{appState.runtimeStatus.detail}</p>
        </div>
        {@render statIcon(Activity)}
      </Card.Content>
    </Card.Root>
    <Card.Root size="sm" class="bg-card/70 shadow-sm">
      <Card.Content class="flex items-start justify-between gap-3">
        <div class="min-w-0 space-y-1.5">
          <p class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Active profiles</p>
          <p class="text-2xl font-semibold tabular-nums">{appState.activeProfileNames.length}{capabilities.singleActiveProfile ? ' / 1' : ''}</p>
          <p class="truncate text-sm text-muted-foreground">{appState.activeProfileNames.join(', ') || 'None running'}</p>
        </div>
        {@render statIcon(Server)}
      </Card.Content>
    </Card.Root>
    <Card.Root size="sm" class="bg-card/70 shadow-sm">
      <Card.Content class="flex items-start justify-between gap-3">
        <div class="min-w-0 space-y-1.5">
          <p class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Models space used</p>
          <p class="text-2xl font-semibold tabular-nums">{formatBytes(localModelBytes)}</p>
          <p class="truncate text-sm text-muted-foreground">{appState.localModels.length} local models</p>
          <Button size="sm" variant="link" class="h-auto p-0" onclick={() => (appState.activeSection = 'models')}>Manage models</Button>
        </div>
        {@render statIcon(Box)}
      </Card.Content>
    </Card.Root>
    <Card.Root size="sm" class={`bg-card/70 shadow-sm ${stale ? 'ring-warning/50' : ''}`}>
      <Card.Content class="flex items-start justify-between gap-3">
        <div class="min-w-0 space-y-1.5">
          <p class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Telemetry</p>
          {#if stale}
            <p class="flex items-center gap-2 text-2xl font-semibold text-warning-foreground dark:text-warning"><span class="size-2.5 shrink-0 rounded-full bg-warning"></span>Stale</p>
          {:else}
            <p class="flex items-center gap-2 text-2xl font-semibold text-success"><span class="relative flex size-2.5 shrink-0"><span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-60"></span><span class="relative inline-flex size-2.5 rounded-full bg-success"></span></span>Live</p>
          {/if}
          <p class="truncate text-sm text-muted-foreground">{capturedAt ? formatDate(capturedAt) : 'Awaiting first sample'}</p>
        </div>
        {@render statIcon(Radio)}
      </Card.Content>
    </Card.Root>
  </section>

  <div class="grid gap-3 lg:grid-cols-2">
    <Card.Root class="overflow-hidden border-primary/15 shadow-sm">
      <Card.Header>
        <Card.Title>Active profiles</Card.Title>
        <Card.Description>Stop or restart a running profile. Active requests may be interrupted.</Card.Description>
        <Card.Action><Button size="sm" variant="outline" onclick={() => (appState.activeSection = 'profiles')}>Manage profiles</Button></Card.Action>
      </Card.Header>
      <Card.Content>
        {#if appState.activeProfileNames.length}
          <Item.Group>
            {#each appState.activeProfileNames as name}
              <Item.Root variant="outline" class="bg-muted/20">
                <Item.Content><Item.Title class="flex items-center gap-2"><span class="size-2 rounded-full bg-success shadow-[0_0_0_3px_color-mix(in_oklab,var(--color-success)_18%,transparent)]"></span>{name}</Item.Title><Item.Description>Running</Item.Description></Item.Content>
                <Item.Actions>
                  <AlertDialog.Root>
                    <AlertDialog.Trigger class={buttonVariants({ variant: 'outline', size: 'sm' })} disabled={appState.busy}>Restart</AlertDialog.Trigger>
                    <AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>Restart {name}?</AlertDialog.Title><AlertDialog.Description>Restarting this profile may interrupt active requests.</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={() => app.restartProfile(name)}>Restart profile</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content>
                  </AlertDialog.Root>
                  <AlertDialog.Root>
                    <AlertDialog.Trigger class={buttonVariants({ variant: 'destructive', size: 'sm' })} disabled={appState.busy}>Stop</AlertDialog.Trigger>
                    <AlertDialog.Content><AlertDialog.Header><AlertDialog.Title>Stop {name}?</AlertDialog.Title><AlertDialog.Description>Stopping this profile may interrupt active requests.</AlertDialog.Description></AlertDialog.Header><AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={() => app.stopProfile(name)}>Stop profile</AlertDialog.Action></AlertDialog.Footer></AlertDialog.Content>
                  </AlertDialog.Root>
                </Item.Actions>
              </Item.Root>
            {/each}
          </Item.Group>
        {:else}
          <div class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-dashed p-4"><p class="text-sm text-muted-foreground">No active profiles.</p><Button size="sm" onclick={() => (appState.activeSection = 'profiles')}>Choose a profile</Button></div>
        {/if}
      </Card.Content>
    </Card.Root>

    <Card.Root>
      <Card.Header><Card.Title>Operational details</Card.Title><Card.Description>Processes, warnings, and most recent action.</Card.Description></Card.Header>
      <Card.Content class="space-y-4">
        {#if appState.signalsLastError}<p class="rounded-md border border-warning/40 bg-warning/10 p-3 text-sm text-warning-foreground dark:text-warning">Telemetry refresh failed: {appState.signalsLastError}</p>{/if}
        {#each appState.signals?.warnings || [] as warning}<p class="rounded-md border border-warning/30 p-3 text-sm text-warning-foreground dark:text-warning">{warning}</p>{/each}
        {#if (appState.signals?.runtime || []).length}
          <Item.Group>{#each appState.signals?.runtime || [] as proc}<Item.Root variant="outline"><Item.Content><Item.Title>{proc.name || String(proc.pid)}</Item.Title><Item.Description>pid {proc.pid} · RSS {formatBytes(Number(proc.rssBytes))} · CPU {proc.cpuPercent.toFixed(1)}%</Item.Description></Item.Content></Item.Root>{/each}</Item.Group>
        {:else}<p class="text-sm text-muted-foreground">No runtime processes reported.</p>{/if}
        {#if appState.lastOperation}
          {@const op = appState.lastOperation}
          {@const failed = op.exitCode !== 0}
          <div class={`rounded-lg border p-3 ${failed ? 'border-destructive/30 bg-destructive/10 text-destructive' : 'border-success/30 bg-success/10 text-success'}`}>
            <div class="flex items-start justify-between gap-3">
              <div class="space-y-1">
                <Badge variant="outline" class={failed ? 'border-destructive/30 bg-destructive/10 text-destructive' : 'border-success/30 bg-success/10 text-success'}>{failed ? 'failed' : 'succeeded'}</Badge>
                <p class="font-medium text-foreground">{op.action || 'Last operation'}</p>
                {#if op.stderr || op.stdout}<p class="text-sm text-muted-foreground break-all">{op.stderr || op.stdout}</p>{/if}
              </div>
              <span class="font-mono text-sm text-muted-foreground">{(Number(op.durationMs) / 1000).toFixed(2)}s</span>
            </div>
          </div>
        {/if}
      </Card.Content>
    </Card.Root>
  </div>

  {#if capabilities.managedInstall}
    <BackendInstallCard
      backend={appState.selectedBackend}
      status={appState.backendInstall}
      platform={appState.hostPlatform}
      busy={appState.busy}
      oninstall={app.installBackend}
    />
  {/if}

  <section class="space-y-3" aria-labelledby="resources-title">
    <div><p class="text-xs font-semibold uppercase tracking-[0.18em] text-muted-foreground">Telemetry</p><h2 id="resources-title" class="text-lg font-semibold">Resources</h2><p class="text-sm text-muted-foreground">Current machine load.</p></div>
    <div class="grid gap-3 md:grid-cols-2">
      <Card.Root>
        <Card.Header><Card.Title>System</Card.Title><Card.Description>{unified ? 'CPU and unified memory' : 'CPU and memory'}</Card.Description></Card.Header>
        <Card.Content class="space-y-4">
          <ResourceMeter label="CPU" percent={appState.signals?.cpuUsedPercent} detail={`${appState.signals?.logicalCpuCores || '-'} cores`} />
          {#if unified}
            <ResourceMeter
              label="Unified memory"
              percent={memoryPercent}
              detail={`${formatBytes(memoryFree)} free of ${formatBytes(memoryTotal)} — shared by CPU and GPU`}
            />
          {:else}
            <ResourceMeter label="Memory" percent={memoryPercent} detail={`${formatBytes(memoryFree)} free`} />
          {/if}
        </Card.Content>
      </Card.Root>
      {#each accelerators as accelerator, index (acceleratorKey(accelerator.source, accelerator.name, index))}
        <Card.Root>
          <Card.Header><Card.Title class="truncate" title={accelerator.name}>{accelerator.name || `Accelerator ${index + 1}`}</Card.Title><Card.Description>{accelerator.source || 'accelerator'}</Card.Description></Card.Header>
          <Card.Content class="space-y-4">
            {#if showsUtilization(accelerator)}
              <ResourceMeter label="Utilisation" percent={accelerator.utilizationPercent} />
            {:else}
              <div class="flex items-baseline justify-between text-sm"><span class="font-medium">Utilisation</span><span class="text-muted-foreground">Not reported</span></div>
            {/if}
            {#if unified}
              <p class="rounded-md border border-dashed p-3 text-sm text-muted-foreground">
                This device shares system memory; see the unified memory meter above.
              </p>
            {:else}
              <ResourceMeter
                label="VRAM"
                percent={percent(Number(accelerator.usedMemoryBytes), Number(accelerator.totalMemoryBytes))}
                detail={`${formatBytes(Number(accelerator.usedMemoryBytes))} / ${formatBytes(Number(accelerator.totalMemoryBytes))}`}
              />
            {/if}
            <div class="flex items-baseline justify-between text-sm">
              <span class="font-medium">Temperature</span>
              <span class="tabular-nums text-muted-foreground">{showsTemperature(accelerator) ? `${accelerator.temperatureCelsius.toFixed(0)}°C` : 'Not reported'}</span>
            </div>
          </Card.Content>
        </Card.Root>
      {/each}
    </div>
    {#if !accelerators.length}<p class="rounded-lg border border-dashed p-4 text-sm text-muted-foreground">Accelerator telemetry unavailable.</p>{/if}
  </section>

  {#if appState.disks.length}
    <Card.Root>
      <Card.Header>
        <Card.Title class="flex items-center gap-2"><HardDrive class="size-4" /> Disks</Card.Title>
        <Card.Description>Model downloads land on these volumes.</Card.Description>
      </Card.Header>
      <Card.Content class="grid gap-4 md:grid-cols-2">
        {#each appState.disks as disk (disk.path)}
          <ResourceMeter
            label={disk.label || disk.path}
            percent={disk.usedPercent || percent(Number(disk.usedBytes), Number(disk.totalBytes))}
            detail={`${formatBytes(Number(disk.freeBytes))} free of ${formatBytes(Number(disk.totalBytes))}`}
          />
        {/each}
      </Card.Content>
    </Card.Root>
  {/if}

  <Card.Root>
    <Card.Header><Card.Title>Live trends</Card.Title><Card.Description>Browser-local rolling window; history resets when the page reloads.</Card.Description></Card.Header>
    <Card.Content class="grid gap-6 md:grid-cols-2 xl:grid-cols-3">
      <TrendChart label="CPU" points={appState.runtimeHistory.map((sample) => ({ capturedAt: sample.capturedAt, value: sample.cpu }))} />
      <TrendChart label={unified ? 'Unified memory' : 'Memory'} points={appState.runtimeHistory.map((sample) => ({ capturedAt: sample.capturedAt, value: sample.memory }))} />
      {#each accelerators as accelerator, index}
        {@const key = acceleratorKey(accelerator.source, accelerator.name, index)}
        {@const label = accelerator.name || `Accelerator ${index + 1}`}
        {#if showsUtilization(accelerator)}
          <TrendChart label={`${label} utilisation`} points={appState.runtimeHistory.map((sample) => ({ capturedAt: sample.capturedAt, value: sample.accelerators.find((item) => item.key === key)?.utilization ?? null }))} />
        {/if}
        {#if !unified}
          <TrendChart label={`${label} VRAM`} points={appState.runtimeHistory.map((sample) => ({ capturedAt: sample.capturedAt, value: sample.accelerators.find((item) => item.key === key)?.memory ?? null }))} />
        {/if}
        {#if showsTemperature(accelerator)}
          <TrendChart label={`${label} temperature`} unit="°C" points={appState.runtimeHistory.map((sample) => ({ capturedAt: sample.capturedAt, value: sample.accelerators.find((item) => item.key === key)?.temperature ?? null }))} />
        {/if}
      {/each}
    </Card.Content>
  </Card.Root>
</div>
