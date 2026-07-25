import { defineConfig } from 'vite'
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
  plugins: [vue()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/primevue') || id.includes('node_modules/@primeuix')) return 'ui'
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
          '/api/v1': {
            target: apiProxyTarget,
            changeOrigin: true,
            secure: false,
          },
        }
      : undefined,
  }
})
