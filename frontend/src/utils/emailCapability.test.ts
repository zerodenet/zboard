import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = resolve(__dirname, '../../..')
const read = (path: string) => readFileSync(resolve(root, path), 'utf8')

describe('SMTP and email template capability', () => {
  it('exposes connection and delivery tests beside saved SMTP settings', () => {
    const settings = read('frontend/src/views/Settings.vue')
    const client = read('frontend/src/api/client.ts')
    expect(settings).toContain("runSMTPTest('connection')")
    expect(settings).toContain("runSMTPTest('delivery')")
    expect(settings).toContain('<EmailTemplateManager')
    expect(settings).toContain('smtpTestRecipient')
    expect(client).toContain("api.post('/admin/smtp/test', { mode, ...(recipient ? { recipient } : {}) })")
  })

  it('provides registration and reusable operational template workflows', () => {
    const manager = read('frontend/src/components/EmailTemplateManager.vue')
    const tasks = read('frontend/src/views/Tasks.vue')
    expect(manager).toContain('注册通知模板')
    expect(manager).toContain('运营模板')
    expect(manager).toContain('previewEmailTemplate')
    expect(tasks).toContain("fetchEmailTemplates('operational')")
    expect(tasks).toContain('applyEmailTemplate')
  })

  it('queues registration delivery through the persisted task boundary', () => {
    const registration = read('backend/internal/handler/handlers.go')
    const capability = read('backend/internal/handler/email_templates.go')
    const migration = read('backend/migrations/0001_init.up.sql')
    expect(registration).toContain('enqueueRegistrationWelcome(user)')
    expect(capability).toContain('startPersistedAdminTask(&task)')
    expect(capability).toContain('notification.enqueue')
    expect(migration).toContain('CREATE TABLE `email_templates`')
  })

  it('gates registration with a short-lived email code and visualizes persisted task status', () => {
    const registerView = read('frontend/src/views/Register.vue')
    const verification = read('backend/internal/handler/registration_email_verification.go')
    const tasks = read('frontend/src/views/Tasks.vue')
    const client = read('frontend/src/api/client.ts')
    expect(registerView).toContain('requestRegistrationCode')
    expect(registerView).toContain('register_email_verification')
    expect(verification).toContain('registrationCodeDigest')
    expect(verification).toContain('subtle.ConstantTimeCompare')
    expect(verification).not.toContain('Code string `gorm')
    expect(tasks).toContain('task-overview')
    expect(tasks).toContain('activeProgress')
    expect(client).toContain("api.get('/admin/tasks/summary')")
  })
})
