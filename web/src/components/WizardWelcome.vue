<script setup lang="ts">
import AppButton from './AppButton.vue';
import BrandMark from './BrandMark.vue';
import PouMascot from './PouMascot.vue';

defineEmits<{ start: [] }>();

const STEPS = [
  { name: 'Connect Slack', note: 'one click, one token' },
  { name: 'Connect Codex', note: 'sign in with ChatGPT' },
];
</script>

<template>
  <div class="grid min-h-full grid-cols-1 min-[700px]:grid-cols-2">
    <div class="flex flex-col justify-between gap-8 px-6 py-8 min-[700px]:px-12 min-[700px]:py-12">
      <BrandMark size="lg" />

      <div class="flex flex-col gap-5">
        <h1 class="font-display text-4xl font-semibold leading-tight tracking-tight text-ink min-[900px]:text-5xl">Hi, I&rsquo;m Pouple.<br />Let&rsquo;s clear your Slack.</h1>
        <p class="max-w-md text-base leading-relaxed text-ink-muted">
          I watch the messages that would notify you, ask Codex to look into them, and keep the answer in a corner of your screen. You make the decisions.
        </p>
        <ol class="flex flex-col gap-2.5" aria-label="What we will set up">
          <li v-for="(step, index) in STEPS" :key="step.name" class="flex items-center gap-3 text-sm text-ink">
            <span class="flex size-6 items-center justify-center rounded-full border border-line-strong bg-raised text-xs font-semibold" aria-hidden="true">{{ index + 1 }}</span>
            {{ step.name }}
            <span class="text-ink-muted">&middot; {{ step.note }}</span>
          </li>
        </ol>
        <div class="mt-1 flex flex-wrap items-center gap-4">
          <AppButton variant="primary" size="lg" class="px-6 text-base" data-testid="get-started" @click="$emit('start')">Get started</AppButton>
          <span class="text-xs text-ink-muted">About three minutes. Secrets stay in your keychain.</span>
        </div>
      </div>

      <p class="font-mono text-xs tracking-wide text-ink-muted">Everything runs on your Mac.</p>
    </div>

    <div class="relative flex min-h-80 items-center justify-center overflow-hidden bg-lavender" aria-hidden="true">
      <div class="absolute size-[26rem] rounded-full bg-lavender-soft" />
      <PouMascot pose="multitask" class="relative w-4/5 max-w-md motion-safe:animate-rise" />
      <p class="absolute bottom-8 left-6 rounded-2xl rounded-bl-sm bg-white px-4 py-3 text-sm font-medium text-navy shadow-lg">Eight arms. Zero meetings.</p>
    </div>
  </div>
</template>
