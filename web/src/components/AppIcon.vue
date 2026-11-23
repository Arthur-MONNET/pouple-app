<script setup lang="ts">
export type IconName =
  | 'logo'
  | 'refresh'
  | 'external'
  | 'message'
  | 'plus'
  | 'close'
  | 'check'
  | 'chevron-right'
  | 'chevron-down'
  | 'eye'
  | 'eye-off'
  | 'paste'
  | 'alert'
  | 'plug'
  | 'trash'
  | 'terminal'
  | 'send'
  | 'stop'
  | 'info'
  | 'inbox'
  | 'bell'
  | 'pencil'
  | 'settings'
  | 'book'
  | 'more'
  | 'copy'
  | 'chevron-up'
  | 'x-circle'
  | 'check-circle'
  | 'phone'
  | 'lock';

const PATHS: Record<IconName, string[]> = {
  logo: ['M8 3h8a5 5 0 0 1 5 5v8a5 5 0 0 1-5 5H8a5 5 0 0 1-5-5V8a5 5 0 0 1 5-5z', 'M8 15a4 4 0 0 1 8 0', 'M12 15l2.5-3'],
  refresh: ['M20 11a8 8 0 1 0-2.3 5.7', 'M20 5v6h-6'],
  external: ['M14 5h5v5M19 5l-8 8M18 14v4a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h4'],
  message: ['M5 6h14v10H9l-4 3z'],
  plus: ['M12 5v14M5 12h14'],
  close: ['M6 6l12 12M18 6L6 18'],
  check: ['M5 12l4 4 10-10'],
  'chevron-right': ['M9 6l6 6-6 6'],
  'chevron-down': ['M6 9l6 6 6-6'],
  eye: ['M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z', 'M12 9.5a2.5 2.5 0 1 0 0 5 2.5 2.5 0 0 0 0-5z'],
  'eye-off': ['M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z', 'M4 4l16 16'],
  paste: ['M9 4h6v3H9z', 'M8 5.5H6a1 1 0 0 0-1 1V19a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V6.5a1 1 0 0 0-1-1h-2'],
  alert: ['M12 4l9 16H3z', 'M12 10v4', 'M12 17h.01'],
  plug: ['M9 3v5M15 3v5', 'M6 8h12v3a6 6 0 0 1-12 0z', 'M12 17v4'],
  trash: ['M5 7h14', 'M10 7V4h4v3', 'M7 7l1 13h8l1-13'],
  terminal: ['M4 6h16v12H4z', 'M8 10l3 2-3 2', 'M13 15h3'],
  send: ['M4 12l16-7-6 15-3-6z'],
  stop: ['M7 7h10v10H7z'],
  info: ['M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16z', 'M12 11v5', 'M12 8h.01'],
  inbox: ['M4 13l2-7h12l2 7', 'M4 13v5h16v-5h-5l-1 2h-4l-1-2z'],
  bell: ['M6 16v-5a6 6 0 0 1 12 0v5l1.5 2h-15z', 'M10 21h4'],
  pencil: ['M5 19l1-4L16.5 4.5a1.8 1.8 0 0 1 2.5 0l.5.5a1.8 1.8 0 0 1 0 2.5L9 18z', 'M14.5 6.5l3 3'],
  settings: ['M4 7h10M18 7h2M4 17h2M10 17h10', 'M16 5v4M8 15v4'],
  book: ['M5 5.5A1.5 1.5 0 0 1 6.5 4H19v14H6.5A1.5 1.5 0 0 0 5 19.5z', 'M5 19.5A1.5 1.5 0 0 0 6.5 21H19', 'M9 8h6'],
  more: ['M6 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2z', 'M12 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2z', 'M18 11a1 1 0 1 0 0 2 1 1 0 0 0 0-2z'],
  copy: ['M9 9h10v11H9z', 'M15 9V5H5v11h4'],
  'chevron-up': ['M6 15l6-6 6 6'],
  'x-circle': ['M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16z', 'M9.5 9.5l5 5M14.5 9.5l-5 5'],
  phone: ['M9.5 2.5h5A2.5 2.5 0 0 1 17 5v14a2.5 2.5 0 0 1-2.5 2.5h-5A2.5 2.5 0 0 1 7 19V5a2.5 2.5 0 0 1 2.5-2.5z', 'M11 18.5h2'],
  lock: ['M7 11h10a2 2 0 0 1 2 2v5a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2v-5a2 2 0 0 1 2-2z', 'M8 11V8a4 4 0 0 1 8 0v3'],
  'check-circle': ['M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16z', 'M8.5 12.2l2.3 2.3 4.7-4.7'],
};

withDefaults(defineProps<{ name: IconName; size?: 'sm' | 'md' | 'lg' }>(), { size: 'md' });
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    class="shrink-0"
    :class="{ 'size-3.5': size === 'sm', 'size-4': size === 'md', 'size-5': size === 'lg' }"
  >
    <path v-for="d in PATHS[name]" :key="d" :d="d" />
  </svg>
</template>
