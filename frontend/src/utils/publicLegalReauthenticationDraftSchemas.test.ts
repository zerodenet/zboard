import { expect, it } from 'vitest'
import { settingsRecoveryDraft, settingsRecoveryValueType } from './settingsReauthenticationDraftSchemas'
const document = { slug: 'terms', title: 'Terms', summary: '', content: '# Public terms\n\nOrdinary text.', published: false, placements: ['footer', 'purchase'] }
function draft(key: string, value: string, baseline = '[]') { return { site: null, configs: [{ key, value, baseline, revision: 7 }] } }
it('preserves policy JSON bytes and its exact selected ordinary structure', () => {
  const value = JSON.stringify([document], null, 2)
  expect(settingsRecoveryDraft(draft('site_policy_documents', value))).toEqual(draft('site_policy_documents', value))
  expect(settingsRecoveryValueType('site_policy_documents')).toBe('json')
  expect(settingsRecoveryValueType('smtp_password')).toBeUndefined()
})
it('preserves incomplete business fields and public legal URLs without pretending to validate submission', () => {
  const value = JSON.stringify([{ label: '', value: 'Not yet complete', url: '/invalid-unsubmitted-url' }], null, 2)
  expect(settingsRecoveryDraft(draft('site_legal_items', value))).toEqual(draft('site_legal_items', value))
  const invalidBusiness = JSON.stringify([{ ...document, slug: '', title: '', content: ' ', placements: [] }])
  expect(settingsRecoveryDraft(draft('site_policy_documents', invalidBusiness))).toEqual(draft('site_policy_documents', invalidBusiness))
})
it.each(['site_terms_content', 'site_privacy_content', 'site_refund_content'])('keeps ordinary legacy public content verbatim for %s', key => {
  const value = '# Public policy\n' + 'Long ordinary body '.repeat(1200)
  expect(settingsRecoveryDraft(draft(key, value, 'old'))).toEqual(draft(key, value, 'old'))
})
it('rejects malformed JSON, unknown object fields and non-content config keys without silently picking a subset', () => {
  expect(settingsRecoveryDraft(draft('site_policy_documents', '[invalid'))).toBeUndefined()
  expect(settingsRecoveryDraft(draft('site_policy_documents', JSON.stringify([{ ...document, smtp_password: 'secret' }])))).toBeUndefined()
  expect(settingsRecoveryDraft(draft('site_legal_items', JSON.stringify([{ label: 'Public', value: 'x', token: 'secret' }])))).toBeUndefined()
  expect(settingsRecoveryDraft(draft('smtp_password', 'secret', ''))).toBeUndefined()
})
it.each(['null', '[]', ''])('keeps an unset or empty policy representation unchanged (%s)', value => {
  expect(settingsRecoveryDraft(draft('site_policy_documents', value, value))).toEqual(draft('site_policy_documents', value, value))
})
