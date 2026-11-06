<script setup lang="ts">
import AppButton from './AppButton.vue';
import PouMascot from './PouMascot.vue';

defineEmits<{ open: [] }>();

const NEXT = [
  { title: 'Bubbles in a corner', body: 'When something needs you, a small bubble appears in a corner of your screen with what Codex found.' },
  { title: 'Conversations in this window', body: 'Open any bubble to read the whole investigation, answer questions, or ask for more detail.' },
  { title: 'You stay in control', body: 'Codex only reads. It changes code only after you approve a proposal, and it never posts in Slack for you.' },
];
</script>

<template>
  <div class="mx-auto flex max-w-2xl flex-col items-center gap-6 py-8 text-center min-[700px]:py-12">
    <div class="flex h-48 w-full max-w-sm items-center justify-center rounded-3xl bg-lavender"><PouMascot pose="done" surface="lavender" class="h-40" /></div>
    <div class="flex flex-col gap-2">
      <h1 class="font-display text-4xl font-semibold text-ink">You&rsquo;re all set</h1>
      <p class="text-base text-ink-muted">Slack and Codex are connected. Here is what happens next.</p>
    </div>
    <ul class="grid w-full gap-3 text-left min-[700px]:grid-cols-3">
      <li v-for="item in NEXT" :key="item.title" class="rounded-xl border border-line bg-surface p-4">
        <h2 class="mb-1 text-sm font-medium text-ink">{{ item.title }}</h2>
        <p class="text-xs leading-relaxed text-ink-muted">{{ item.body }}</p>
      </li>
    </ul>
    <AppButton variant="primary" size="lg" class="px-6" data-testid="open-main" @click="$emit('open')">Open Pouple</AppButton>
  </div>
</template>
