<script lang="ts">
  import type { Snippet } from 'svelte';
  import Activity from '@lucide/svelte/icons/activity';
  import Box from '@lucide/svelte/icons/box';
  import Cpu from '@lucide/svelte/icons/cpu';
  import Gauge from '@lucide/svelte/icons/gauge';
  import Moon from '@lucide/svelte/icons/moon';
  import ScrollText from '@lucide/svelte/icons/scroll-text';
  import Server from '@lucide/svelte/icons/server';
  import Settings from '@lucide/svelte/icons/settings';
  import Sun from '@lucide/svelte/icons/sun';
  import { resetMode, setMode } from 'mode-watcher';
  import type { InferenceRigState } from '../../lib/state.svelte';
  import { Alert, AlertDescription, AlertTitle } from '$lib/components/ui/alert';
  import { Button, buttonVariants } from '$lib/components/ui/button';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
  import * as Field from '$lib/components/ui/field';
  import { Input } from '$lib/components/ui/input';
  import { projectDisplayName, projectTitle } from '$lib/project';
  import LogoSmall from './LogoSmall.svelte';
  import LogoWide from './LogoWide.svelte';
  import { loadPrimaryColors, resetPrimaryColors, savePrimaryColors } from '$lib/theme';
  import { Separator } from '$lib/components/ui/separator';
  import * as Sheet from '$lib/components/ui/sheet';
  import * as Sidebar from '$lib/components/ui/sidebar';

  type Section = { id: string; label: string };
  type Props = {
    app: InferenceRigState;
    sections: Section[];
    sectionTitle: string | undefined;
    errorMessage: string;
    onSaveApiBase: () => void;
    onSaveToken: () => void;
    onTestConnection: () => void;
    children: Snippet;
  };

  let { app, sections, sectionTitle, errorMessage, onSaveApiBase, onSaveToken, onTestConnection, children }: Props = $props();
  let primaryColors = $state(loadPrimaryColors(localStorage));

  const icons = { runtime: Gauge, profiles: Server, models: Box, logs: ScrollText };

  function statusDot(status: string) {
    if (status === 'running') return 'bg-success';
    if (status === 'failed') return 'bg-destructive';
    return 'bg-warning';
  }

  function saveThemeColors() {
    savePrimaryColors(localStorage, primaryColors);
  }

  function resetThemeColors() {
    primaryColors = resetPrimaryColors(localStorage);
  }
</script>

<Sidebar.Provider>
  <Sidebar.Root collapsible="icon">
    <Sidebar.Header>
      <Sidebar.Menu>
        <Sidebar.MenuItem>
          <Sidebar.MenuButton size="lg" tooltipContent={projectTitle} class="h-auto pointer-events-none text-sidebar-foreground">
            <LogoSmall class="hidden size-full! shrink-0 group-data-[collapsible=icon]:block" />
            <LogoWide class="h-auto! w-full! group-data-[collapsible=icon]:hidden" />
            <span class="sr-only">{projectDisplayName}</span>
          </Sidebar.MenuButton>
        </Sidebar.MenuItem>
      </Sidebar.Menu>
    </Sidebar.Header>
    <Sidebar.Content>
      <Sidebar.Group>
        <Sidebar.GroupLabel>Operations</Sidebar.GroupLabel>
        <Sidebar.GroupContent>
          <Sidebar.Menu>
            {#each sections as section}
              {@const Icon = icons[section.id as keyof typeof icons]}
              <Sidebar.MenuItem>
                <Sidebar.MenuButton
                  isActive={app.activeSection === section.id}
                  tooltipContent={section.label}
                  onclick={() => (app.activeSection = section.id)}
                >
                  <Icon /> <span>{section.label}</span>
                </Sidebar.MenuButton>
              </Sidebar.MenuItem>
            {/each}
          </Sidebar.Menu>
        </Sidebar.GroupContent>
      </Sidebar.Group>
    </Sidebar.Content>
    <Sidebar.Footer>
      <Sidebar.Menu>
        <Sidebar.MenuItem>
          <Sidebar.MenuButton tooltipContent={`Backend: ${app.selectedBackend || 'none'}`} class="pointer-events-none">
            <Cpu /> <span class="truncate">{app.selectedBackend || 'no backend'}</span>
          </Sidebar.MenuButton>
        </Sidebar.MenuItem>
        <Sidebar.MenuItem>
          <Sidebar.MenuButton tooltipContent={`Runtime: ${app.runtimeStatus.status}`}>
            <Activity /> <span class={`size-2 rounded-full ${statusDot(app.runtimeStatus.status)}`}></span>
            <Sidebar.MenuBadge>{app.activeProfileNames.length}</Sidebar.MenuBadge>
          </Sidebar.MenuButton>
        </Sidebar.MenuItem>
      </Sidebar.Menu>
    </Sidebar.Footer>
    <Sidebar.Rail />
  </Sidebar.Root>

  <Sidebar.Inset>
    <header class="sticky top-0 z-10 flex min-h-16 items-center gap-3 border-b bg-background/80 px-4 py-3 backdrop-blur-md md:px-6">
      <Sidebar.Trigger />
      <div class="min-w-0 flex-1">
        <h1 class="truncate text-xl font-semibold">{sectionTitle}</h1>
        <p class="flex items-center gap-1.5 text-xs text-muted-foreground">
          <span class="relative flex size-2 shrink-0">
            {#if app.runtimeStatus.status === 'running'}<span class="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-60"></span>{/if}
            <span class={`relative inline-flex size-2 rounded-full ${statusDot(app.runtimeStatus.status)}`}></span>
          </span>
          <span class="capitalize">{app.runtimeStatus.status}</span>
          <span class="truncate">· {app.activeProfileNames.length ? app.activeProfileNames.join(', ') : 'no active profile'}</span>
        </p>
      </div>
      <Sheet.Root>
        <Sheet.Trigger class={buttonVariants({ variant: 'outline', size: 'icon' })} aria-label="Settings">
          <Settings class="size-4" />
        </Sheet.Trigger>
        <Sheet.Content side="right" class="w-full sm:max-w-sm">
          <Sheet.Header>
            <Sheet.Title>Settings</Sheet.Title>
            <Sheet.Description>Customize appearance and connect to an {projectDisplayName} API.</Sheet.Description>
          </Sheet.Header>
          <div class="grid gap-4 px-4">
            <div class="space-y-1"><h3 class="font-medium">Appearance</h3><p class="text-sm text-muted-foreground">Choose a primary color for each theme.</p></div>
            <Field.Field>
              <Field.Label for="light-primary">Light mode primary</Field.Label>
              <div class="flex items-center gap-3"><Input id="light-primary" type="color" class="h-10 w-16 cursor-pointer p-1" bind:value={primaryColors.light} oninput={saveThemeColors} /><span class="font-mono text-sm text-muted-foreground">{primaryColors.light}</span></div>
            </Field.Field>
            <Field.Field>
              <Field.Label for="dark-primary">Dark mode primary</Field.Label>
              <div class="flex items-center gap-3"><Input id="dark-primary" type="color" class="h-10 w-16 cursor-pointer p-1" bind:value={primaryColors.dark} oninput={saveThemeColors} /><span class="font-mono text-sm text-muted-foreground">{primaryColors.dark}</span></div>
            </Field.Field>
            <Button variant="outline" onclick={resetThemeColors}>Reset theme colors</Button>
            <Separator />
            <div class="space-y-1"><h3 class="font-medium">Connection</h3><p class="text-sm text-muted-foreground">Point the UI at an {projectDisplayName} API and authenticate.</p></div>
            <Field.Field>
              <Field.Label for="api-base">API base</Field.Label>
              <Input id="api-base" bind:value={app.apiBase} autocomplete="off" placeholder="same-origin API" oninput={onSaveApiBase} />
            </Field.Field>
            <Field.Field>
              <Field.Label for="api-token">Session token</Field.Label>
              <Input id="api-token" bind:value={app.token} type="password" autocomplete="off" placeholder="session token" oninput={onSaveToken} />
            </Field.Field>
            <Button variant="outline" onclick={onTestConnection} disabled={app.busy}>Test connection</Button>
          </div>
        </Sheet.Content>
      </Sheet.Root>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger class={buttonVariants({ variant: 'outline', size: 'icon' })} aria-label="Choose color theme">
          <Sun class="size-4 scale-100 rotate-0 transition-all dark:scale-0 dark:-rotate-90" />
          <Moon class="absolute size-4 scale-0 rotate-90 transition-all dark:scale-100 dark:rotate-0" />
        </DropdownMenu.Trigger>
        <DropdownMenu.Content align="end">
          <DropdownMenu.Item onclick={() => resetMode()}>System</DropdownMenu.Item>
          <DropdownMenu.Item onclick={() => setMode('light')}>Light</DropdownMenu.Item>
          <DropdownMenu.Item onclick={() => setMode('dark')}>Dark</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Root>
    </header>

    <main class="flex-1 p-4 md:p-6">
      <div class="mx-auto w-full max-w-7xl">
        {#if errorMessage}
          <Alert variant="destructive" class="mb-4">
            <AlertTitle>Operation failed</AlertTitle>
            <AlertDescription>{errorMessage}</AlertDescription>
          </Alert>
        {/if}
        {@render children()}
      </div>
    </main>
  </Sidebar.Inset>
</Sidebar.Provider>
