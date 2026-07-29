<script lang="ts">
  import Download from '@lucide/svelte/icons/download';
  import Info from '@lucide/svelte/icons/info';
  import type { GetBackendInstallStatusResponse } from '../../lib/gen/inferencerig/control/v1/control_pb';
  import { installUnavailableReason } from '../../lib/backends';
  import { Badge } from '$lib/components/ui/badge';
  import { Button } from '$lib/components/ui/button';
  import * as Card from '$lib/components/ui/card';

  // Shown only for backends whose capabilities declare managed_install. When
  // the installer cannot run on this host — MLX's is macOS/arm64-only by
  // design — the action is disabled with the reason stated up front, rather
  // than letting the user press it and reading the failure in a toast.
  let {
    backend,
    status,
    platform,
    busy = false,
    oninstall
  }: {
    backend: string;
    status: GetBackendInstallStatusResponse | null;
    platform: string;
    busy?: boolean;
    oninstall: (options: { upgrade?: boolean }) => void;
  } = $props();

  const blocked = $derived(installUnavailableReason(backend, platform));
</script>

<Card.Root>
  <Card.Header>
    <Card.Title>Backend install</Card.Title>
    <Card.Description>{backend} is installed and upgraded by {'InferenceRig'} itself.</Card.Description>
    <Card.Action>
      {#if status?.installed}
        <Badge variant="outline" class="border-success/30 bg-success/15 text-success">installed</Badge>
      {:else}
        <Badge variant="outline" class="border-warning/30 bg-warning/15 text-warning-foreground dark:text-warning">not installed</Badge>
      {/if}
    </Card.Action>
  </Card.Header>
  <Card.Content class="space-y-3">
    <dl class="grid gap-2 text-sm sm:grid-cols-3">
      <div><dt class="text-muted-foreground">Version</dt><dd class="font-mono">{status?.version || '-'}</dd></div>
      <div><dt class="text-muted-foreground">Managed</dt><dd>{status?.managed ? 'yes' : 'no'}</dd></div>
      <div class="min-w-0"><dt class="text-muted-foreground">Path</dt><dd class="truncate font-mono" title={status?.path}>{status?.path || '-'}</dd></div>
    </dl>
    {#if blocked}
      <p class="flex items-start gap-2 rounded-md border border-warning/40 bg-warning/10 p-3 text-sm text-warning-foreground dark:text-warning">
        <Info class="mt-0.5 size-4 shrink-0" />
        <span>{blocked}</span>
      </p>
    {/if}
    <div class="flex flex-wrap gap-2">
      <Button size="sm" disabled={busy || Boolean(blocked) || status?.installed} onclick={() => oninstall({})}>
        <Download /> Install
      </Button>
      <Button size="sm" variant="outline" disabled={busy || Boolean(blocked) || !status?.installed} onclick={() => oninstall({ upgrade: true })}>
        Upgrade
      </Button>
    </div>
  </Card.Content>
</Card.Root>
