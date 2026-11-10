<script setup lang="ts">
import BrandMark from './BrandMark.vue';
import { useUpdate } from '../composables/useUpdate';
import LanguageSwitch from './LanguageSwitch.vue';

defineProps<{ attention: number }>();

const { available } = useUpdate();
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
    <RouterLink
      v-if="available"
      :to="{ path: '/settings', hash: '#updates' }"
      :aria-label="$t('update.badgeAria')"
      class="ml-auto flex items-center gap-1.5 rounded-full border border-accent/50 bg-accent/10 px-2.5 py-1 text-xs text-accent transition hover:bg-accent/20 motion-reduce:transition-none"
      data-testid="update-badge"
    >
      <span class="size-1.5 rounded-full bg-accent" aria-hidden="true" />
      {{ $t('update.badge') }}
    </RouterLink>
    <LanguageSwitch :class="available ? '' : 'ml-auto'" />
  </header>
</template>
