import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  root: 'web',
  plugins: [svelte()],
  build: { target: 'es2022', outDir: 'dist', emptyOutDir: true },
});
