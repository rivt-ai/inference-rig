<script lang="ts">
  import AppShell from '../components/shell/AppShell.svelte';
  import type { InferenceRigState } from '../lib/state.svelte';

  let { app }: { app: InferenceRigState } = $props();
</script>

<AppShell
  {app}
  sections={[{ id: 'runtime', label: 'Dashboard' }, { id: 'profiles', label: 'Profiles' }]}
  sectionTitle="Dashboard"
  errorMessage=""
  onSaveApiBase={() => {}}
  onSaveToken={() => {}}
  onTestConnection={() => {}}
>
  <p>Dashboard content</p>
</AppShell>
