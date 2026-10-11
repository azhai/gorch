import { defineConfig } from 'vite'

// No framework plugin: the SPA is plain Mithril rendered through hyperscript,
// so Vite only needs to handle TS/TSX transforms and the bundling itself.
export default defineConfig({
  base: './',
  build: {
    outDir: 'dist',
  },
  server: {
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
})