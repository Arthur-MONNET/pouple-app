<script setup lang="ts">
import AppButton from './AppButton.vue';
import PouMascot from './PouMascot.vue';

defineEmits<{ open: [] }>();

const NEXT = [
  { title: 'done.bubblesTitle', body: 'done.bubblesBody' },
  { title: 'done.windowTitle', body: 'done.windowBody' },
  { title: 'done.controlTitle', body: 'done.controlBody' },
] as const;
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col items-center gap-6 py-8 text-center min-[700px]:py-12">
    <div class="flex h-48 w-full max-w-sm items-center justify-center rounded-3xl bg-lavender"><PouMascot pose="done" class="h-40" /></div>
    <div class="flex flex-col gap-2">
      <h1 class="font-display text-4xl font-semibold text-ink">{{ $t('done.title') }}</h1>
      <p class="text-base text-ink-muted">{{ $t('done.subtitle') }}</p>
    </div>
    <ul class="grid w-full gap-3 text-left min-[700px]:grid-cols-3">
      <li v-for="item in NEXT" :key="item.title" class="rounded-xl border border-line bg-surface p-4">
        <h2 class="mb-1 text-sm font-medium text-ink">{{ $t(item.title) }}</h2>
        <p class="text-xs leading-relaxed text-ink-muted">{{ $t(item.body) }}</p>
      </li>
    </ul>
    <AppButton variant="primary" size="lg" class="px-6" data-testid="open-main" @click="$emit('open')">{{ $t('done.open') }}</AppButton>
  </div>
</template>
