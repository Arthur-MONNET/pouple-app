<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { MemoryOffer, MemoryPick } from '../api/types';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';

const props = withDefaults(defineProps<{ title: string; offer: MemoryOffer; busy?: boolean }>(), { busy: false });
const emit = defineEmits<{ confirm: [items: MemoryPick[]]; skip: [] }>();

/** What the user did so far: the facts they ticked, and which of those they share. */
const ticked = ref<string[]>([]);
const shared = ref<string[]>([]);

// A new suggestion starts over: the facts the assistant suggested are ticked, and every fact is private.
watch(
  () => props.offer.id,
  () => {
    ticked.value = props.offer.items.filter((item) => item.ticked).map((item) => item.id);
    shared.value = [];
  },
  { immediate: true },
);

const isTicked = (id: string): boolean => ticked.value.includes(id);
const isShared = (id: string): boolean => shared.value.includes(id);

/** The ticked facts, in the order they are listed. */
const chosen = computed(() => props.offer.items.filter((item) => isTicked(item.id)));
const canConfirm = computed(() => chosen.value.length > 0 && !props.busy);

function toggle(id: string): void {
  if (props.busy) return;
  ticked.value = isTicked(id) ? ticked.value.filter((item) => item !== id) : [...ticked.value, id];
}

function toggleShared(id: string): void {
  if (props.busy) return;
  shared.value = isShared(id) ? shared.value.filter((item) => item !== id) : [...shared.value, id];
}

const confirm = (): void => {
  if (canConfirm.value) emit('confirm', chosen.value.map((item) => (isShared(item.id) ? { id: item.id, shared: true } : { id: item.id })));
};
</script>

<template>
  <section class="flex flex-col gap-2" data-testid="memory-offer">
    <span class="w-fit max-w-full truncate rounded-lg bg-raised px-3 py-1.5 text-xs text-ink">{{ title }}</span>
    <div class="rounded-xl border border-line bg-surface p-3">
      <p class="mb-1 flex items-center gap-1.5 font-medium text-ink">
        <AppIcon name="pencil" />
        {{ $t('chat.memories.question') }}
      </p>
      <p class="mb-3 text-xs text-ink-muted">{{ $t('chat.memories.hint') }}</p>
      <ul class="flex flex-col gap-1.5" role="group" :aria-label="$t('chat.memories.title')">
        <li
          v-for="item in offer.items"
          :key="item.id"
          class="flex flex-wrap items-center gap-2 rounded-lg border px-2.5 py-2 transition motion-reduce:transition-none"
          :class="{ 'border-accent bg-selected': isTicked(item.id), 'border-line': !isTicked(item.id) }"
          data-testid="memory-item"
        >
          <button
            type="button"
            role="checkbox"
            :aria-checked="isTicked(item.id)"
            :aria-label="$t('chat.memories.pickLabel', { fact: item.fact })"
            class="flex min-w-0 flex-1 items-start gap-2.5 rounded-md text-left"
            @click="toggle(item.id)"
          >
            <span
              class="mt-px flex size-5 shrink-0 items-center justify-center rounded border border-line-strong text-ink"
              :class="{ 'border-accent bg-accent text-canvas': isTicked(item.id) }"
            >
              <AppIcon v-if="isTicked(item.id)" name="check" size="sm" />
            </span>
            <span class="min-w-0">
              <span class="block break-words text-ink">{{ item.fact }}</span>
              <span class="block break-words text-xs text-ink-muted">
                {{ item.noteTitle }}
                <span v-if="!item.note" class="ml-1 rounded bg-raised px-1.5 py-0.5 text-ink-dim" data-testid="memory-new-note">{{ $t('chat.memories.newNote') }}</span>
              </span>
              <span v-if="item.replaces" class="block break-words text-xs text-ink-muted">{{ $t('chat.memories.replaces', { fact: item.replaces }) }}</span>
            </span>
          </button>
          <button
            v-if="isTicked(item.id)"
            type="button"
            role="switch"
            :aria-checked="isShared(item.id)"
            :aria-label="$t('chat.memories.sharedLabel', { fact: item.fact })"
            class="h-8 rounded-md border px-2.5 text-xs transition motion-reduce:transition-none"
            :class="{ 'border-accent bg-accent text-canvas': isShared(item.id), 'border-line-strong text-ink-muted': !isShared(item.id) }"
            data-testid="memory-shared"
            @click="toggleShared(item.id)"
          >
            {{ isShared(item.id) ? $t('chat.memories.shared') : $t('chat.memories.private') }}
          </button>
        </li>
      </ul>
      <p class="mt-2 text-xs text-ink-dim">{{ $t('chat.memories.privacy') }}</p>
      <div class="mt-3 flex items-center gap-2">
        <AppButton variant="ghost" size="sm" :disabled="busy" data-testid="memories-skip" @click="emit('skip')">{{ $t('chat.memories.skip') }}</AppButton>
        <AppButton variant="primary" size="sm" class="ml-auto" :disabled="!canConfirm" data-testid="memories-confirm" @click="confirm">
          {{ chosen.length === 0 ? $t('chat.memories.confirmNone') : $t('chat.memories.confirm', { n: chosen.length }, chosen.length) }}
        </AppButton>
      </div>
    </div>
  </section>
</template>
