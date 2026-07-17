import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const API_BASE = process.env.VITE_API_BASE || '/api/v1'
const apiProxyTarget = (() => {
  try {
    const url = new URL(API_BASE)
    return `${url.origin}`
  } catch {
    return ''
  }
})()

export default defineConfig({
  plugins: [vue()],
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
