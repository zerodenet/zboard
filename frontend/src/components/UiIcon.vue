<template>
  <svg
    class="ui-icon"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path v-for="path in paths" :key="path" :d="path" />
  </svg>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ name: string }>()

const icons: Record<string, string[]> = {
  dashboard: ['M4 4h6v6H4z', 'M14 4h6v10h-6z', 'M4 14h6v6H4z', 'M14 18h6v2h-6z'],
  nodes: ['M5 4h14v5H5z', 'M5 15h14v5H5z', 'M8 6.5h.01', 'M8 17.5h.01', 'M12 9v6'],
  plans: ['M4 7.5 12 3l8 4.5v9L12 21l-8-4.5z', 'M4.5 7.7 12 12l7.5-4.3', 'M12 12v9'],
  billing: ['M3 6h18v12H3z', 'M3 10h18', 'M7 15h3'],
  users: ['M16 20v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2', 'M9 10a4 4 0 1 0 0-8 4 4 0 0 0 0 8', 'M22 20v-2a4 4 0 0 0-3-3.87', 'M16 2.13a4 4 0 0 1 0 7.75'],
  audit: ['M6 3h12v18H6z', 'M9 7h6', 'M9 11h6', 'M9 15h4'],
  tasks: ['M9 4h11', 'M9 12h11', 'M9 20h11', 'm3 4 1 1 2-2', 'm3 12 1 1 2-2', 'm3 20 1 1 2-2'],
  ticket: ['M4 5h16v11H8l-4 4z', 'M8 9h8', 'M8 12h5'],
  settings: ['M12 15.5a3.5 3.5 0 1 0 0-7 3.5 3.5 0 0 0 0 7', 'M19.4 15a1.7 1.7 0 0 0 .34 1.88l.06.06-2 3.46-.08-.02a1.7 1.7 0 0 0-1.8-.2l-.23.14a1.7 1.7 0 0 0-.82 1.5V22h-4v-.18a1.7 1.7 0 0 0-.82-1.5l-.23-.14a1.7 1.7 0 0 0-1.8.2l-.08.02-2-3.46.06-.06A1.7 1.7 0 0 0 6.6 15l-.24-.14a1.7 1.7 0 0 0-1.7 0l-.16.09-2-3.46.16-.09a1.7 1.7 0 0 0 .86-1.48v-.28a1.7 1.7 0 0 0-.86-1.48l-.16-.09 2-3.46.16.09a1.7 1.7 0 0 0 1.7 0l.24-.14A1.7 1.7 0 0 0 7.4 3l-.06-.06 2-3.46.08.02a1.7 1.7 0 0 0 1.8.2l.23-.14A1.7 1.7 0 0 0 12.27 0h4a1.7 1.7 0 0 0 .82 1.5l.23.14a1.7 1.7 0 0 0 1.8-.2l.08-.02 2 3.46-.06.06a1.7 1.7 0 0 0-.34 1.88l.24.14a1.7 1.7 0 0 0 1.7 0l.16-.09 2 3.46-.16.09a1.7 1.7 0 0 0-.86 1.48v.28a1.7 1.7 0 0 0 .86 1.48l.16.09-2 3.46-.16-.09a1.7 1.7 0 0 0-1.7 0z'],
  logout: ['M10 17l5-5-5-5', 'M15 12H3', 'M15 4h4a2 2 0 0 1 2 2v12a2 2 0 0 1-2 2h-4'],
  menu: ['M4 6h16', 'M4 12h16', 'M4 18h16'],
  close: ['M5 5l14 14', 'M19 5 5 19'],
  plus: ['M12 5v14', 'M5 12h14'],
  refresh: ['M20 6v5h-5', 'M4 18v-5h5', 'M18.5 9A7 7 0 0 0 6 6.5L4 11', 'M5.5 15A7 7 0 0 0 18 17.5l2-4.5'],
  activity: ['M3 12h4l2-7 4 14 2-7h6'],
  dollar: ['M12 2v20', 'M17 6.5c-1-1-2.5-1.5-5-1.5-3 0-5 1.5-5 3.5S9 12 12 12s5 1.5 5 3.5S15 19 12 19c-2.5 0-4-.5-5-1.5'],
  wifi: ['M5 12.5a10 10 0 0 1 14 0', 'M8.5 16a5 5 0 0 1 7 0', 'M12 20h.01'],
  check: ['M5 12l4 4L19 6'],
  alert: ['M12 3 2 21h20z', 'M12 9v4', 'M12 17h.01'],
  database: ['M4 6c0-2 16-2 16 0v12c0 2-16 2-16 0z', 'M4 6c0 2 16 2 16 0', 'M4 12c0 2 16 2 16 0'],
  key: ['M15 7a5 5 0 1 1-2 9.58L9 21H5v-4H2v-3l5.42-5A5 5 0 0 1 15 7'],
  copy: ['M8 8h11v11H8z', 'M5 16H3V3h13v2'],
  edit: ['M4 20h4L19 9l-4-4L4 16z', 'm13.5-13.5 4 4'],
  play: ['m8 5 11 7-11 7z'],
  search: ['M11 19a8 8 0 1 1 0-16 8 8 0 0 1 0 16', 'm21 21-4.3-4.3'],
  chevron: ['m9 18 6-6-6-6'],
  shield: ['M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10', 'm9 12 2 2 4-5'],
  clock: ['M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20', 'M12 6v6l4 2'],
  terminal: ['M4 5h16v14H4z', 'm7 9 3 3-3 3', 'M12 15h5'],
  maximize: ['M8 3H3v5', 'M16 3h5v5', 'M8 21H3v-5', 'M16 21h5v-5'],
  minimize: ['M8 8H3V3', 'M16 8h5V3', 'M8 16H3v5', 'M16 16h5v5'],
}

const paths = computed(() => icons[props.name] || icons.activity)
</script>

<style scoped>
.ui-icon { display: block; width: 1.15em; height: 1.15em; flex: 0 0 auto; }
</style>
