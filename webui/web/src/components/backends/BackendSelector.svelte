<script lang="ts">
  import type { BackendInfo } from '../../lib/gen/inferencerig/control/v1/control_pb';
  import { Badge } from '$lib/components/ui/badge';
  import * as Select from '$lib/components/ui/select';

  // The backend selector is the axis the whole UI now turns on: every panel
  // reads capabilities for whichever backend is chosen here rather than
  // assuming llama.cpp's shape.
  //
  // ponytail: with one registered backend there is nothing to choose, so the
  // whole control renders as nothing and the sidebar footer is the only place
  // the active backend is named. The guard lives here rather than in each of
  // the three panels that mount it.
  let {
    backends,
    value,
    disabled = false,
    onselect
  }: {
    backends: BackendInfo[];
    value: string;
    disabled?: boolean;
    onselect: (name: string) => void;
  } = $props();

  const selected = $derived(backends.find((backend) => backend.name === value));

  const capabilityChips = $derived.by(() => {
    const capabilities = selected?.capabilities;
    if (!capabilities) return [] as string[];
    return [
      capabilities.unifiedMemory ? 'unified memory' : '',
      capabilities.discreteVram ? 'discrete VRAM' : '',
      capabilities.multiFileArtifacts ? 'multi-file models' : '',
      capabilities.singleActiveProfile ? 'one profile at a time' : '',
      capabilities.managedInstall ? 'managed install' : '',
      capabilities.parameterIntrospection ? 'parameter introspection' : ''
    ].filter(Boolean);
  });
</script>

{#if backends.length > 1}
  <div class="flex flex-wrap items-center gap-2">
    <span class="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Backend</span>
    <Select.Root type="single" {value} onValueChange={(next: string) => onselect(next)} {disabled}>
      <Select.Trigger class="w-44" aria-label="Backend">{selected?.name || 'No backends'}</Select.Trigger>
      <Select.Content>
        {#each backends as backend (backend.name)}
          <Select.Item value={backend.name}>{backend.name}</Select.Item>
        {/each}
      </Select.Content>
    </Select.Root>
    {#each capabilityChips as chip}
      <Badge variant="outline" class="text-[11px]">{chip}</Badge>
    {/each}
  </div>
{/if}
