<script setup lang="ts">
import { useLanguage } from '../composables/useLanguage';
import { LANGS } from '../i18n';

const { lang, switchTo } = useLanguage();
</script>

<template>
  <div role="group" :aria-label="$t('language.switchLabel')" class="inline-flex rounded-lg border border-line-strong bg-surface p-0.5" data-testid="language-switch">
    <button
      v-for="code in LANGS"
      :key="code"
      type="button"
      :aria-pressed="lang === code"
      :lang="code"
      class="rounded-md px-2.5 py-1 text-xs font-medium uppercase transition motion-reduce:transition-none"
      :class="lang === code ? 'bg-selected text-ink' : 'text-ink-muted hover:text-ink'"
      @click="switchTo(code)"
    >
      {{ code }}
    </button>
  </div>
</template>
