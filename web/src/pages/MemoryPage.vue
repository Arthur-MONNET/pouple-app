<script setup lang="ts">
import { onMounted } from 'vue';
import AppButton from '../components/AppButton.vue';
import AppIcon from '../components/AppIcon.vue';
import InlineError from '../components/InlineError.vue';
import { useApi } from '../composables/useApi';
import { useMemory } from '../composables/useMemory';
import { formatPlainDay, formatReceived } from '../lib/format';

const memory = useMemory(useApi());
const view = memory.view;
onMounted(() => void memory.load());

async function tidy(): Promise<void> {
  if (await memory.tidy()) await memory.load();
}
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto">
    <div class="mx-auto flex max-w-2xl flex-col gap-6 px-5 py-6">
      <header>
        <h1 class="flex items-center gap-2 text-title font-semibold text-ink"><AppIcon name="inbox" size="lg" />{{ $t('memory.title') }}</h1>
        <p class="text-sm text-ink-muted">{{ $t('memory.intro') }}</p>
      </header>

      <InlineError v-if="memory.error.value" box>{{ memory.error.value }}</InlineError>

      <section v-if="view && !view.enabled" class="rounded-xl border border-dashed border-line px-4 py-8 text-center" data-testid="memory-off">
        <p class="font-medium text-ink">{{ $t('memory.offTitle') }}</p>
        <p class="mt-1 text-sm text-ink-muted">{{ $t('memory.offBody') }}</p>
        <RouterLink :to="{ path: '/settings', hash: '#assistant' }" class="mt-3 inline-block text-sm text-accent underline-offset-2 hover:underline" data-testid="memory-off-link">
          {{ $t('memory.offAction') }}
        </RouterLink>
      </section>

      <template v-else-if="view">
        <section :aria-label="$t('memory.folderLabel')" class="flex flex-col gap-2" data-testid="memory-header">
          <p class="break-all font-mono text-sm text-ink" data-testid="memory-folder-path">{{ view.folder }}</p>
          <p v-if="!view.exists" class="text-xs text-ink-muted">{{ $t('memory.missing') }}</p>
          <div class="flex flex-wrap items-center gap-2">
            <AppButton variant="outline" :disabled="memory.busy.value || !view.exists" data-testid="memory-reveal" @click="memory.reveal()">{{ $t('memory.reveal') }}</AppButton>
            <AppButton variant="outline" :disabled="memory.busy.value" data-testid="memory-tidy" @click="tidy">{{ $t('memory.tidy') }}</AppButton>
          </div>
          <p class="text-xs text-ink-muted" role="status" data-testid="memory-tidied">
            <template v-if="memory.tidyStarted.value">{{ $t('memory.tidyStarted') }} </template>
            {{ view.tidiedAt ? $t('memory.tidiedAt', { date: formatReceived(view.tidiedAt) }) : $t('memory.neverTidied') }}
          </p>
        </section>

        <section aria-labelledby="memory-review-title">
          <h2 id="memory-review-title" class="text-2xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('memory.review.title') }}</h2>
          <p class="mt-1 text-sm text-ink-muted">{{ $t('memory.review.intro') }}</p>
          <div v-if="view.review.length === 0" class="mt-3 rounded-xl border border-dashed border-line px-4 py-6 text-center" data-testid="memory-review-empty">
            <p class="font-medium text-ink">{{ $t('memory.review.emptyTitle') }}</p>
            <p class="mt-1 text-sm text-ink-muted">{{ $t('memory.review.emptyBody') }}</p>
          </div>
          <ul v-else class="mt-2 flex flex-col divide-y divide-line border-y border-line" :aria-label="$t('memory.review.listLabel')">
            <li v-for="item in view.review" :key="item.note + item.line" class="flex flex-col gap-2 py-3 min-[560px]:flex-row min-[560px]:items-center min-[560px]:justify-between" data-testid="memory-review-item">
              <div class="min-w-0">
                <p class="text-sm text-ink">{{ item.fact }}</p>
                <p class="text-xs text-ink-muted">{{ $t('memory.review.from', { note: item.noteTitle, date: formatPlainDay(item.date) }) }}</p>
              </div>
              <div class="flex shrink-0 gap-2">
                <AppButton variant="primary" :disabled="memory.busy.value" :aria-label="$t('memory.review.keepLabel', { fact: item.fact })" data-testid="memory-keep" @click="memory.review(item.note, item.line, 'keep')">{{ $t('memory.review.keep') }}</AppButton>
                <AppButton variant="outline" :disabled="memory.busy.value" :aria-label="$t('memory.review.dropLabel', { fact: item.fact })" data-testid="memory-drop" @click="memory.review(item.note, item.line, 'drop')">{{ $t('memory.review.drop') }}</AppButton>
              </div>
            </li>
          </ul>
        </section>

        <section aria-labelledby="memory-notes-title">
          <h2 id="memory-notes-title" class="text-2xs font-medium uppercase tracking-wider text-ink-dim">{{ $t('memory.notes.title') }}</h2>
          <div v-if="view.notes.length === 0" class="mt-3 rounded-xl border border-dashed border-line px-4 py-6 text-center" data-testid="memory-notes-empty">
            <p class="font-medium text-ink">{{ $t('memory.notes.emptyTitle') }}</p>
            <p class="mt-1 text-sm text-ink-muted">{{ $t('memory.notes.emptyBody') }}</p>
          </div>
          <ul v-else class="mt-2 flex flex-col divide-y divide-line border-y border-line" :aria-label="$t('memory.notes.listLabel')">
            <li v-for="note in view.notes" :key="note.path" class="flex items-baseline justify-between gap-3 py-2.5" data-testid="memory-note">
              <div class="min-w-0">
                <p class="text-sm text-ink">{{ note.title }}</p>
                <p class="break-all font-mono text-xs text-ink-muted">{{ note.path }}</p>
              </div>
              <span class="shrink-0 text-xs text-ink-muted">{{ $t('memory.notes.facts', { count: note.facts }, note.facts) }}</span>
            </li>
          </ul>
        </section>
      </template>
    </div>
  </main>
</template>
