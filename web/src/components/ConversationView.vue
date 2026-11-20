<script setup lang="ts">
import { defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
import type { AssistantRequest } from '../api/types';
import AssistantRequestBlock from './AssistantRequestBlock.vue';
import ConversationComposer from './ConversationComposer.vue';
import { hasFiles } from '../lib/dropFiles';

const ConversationTerminal = defineAsyncComponent(() => import('./ConversationTerminal.vue'));
const terminalAvailable = window.__POUPLE__?.terminal === true;

const props = defineProps<{ request: AssistantRequest }>();
defineEmits<{ removed: [] }>();

const NEAR_BOTTOM_PX = 140;
const composer = ref<InstanceType<typeof ConversationComposer> | null>(null);
/** A file is being dragged over the conversation. A counter, since entering a child fires dragenter before dragleave. */
const dragDepth = ref(0);
const interactiveTerminal = ref(false);

function onDragEnter(event: DragEvent): void {
  if (hasFiles(event)) dragDepth.value += 1;
}

function onDragLeave(event: DragEvent): void {
  if (hasFiles(event)) dragDepth.value = Math.max(0, dragDepth.value - 1);
}

function onDrop(event: DragEvent): void {
  dragDepth.value = 0;
  if (hasFiles(event)) void composer.value?.addFiles([...(event.dataTransfer?.files ?? [])]);
}
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
    interactiveTerminal.value = false;
    void scrollToBottom();
  },
);
const layout = ref<HTMLElement>();
const composerBar = ref<HTMLElement>();
const terminalSpace = ref(0);
let layoutObserver: ResizeObserver | undefined;

onMounted(() => {
  void scrollToBottom();
  const container = layout.value;
  const footer = composerBar.value;
  if (!terminalAvailable || !container || !footer) return;

  const measure = () => {
    const space = Math.max(0, container.clientHeight - footer.offsetHeight);
    terminalSpace.value = space - Math.min(96, Math.floor(space / 4));
  };

  measure();
  layoutObserver = new ResizeObserver(measure);
  layoutObserver.observe(container);
  layoutObserver.observe(footer);
});
onUnmounted(() => layoutObserver?.disconnect());
</script>

<template>
  <div
    ref="layout"
    class="relative flex min-h-0 flex-1 flex-col"
    data-testid="conversation-drop"
    @dragenter.prevent="onDragEnter"
    @dragover.prevent
    @dragleave="onDragLeave"
    @drop.prevent="onDrop"
  >
    <div
      v-if="dragDepth > 0"
      class="pointer-events-none absolute inset-2 z-10 flex items-center justify-center rounded-2xl border-2 border-dashed border-accent bg-canvas/80 text-sm font-medium text-ink"
      role="status"
      data-testid="drop-hint"
    >
      {{ $t('composer.dropHint') }}
    </div>
    <div ref="scroller" class="min-h-0 flex-1 overflow-y-auto px-4 py-3" @scroll.passive="onScroll">
      <div class="mx-auto max-w-2xl">
        <AssistantRequestBlock :key="request.id" :request="request" :interactive-terminal="interactiveTerminal" @removed="$emit('removed')" />
      </div>
    </div>
    <ConversationTerminal v-if="terminalAvailable" :key="request.id" :request-id="request.id" :available-height="terminalSpace" @interactive="interactiveTerminal = $event" />
    <div ref="composerBar" class="shrink-0 border-t border-line-soft">
      <div class="mx-auto w-full max-w-2xl"><ConversationComposer ref="composer" :request="request" /></div>
    </div>
  </div>
</template>
