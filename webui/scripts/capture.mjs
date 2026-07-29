import { spawn, spawnSync } from 'node:child_process';
import { mkdir, stat } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';

const webuiDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const outDir = path.resolve(webuiDir, argValue('--out') || 'screenshots');
const baseUrl = 'http://127.0.0.1:5180';
const requestedSection = argValue('--section').toLowerCase();
const sections = [
  { label: 'Dashboard', file: 'runtime' },
  { label: 'Profiles', file: 'profiles' },
  { label: 'Models', file: 'models' },
  { label: 'Logs', file: 'logs' }
].filter((section) => !requestedSection || section.label.toLowerCase() === requestedSection);
const modes = ['light', 'dark'];
const now = new Date().toISOString();

// One profile per backend, so the shots exercise both the discrete-VRAM and the
// unified-memory presentation rather than llama.cpp twice.
const profiles = [
  {
    name: 'qwen-coder',
    backend: 'llamacpp',
    profileYaml: 'version: 1\nname: qwen-coder\nbackend: llamacpp\n',
    modelSource: '/models/qwen2.5-coder-7b/qwen.gguf',
    host: '127.0.0.1',
    port: 8080,
    engineArgs: { 'ctx-size': 8192, 'flash-attn': true }
  },
  {
    name: 'llama-mlx',
    backend: 'mlx',
    profileYaml: 'version: 1\nname: llama-mlx\nbackend: mlx\n',
    modelSource: 'mlx-community/Llama-3.2-3B-Instruct-4bit',
    host: '127.0.0.1',
    port: 8081,
    engineArgs: { temp: 0.7 }
  }
];

await mkdir(outDir, { recursive: true });
const server = spawn('pnpm', ['exec', 'vite', '--port', '5180', '--host', '127.0.0.1'], { cwd: webuiDir, detached: true, stdio: ['ignore', 'pipe', 'pipe'] });
let browser;

try {
  await waitForServer(server);
  // CHROMIUM_PATH lets a sandbox with a preinstalled browser opt out of
  // Playwright's pinned-build lookup, which fails when the two disagree.
  browser = await chromium.launch(process.env.CHROMIUM_PATH ? { executablePath: process.env.CHROMIUM_PATH } : {});
  const files = [];

  for (const mode of modes) {
    const context = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 2 });
    await context.route('**/inferencerig.control.v1.ControlService/*', mockApi);
    await context.addInitScript((value) => localStorage.setItem('mode-watcher-mode', value), mode);
    const page = await context.newPage();
    page.on('pageerror', (err) => console.error(`[pageerror ${mode}] ${err.message}`));
    page.on('console', (msg) => msg.type() === 'error' && console.error(`[console ${mode}] ${msg.text()}`));
    await page.goto(baseUrl, { waitUntil: 'domcontentloaded' });
    await page.waitForLoadState('networkidle');

    for (const section of sections) {
      await page.getByRole('button', { name: section.label, exact: true }).click();
      await page.waitForLoadState('networkidle');
      await page.locator('main .rounded-xl, main [data-slot="card"]').first().waitFor({ timeout: 5000 }).catch(() => console.error(`[no-content ${mode}] ${section}`));
      // Runtime sparklines need a few signal poll samples (app polls every 5s).
      if (section.label === 'Dashboard') await page.waitForTimeout(11_000);
      const file = path.join(outDir, `${section.file}-${mode}.png`);
      await page.screenshot({ path: file, fullPage: true });
      files.push(file);
    }
    await context.close();
  }

  await browser.close();
  browser = null;
  for (const file of files) {
    const size = (await stat(file)).size;
    console.log(`${path.relative(webuiDir, file)} ${size} bytes`);
  }
} finally {
  if (browser) await browser.close().catch(() => undefined);
  if (!server.killed && server.pid) {
    try {
      if (process.platform === 'win32') {
        spawnSync('taskkill', ['/pid', server.pid.toString(), '/T', '/F'], { stdio: 'ignore' });
      } else {
        process.kill(-server.pid, 'SIGTERM');
      }
    } catch (err) {
      if (err.code !== 'ESRCH') throw err;
    }
  }
}

function argValue(name) {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : '';
}

function waitForServer(proc) {
  return new Promise((resolve, reject) => {
    let done = false;
    let log = '';
    const timeout = setTimeout(() => finish(reject, new Error('Vite server timed out')), 30_000);
    const finish = (fn, value) => {
      if (done) return;
      done = true;
      clearTimeout(timeout);
      fn(value);
    };
    proc.on('error', (err) => finish(reject, err));
    proc.on('exit', (code) => finish(reject, new Error(`Vite exited early with code ${code}\n${log}`)));
    const onData = (chunk) => {
      log = (log + chunk).slice(-4000);
    };
    proc.stdout.on('data', onData);
    proc.stderr.on('data', onData);
    const poll = async () => {
      try {
        const response = await fetch(baseUrl);
        if (response.ok) return finish(resolve);
      } catch {
        // keep polling
      }
      if (!done) setTimeout(poll, 250);
    };
    poll();
  });
}

// Connect puts the method name in the last path segment and always POSTs, so
// the fixture table keys off that rather than off a REST route. Shapes are
// protobuf-JSON: camelCase fields, 64-bit ints as strings.
async function mockApi(route) {
  const method = new URL(route.request().url()).pathname.split('/').pop();
  await route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify(fixture(method))
  });
}

function fixture(method) {
  switch (method) {
    case 'GetInfo':
      return {
        ok: true,
        profiles: profiles.length,
        backends: 2,
        runningProfiles: ['qwen-coder'],
        autostartProfiles: ['qwen-coder'],
        startupServices: ['control', 'web'],
        build: { version: '0.1.0', commit: 'abc1234', commitTime: now }
      };
    case 'ListBackends':
      return {
        ok: true,
        backends: [
          { name: 'llamacpp', capabilities: { singleFileArtifacts: true, discreteVram: true, managedInstall: true, parameterIntrospection: true } },
          { name: 'mlx', capabilities: { multiFileArtifacts: true, unifiedMemory: true, managedInstall: true, singleActiveProfile: true, parameterIntrospection: true } }
        ]
      };
    case 'GetBackendInstallStatus':
      return { ok: true, installed: true, managed: true, version: 'b4321', path: '/opt/llama.cpp/bin/llama-server' };
    case 'GetBackendParams':
      return {
        ok: true,
        params: [
          { name: 'model.source', description: 'model repository, URL, or local path', required: true, valueHint: 'TheBloke/Qwen2.5-Coder-7B-GGUF', type: 'PARAMETER_TYPE_STRING' },
          { name: 'listen.port', description: 'server listen port', required: true, valueHint: '8080', type: 'PARAMETER_TYPE_INT' },
          { name: 'engine_args.ctx-size', description: 'context window in tokens', valueHint: '8192', defaultValue: '4096', type: 'PARAMETER_TYPE_INT' },
          { name: 'engine_args.flash-attn', description: 'enable flash attention', type: 'PARAMETER_TYPE_BOOL' }
        ]
      };
    case 'GetRuntimeStatus':
      return {
        ok: true,
        status: { state: 'running', detail: '1 profile active', checkedAt: now, processes: [{ name: 'llamacpp-qwen-coder', state: 'running', pid: 12345, host: '127.0.0.1', port: 8080, ready: true }] },
        profiles: [
          { name: 'qwen-coder', backend: 'llamacpp', status: { state: 'running', checkedAt: now } },
          { name: 'llama-mlx', backend: 'mlx', status: { state: 'stopped', checkedAt: now } }
        ]
      };
    case 'GetSignals':
      return { ok: true, signals: signals() };
    case 'ListEvents':
      return { ok: true, events: events() };
    case 'ListProfiles':
      return { ok: true, profiles };
    case 'GetProfile':
      return { ok: true, profile: profiles[0] };
    case 'ListLocalModels':
      return { ok: true, models: localModels() };
    case 'ListModelCatalog':
      return catalog();
    case 'GetLogs':
      return { ok: true, service: 'control', text: logText() };
    case 'ListLogArchives':
      return {
        ok: true,
        archives: [
          { id: 'control-20260726T090000.000000000Z.log', service: 'control', sizeBytes: '184320', archivedAt: now },
          { id: 'web-20260725T090000.000000000Z.log', service: 'web', sizeBytes: '92160', archivedAt: now }
        ]
      };
    default:
      return { ok: true };
  }
}

function signals() {
  const jitter = (base, spread) => Math.max(0, Math.min(100, base + (Math.random() - 0.5) * spread));
  return {
    capturedAt: new Date().toISOString(),
    host: { hostname: 'workstation', os: 'linux', platform: 'ubuntu' },
    logicalCpuCores: 16,
    cpuUsedPercent: jitter(42, 30),
    totalMemoryBytes: '34359738368',
    availableMemoryBytes: '18000000000',
    usedMemoryBytes: '16359738368',
    usedMemoryPercent: jitter(48, 20),
    accelerators: [
      {
        name: 'NVIDIA RTX 4090',
        unifiedMemory: false,
        totalMemoryBytes: '25769803776',
        usedMemoryBytes: String(Math.round(12000000000 + Math.random() * 4000000000)),
        utilizationPercent: jitter(67, 40),
        hasUtilization: true,
        temperatureCelsius: jitter(64, 8),
        hasTemperature: true,
        source: 'nvidia'
      }
    ],
    disks: [
      { label: 'models', path: '/models', totalBytes: '2000398934016', usedBytes: '1240000000000', freeBytes: '760398934016', usedPercent: 62 },
      { label: 'root', path: '/', totalBytes: '512110190592', usedBytes: '210000000000', freeBytes: '302110190592', usedPercent: 41 }
    ],
    runtime: [{ name: 'llamacpp-qwen-coder', pid: 12345, rssBytes: '8200000000', cpuPercent: 23.4, command: 'llama-server --ctx-size 8192' }],
    warnings: []
  };
}

function events() {
  return ['runtime.start', 'download.start', 'profile.put', 'model.resolve', 'runtime.restart', 'download.apply', 'profile.delete', 'runtime.stop'].map((action, i) => ({
    id: `evt-${i}`,
    time: new Date(Date.now() - i * 7 * 60_000).toISOString(),
    action,
    success: i !== 3 && i !== 5,
    errorKind: i === 3 ? 'timeout' : i === 5 ? 'runtime_error' : undefined,
    duration: ['1.2s', '34s', '220ms', '10s', '3.1s', '45s', '90ms', '800ms'][i]
  }));
}

function localModels() {
  return [
    { path: '/models/qwen2.5-coder-7b/qwen.gguf', filename: 'qwen.gguf', sizeBytes: '4680000000', modifiedAt: now, usedByProfiles: ['qwen-coder'] },
    { path: '/models/mlx-community/Llama-3.2-3B-Instruct-4bit', filename: 'Llama-3.2-3B-Instruct-4bit', sizeBytes: '1800000000', modifiedAt: now, usedByProfiles: ['llama-mlx'] },
    { path: '/models/mistral-7b/mistral.gguf', filename: 'mistral.gguf', sizeBytes: '4300000000', modifiedAt: now, usedByProfiles: [] },
    { path: '/models/embed/nomic.gguf', filename: 'nomic.gguf', sizeBytes: '740000000', modifiedAt: now, usedByProfiles: [] }
  ];
}

function catalog() {
  const names = ['Qwen2.5 Coder 7B', 'Llama 3 8B Instruct', 'Mistral 7B Instruct', 'Gemma 2 9B', 'DeepSeek Coder 6.7B'];
  return {
    ok: true,
    cacheHit: true,
    stale: false,
    models: names.map((repo, i) => ({
      id: `${i % 2 ? 'meta-llama' : 'Qwen'}/${repo.replaceAll(' ', '-')}`,
      url: `https://huggingface.co/${repo.replaceAll(' ', '-')}`,
      downloads: String(90000 - i * 8000),
      likes: String(1200 - i * 90),
      lastModified: now,
      tags: ['gguf', 'text-generation'],
      variants: [
        {
          name: 'Q4_K_M',
          reference: `${repo.toLowerCase().replaceAll(' ', '-')}.Q4_K_M.gguf`,
          sizeBytes: String(4200000000 + i * 400000000),
          multiFile: false,
          fit: {
            level: i === 3 ? 'FIT_LEVEL_MARGINAL' : 'FIT_LEVEL_FITS',
            reason: i === 3 ? 'Close to VRAM headroom' : 'Fits comfortably in VRAM',
            requiredBytes: String(4600000000 + i * 400000000),
            availableBytes: '25769803776'
          }
        }
      ]
    }))
  };
}

function logText() {
  return [
    `${now} INF control listening on /home/demo/.inferencerig/run/control.sock`,
    `${now} INF backend registered name=llamacpp`,
    `${now} INF backend registered name=mlx`,
    `${now} INF runtime.start profile=qwen-coder pid=12345`,
    `${now} WRN catalog refresh served stale entry backend=llamacpp`,
    `${now} INF readiness probe ok profile=qwen-coder port=8080`
  ].join('\n');
}
