<template>
  <FormField v-slot="{ controlAttrs }" :label="label" name="migration-dsn" :hint="hint">
    <div class="datasource-input">
      <UiInput
        v-model.trim="model"
        v-bind="controlAttrs"
        :type="isSQLite || revealed ? 'text' : 'password'"
        :autocomplete="isSQLite ? 'off' : 'new-password'"
        :placeholder="isSQLite ? './data/zboard.db' : 'user:password@tcp(host:3306)/database?parseTime=true'"
        spellcheck="false"
        autocapitalize="none"
      />
      <UiButton
        v-if="!isSQLite"
        type="button"
        variant="secondary"
        :aria-label="revealed ? '隐藏 MySQL 连接串' : '显示 MySQL 连接串'"
        :aria-pressed="revealed"
        aria-controls="migration-dsn"
        @click="revealed = !revealed"
      >{{ revealed ? '隐藏' : '显示' }}</UiButton>
    </div>
  </FormField>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import FormField from './FormField.vue'
import UiButton from './UiButton.vue'
import UiInput from './UiInput.vue'

const props = defineProps<{ driver: 'mysql' | 'sqlite' }>()
const model = defineModel<string>({ required: true })
const revealed = ref(false)
const isSQLite = computed(() => props.driver === 'sqlite')
const label = computed(() => isSQLite.value ? '目标 SQLite 文件路径' : '目标 MySQL 连接串')
const hint = computed(() => isSQLite.value
  ? '填写 zboard 服务器（容器）内的持久化文件路径，不是本机路径；不允许 :memory:。'
  : '填写完整 DSN，目标数据库须已创建且无业务数据。包含密码，默认隐藏；可点击显示核对。')

watch(() => props.driver, () => {
  // Never expose a previous MySQL credential in the plaintext SQLite field.
  model.value = ''
  revealed.value = false
}, { flush: 'sync' })
watch(model, value => { if (!value) revealed.value = false })
</script>

<style scoped>
.datasource-input { display: flex; align-items: stretch; gap: 8px; min-width: 0; }
.datasource-input :deep(input) { flex: 1; min-width: 0; font-family: var(--font-mono, monospace); }
.datasource-input :deep(button) { flex-shrink: 0; }
</style>
