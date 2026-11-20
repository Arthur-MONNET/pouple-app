<script setup lang="ts">
import { computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import ConversationView from '../components/ConversationView.vue';
import MascotStage from '../components/MascotStage.vue';
import { useAssistantStore } from '../composables/assistantStore';
import { leavesAfterRemoval } from '../lib/assistantRequests';

const route = useRoute();
const router = useRouter();
const store = useAssistantStore();

const selectedId = computed(() => (typeof route.params['id'] === 'string' ? route.params['id'] : null));
const selected = computed(() => store.byId(selectedId.value));

/** Leaves a conversation once it is deleted, unless the user already opened another one while it was being deleted. */
function onRemoved(id: string): void {
  if (leavesAfterRemoval(id, selectedId.value)) void router.replace({ name: 'home' });
}

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
  <main class="flex min-h-0 flex-1 flex-col">
    <ConversationView v-if="selected" :request="selected" @removed="onRemoved" />
    <div v-else class="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center text-ink-dim">
      <template v-if="store.loaded.value">
        <MascotStage pose="sleep" size="md" class="!size-36" />
        <p class="font-display text-xl font-semibold text-ink">{{ store.requests.value.length === 0 ? $t('home.emptyTitle') : $t('home.pickTitle') }}</p>
        <p class="max-w-sm text-sm text-ink-muted">{{ store.requests.value.length === 0 ? $t('home.emptyBody') : $t('home.pickBody') }}</p>
      </template>
      <p v-else role="status">{{ $t('common.loading') }}</p>
    </div>
  </main>
</template>
