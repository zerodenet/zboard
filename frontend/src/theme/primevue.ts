import { definePreset } from '@primeuix/themes'
import Aura from '@primeuix/themes/aura'

export const zboardPrimePreset = definePreset(Aura, {
  semantic: {
    primary: {
      50: '{indigo.50}',
      100: '{indigo.100}',
      200: '{indigo.200}',
      300: '{indigo.300}',
      400: '{indigo.400}',
      500: '{indigo.600}',
      600: '{indigo.700}',
      700: '{indigo.800}',
      800: '{indigo.900}',
      900: '{indigo.950}',
      950: '{indigo.950}',
    },
    colorScheme: {
      light: {
        surface: {
          0: 'var(--surface)',
          50: 'var(--surface-soft)',
          100: 'var(--surface-muted)',
          200: 'var(--line)',
          300: 'var(--line-strong)',
          400: 'var(--theme-surface-400)',
          500: 'var(--muted)',
          600: 'var(--text-secondary)',
          700: 'var(--text-body)',
          800: 'var(--theme-surface-800)',
          900: 'var(--sidebar)',
          950: 'var(--theme-surface-950)',
        },
      },
    },
    formField: {
      borderRadius: '8px',
    },
    focusRing: {
      width: '3px',
      style: 'solid',
      color: '{primary.100}',
      offset: '0',
    },
  },
})

export const primeVueOptions = {
  ripple: true,
  theme: {
    preset: zboardPrimePreset,
    options: {
      darkModeSelector: false,
      cssLayer: {
        name: 'primevue',
        order: 'primevue, zboard',
      },
    },
  },
}
