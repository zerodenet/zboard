<template>
  <span :aria-label="label" :title="display !== label ? label : undefined">{{ display }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatBalance, formatWalletMoney } from '../utils/walletMoney'
const props = withDefaults(defineProps<{ amount?: string | null; currency: string; showCurrency?: boolean }>(), { showCurrency: true })
const display = computed(() => formatBalance(props.amount, props.currency, props.showCurrency))
const label = computed(() => props.amount != null && /^-?\d+$/.test(props.amount) ? formatWalletMoney(props.amount, props.currency) : '余额未知')
</script>
