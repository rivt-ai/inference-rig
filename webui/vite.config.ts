import { defineConfig } from 'vitest/config';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { fileURLToPath } from 'node:url';

export default defineConfig({
  root: 'web',
  base: '/',
  plugins: [tailwindcss(), svelte()],
  resolve: {
    conditions: ['browser'],
    alias: {
      $lib: fileURLToPath(new URL('./web/src/lib', import.meta.url))
    }
  },
  build: {
    outDir: '../dist',
    emptyOutDir: true,
    sourcemap: false
  },
  server: {
    proxy: {
      '/inferencerig.control.v1.ControlService': 'http://127.0.0.1:7000',
      '/health': 'http://127.0.0.1:7000',
      '/mcp': 'http://127.0.0.1:7000'
    }
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    include: ['src/**/*.test.ts']
  }
});
