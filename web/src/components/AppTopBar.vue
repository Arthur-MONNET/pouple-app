<script setup lang="ts">
import BrandMark from './BrandMark.vue';
import LanguageSwitch from './LanguageSwitch.vue';

defineProps<{ attention: number }>();
</script>

<template>
  <header class="flex h-12 shrink-0 items-center gap-4 border-b border-line-soft bg-canvas px-4">
    <RouterLink to="/" class="rounded-md" :aria-label="$t('nav.goHome')"><BrandMark /></RouterLink>
    <nav :aria-label="$t('nav.main')" class="ml-2 flex items-center gap-1">
      <RouterLink
        to="/"
        class="rounded-md px-2.5 py-1 text-sm text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
        active-class="bg-raised !text-ink"
        exact-active-class="bg-raised !text-ink"
        :class="{ 'bg-raised text-ink': $route.path.startsWith('/c/') }"
      >
        {{ $t('nav.conversations') }}
        <span v-if="attention > 0" class="ml-1 rounded bg-accent/20 px-1 text-2xs text-accent">{{ attention }}</span>
      </RouterLink>
      <RouterLink
        to="/connectors"
        class="rounded-md px-2.5 py-1 text-sm text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
        active-class="bg-raised !text-ink"
      >
        {{ $t('nav.connectors') }}
      </RouterLink>
      <RouterLink
        to="/settings"
        class="rounded-md px-2.5 py-1 text-sm text-ink-muted transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
        active-class="bg-raised !text-ink"
      >
        {{ $t('nav.settings') }}
      </RouterLink>
    </nav>
    <LanguageSwitch class="ml-auto" />
  </header>
</template>
