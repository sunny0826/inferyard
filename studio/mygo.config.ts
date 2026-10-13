import { defineConfig } from 'mygo-cli';

export default defineConfig({
  name: 'InferYard Studio',
  identifier: 'com.inferyard.studio',
  version: '0.1.0',
  buildCommand: 'bun run build:web',
  frontendDist: 'web/dist',
  out: 'build',
});
