import { describe, expect, it } from 'vitest'
import { defaultSubscriptionCustomization } from './subscriptionTemplateEditor'
import { publicSubscriptionDraftURL, templateRecoveryCustomization, templateRecoveryFields, subscriptionTemplateRecoveryDraft, restoreTemplateCustomization, templateURLReferenceOrigin, ruleSetRecoveryFields, ruleSetRecoveryDraft } from './subscriptionEditorRecovery'
const templateFields = () => ({ name: 'Local name', slug: 'local', description: 'Ordinary text', renderer: 'clash', customization: defaultSubscriptionCustomization('clash'), is_active: true, sort_order: 1 })
const ruleFields = () => ({ name: 'Rules', description: '', tag: 'rules', mode: 'manual', source_url: '', source_format: 'auto', content: '{"password":"ordinary rule text is not inspected"}', sync_interval: 3600, is_active: true })
describe('subscription editor explicit recovery schemas', () => {
  it('retains structured visual configuration, renderer drafts and original authority without arbitrary object fields', () => {
    const original = defaultSubscriptionCustomization('clash'), fields = templateRecoveryFields(templateFields(), original, true)!
    const recovery = subscriptionTemplateRecoveryDraft({ id: 3, revision: 4, fields, baseline: fields, rendererDrafts: [{ renderer: 'clash', customization: fields.customization }], token: 'DO-NOT-STORE', approved: true })!
    expect(recovery.revision).toBe(4); expect(recovery.fields.customization.policy_groups[1].probe_url).toBeNull()
    expect(restoreTemplateCustomization(recovery.fields.customization, original)).toEqual(original)
    expect(JSON.stringify(recovery)).not.toContain('DO-NOT-STORE')
    expect(subscriptionTemplateRecoveryDraft({ ...recovery, baseline: null })).toBeUndefined()
    expect(subscriptionTemplateRecoveryDraft({ ...recovery, rendererDrafts: [...recovery.rendererDrafts, ...recovery.rendererDrafts] })).toBeUndefined()
  })
  it.each(['password', 'token', 'private_key', 'secret', 'authorization'])('rejects an unknown %s field at every configuration level instead of silently discarding it', (key) => {
    const source = defaultSubscriptionCustomization('clash')
    for (const path of ['root', 'tun', 'dns', 'server', 'group', 'rule']) {
      const value: any = structuredClone(source)
      value.rule_sets = [{ tag: 'ads', url: 'https://rules.example/ads', target: 'direct', interval: 60, format: 'yaml' }]
      const targets: Record<string, any> = { root: value, tun: value.tun, dns: value.dns, server: value.dns.servers[0], group: value.policy_groups[0], rule: value.rule_sets[0] }
      const target = targets[path]!
      target[key] = 'DO-NOT-STORE'
      expect(templateRecoveryCustomization(value, source, true), path).toBeUndefined()
    }
  })
  it('rejects opaque advanced configuration rather than guessing credentials in YAML or JSON', () => {
    const value = defaultSubscriptionCustomization('clash'); value.advanced_source = 'ordinary: true'
    expect(templateRecoveryCustomization(value, value, true)).toBeUndefined()
  })
  it('does not store unchanged server URL credentials, but binds them to the original revision for fresh restoration', () => {
    const source = defaultSubscriptionCustomization('clash')
    source.policy_groups[1]!.probe_url = 'https://user:DO-NOT-STORE@example.test/probe?token=DO-NOT-STORE'
    source.rule_sets = [{ tag: 'ads', url: 'https://rules.test/private/DO-NOT-STORE?key=DO-NOT-STORE', target: 'direct', interval: 60, format: 'yaml' }]
    source.dns.servers = [{ tag: 'doh', type: 'doh', host: '1.1.1.1', port: 443, path: '/private/DO-NOT-STORE' }]
    const stored = templateRecoveryCustomization(source, source, true)!
    expect(JSON.stringify(stored)).not.toContain('DO-NOT-STORE')
    expect(restoreTemplateCustomization(stored, source)).toEqual(source)
    const changed = structuredClone(source); changed.rule_sets[0]!.url = 'https://rules.test/?token=CHANGED'
    expect(templateRecoveryCustomization(changed, source, true)).toBeUndefined()
  })
  it('keeps edited public URLs as edits across repeated capture, and missing-resource null references as references', () => {
    const source = defaultSubscriptionCustomization('clash')
    source.rule_sets = [{ tag: 'ads', url: 'https://rules.test/private?token=DO-NOT-STORE', target: 'direct', interval: 60, format: 'yaml' }]
    const edited = structuredClone(source); edited.policy_groups[1]!.probe_url = 'https://public.test/probe'
    const first = templateRecoveryCustomization(edited, source, true)!
    const origin = templateURLReferenceOrigin(first, defaultSubscriptionCustomization('clash'))
    const restored = restoreTemplateCustomization(first, origin)
    const second = templateRecoveryCustomization(restored, origin, true)!
    expect(second.policy_groups[1]!.probe_url).toBe('https://public.test/probe')
    expect(second.rule_sets[0]!.url).toBeNull()
    expect(JSON.stringify(second)).not.toContain('DO-NOT-STORE')
  })
  it('retains ordinary manual content verbatim without substring secret scanning', () => {
    const fields = ruleSetRecoveryFields(ruleFields())!
    const draft = ruleSetRecoveryDraft({ id: 0, revision: 0, fields, baseline: null, payment_authorization: 'DO-NOT-STORE' })!
    expect(draft.fields.content).toBe(ruleFields().content); expect(JSON.stringify(draft)).not.toContain('DO-NOT-STORE')
    expect(ruleSetRecoveryDraft({ ...draft, revision: 1 })).toBeUndefined()
    expect(ruleSetRecoveryDraft({ ...draft, fields: { ...fields, source_url: null } })).toBeUndefined()
  })
  it('keeps unchanged remote sources out of storage while preserving hidden ordinary content', () => {
    const url = 'https://user:DO-NOT-STORE@rules.test/path?access_token=DO-NOT-STORE'
    const fields = ruleSetRecoveryFields({ ...ruleFields(), mode: 'remote', source_url: url }, url, true)!
    expect(fields.source_url).toBeNull(); expect(fields.content).toBe(ruleFields().content)
    expect(JSON.stringify(ruleSetRecoveryDraft({ id: 8, revision: 4, fields, baseline: fields }))).not.toContain('DO-NOT-STORE')
    expect(ruleSetRecoveryFields({ ...ruleFields(), source_url: url }, '', true)).toBeUndefined()
  })
  it.each(['https://user:secret@host.test/path', 'https://host.test/path?token=secret', 'https://host.test/path#secret', 'javascript:alert(1)', 'not-a-url'])('refuses unsafe or ambiguous edited URL %s', (url) => {
    expect(publicSubscriptionDraftURL(url)).toBe(false)
  })
})
