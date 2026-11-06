<script setup lang="ts">
import { computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import AppIcon from '../components/AppIcon.vue';
import ConversationList from '../components/ConversationList.vue';
import InlineError from '../components/InlineError.vue';
import MascotStage from '../components/MascotStage.vue';
import ConversationView from '../components/ConversationView.vue';
import { provideAssistantStore } from '../composables/assistantStore';
import { useApi } from '../composables/useApi';

const route = useRoute();
const router = useRouter();
const store = provideAssistantStore(useApi());

const selectedId = computed(() => (typeof route.params['id'] === 'string' ? route.params['id'] : null));
const selected = computed(() => store.byId(selectedId.value));
const status = computed(() => store.status.value);
const usage = computed(() => status.value?.usage ?? null);

const select = (id: string): void => void router.push({ name: 'conversation', params: { id } });

// Land on the conversation that most needs the user, or the latest one; a deleted selection falls back the same way.
watch(
  () => [store.loaded.value, selected.value === null, store.requests.value.length] as const,
  ([loaded, missing]) => {
    if (!loaded || !missing) return;
    const target = store.requests.value.find((r) => r.status === 'awaiting-user') ?? store.requests.value[0];
    if (target) void router.replace({ name: 'conversation', params: { id: target.id } });
    else if (selectedId.value) void router.replace({ name: 'home' });
  },
  { immediate: true },
);
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div v-if="status?.problem || store.error.value || (status?.notices.length ?? 0) > 0" class="flex items-start gap-3 border-b border-line-soft bg-surface px-4 py-2 text-xs" aria-live="polite" data-testid="status-banner">
      <MascotStage v-if="status?.problem || store.error.value" pose="oops" size="sm" />
      <div class="flex min-w-0 flex-col justify-center gap-1 self-center">
        <InlineError v-if="store.error.value">Pouple&rsquo;s core is not answering: {{ store.error.value }}</InlineError>
        <InlineError v-if="status?.problem && status.active">Slack check failed: {{ status.problem }}</InlineError>
        <p v-else-if="status?.problem" role="status" class="text-ink-muted">Pouple is paused: {{ status.problem }}</p>
        <p v-for="notice in status?.notices ?? []" :key="notice" class="text-ink-muted">{{ notice }}</p>
      </div>
    </div>

    <div class="flex min-h-0 flex-1">
      <aside
        class="flex w-72 shrink-0 flex-col border-r border-line-soft bg-canvas max-[699px]:w-full max-[699px]:border-r-0"
        :class="{ 'max-[699px]:hidden': selected }"
        aria-label="Conversations"
      >
        <ConversationList :requests="store.requests.value" :selected-id="selectedId" :attention="store.attention.value" :loaded="store.loaded.value" @select="select" />
        <p v-if="usage" class="border-t border-line-soft px-4 py-2 font-mono text-2xs text-ink-dim" :title="usage.limit === null ? 'Codex turns used today (no limit)' : 'Codex turns used today, out of the daily limit'">
          Codex today: {{ usage.today }}{{ usage.limit === null ? '' : ` / ${usage.limit}` }}
        </p>
      </aside>

      <main class="flex min-w-0 flex-1 flex-col bg-canvas" :class="{ 'max-[699px]:hidden': !selected }">
        <button
          v-if="selected"
          type="button"
          class="hidden items-center gap-1.5 border-b border-line-soft px-4 py-2 text-xs text-ink-muted hover:text-ink max-[699px]:flex"
          @click="router.push({ name: 'home' })"
        >
          <AppIcon name="chevron-right" size="sm" class="rotate-180" />
          All conversations
        </button>
        <ConversationView v-if="selected" :request="selected" @removed="router.replace({ name: 'home' })" />
        <div v-else class="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center text-ink-dim">
          <template v-if="store.loaded.value">
            <MascotStage pose="sleep" size="md" class="!size-36" />
            <p class="font-display text-xl font-semibold text-ink">{{ store.requests.value.length === 0 ? 'Nothing to do. Pouple is napping.' : 'Pick a conversation.' }}</p>
            <p class="max-w-sm text-sm text-ink-muted">{{ store.requests.value.length === 0 ? 'The next request that would notify you shows up here.' : 'Or wait for the next request.' }}</p>
          </template>
          <p v-else role="status">Loading…</p>
        </div>
      </main>
    </div>
  </div>
</template>
