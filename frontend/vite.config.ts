import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  base: './',
  plugins: [svelte(), tailwindcss()],
  build: {
    emptyOutDir: true,
    outDir: '../diagnose/web'
  },
  server: {
    port: 5150,
    proxy: {
      '/api': {
        target: 'http://localhost:7653',
        changeOrigin: true,
        secure: false,
        rewrite: (path) => path.replace(/^\/api/, '/diagnostics/api'),
      },
    },
  },
})
