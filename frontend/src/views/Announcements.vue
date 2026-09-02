<template>
  <section class="standard-page">
    <PageHeader title="站点公告" description="发布面向访客、登录用户或管理员的定时公告；前台最多展示 10 条当前有效公告。" eyebrow="Communications">
      <template #actions><PageRefreshButton :loading="loading" label="刷新公告" @click="load" /><UiButton type="button" @click="newAnnouncement">新建公告</UiButton></template>
    </PageHeader>
    <TransientFeedback :success="success" :error="error" />
    <UiSection>
      <DataTable v-if="items.length" caption="站点公告列表" :row-count="items.length" :min-width="880">
        <thead><tr><th>公告</th><th>级别</th><th>受众</th><th>状态</th><th>有效时间</th><th></th></tr></thead>
        <tbody><tr v-for="item in items" :key="item.id"><td><div class="cell-title"><strong>{{ item.title }}</strong><span>{{ item.content }}</span></div></td><td><StatusBadge :tone="severityTone(item.severity)">{{ severityLabel(item.severity) }}</StatusBadge></td><td>{{ audienceLabel(item.audience) }}</td><td><StatusBadge :tone="item.status === 'published' ? 'success' : item.status === 'archived' ? 'neutral' : 'warning'">{{ statusLabel(item.status) }}</StatusBadge></td><td><span>{{ item.starts_at ? formatDate(item.starts_at) : '立即' }}</span><br><small>{{ item.ends_at ? `至 ${formatDate(item.ends_at)}` : '长期有效' }}</small></td><td><UiButton variant="ghost" size="sm" type="button" @click="edit(item)">编辑</UiButton></td></tr></tbody>
      </DataTable>
      <EmptyState v-else icon="info" title="还没有公告" description="创建第一条公告后，可先保存草稿再发布。" />
    </UiSection>

    <div v-if="editing" class="modal-backdrop" @click.self="closeEditor">
      <section class="editor-card" role="dialog" aria-modal="true" aria-label="公告编辑器">
        <header><div><small>Announcement</small><h2>{{ form.id ? '编辑公告' : '新建公告' }}</h2></div><UiButton variant="ghost" icon type="button" aria-label="关闭" @click="closeEditor">×</UiButton></header>
        <div class="stack">
          <FormField v-slot="{ controlAttrs }" label="标题" name="announcement-title" required><UiInput v-model.trim="form.title" v-bind="controlAttrs" maxlength="160" /></FormField>
          <FormField v-slot="{ controlAttrs }" label="正文" name="announcement-content" required><UiTextarea v-model="form.content" v-bind="controlAttrs" rows="7" maxlength="16384" /></FormField>
          <div class="form-grid"><FormField v-slot="{ controlAttrs }" label="级别" name="announcement-severity"><UiSelect v-model="form.severity" v-bind="controlAttrs" :options="severityOptions" /></FormField><FormField v-slot="{ controlAttrs }" label="受众" name="announcement-audience"><UiSelect v-model="form.audience" v-bind="controlAttrs" :options="audienceOptions" /></FormField><FormField v-slot="{ controlAttrs }" label="状态" name="announcement-status"><UiSelect v-model="form.status" v-bind="controlAttrs" :options="statusOptions" /></FormField></div>
          <div class="form-grid"><FormField v-slot="{ controlAttrs }" label="开始时间" name="announcement-start"><UiInput v-model="form.starts_at" v-bind="controlAttrs" type="datetime-local" /></FormField><FormField v-slot="{ controlAttrs }" label="结束时间" name="announcement-end"><UiInput v-model="form.ends_at" v-bind="controlAttrs" type="datetime-local" /></FormField></div>
          <label class="toggle-row"><UiCheckbox v-model="form.dismissible" /><span>允许用户关闭这条公告</span></label>
          <PageAlert v-if="form.status === 'published'" tone="warning" title="发布后立即按时间与受众生效">已经发布的公告不能删除，只能归档；编辑会提升版本，使用户重新看到更新后的公告。</PageAlert>
          <div class="form-actions"><UiButton v-if="form.id && form.status === 'draft'" variant="danger" type="button" :loading="deleting" @click="remove">删除草稿</UiButton><UiButton variant="ghost" type="button" @click="closeEditor">取消</UiButton><UiButton type="button" :loading="saving" @click="save">保存公告</UiButton></div>
        </div>
      </section>
    </div>
  </section>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { createAnnouncement, deleteAnnouncement, fetchAdminAnnouncements, updateAnnouncement, type AdminAnnouncement, type AnnouncementWriteRequest } from '../api/client'
import DataTable from '../components/DataTable.vue'
import EmptyState from '../components/EmptyState.vue'
import PageAlert from '../components/PageAlert.vue'
import PageHeader from '../components/PageHeader.vue'
import StatusBadge from '../components/StatusBadge.vue'
import TransientFeedback from '../components/TransientFeedback.vue'
import { normalizeApiErrorMessage } from '../utils/apiError'
import { confirmAction } from '../utils/feedback'

const loading = ref(false), saving = ref(false), deleting = ref(false), editing = ref(false)
const success = ref(''), error = ref(''), items = ref<AdminAnnouncement[]>([])
type AnnouncementForm = { id: number; title: string; content: string; severity: AdminAnnouncement['severity']; audience: AdminAnnouncement['audience']; status: AdminAnnouncement['status']; dismissible: boolean; starts_at: string; ends_at: string; revision: number }
const blank = (): AnnouncementForm => ({ id: 0, title: '', content: '', severity: 'info', audience: 'all', status: 'draft', dismissible: true, starts_at: '', ends_at: '', revision: 0 })
const form = reactive<AnnouncementForm>(blank())
const severityOptions = [{ label: '信息', value: 'info' }, { label: '成功', value: 'success' }, { label: '警告', value: 'warning' }, { label: '严重', value: 'critical' }]
const audienceOptions = [{ label: '所有人', value: 'all' }, { label: '仅访客', value: 'guest' }, { label: '登录用户', value: 'user' }, { label: '仅管理员', value: 'admin' }]
const statusOptions = [{ label: '草稿', value: 'draft' }, { label: '已发布', value: 'published' }, { label: '已归档', value: 'archived' }]

async function load() { loading.value = true; try { items.value = (await fetchAdminAnnouncements()).items } catch (cause) { error.value = normalizeApiErrorMessage(cause, '公告加载失败。') } finally { loading.value = false } }
function newAnnouncement() { Object.assign(form, blank()); editing.value = true }
function localDate(value?: string | null) { if (!value) return ''; const date = new Date(value); const offset = date.getTimezoneOffset() * 60000; return new Date(date.getTime() - offset).toISOString().slice(0, 16) }
function edit(item: AdminAnnouncement) { Object.assign(form, { ...item, starts_at: localDate(item.starts_at), ends_at: localDate(item.ends_at) }); editing.value = true }
function closeEditor() { if (!saving.value && !deleting.value) editing.value = false }
function payload(): AnnouncementWriteRequest { return { title: form.title, content: form.content, severity: form.severity, audience: form.audience, status: form.status, dismissible: form.dismissible, starts_at: form.starts_at ? new Date(form.starts_at).toISOString() : null, ends_at: form.ends_at ? new Date(form.ends_at).toISOString() : null, ...(form.id ? { expected_revision: form.revision } : {}) } }
async function save() { saving.value = true; error.value = ''; try { if (form.id) await updateAnnouncement(form.id, payload()); else await createAnnouncement(payload()); success.value = '公告已保存。'; editing.value = false; await load() } catch (cause) { error.value = normalizeApiErrorMessage(cause, '公告保存失败。') } finally { saving.value = false } }
async function remove() { if (!await confirmAction({ title: '删除公告草稿？', message: '草稿删除后无法恢复。', confirmText: '删除草稿', tone: 'danger' })) return; deleting.value = true; try { await deleteAnnouncement(form.id); success.value = '公告草稿已删除。'; editing.value = false; await load() } catch (cause) { error.value = normalizeApiErrorMessage(cause, '公告删除失败。') } finally { deleting.value = false } }
const severityLabel = (value: string) => ({ info: '信息', success: '成功', warning: '警告', critical: '严重' } as Record<string, string>)[value] || value
const severityTone = (value: string) => value === 'critical' ? 'danger' : value === 'warning' ? 'warning' : value === 'success' ? 'success' : 'info'
const audienceLabel = (value: string) => ({ all: '所有人', guest: '仅访客', user: '登录用户', admin: '仅管理员' } as Record<string, string>)[value] || value
const statusLabel = (value: string) => ({ draft: '草稿', published: '已发布', archived: '已归档' } as Record<string, string>)[value] || value
const formatDate = (value: string) => new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'short' }).format(new Date(value))
onMounted(load)
</script>

<style scoped>
.cell-title span { max-width: 420px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.modal-backdrop { position: fixed; z-index: 1300; inset: 0; display: grid; place-items: center; padding: 20px; background: var(--navigation-scrim); }.editor-card { width: min(760px, 100%); max-height: calc(100vh - 40px); overflow: auto; padding: 24px; border-radius: 18px; background: var(--surface); box-shadow: 0 30px 90px var(--sidebar-shadow); }.editor-card > header { display: flex; align-items: center; justify-content: space-between; margin-bottom: 20px; }.editor-card h2 { margin: 4px 0 0; }.editor-card header small { color: var(--primary); font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }.toggle-row { display: flex; gap: 10px; align-items: center; }
</style>
