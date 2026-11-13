<script setup lang="ts">
import { computed, ref, toRef } from 'vue';
import type { BrowserState } from '../api/types';
import { useBrowserFrame } from '../composables/useBrowserFrame';
import AppIcon from './AppIcon.vue';

const props = defineProps<{ requestId: string; browser: BrowserState }>();

/** Open while it works, so the user sees what the browser does; one click folds it away. */
const open = ref(true);
const host = computed(() => {
  try {
    return new URL(props.browser.url).host;
  } catch {
    return props.browser.url;
  }
});
const { url } = useBrowserFrame(toRef(props, 'requestId'), open);
</script>

<template>
  <section class="overflow-hidden rounded-lg border border-line bg-surface" :aria-label="$t('chat.browser.title')" data-testid="browser-live">
    <button
      type="button"
      class="flex w-full items-center gap-2 px-3 py-2 text-left text-xs text-ink-muted hover:bg-raised focus-visible:outline-2 focus-visible:outline-accent"
      :aria-expanded="open"
      :aria-label="open ? $t('chat.browser.collapse') : $t('chat.browser.expand')"
      @click="open = !open"
    >
      <span class="size-1.5 shrink-0 rounded-full bg-ok motion-safe:animate-pulse" aria-hidden="true" />
      <span class="shrink-0 font-medium text-ink">{{ $t('chat.browser.title') }}</span>
      <span class="min-w-0 flex-1 truncate" :title="browser.url">{{ host }} · {{ browser.step }}</span>
      <AppIcon name="chevron-down" size="sm" class="shrink-0 transition-transform motion-reduce:transition-none" :class="{ 'rotate-180': open }" />
    </button>
    <div v-if="open" class="border-t border-line bg-canvas">
      <img v-if="url" :src="url" :alt="$t('chat.browser.alt', { site: host })" class="max-h-72 w-full object-contain object-top" data-testid="browser-frame" />
      <p v-else class="px-3 py-6 text-center text-xs text-ink-dim" role="status">{{ browser.step }}</p>
    </div>
  </section>
</template>
