<template>
  <section class="account-page stack">
    <PageHeader title="账户安全" />
    <TransientFeedback :success="message" />
    <PageAlert v-if="error && !loaded" tone="danger" title="账户安全信息加载失败">{{ error }}</PageAlert>
    <UiSection variant="outlined" v-if="loaded" :title="passwordSet ? '修改密码' : '设置本地密码'">
      <div class="account-section-body">
        <form ref="formElement" class="account-form" novalidate @submit.prevent="setPassword">
          <p v-if="!passwordSet" class="muted">请在第三方登录后五分钟内设置；过期后需重新登录。</p>
          <FormField v-if="passwordSet" v-slot="{ controlAttrs }" label="当前密码" name="account-current-password" :error="formErrors.fields.current_password" required full><UiInput v-model="currentPassword" v-bind="controlAttrs" type="password" autocomplete="current-password" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="新密码" name="identity-new-password" :error="formErrors.fields.new_password" required full><UiInput v-model="newPassword" v-bind="controlAttrs" type="password" autocomplete="new-password" minlength="12" maxlength="72" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="确认新密码" name="account-confirm-password" :error="formErrors.fields.confirm_password" required full><UiInput v-model="repeatPassword" v-bind="controlAttrs" type="password" autocomplete="new-password" /></FormField>
          <PageAlert v-if="formErrors.formError.value || error" tone="danger" title="密码未修改">{{ formErrors.formError.value || error }}</PageAlert>
          <div class="account-form-actions"><UiButton type="submit" :loading="busy" :disabled="busy || !newPassword || !repeatPassword || (passwordSet && !currentPassword)" @click.prevent="setPassword">{{ passwordSet ? '修改密码' : '设置密码' }}</UiButton></div>
        </form>
      </div>
    </UiSection>
    <TableSkeleton v-else-if="!error" label="正在读取账户安全信息" :columns="1" :rows="3" />
    <RouterLink class="account-settings-link" to="/account/integrations"><UiIcon name="key" /><strong>外部集成</strong><UiIcon name="chevron" /></RouterLink>
    <PluginSlot name="account.security.identities" surface="account" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import PageAlert from '../../components/PageAlert.vue'
import TableSkeleton from '../../components/TableSkeleton.vue'
import { collectFieldErrors, isUtf8LengthInRange } from '../../utils/validation'
import { useFormErrors } from '../../composables/useFormState'
import PageHeader from '../../components/PageHeader.vue'
import UiSection from '../../components/UiSection.vue'
import FormField from '../../components/FormField.vue'
import TransientFeedback from '../../components/TransientFeedback.vue'
import { fetchIdentityPasswordStatus, setupIdentityPassword } from '../../api/identities'
import { changeAccountPassword } from '../../api/accountSecurity'
import UiInput from '../../components/UiInput.vue'
import UiButton from '../../components/UiButton.vue'
import UiIcon from '../../components/UiIcon.vue'
import PluginSlot from '../../plugins/PluginSlot.vue'
const passwordSet = ref(true), loaded = ref(false), currentPassword = ref(''), repeatPassword = ref(''), newPassword = ref('')
const formElement = ref<HTMLElement | null>(null), formErrors = useFormErrors()
const busy = ref(false), message = ref(''), error = ref('')
watch(currentPassword, () => { if (!busy.value) formErrors.clear('current_password') }, { flush: 'sync' })
watch(newPassword, () => { if (!busy.value) { formErrors.clear('new_password'); formErrors.clear('confirm_password') } }, { flush: 'sync' })
watch(repeatPassword, () => { if (!busy.value) formErrors.clear('confirm_password') }, { flush: 'sync' })
onMounted(async () => { try { passwordSet.value = (await fetchIdentityPasswordStatus()).password_set; loaded.value = true } catch { error.value = '账户安全信息加载失败，请刷新重试。' } })
async function setPassword() {
  if (busy.value) return
  formErrors.clear(); error.value = ''
  if (!await formErrors.applyValidation(collectFieldErrors({ current_password: passwordSet.value && !currentPassword.value && '请输入当前密码。', new_password: !isUtf8LengthInRange(newPassword.value, 12, 72) && '密码必须为 12–72 个 UTF-8 字节。', confirm_password: newPassword.value !== repeatPassword.value && '两次输入的新密码不一致。' }), formElement)) return
  const changing = passwordSet.value
  busy.value = true; error.value = ''; message.value = ''
  try { if (passwordSet.value) await changeAccountPassword(currentPassword.value, newPassword.value); else await setupIdentityPassword(newPassword.value); passwordSet.value = true; message.value = changing ? '密码已修改。' : '本地密码已设置，可使用邮箱密码登录。' }
  catch (cause) { await formErrors.applyApiError(cause, '修改失败，请确认当前密码及新密码长度；第三方授权过期时请重新登录。', formElement, { current_password: 'current_password', password: 'new_password' }) }
  finally { currentPassword.value = ''; repeatPassword.value = ''; newPassword.value = ''; busy.value = false }
}
</script>
