<template>
  <PopoverRoot :open="open" @update:open="setOpen">
    <PopoverTrigger as-child>
      <button class="protocol-multiplier-trigger" type="button" :disabled="disabled || saving || !endpointId || !app.isAdmin" :title="!endpointId ? '协议不可用，无法修改倍率' : undefined" :aria-label="`修改 ${name} 的倍率，当前 ${displayValue} 倍`">
        <UiIcon name="edit" /><span>{{ displayValue }}</span>
      </button>
    </PopoverTrigger>
    <PopoverPortal>
      <PopoverContent as-child align="end" :side-offset="6" :collision-padding="16" @open-auto-focus="focusInput" @interact-outside="guardClosing" @escape-key-down="guardClosing">
        <form class="ui-popover-content protocol-multiplier-editor" novalidate @submit.prevent="save">
          <FormField v-slot="{ controlAttrs }" label="计费倍率" :name="inputId" :error="error">
            <UiInput ref="input" v-model="draft" v-bind="controlAttrs" inputmode="decimal" autocomplete="off" maxlength="16" :disabled="saving" @input="error = ''" />
          </FormField>
          <div class="protocol-multiplier-actions"><UiButton variant="ghost" size="sm" :disabled="saving" @click="setOpen(false)">取消</UiButton><UiButton size="sm" type="submit" :loading="saving">保存</UiButton></div>
        </form>
      </PopoverContent>
    </PopoverPortal>
  </PopoverRoot>
</template>
<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { PopoverContent, PopoverPortal, PopoverRoot, PopoverTrigger } from 'reka-ui'
import { updateProtocolEndpointMultiplier } from '../api/client'
import { useAppStore } from '../stores/app'
import FormField from './FormField.vue'
import UiInput from './UiInput.vue'
import UiButton from './UiButton.vue'
import UiIcon from './UiIcon.vue'
const props = defineProps<{ endpointId: number; multiplierMilli: number; name: string; disabled?: boolean }>()
const emit = defineEmits<{ saved: [value: { id: number; multiplier_milli: number }] }>()
const app = useAppStore(), open = ref(false), saving = ref(false), draft = ref(''), error = ref('')
const input = ref<InstanceType<typeof UiInput> | null>(null), inputId = `protocol-multiplier-${useId()}`
const displayValue = computed(() => new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(props.multiplierMilli / 1000))
let generation = 0
function setOpen(value: boolean) {
  if (saving.value || value && (props.disabled || !props.endpointId || !app.isAdmin)) return
  if (value && !open.value) { draft.value = String(props.multiplierMilli / 1000); error.value = '' }
  open.value = value
}
function guardClosing(event: Event) { if (saving.value) event.preventDefault() }
async function focusInput(event: Event) {
  event.preventDefault(); await nextTick()
  const element = input.value?.$el as HTMLInputElement | undefined
  element?.focus(); element?.select()
}
function parsedMilli() {
  const text = draft.value.trim()
  if (!/^(?:\d+(?:\.\d{1,3})?|\.\d{1,3})$/.test(text)) return null
  const [whole, fraction = ''] = text.split('.')
  const value = BigInt(whole || '0') * 1000n + BigInt(fraction.padEnd(3, '0'))
  return value >= 1n && value <= 100000n ? Number(value) : null
}
async function save() {
  if (saving.value || props.disabled || !props.endpointId || !app.isAdmin) return
  const value = parsedMilli()
  if (value === null) { error.value = '请输入 0.001–100 之间的倍率，最多三位小数。'; return }
  if (value === props.multiplierMilli) { setOpen(false); return }
  const request = ++generation, id = props.endpointId
  saving.value = true; error.value = ''
  try {
    const result = await updateProtocolEndpointMultiplier(id, value)
    if (request !== generation) return
    if (result?.id !== id || !Number.isInteger(result.multiplier_milli) || result.multiplier_milli < 1 || result.multiplier_milli > 100000) throw new Error('倍率保存结果暂无法确认，请刷新列表核对。')
    emit('saved', { id, multiplier_milli: result.multiplier_milli }); open.value = false
  } catch (cause: any) {
    if (request === generation) error.value = cause?.response?.data?.errors?.multiplier_milli || cause?.response?.data?.message || cause?.message || '倍率保存失败，请重试。'
  } finally { if (request === generation) saving.value = false }
}
function invalidate() { generation++; open.value = false; saving.value = false; error.value = '' }
watch(() => [props.endpointId, props.disabled, app.user.id, app.isAdmin], invalidate, { flush: 'sync' })
onBeforeUnmount(invalidate)
</script>
<style scoped>
.protocol-multiplier-trigger { display: inline-flex; align-items: center; justify-content: flex-end; gap: 5px; min-height: 28px; padding: 3px 0; border: 0; border-radius: 3px; color: inherit; background: transparent; font: inherit; font-variant-numeric: tabular-nums; cursor: pointer; }
.protocol-multiplier-trigger .ui-icon { width: 12px; height: 12px; opacity: 0; }
.protocol-multiplier-trigger:is(:hover, :focus-visible) .ui-icon { opacity: 1; }
.protocol-multiplier-trigger:hover span { text-decoration: underline; }
.protocol-multiplier-trigger:focus-visible { outline: 2px solid var(--ring); outline-offset: 3px; }
.protocol-multiplier-trigger:disabled { cursor: default; }
.protocol-multiplier-trigger:disabled .ui-icon { visibility: hidden; }
.protocol-multiplier-editor { box-sizing: border-box; width: min(288px, calc(100vw - 32px)); max-height: min(70vh, var(--reka-popover-content-available-height, 70vh)); overflow: auto; display: grid; gap: 12px; }
.protocol-multiplier-editor :deep(input) { text-align: right; font-variant-numeric: tabular-nums; }
.protocol-multiplier-actions { display: flex; justify-content: flex-end; gap: 8px; }
</style>
