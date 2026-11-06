<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue';
import type { AssistantRequest } from '../api/types';
import AssistantRequestBlock from './AssistantRequestBlock.vue';
import ConversationComposer from './ConversationComposer.vue';

const props = defineProps<{ request: AssistantRequest }>();
defineEmits<{ removed: [] }>();

const NEAR_BOTTOM_PX = 140;
const scroller = ref<HTMLElement | null>(null);
let followBottom = true;

function onScroll(): void {
  const element = scroller.value;
  if (element) followBottom = element.scrollHeight - element.scrollTop - element.clientHeight < NEAR_BOTTOM_PX;
}

async function scrollToBottom(): Promise<void> {
  await nextTick();
  scroller.value?.scrollTo({ top: scroller.value.scrollHeight });
}

// New activity keeps the view at the bottom, unless the user scrolled up to read something.
watch(
  () => [props.request.entries.length, props.request.status],
  () => followBottom && void scrollToBottom(),
);
watch(
  () => props.request.id,
  () => {
    followBottom = true;
    void scrollToBottom();
  },
);
onMounted(scrollToBottom);
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto px-4 py-3" @scroll.passive="onScroll">
      <div class="mx-auto max-w-2xl">
        <AssistantRequestBlock :key="request.id" :request="request" @removed="$emit('removed')" />
      </div>
    </div>
    <div class="border-t border-line-soft">
      <div class="mx-auto w-full max-w-2xl"><ConversationComposer :request="request" /></div>
    </div>
  </div>
</template>
