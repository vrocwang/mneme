import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'url'
import path from 'path'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// Vite 3 does not support Node.js subpath imports (#prefix) used by vfile.
// This plugin resolves them to browser-compatible entrypoints.
function subpathImportPlugin() {
  const vfileDir = path.resolve(__dirname, 'node_modules/vfile/lib')
  const map: Record<string, string> = {
    '#minpath': path.join(vfileDir, 'minpath.browser.js'),
    '#minproc': path.join(vfileDir, 'minproc.browser.js'),
    '#minurl':  path.join(vfileDir, 'minurl.browser.js'),
  }
  return {
    name: 'subpath-imports',
    resolveId(id: string) {
      if (map[id]) return map[id]
      return undefined
    },
  }
}

export default defineConfig({
  plugins: [subpathImportPlugin(), react()],
  build: {
    rollupOptions: {
      output: {
        // Split large vendors into separate chunks to keep the main bundle
        // under the 500 KiB warning threshold and improve cacheability.
        manualChunks: {
          'react-vendor': ['react', 'react-dom', 'react-router-dom'],
          'redux': ['@reduxjs/toolkit', 'react-redux', 'redux-persist'],
          'markdown': ['react-markdown'],
          'charts': ['recharts'],
        },
      },
    },
  },
})
