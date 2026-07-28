<script lang="ts">
  import { onMount } from 'svelte';
  import { get, post, put, remove, setToken } from './api';

  type Section = 'overview' | 'profiles' | 'models' | 'activity';
  let section: Section = $state('overview');
  let loading = $state(false);
  let error = $state('');
  let token = $state('');
  let info: any = $state({});
  let backends: any[] = $state([]);
  let profiles: any[] = $state([]);
  let signals: any = $state({});
  let events: any[] = $state([]);
  let catalog: any[] = $state([]);
  let localModels: any[] = $state([]);
  let selectedBackend = $state('');
  let query = $state('');
  let editing: any = $state(null);

  async function refresh() {
    loading = true;
    error = '';
    try {
      const [infoResponse, backendResponse, profileResponse, signalResponse, eventResponse] = await Promise.all([
        get('/api/info'), get('/api/backends'), get('/api/profiles'), get('/api/signals'), get('/api/events')
      ]);
      info = infoResponse;
      backends = backendResponse.backends || [];
      profiles = profileResponse.profiles || [];
      signals = signalResponse.signals || {};
      events = eventResponse.events || [];
      selectedBackend ||= backends[0]?.name || '';
      if (section === 'models' && selectedBackend) await refreshModels();
    } catch (cause) {
      error = cause instanceof Error ? cause.message : String(cause);
    } finally {
      loading = false;
    }
  }

  async function refreshModels() {
    const encoded = encodeURIComponent(selectedBackend);
    const [remote, local] = await Promise.all([
      get(`/api/catalog?backend=${encoded}&query=${encodeURIComponent(query)}`),
      get(`/api/models/local?backend=${encoded}`)
    ]);
    catalog = remote.models || [];
    localModels = local.models || [];
  }

  async function action(work: () => Promise<unknown>) {
    error = '';
    try { await work(); await refresh(); }
    catch (cause) { error = cause instanceof Error ? cause.message : String(cause); }
  }

  async function editProfile(name: string) {
    const response: any = await get(`/api/profiles/${encodeURIComponent(name)}`);
    editing = { ...response.profile };
  }

  async function saveProfile() {
    await put(`/api/profiles/${encodeURIComponent(editing.name)}`, { profileYaml: editing.profileYaml });
    editing = null;
    await refresh();
  }

  function choose(next: Section) {
    section = next;
    refresh();
  }

  onMount(refresh);
</script>

<svelte:head><meta name="description" content="Neutral local inference control plane" /></svelte:head>

<header>
  <div><span class="mark">IR</span><strong>InferenceRig</strong><small>local control plane</small></div>
  <nav aria-label="Main navigation">
    {#each ['overview', 'profiles', 'models', 'activity'] as item}
      <button class:active={section === item} onclick={() => choose(item as Section)}>{item}</button>
    {/each}
  </nav>
  <label>Token <input type="password" bind:value={token} onchange={() => setToken(token)} placeholder="optional" /></label>
  <button class="primary" onclick={refresh} disabled={loading}>{loading ? 'Loading…' : 'Refresh'}</button>
</header>

<main>
  {#if error}<div class="error" role="alert">{error}</div>{/if}

  {#if section === 'overview'}
    <section class="hero">
      <p class="eyebrow">SYSTEM OVERVIEW</p>
      <h1>{info.runningProfiles?.length ? 'Inference is active' : 'Ready when you are'}</h1>
      <p>One control plane for every installed inference backend.</p>
    </section>
    <section class="metrics">
      <article><span>Backends</span><strong>{info.backends || backends.length}</strong></article>
      <article><span>Profiles</span><strong>{info.profiles || profiles.length}</strong></article>
      <article><span>Running</span><strong>{info.runningProfiles?.length || 0}</strong></article>
      <article><span>Memory free</span><strong>{Math.round((signals.availableMemoryBytes || 0) / 1073741824)} GiB</strong></article>
    </section>
    <section class="panel">
      <h2>Backend capabilities</h2>
      {#each backends as backend}
        <div class="row"><strong>{backend.name}</strong><code>{JSON.stringify(backend.capabilities)}</code>
          <button onclick={() => action(() => post(`/api/backends/${backend.name}/install`, {}))}>Install / update</button>
        </div>
      {/each}
    </section>
  {:else if section === 'profiles'}
    <section class="heading"><div><p class="eyebrow">CANONICAL YAML</p><h1>Profiles</h1></div></section>
    <section class="grid">
      {#each profiles as profile}
        <article class="card">
          <div><span class="badge">{profile.backend}</span><h2>{profile.name}</h2><p>{profile.modelSource}</p></div>
          <div class="actions">
            <button onclick={() => action(() => post(`/api/runtime/${profile.name}/start`))}>Start</button>
            <button onclick={() => action(() => post(`/api/runtime/${profile.name}/stop`))}>Stop</button>
            <button onclick={() => action(() => post(`/api/runtime/${profile.name}/restart`))}>Restart</button>
            <button onclick={() => editProfile(profile.name)}>Edit YAML</button>
          </div>
        </article>
      {/each}
    </section>
    {#if editing}
      <dialog open>
        <h2>Edit {editing.name}</h2>
        <textarea bind:value={editing.profileYaml} rows="18"></textarea>
        <div class="actions"><button onclick={() => editing = null}>Cancel</button><button class="primary" onclick={saveProfile}>Save</button></div>
      </dialog>
    {/if}
  {:else if section === 'models'}
    <section class="heading">
      <div><p class="eyebrow">REMOTE + LOCAL</p><h1>Models</h1></div>
      <div class="filters">
        <select bind:value={selectedBackend}>{#each backends as backend}<option>{backend.name}</option>{/each}</select>
        <input type="search" bind:value={query} placeholder="Search catalog" />
        <button class="primary" onclick={refreshModels}>Search</button>
      </div>
    </section>
    <section class="split">
      <div class="panel"><h2>Catalog</h2>{#each catalog as model}<div class="row"><div><strong>{model.id}</strong><small>{model.downloads || 0} downloads</small></div><span>{model.variants?.length || 0} variants</span></div>{/each}</div>
      <div class="panel"><h2>Local</h2>{#each localModels as model}<div class="row"><div><strong>{model.filename}</strong><small>{model.path}</small></div><button class="danger" onclick={() => action(() => remove(`/api/models/local?backend=${encodeURIComponent(selectedBackend)}&path=${encodeURIComponent(model.path)}`))}>Delete</button></div>{/each}</div>
    </section>
  {:else}
    <section class="heading"><div><p class="eyebrow">AUDIT TRAIL</p><h1>Activity</h1></div></section>
    <section class="panel">{#each events as event}<div class="row"><time>{event.time}</time><strong>{event.action}</strong><span class:ok={event.success}>{event.success ? 'succeeded' : event.errorKind}</span></div>{/each}</section>
  {/if}
</main>
