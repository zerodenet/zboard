import { describe, expect, it } from 'vitest'
import { emailTemplateRecoveryDraft, registrationRecoveryDraft, settingsRecoveryDraft } from './settingsReauthenticationDraftSchemas'
const site = { site_name: 'Panel', site_url: 'https://panel.example', allow_registration: false }
const fields = { name: 'Draft', slug: 'draft', category: 'operational', subject_template: 'Subject', body_template: 'Unsubmitted body', is_active: false, sort_order: 2 }
describe('settings recovery view-specific allowlists', () => {
  it('picks only public text fields and rejects SMTP keys rather than storing a whole config draft', () => {
    const source = { site: { ...site, baseline: { ...site, password: 'secret' }, smtp_password: 'secret' }, configs: [{ key: 'site_desc', value: 'Draft', baseline: 'Server', revision: 3, smtp_password: 'secret' }], smtp_password: 'secret' }
    expect(JSON.stringify(settingsRecoveryDraft(source))).not.toContain('secret')
    expect(settingsRecoveryDraft({ site: null, configs: [{ key: 'smtp_password', value: 'secret', baseline: '', revision: 1 }] })).toBeUndefined()
    expect(settingsRecoveryDraft({ site: null, configs: [source.configs[0], source.configs[0]] })).toBeUndefined()
  })
  it('keeps two booleans and their concurrency evidence without SMTP/configured fields', () => {
    expect(registrationRecoveryDraft({ registration: { value: true, baseline: site }, verification: { value: true, baseline: false, revision: 4, configured: true }, smtp_password: 'secret' })).toEqual({ registration: { value: true, baseline: site }, verification: { value: true, baseline: false, revision: 4 } })
    expect(registrationRecoveryDraft({ registration: null, verification: { value: 'true', baseline: false, revision: 4 } })).toBeUndefined()
  })
  it('retains original template revision and ordinary content, excluding recipients and execution data', () => {
    const draft = emailTemplateRecoveryDraft({ id: 3, revision: 7, fields: { ...fields, recipients: ['private@example.test'], token: 'secret' }, baseline: fields, smtp_password: 'secret' })
    expect(draft).toEqual({ id: 3, revision: 7, fields, baseline: fields })
    expect(emailTemplateRecoveryDraft({ id: 3, revision: 7, fields, baseline: null })).toBeUndefined()
    expect(emailTemplateRecoveryDraft({ id: 0, revision: 7, fields, baseline: null })).toBeUndefined()
    expect(emailTemplateRecoveryDraft({ id: 0, revision: 0, fields: { ...fields, body_template: 'x'.repeat(100001) }, baseline: null })).toBeUndefined()
  })
})
