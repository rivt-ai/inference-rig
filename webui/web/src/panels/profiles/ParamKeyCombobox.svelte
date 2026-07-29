<script lang="ts">
  import { Combobox } from 'bits-ui';
  import Check from '@lucide/svelte/icons/check';
  import ChevronsUpDown from '@lucide/svelte/icons/chevrons-up-down';
  import type { BackendParameter } from '../../lib/gen/inferencerig/control/v1/control_pb';
  import { Input } from '$lib/components/ui/input';

  // Completion is offered only when the backend can enumerate its own
  // parameters. When parameter_introspection is false the combobox would be a
  // permanently empty dropdown implying no key is valid, so the field degrades
  // to plain free text instead.
  let {
    value = $bindable(''),
    params,
    introspection = true,
    disabled = false,
    oninput
  }: {
    value: string;
    params: BackendParameter[];
    introspection?: boolean;
    disabled?: boolean;
    oninput?: () => void;
  } = $props();

  let inputValue = $state(value);

  $effect(() => {
    inputValue = value;
  });

  const filtered = $derived(
    inputValue.trim()
      ? params.filter(
          (param) =>
            param.name.includes(inputValue.toLowerCase()) ||
            param.aliases.some((alias) => alias.includes(inputValue.toLowerCase()))
        )
      : params
  );

  function onValueChange(next: string) {
    value = next;
    inputValue = next;
    oninput?.();
  }

  function onInputChange(event: Event) {
    inputValue = (event.target as HTMLInputElement).value;
    value = inputValue;
    oninput?.();
  }
</script>

{#if introspection}
  <Combobox.Root type="single" {value} {inputValue} {onValueChange} {disabled}>
    <div class="relative w-40">
      <Combobox.Input
        class="flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 pr-8 font-mono text-sm shadow-xs transition-colors placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
        placeholder="key"
        oninput={onInputChange}
        aria-label="Parameter key"
      />
      <Combobox.Trigger class="absolute inset-y-0 right-0 flex items-center pr-2 text-muted-foreground">
        <ChevronsUpDown class="size-3.5" />
      </Combobox.Trigger>
    </div>
    <Combobox.Portal>
      <Combobox.Content class="z-50 min-w-64 max-w-80 rounded-md border bg-popover p-1 shadow-md">
        <Combobox.Viewport class="max-h-60 overflow-y-auto">
          {#each filtered as param (param.name)}
            <Combobox.Item
              value={param.name}
              label={param.name}
              class="flex cursor-default select-none items-start gap-2 rounded-sm px-2 py-1.5 text-sm outline-none data-[highlighted]:bg-accent data-[highlighted]:text-accent-foreground"
            >
              {#snippet children({ selected })}
                <Check class="mt-0.5 size-4 shrink-0 {selected ? 'opacity-100' : 'opacity-0'}" />
                <div class="min-w-0">
                  <p class="font-mono leading-snug">{param.name}</p>
                  <p class="text-xs text-muted-foreground leading-snug truncate">{param.description}</p>
                </div>
              {/snippet}
            </Combobox.Item>
          {:else}
            <p class="px-2 py-1.5 text-sm text-muted-foreground">No matching params.</p>
          {/each}
        </Combobox.Viewport>
      </Combobox.Content>
    </Combobox.Portal>
  </Combobox.Root>
{:else}
  <Input
    class="w-40 font-mono text-sm"
    bind:value
    placeholder="key"
    aria-label="Parameter key"
    {disabled}
    oninput={() => oninput?.()}
  />
{/if}
