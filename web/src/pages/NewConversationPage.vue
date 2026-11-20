<script setup lang="ts">
import { useRouter } from 'vue-router';
import NewConversationComposer from '../components/NewConversationComposer.vue';

const router = useRouter();

/** The new conversation replaces this screen in the history: going back does not reopen an empty composer. */
const open = (id: string): void => void router.replace({ name: 'conversation', params: { id } });
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto">
    <div class="mx-auto flex max-w-2xl flex-col gap-6 px-5 py-8">
      <header class="flex flex-col gap-1">
        <h1 class="font-display text-2xl font-semibold text-ink">{{ $t('newConversation.title') }}</h1>
        <p class="text-sm text-ink-muted">{{ $t('newConversation.intro') }}</p>
      </header>
      <NewConversationComposer @created="open" />
    </div>
  </main>
</template>
