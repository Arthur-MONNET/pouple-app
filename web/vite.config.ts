import { fileURLToPath } from 'node:url';
import tailwindcss from '@tailwindcss/vite';
import vue from '@vitejs/plugin-vue';
import { defineConfig, type Plugin } from 'vitest/config';

const CORE_PORT = Number(process.env.POUPLE_PORT ?? 4400);
const BOOT_PLACEHOLDER = '<!--POUPLE_BOOT-->';

/** In development the core does not serve the page, so this plays its part: it injects the token the core expects. */
function injectBoot(): Plugin {
  return {
    name: 'pouple-boot',
    apply: 'serve',
    transformIndexHtml: (html) => {
      const boot = JSON.stringify({ token: process.env.POUPLE_TOKEN ?? 'dev' }).replaceAll('<', '\\u003c');
      return html.replace(BOOT_PLACEHOLDER, `<script>window.__POUPLE__=${boot}</script>`);
    },
  };
}

export default defineConfig({
  plugins: [vue(), tailwindcss(), injectBoot()],
  server: {
    host: '127.0.0.1',
    port: 5173,
    strictPort: true,
    cors: false,
    proxy: {
      // changeOrigin: the core only accepts a Host header that is its own loopback address.
      '/api': { target: `http://127.0.0.1:${CORE_PORT}`, changeOrigin: true },
    },
  },
  build: {
    outDir: fileURLToPath(new URL('./dist', import.meta.url)),
    emptyOutDir: true,
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
});
