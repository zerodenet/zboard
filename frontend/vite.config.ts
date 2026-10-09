import { defineConfig } from 'vite'
import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import vue from '@vitejs/plugin-vue'

const API_BASE = process.env.VITE_API_BASE || '/api/v1'
const apiProxyTarget = (() => {
  if (process.env.VITE_API_PROXY_TARGET) return process.env.VITE_API_PROXY_TARGET
  try {
    const url = new URL(API_BASE)
    return `${url.origin}`
  } catch {
    return ''
  }
})()

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: { alias: { '@': path.resolve(__dirname, './src') } },
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/reka-ui')) return 'ui'
          if (id.includes('node_modules/vue') || id.includes('node_modules/pinia') || id.includes('node_modules/vue-router')) return 'vue-vendor'
        },
      },
    },
  },
  server: {
    port: 5173,
    host: true,
    proxy: apiProxyTarget
      ? {
          '/media': { target: apiProxyTarget, changeOrigin: true, secure: false },
          '/api/v1': {
            target: apiProxyTarget,
            changeOrigin: true,
            secure: false,
            configure(proxy) {
              proxy.on('proxyRes', (response, request) => {
                // The plugin host scopes CSP assets to its request Host. In
                // local preview that Host is the upstream, while the iframe
                // and its relative assets are loaded from the Vite origin.
                const assetPath = request.url?.match(/^\/api\/v1\/plugin-assets\/[a-f0-9]{64}\//)?.[0]
                const csp = response.headers['content-security-policy']
                const localHost = request.headers.host
                if (!assetPath || typeof csp !== 'string' || !localHost) return
                const upstreamHost = new URL(apiProxyTarget).host
                const upstreamScope = `${upstreamHost}${assetPath}`
                const localScope = `http://${localHost}${assetPath}`
                response.headers['content-security-policy'] = csp
                  .replaceAll(`http://${upstreamScope}`, localScope)
                  .replaceAll(`https://${upstreamScope}`, localScope)
              })
            },
          },
        }
      : undefined,
  }
})
