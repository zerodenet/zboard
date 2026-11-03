<template>
  <dl class="balance-summary" :aria-label="`${balance.currency} 余额`">
    <div v-if="showTotal"><dt>余额</dt><dd><BalanceValue :amount="balance.balance_minor" :currency="balance.currency" :show-currency="false" /></dd></div>
    <div><dt>可用</dt><dd><BalanceValue :amount="balance.available_minor" :currency="balance.currency" :show-currency="false" /></dd></div>
    <div><dt>已预留</dt><dd><BalanceValue :amount="balance.reserved_minor" :currency="balance.currency" :show-currency="false" /></dd></div>
  </dl>
</template>

<script setup lang="ts">
import BalanceValue from './BalanceValue.vue'
withDefaults(defineProps<{ balance: { currency: string; balance_minor?: string; available_minor?: string; reserved_minor?: string }; showTotal?: boolean }>(), { showTotal: true })
</script>

<style scoped>
.balance-summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(100px, 1fr)); gap: 12px; margin: 0; }
.balance-summary div { min-width: 0; }
.balance-summary dt { color: var(--muted-foreground); font-size: 12px; }
.balance-summary dd { margin: 4px 0 0; font-size: 18px; font-weight: 600; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
</style>
