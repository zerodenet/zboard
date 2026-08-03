<template>
  <ModalDialog
    :open="open"
    :title="`SSH 终端${node?.name ? ` · ${node.name}` : ''}`"
    description="终端通过面板后端连接 VPS；SSH 凭证不会发送到浏览器。"
    size="xl"
    @close="requestClose"
  >
    <div class="terminal-panel">
      <div class="terminal-toolbar">
        <StatusBadge :tone="statusTone" :icon="statusIcon" role="status" aria-live="polite" aria-atomic="true">{{ statusLabel }}</StatusBadge>
        <span v-if="node" class="terminal-target">{{ node.ssh_user || 'root' }}@{{ node.ssh_host }}:{{ node.ssh_port || 22 }}</span>
        <div class="terminal-actions">
          <UiButton v-if="status === 'closed' || status === 'error'" variant="secondary" size="sm" type="button" @click="connect">重新连接</UiButton>
          <UiButton variant="secondary" size="sm" type="button" aria-label="终端全屏" @click="enterTerminalFullscreen">
            <UiIcon name="maximize" />终端全屏
          </UiButton>
        </div>
      </div>
      <div class="terminal-workspace">
        <div ref="terminalStage" class="terminal-stage" :class="{ 'terminal-stage-fullscreen': terminalFullscreenFallback }">
          <div ref="terminalHost" class="terminal-host" aria-label="SSH 交互终端"></div>
          <div v-if="terminalFullscreen" class="terminal-fullscreen-controls">
            <UiButton variant="secondary" size="sm" type="button" aria-label="退出终端全屏" @click="exitTerminalFullscreen">
              <UiIcon name="minimize" />退出全屏
            </UiButton>
          </div>
        </div>
        <aside class="terminal-shortcuts" aria-label="SSH 快捷指令">
          <div class="shortcut-heading">
            <strong>快捷指令</strong>
            <span>点击填入，按 Enter 执行</span>
          </div>
          <UiButton
            v-for="item in quickCommands"
            :key="item.command"
            class="shortcut-command"
            type="button"
            :disabled="status !== 'connected'"
            :title="item.command"
            @click="stageQuickCommand(item.command)"
          >
            <span>{{ item.label }}</span>
            <code>{{ item.command }}</code>
          </UiButton>
        </aside>
      </div>
      <p v-if="error" class="terminal-error" role="alert">{{ error }}</p>
      <p class="terminal-hint">空闲 15 分钟或连续使用 2 小时后自动断开。关闭窗口会立即结束远程 Shell。</p>
    </div>
    <template #footer>
      <UiButton variant="secondary" type="button" @click="requestClose">关闭终端</UiButton>
    </template>
  </ModalDialog>
</template>

<script setup lang="ts">
import type { FitAddon as XtermFitAddon } from '@xterm/addon-fit'
import type { IDisposable, Terminal as XtermTerminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { buildNodeSSHTerminalURL, createNodeSSHTerminalTicket } from '../api/client'
import { normalizeApiErrorMessage, normalizeApiMessage } from '../utils/apiError'
import ModalDialog from './ModalDialog.vue'
import StatusBadge from './StatusBadge.vue'
import UiIcon from './UiIcon.vue'

type TerminalStatus = 'idle' | 'connecting' | 'connected' | 'closed' | 'error'

const props = defineProps<{
  open: boolean
  node: any | null
}>()
const emit = defineEmits<{ close: [] }>()

const terminalHost = ref<HTMLElement | null>(null)
const terminalStage = ref<HTMLElement | null>(null)
const status = ref<TerminalStatus>('idle')
const error = ref('')
const terminalFullscreen = ref(false)
const terminalFullscreenFallback = ref(false)
const quickCommands = [
  { label: '系统与运行时间', command: 'uname -a && uptime' },
  { label: 'CPU 与内存', command: 'nproc && free -h' },
  { label: '磁盘空间', command: 'df -hT' },
  { label: '监听端口', command: 'ss -lntup' },
  { label: '失败的服务', command: 'systemctl --failed --no-pager' },
  { label: '最近系统错误', command: 'journalctl -p err -n 50 --no-pager' },
  { label: 'Docker 容器', command: "docker ps --format 'table {{.Names}}\\t{{.Image}}\\t{{.Status}}'" }
]
let terminal: XtermTerminal | null = null
let fitAddon: XtermFitAddon | null = null
let socket: WebSocket | null = null
let inputDisposable: IDisposable | null = null
let resizeDisposable: IDisposable | null = null
let resizeObserver: ResizeObserver | null = null
let fullscreenFitTimer: ReturnType<typeof setTimeout> | null = null
let connectionGeneration = 0
let closing = false

const statusLabel = computed(() => ({
  idle: '等待连接',
  connecting: '正在连接',
  connected: '已连接',
  closed: '连接已关闭',
  error: '连接失败'
} as Record<TerminalStatus, string>)[status.value])
const statusTone = computed<'neutral' | 'warning' | 'success' | 'danger'>(() => ({
  idle: 'neutral',
  connecting: 'warning',
  connected: 'success',
  closed: 'neutral',
  error: 'danger'
})[status.value] as 'neutral' | 'warning' | 'success' | 'danger')
const statusIcon = computed(() => status.value === 'connecting' ? 'refresh' : status.value === 'connected' ? 'check' : status.value === 'error' ? 'alert' : 'minus')

function cssColor(token: string) {
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(token).trim()
  if (!value) throw new Error(`缺少终端颜色令牌：${token}`)
  return value
}

function send(message: Record<string, unknown>) {
  if (socket?.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(message))
  }
}

function fitAndResize() {
  if (!terminal || !fitAddon || !terminalHost.value) return
  try {
    fitAddon.fit()
    send({ type: 'resize', cols: terminal.cols, rows: terminal.rows })
  } catch (_) {
    // The modal can be closing while ResizeObserver delivers its last event.
  }
}

function scheduleFitAndResize() {
  if (fullscreenFitTimer) clearTimeout(fullscreenFitTimer)
  requestAnimationFrame(() => requestAnimationFrame(fitAndResize))
  fullscreenFitTimer = setTimeout(() => {
    fullscreenFitTimer = null
    fitAndResize()
  }, 120)
}

async function enterTerminalFullscreen() {
  if (!terminalStage.value) return
  error.value = ''
  try {
    if (!terminalStage.value.requestFullscreen) throw new Error('Fullscreen API unavailable')
    await terminalStage.value.requestFullscreen()
  } catch (_) {
    terminalFullscreenFallback.value = true
    terminalFullscreen.value = true
    await nextTick()
    scheduleFitAndResize()
  }
}

async function exitTerminalFullscreen() {
  if (document.fullscreenElement === terminalStage.value) {
    try { await document.exitFullscreen() } catch (_) { /* browser is already leaving fullscreen */ }
  }
  terminalFullscreenFallback.value = false
  terminalFullscreen.value = false
  await nextTick()
  scheduleFitAndResize()
}

async function onFullscreenChange() {
  terminalFullscreen.value = document.fullscreenElement === terminalStage.value || terminalFullscreenFallback.value
  await nextTick()
  scheduleFitAndResize()
}

function onTerminalFullscreenKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !terminalFullscreenFallback.value) return
  event.preventDefault()
  event.stopImmediatePropagation()
  void exitTerminalFullscreen()
}

function stageQuickCommand(command: string) {
  if (status.value !== 'connected' || !terminal) return
  send({ type: 'input', data: command })
  terminal.focus()
}

function disposeConnection() {
  connectionGeneration++
  closing = true
  if (fullscreenFitTimer) clearTimeout(fullscreenFitTimer)
  fullscreenFitTimer = null
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) {
    try { socket.close(1000, 'terminal closed') } catch (_) { /* connection is already closing */ }
  }
  socket = null
  inputDisposable?.dispose()
  resizeDisposable?.dispose()
  inputDisposable = null
  resizeDisposable = null
  resizeObserver?.disconnect()
  resizeObserver = null
  terminal?.dispose()
  terminal = null
  fitAddon = null
}

async function connect() {
  if (!props.open || !props.node?.id || !terminalHost.value) return
  disposeConnection()
  closing = false
  const generation = connectionGeneration
  status.value = 'connecting'
  error.value = ''

  try {
    const [{ Terminal }, { FitAddon }] = await Promise.all([
      import('@xterm/xterm'),
      import('@xterm/addon-fit')
    ])
    if (generation !== connectionGeneration || !props.open || !terminalHost.value) return

    terminal = new Terminal({
      cursorBlink: true,
      convertEol: false,
      fontFamily: 'Consolas, "Cascadia Mono", "Liberation Mono", monospace',
      fontSize: 13,
      lineHeight: 1.25,
      scrollback: 5000,
      theme: {
        background: cssColor('--terminal-bg'),
        foreground: cssColor('--terminal-text'),
        cursor: cssColor('--terminal-cursor'),
        selectionBackground: cssColor('--terminal-border-strong')
      }
    })
    fitAddon = new FitAddon()
    terminal.loadAddon(fitAddon)
    terminal.open(terminalHost.value)
    terminal.writeln('\x1b[36m正在申请一次性终端连接…\x1b[0m')
    inputDisposable = terminal.onData((data) => send({ type: 'input', data }))
    resizeDisposable = terminal.onResize(({ cols, rows }) => send({ type: 'resize', cols, rows }))
    resizeObserver = new ResizeObserver(fitAndResize)
    resizeObserver.observe(terminalHost.value)
    fitAndResize()

    const ticket = await createNodeSSHTerminalTicket(props.node.id)
    if (generation !== connectionGeneration || !props.open) return
    socket = new WebSocket(buildNodeSSHTerminalURL(props.node.id, ticket.ticket))
    socket.binaryType = 'arraybuffer'
    socket.onopen = () => {
      if (generation !== connectionGeneration) return
      fitAndResize()
    }
    socket.onmessage = (event) => {
      if (generation !== connectionGeneration || !terminal) return
      if (typeof event.data !== 'string') {
        terminal.write(new Uint8Array(event.data as ArrayBuffer))
        return
      }
      try {
        const message = JSON.parse(event.data)
        if (message.type === 'connected') {
          status.value = 'connected'
          terminal.focus()
        } else if (message.type === 'connecting') {
          status.value = 'connecting'
        } else if (message.type === 'error') {
          status.value = 'error'
          error.value = normalizeApiMessage(message.message, 'SSH 终端连接失败。')
          terminal.writeln(`\r\n\x1b[31m${error.value}\x1b[0m`)
        }
      } catch (_) {
        terminal.write(event.data)
      }
    }
    socket.onerror = () => {
      if (generation !== connectionGeneration || closing) return
      status.value = 'error'
      error.value = '无法建立 SSH 终端连接。'
    }
    socket.onclose = () => {
      if (generation !== connectionGeneration || closing) return
      if (status.value !== 'error') status.value = 'closed'
      terminal?.writeln('\r\n\x1b[33mSSH 连接已关闭。\x1b[0m')
    }
  } catch (requestError: any) {
    if (generation !== connectionGeneration) return
    status.value = 'error'
    error.value = normalizeApiErrorMessage(requestError, '无法创建 SSH 终端连接。')
    terminal?.writeln(`\r\n\x1b[31m${error.value}\x1b[0m`)
  }
}

async function requestClose() {
  await exitTerminalFullscreen()
  disposeConnection()
  status.value = 'idle'
  error.value = ''
  emit('close')
}

watch(() => props.open, async (open) => {
  if (!open) {
    void exitTerminalFullscreen()
    disposeConnection()
    status.value = 'idle'
    error.value = ''
    return
  }
  await nextTick()
  await connect()
})

onMounted(() => {
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('keydown', onTerminalFullscreenKeydown, true)
})
onBeforeUnmount(() => {
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  document.removeEventListener('keydown', onTerminalFullscreenKeydown, true)
  void exitTerminalFullscreen()
  disposeConnection()
})
</script>

<style scoped>
.terminal-panel{min-height:0;display:grid;grid-template-rows:auto auto auto auto;gap:10px}.terminal-toolbar{min-height:34px;display:flex;align-items:center;gap:12px;padding:0 2px}.terminal-target{min-width:0;overflow:hidden;text-overflow:ellipsis;color:var(--muted);font-family:Consolas,monospace;font-size:11px;white-space:nowrap}.terminal-actions{display:flex;gap:7px;margin-left:auto}.terminal-actions .button{gap:6px}.terminal-workspace{height:clamp(320px,52vh,560px);min-height:320px;display:grid;grid-template-columns:minmax(0,1fr) 230px;gap:10px}.terminal-stage{position:relative;min-width:0;min-height:0;height:100%;overflow:hidden;background:var(--terminal-bg)}.terminal-host{box-sizing:border-box;width:100%;height:100%;min-height:0;overflow:hidden;padding:10px;border:1px solid var(--terminal-border);border-radius:10px;background:var(--terminal-bg);box-shadow:inset 0 0 0 1px var(--terminal-inset)}.terminal-stage:fullscreen,.terminal-stage-fullscreen{width:100vw;height:100vh;background:var(--terminal-bg)}.terminal-stage-fullscreen{position:fixed;inset:0;z-index:2000}.terminal-stage:fullscreen .terminal-host,.terminal-stage-fullscreen .terminal-host{padding:16px;border:0;border-radius:0}.terminal-fullscreen-controls{position:absolute;right:14px;top:14px;z-index:10;opacity:.2;transition:opacity .15s ease}.terminal-stage:hover .terminal-fullscreen-controls,.terminal-fullscreen-controls:focus-within{opacity:1}.terminal-fullscreen-controls .button{color:var(--terminal-text-strong);border-color:var(--terminal-border-muted);background:var(--terminal-control-bg)}.terminal-shortcuts{height:100%;min-height:0;display:flex;flex-direction:column;gap:7px;overflow-y:auto;padding:10px;border:1px solid var(--line);border-radius:10px;background:var(--surface-soft)}.shortcut-heading{display:grid;gap:2px;margin-bottom:2px}.shortcut-heading strong{font-size:12px}.shortcut-heading span{color:var(--muted);font-size:9px}.shortcut-command{display:grid;gap:4px;width:100%;padding:9px 10px;text-align:left;border:1px solid var(--line);border-radius:8px;color:var(--text);background:var(--surface);cursor:pointer}.shortcut-command:hover:not(:disabled){border-color:var(--info-border-strong);background:var(--info-canvas)}.shortcut-command:disabled{cursor:not-allowed;opacity:.5}.shortcut-command span{font-size:10px;font-weight:700}.shortcut-command code{overflow:hidden;color:var(--muted);font-size:9px;text-overflow:ellipsis;white-space:nowrap}.terminal-error{margin:0;padding:10px 12px;border-radius:8px;color:var(--danger);background:var(--danger-soft);font-size:11px}.terminal-hint{margin:0;color:var(--subtle);font-size:10px}.terminal-host :deep(.xterm){height:100%}.terminal-host :deep(.xterm-viewport){overflow-y:scroll!important;overscroll-behavior:contain;touch-action:pan-y;scrollbar-gutter:stable;border-radius:6px}@media(max-width:900px){.terminal-workspace{height:clamp(420px,62vh,620px);grid-template-columns:1fr;grid-template-rows:minmax(260px,1fr) auto}.terminal-shortcuts{height:auto;max-height:160px;display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}.shortcut-heading{grid-column:1/-1}}@media(max-width:720px){.terminal-workspace{height:clamp(400px,64vh,580px);grid-template-rows:minmax(240px,1fr) auto}.terminal-target{display:none}.terminal-actions .button{padding-inline:9px}.terminal-shortcuts{max-height:145px;grid-template-columns:1fr}}
.terminal-stage:fullscreen,.terminal-stage-fullscreen{box-sizing:border-box;position:fixed;inset:0 0 1px;width:auto;height:auto;max-width:none;max-height:calc(100dvh - 1px);margin:0;overflow:hidden;background:var(--terminal-bg)}.terminal-stage-fullscreen{z-index:2000}.terminal-stage:fullscreen .terminal-host,.terminal-stage-fullscreen .terminal-host{position:absolute;inset:0;width:auto;height:auto;padding:12px max(12px,env(safe-area-inset-right)) max(12px,env(safe-area-inset-bottom)) max(12px,env(safe-area-inset-left));border:0;border-radius:0}
</style>
