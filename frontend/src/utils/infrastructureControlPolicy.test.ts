import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const sourceRoot = join(import.meta.dirname, '..')
const read = (...parts: string[]) => readFileSync(join(sourceRoot, ...parts), 'utf8')

describe('infrastructure control policy', () => {
  it('gives wrapped workbench controls one compact component height', () => {
    const styles = read('styles.css')
    const tokens = read('theme', 'tokens.css')

    expect(tokens).toContain('--control-height-compact: 36px')
    expect(styles).toContain('.workbench-filters > .date-range-filter')
    expect(styles).toContain('.workbench-filters .p-autocomplete-input')
    expect(styles).toContain('height:var(--control-height-compact)')
  })

  it('uses shared step navigation and remote lookup controls in infrastructure forms', () => {
    const protocols = read('views', 'Protocols.vue')
    const stepNav = read('components', 'UiStepNav.vue')
    const autocomplete = read('components', 'UiAutocomplete.vue')

    expect(protocols).toContain('<UiStepNav')
    expect(protocols).not.toContain('class="wizard-steps"')
    expect(stepNav).toContain('gap: 8px')
    expect(stepNav).toContain('aria-current')
    expect(autocomplete).toContain('class="ui-autocomplete"')
    expect(autocomplete).toContain('height: var(--control-height)')

    for (const component of ['NodeLookup.vue', 'NodeGroupLookup.vue', 'EndpointLookup.vue']) {
      const source = read('components', component)
      expect(source).toContain('<UiAutocomplete')
      expect(source).not.toContain('<PrimeAutoComplete')
    }
  })

  it('keeps protocol configuration reusable through copy and node switching', () => {
    const protocols = read('views', 'Protocols.vue')

    expect(protocols).toContain('openCopy(endpoint)')
    expect(protocols).toContain('复制协议配置')
    expect(protocols).toContain('保存为新服务')
    expect(protocols).toContain('协议配置可切换承载 VPS')
    expect(protocols).not.toContain(':disabled="Boolean(form.id)" @select="handleNodeSelect"')
  })

  it('preserves table-cell geometry for protocol node-group headings', () => {
    const protocols = read('views', 'Protocols.vue')

    expect(protocols).toContain('class="protocol-group-row"><td colspan="13"><div class="protocol-group-content">')
    expect(protocols).toContain('.protocol-group-content{display:flex')
    expect(protocols).not.toContain('.protocol-group-row td{display:flex')
  })

  it('promotes the protocol Stripe baseline to every administrator surface', () => {
    const protocols = read('views', 'Protocols.vue')
    const layout = read('layouts', 'AdminLayout.vue')
    const styles = read('styles.css')
    const pager = read('components', 'TablePager.vue')
    const refresh = read('components', 'PageRefreshButton.vue')

    expect(layout).toContain('class="app-content admin-stripe-surface"')
    expect(styles).toContain('.admin-stripe-surface .standard-page')
    expect(styles).toContain('.admin-stripe-surface .data-workbench')
    expect(styles).toContain('.admin-stripe-surface .data-workbench .data-table th')
    expect(pager).toContain("variant: 'stripe'")
    expect(refresh).toContain('class="page-refresh-button"')
    expect(protocols).toContain('class="standard-page"')
    expect(protocols).toContain('class="protocol-status-overview"')
    expect(protocols).toContain('<OverviewCard')
    expect(protocols).toContain('selectDeploymentStatus(item.value)')
    expect(protocols).toContain('loadProtocolOverview')
    expect(protocols).toContain('<TablePager variant="stripe"')
    expect(protocols).not.toContain('protocol-stripe-page')
    expect(protocols).not.toContain('protocol-stripe-workbench')

    for (const view of ['Dashboard.vue', 'Nodes.vue', 'NodeGroups.vue', 'Plans.vue', 'Users.vue', 'Orders.vue', 'Subscriptions.vue', 'SubscriptionTemplates.vue', 'SubscriptionRuleSets.vue', 'Tasks.vue', 'Traffic.vue', 'OperationLogs.vue', 'AuditLogs.vue', 'Settings.vue']) {
      const source = read('views', view)
      expect(source).toContain('class="standard-page"')
      expect(source).toContain('<PageRefreshButton')
    }
    expect(read('components', 'TicketCenter.vue')).toContain('<PageRefreshButton')
  })

  it('uses one adaptive row-action contract for dense administrator lists', () => {
    const rowActions = read('components', 'RowActions.vue')
    const protocols = read('views', 'Protocols.vue')

    expect(rowActions).toContain("actions.length === 1 ? 'single' : 'menu'")
    expect(rowActions).toContain("role: 'menuitem'")
    expect(rowActions).toContain('RowActionMenu')
    expect(protocols).toContain('<RowActions')
    expect(protocols).not.toContain('protocol-row-actions')

    for (const view of ['Users.vue', 'Subscriptions.vue', 'Orders.vue', 'Tasks.vue', 'SubscriptionTemplates.vue', 'SubscriptionRuleSets.vue']) {
      expect(read('views', view)).toContain('<RowActions')
    }
  })

  it('keeps Stripe-style workbench filters inside shared components', () => {
    const input = read('components', 'WorkbenchFilterInput.vue')
    const bar = read('components', 'WorkbenchFilterBar.vue')
    const chip = read('components', 'WorkbenchFilterChip.vue')
    const select = read('components', 'WorkbenchFilterSelect.vue')
    const styles = read('styles.css')

    expect(input).toContain('v-model="draft"')
    expect(input).toContain('model.value = draft.value.trim()')
    expect(bar).toContain('class="workbench-stripe-filters"')
    expect(chip).toContain('class="workbench-filter-chip"')
    expect(chip).toContain('aria-haspopup="dialog"')
    expect(select).toContain('role="listbox"')
    expect(styles).toContain('.workbench-filter-chip-trigger.p-button')
    expect(styles).toContain('border:1px dashed var(--line-strong)')

    for (const view of ['Protocols.vue', 'Nodes.vue', 'NodeGroups.vue', 'Plans.vue', 'Users.vue', 'Orders.vue', 'Subscriptions.vue', 'SubscriptionTemplates.vue', 'SubscriptionRuleSets.vue']) {
      const source = read('views', view)
      expect(source).toContain('<WorkbenchFilterBar')
      expect(source).toContain('<WorkbenchFilterInput')
      expect(source).not.toContain('<UiSearchInput')
    }
  })

  it('protects plan edits with the loaded revision', () => {
    const plans = read('views', 'Plans.vue')
    const client = read('api', 'client.ts')

    expect(client).toContain('revision: number')
    expect(plans).toContain('expected_revision: planDraft.revision')
    expect(plans).toContain('expected_revision: plan.revision')
    expect(plans).toContain('planRevisionConflict')
    expect(plans).toContain('重新加载最新版本')
  })
})
