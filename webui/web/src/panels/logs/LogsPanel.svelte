<script lang="ts">
  import { onMount } from 'svelte';
  import RefreshCw from '@lucide/svelte/icons/refresh-cw';
  import Pause from '@lucide/svelte/icons/pause';
  import Play from '@lucide/svelte/icons/play';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import type { InferenceRigClient } from '../../lib/setup/createInferenceRigClient.svelte';
  import type { Event as ServerEvent } from '../../lib/gen/inferencerig/control/v1/control_pb';
  import { parseLogText, type ZapEntry } from '../../lib/logs';
  import { Badge } from '$lib/components/ui/badge';
  import * as AlertDialog from '$lib/components/ui/alert-dialog';
  import { Button, buttonVariants } from '$lib/components/ui/button';
  import { Input } from '$lib/components/ui/input';
  import * as Card from '$lib/components/ui/card';
  import * as Collapsible from '$lib/components/ui/collapsible';
  import * as Empty from '$lib/components/ui/empty';
  import * as Tabs from '$lib/components/ui/tabs';
  import { ScrollArea } from '$lib/components/ui/scroll-area';

  let { app }: { app: InferenceRigClient } = $props();
  const appState = $derived(app.state);

  let activeTab = $state('events');

  type Filter = 'all' | 'failures' | 'downloads';
  let filter = $state<Filter>('all');
  const filters: { value: Filter; label: string }[] = [
    { value: 'all', label: 'All' },
    { value: 'failures', label: 'Failures' },
    { value: 'downloads', label: 'Downloads' }
  ];
  const matchesEvent = (event: ServerEvent) =>
    filter === 'all' ||
    (filter === 'failures' && !event.success) ||
    (filter === 'downloads' && /download|resolve|model/i.test(event.action));
  const visibleEvents = $derived(appState.serverEvents.filter(matchesEvent));
  const eventTime = (time: string) => new Date(time).toLocaleTimeString();
  const statusLabel = (event: ServerEvent) => (event.success ? 'ok' : event.errorKind || 'failed');
  const statusClass = (event: ServerEvent) =>
    event.success ? 'bg-success/15 text-success border-success/30' : 'bg-destructive/15 text-destructive border-destructive/30';

  // Each tab reads its own service log now, so the split is by file rather
  // than by guessing which lines of one interleaved file came from which
  // process. Control lines are structured; engine output stays verbatim.
  const parsed = $derived(parseLogText(appState.logText));
  const engineLines = $derived(
    appState.engineLogText
      .split('\n')
      .filter((line) => line.trim())
      .map((text) => ({ text }))
  );

  let daemonSearch = $state('');
  let daemonLevel = $state('all');
  const daemonLevels = ['all', 'info', 'warn', 'error'];
  const daemonRows = $derived(
    parsed.daemon.filter(
      (entry) =>
        (daemonLevel === 'all' ||
          entry.level === daemonLevel ||
          (daemonLevel === 'error' && (entry.level === 'fatal' || entry.level === 'dpanic' || entry.level === 'panic'))) &&
        zapText(entry).toLowerCase().includes(daemonSearch.toLowerCase())
    )
  );

  // Raw lines are shown verbatim: the log service is a free-form name, so any
  // backend's runtime output can land here and there is no severity format the
  // client can assume across all of them.
  let rawSearch = $state('');
  const rawRows = $derived(engineLines.filter((line) => line.text.toLowerCase().includes(rawSearch.toLowerCase())));

  const levelClass = (level: string) =>
    level === 'warn'
      ? 'text-warning'
      : level === 'error' || level === 'fatal' || level === 'dpanic' || level === 'panic'
        ? 'text-destructive'
        : level === 'debug'
          ? 'text-muted-foreground'
          : 'text-success';

  const fieldText = (fields: Record<string, unknown>) =>
    Object.entries(fields)
      .map(([key, value]) => `${key}=${formatValue(value)}`)
      .join(' ');
  function formatValue(value: unknown): string {
    if (typeof value === 'string') return /\s/.test(value) ? JSON.stringify(value) : value;
    if (value === null || typeof value !== 'object') return String(value);
    return JSON.stringify(value);
  }
  function zapText(entry: ZapEntry): string {
    return `${entry.level} ${entry.msg} ${entry.caller} ${fieldText(entry.fields)}`;
  }
  const stackFrame = (stacktrace: string) => stacktrace.split('\n')[0];
  const fileTab = $derived(activeTab === 'daemon' || activeTab === 'raw');

  function selectService(service: string) {
    appState.logService = service;
    if (!appState.logPaused) app.refreshLogs().catch(() => undefined);
  }

  function setLineLimit(lines: number) {
    appState.logLines = lines;
    if (activeTab === 'archives') {
      if (appState.selectedLogArchiveId) app.selectLogArchive(appState.selectedLogArchiveId).catch(() => undefined);
    } else if (!appState.logPaused) {
      app.refreshLogs().catch(() => undefined);
    }
  }

  function refreshActive() {
    let promise;
    if (activeTab === 'events') {
      promise = app.refreshEvents();
    } else if (activeTab === 'archives') {
      promise = appState.selectedLogArchiveId ? app.selectLogArchive(appState.selectedLogArchiveId) : app.loadLogArchives();
    } else {
      promise = app.refreshLogs();
    }
    promise?.catch(app.showError);
  }

  let daemonViewport = $state<HTMLElement | null>(null);
  let rawViewport = $state<HTMLElement | null>(null);
  let daemonFollow = $state(true);
  let rawFollow = $state(true);

  const atBottom = (el: HTMLElement) => el.scrollHeight - el.scrollTop - el.clientHeight < 24;
  const stickToBottom = (el: HTMLElement | null, follow: boolean) => {
    if (el && follow) el.scrollTop = el.scrollHeight;
  };

  $effect(() => {
    void daemonRows.length;
    stickToBottom(daemonViewport, daemonFollow);
  });
  $effect(() => {
    void rawRows.length;
    stickToBottom(rawViewport, rawFollow);
  });

  onMount(() => {
    app.refreshLogs().catch(() => undefined);
    app.loadLogArchives().catch(() => undefined);
  });
</script>

<Card.Root>
  <Card.Header>
    <Card.Title>Logs</Card.Title>
    <Card.Description>Operations, the control daemon log, and the runtime engine log.</Card.Description>
    <Card.Action>
      <Button size="sm" variant="outline" onclick={refreshActive}><RefreshCw /> Refresh</Button>
    </Card.Action>
  </Card.Header>
  <Card.Content>
    <Tabs.Root bind:value={activeTab} class="space-y-4">
      <Tabs.List>
        <Tabs.Trigger value="events">Events</Tabs.Trigger>
        <Tabs.Trigger value="daemon">Control <Badge variant="secondary">{parsed.daemon.length}</Badge></Tabs.Trigger>
        <Tabs.Trigger value="raw">Engine <Badge variant="secondary">{engineLines.length}</Badge></Tabs.Trigger>
        <Tabs.Trigger value="archives" onclick={app.loadLogArchives}>Archives <Badge variant="secondary">{appState.logArchives.length}</Badge></Tabs.Trigger>
      </Tabs.List>

      {#if fileTab || activeTab === 'archives'}
        <div class="flex flex-wrap items-center gap-2">
          {#if activeTab === 'daemon'}
            <!-- The engine tab is pinned to the engine service; only the
                 control tab picks among the other service logs. -->
            <span class="text-xs text-muted-foreground">Service</span>
            {#each appState.logServices.filter((service) => service !== appState.engineLogService) as service}
              <Button size="sm" variant={appState.logService === service ? 'secondary' : 'ghost'} onclick={() => selectService(service)}>{service}</Button>
            {/each}
          {/if}
          <span class="text-xs text-muted-foreground">Tail</span>
          {#each [200, 500, 2000] as lines}
            <Button size="sm" variant={appState.logLines === lines ? 'secondary' : 'ghost'} onclick={() => setLineLimit(lines)}>{lines}</Button>
          {/each}
          {#if fileTab}
            {#if appState.logPaused}
              <Button size="sm" variant="outline" onclick={app.resumeLogs}><Play /> Resume</Button>
            {:else}
              <Button size="sm" variant="outline" onclick={() => (appState.logPaused = true)}><Pause /> Pause</Button>
            {/if}
          {/if}
        </div>
      {/if}

      <Tabs.Content value="events" class="space-y-4">
        <div class="flex flex-wrap gap-2">
          {#each filters as option}
            <Button size="sm" variant={filter === option.value ? 'secondary' : 'ghost'} onclick={() => (filter = option.value)}>{option.label}</Button>
          {/each}
        </div>

        <ScrollArea class="panel-scroll rounded-md border">
          {#if visibleEvents.length}
            <ul class="divide-y">
              {#each visibleEvents as event (event.id)}
                <li class={`flex items-center gap-3 px-3 py-2 text-sm ${event.success ? '' : 'bg-destructive/5'}`}>
                  <span class="min-w-20 shrink-0 font-mono text-xs text-muted-foreground">{eventTime(event.time)}</span>
                  <span class="min-w-0 flex-1 truncate font-medium">{event.action}{event.recovery ? ` ${event.recovery}` : ''}</span>
                  <Badge variant="outline" class={`shrink-0 ${statusClass(event)}`}>{statusLabel(event)}</Badge>
                  <span class="ml-auto shrink-0 font-mono text-xs text-muted-foreground">{event.duration || ''}</span>
                </li>
              {/each}
            </ul>
          {:else}
            <Empty.Root class="py-12"><Empty.Header><Empty.Title>No events</Empty.Title><Empty.Description>{filter === 'all' ? 'Operations will appear here as they run.' : 'No events match this filter.'}</Empty.Description></Empty.Header></Empty.Root>
          {/if}
        </ScrollArea>

        {#if appState.logEntries.length}
          <Collapsible.Root>
            <Collapsible.Trigger class={buttonVariants({ variant: 'ghost', size: 'sm' })}>UI activity</Collapsible.Trigger>
            <Collapsible.Content>
              <pre class="font-mono text-xs whitespace-pre-wrap text-muted-foreground">{appState.logEntries.join('\n')}</pre>
            </Collapsible.Content>
          </Collapsible.Root>
        {/if}
      </Tabs.Content>

      <Tabs.Content value="daemon" class="space-y-3">
        <div class="flex flex-wrap items-center gap-2">
          <Input class="h-8 max-w-xs" placeholder="Search control log…" bind:value={daemonSearch} />
          {#each daemonLevels as level}
            <Button size="sm" variant={daemonLevel === level ? 'secondary' : 'ghost'} onclick={() => (daemonLevel = level)}>{level === 'all' ? 'All' : level}</Button>
          {/each}
        </div>
        <ScrollArea
          class="panel-scroll rounded-md border"
          bind:viewportRef={daemonViewport}
          onscrollcapture={() => daemonViewport && (daemonFollow = atBottom(daemonViewport))}
        >
          {#if daemonRows.length}
            <div class="divide-y font-mono text-xs">
              {#each daemonRows as entry}
                <div class="px-3 py-1">
                  <div class="flex gap-2">
                    <span class="shrink-0 text-muted-foreground">{new Date(entry.ts * 1000).toLocaleTimeString()}</span>
                    <span class={`w-12 shrink-0 font-medium uppercase ${levelClass(entry.level)}`}>{entry.level}</span>
                    <span class="min-w-0 break-all">{entry.msg}</span>
                    {#if Object.keys(entry.fields).length}<span class="min-w-0 break-all text-muted-foreground">{fieldText(entry.fields)}</span>{/if}
                  </div>
                  {#if entry.stacktrace}<div class="pl-14 text-muted-foreground">↳ {stackFrame(entry.stacktrace)}</div>{/if}
                </div>
              {/each}
            </div>
          {:else}
            <Empty.Root class="py-12"><Empty.Header><Empty.Title>No control log</Empty.Title><Empty.Description>Lines appear once the {appState.logService} service writes structured output.</Empty.Description></Empty.Header></Empty.Root>
          {/if}
        </ScrollArea>
      </Tabs.Content>

      <Tabs.Content value="raw" class="space-y-3">
        <div class="flex flex-wrap items-center gap-2">
          <Input class="h-8 max-w-xs" placeholder="Search engine output…" bind:value={rawSearch} />
        </div>
        <ScrollArea
          class="panel-scroll rounded-md border"
          bind:viewportRef={rawViewport}
          onscrollcapture={() => rawViewport && (rawFollow = atBottom(rawViewport))}
        >
          {#if rawRows.length}
            <div class="font-mono text-xs">
              {#each rawRows as line}
                <div class="px-3 py-0.5 break-all">{line.text}</div>
              {/each}
            </div>
          {:else}
            <Empty.Root class="py-12"><Empty.Header><Empty.Title>No engine output</Empty.Title><Empty.Description>Output appears once a profile is running.</Empty.Description></Empty.Header></Empty.Root>
          {/if}
        </ScrollArea>
      </Tabs.Content>

      <Tabs.Content value="archives" class="space-y-3">
        <div class="flex justify-end">
          <AlertDialog.Root>
            <AlertDialog.Trigger class={buttonVariants({ variant: 'destructive', size: 'sm' })} disabled={!appState.logArchives.length}><Trash2 /> Clear archives</AlertDialog.Trigger>
            <AlertDialog.Content>
              <AlertDialog.Header><AlertDialog.Title>Clear all log archives?</AlertDialog.Title><AlertDialog.Description>This permanently deletes every archived service log. Active logs are not affected.</AlertDialog.Description></AlertDialog.Header>
              <AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={app.clearLogArchives}>Clear archives</AlertDialog.Action></AlertDialog.Footer>
            </AlertDialog.Content>
          </AlertDialog.Root>
        </div>
        <div class="grid gap-3 lg:grid-cols-[24rem_minmax(0,1fr)]">
          <div class="space-y-2">
            {#each appState.logArchives as archive (archive.id)}
              <div class="flex items-center gap-2 rounded-md border p-2">
                <Button variant="ghost" class="h-auto min-w-0 flex-1 justify-start text-left" onclick={() => app.selectLogArchive(archive.id)}>
                  <span class="min-w-0"><span class="block truncate font-medium">{archive.service}</span><span class="block text-xs text-muted-foreground">{new Date(archive.archivedAt).toLocaleString()} · {Number(archive.sizeBytes).toLocaleString()} bytes</span></span>
                </Button>
                <AlertDialog.Root>
                  <AlertDialog.Trigger class={buttonVariants({ variant: 'ghost', size: 'icon-sm' })} aria-label={`Delete ${archive.id}`}><Trash2 /></AlertDialog.Trigger>
                  <AlertDialog.Content>
                    <AlertDialog.Header><AlertDialog.Title>Delete this log archive?</AlertDialog.Title><AlertDialog.Description>{archive.id} will be permanently deleted.</AlertDialog.Description></AlertDialog.Header>
                    <AlertDialog.Footer><AlertDialog.Cancel>Cancel</AlertDialog.Cancel><AlertDialog.Action onclick={() => app.deleteLogArchive(archive.id)}>Delete archive</AlertDialog.Action></AlertDialog.Footer>
                  </AlertDialog.Content>
                </AlertDialog.Root>
              </div>
            {:else}
              <Empty.Root><Empty.Header><Empty.Title>No log archives</Empty.Title><Empty.Description>Previous process logs appear here after restart.</Empty.Description></Empty.Header></Empty.Root>
            {/each}
          </div>
          <ScrollArea class="panel-scroll rounded-md border">
            {#if appState.selectedLogArchiveId}
              <pre class="p-3 font-mono text-xs whitespace-pre-wrap break-all">{appState.logArchiveText}</pre>
            {:else}
              <Empty.Root class="py-12"><Empty.Header><Empty.Title>Select an archive</Empty.Title><Empty.Description>Choose an archived log to view its tail.</Empty.Description></Empty.Header></Empty.Root>
            {/if}
          </ScrollArea>
        </div>
      </Tabs.Content>
    </Tabs.Root>
  </Card.Content>
</Card.Root>
