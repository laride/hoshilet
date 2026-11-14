import { defineConfig } from 'tsdown';

export default defineConfig({
  entry: { hoshilet: 'src/cli.ts' },
  platform: 'node',
  target: 'node22',
  format: 'esm',
  outDir: 'dist',
  outExtensions: () => ({ js: '.mjs' }),
  clean: true,
  dts: false,
});
