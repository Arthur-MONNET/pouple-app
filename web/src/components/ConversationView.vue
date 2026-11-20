<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, onMounted, onUnmounted, ref, watch } from 'vue';
import type { AssistantRequest } from '../api/types';
import AssistantRequestBlock from './AssistantRequestBlock.vue';
import ConversationComposer from './ConversationComposer.vue';
import ConversationHeader from './ConversationHeader.vue';
import DecisionDock from './DecisionDock.vue';
import { hasFiles } from '../lib/dropFiles';
import { pendingDecision } from '../lib/requestDecisions';

const ConversationTerminal = defineAsyncComponent(() => import('./ConversationTerminal.vue'));
const terminalAvailable = window.__POUPLE__?.terminal === true;

const props = defineProps<{ request: AssistantRequest }>();
defineEmits<{ removed: [] }>();

const NEAR_BOTTOM_PX = 140;
const composer = ref<InstanceType<typeof ConversationComposer> | null>(null);
/** A file is being dragged over the conversation. A counter, since entering a child fires dragenter before dragleave. */
const dragDepth = ref(0);
const interactiveTerminal = ref(false);
/** A question, a plan or a change waiting for the user takes the message field's place. */
const deciding = computed(() => props.request.status === 'awaiting-user' && pendingDecision(props.request) !== null);

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
    <ConversationHeader :request="request" @removed="$emit('removed')" />
    <!-- A short thread sits at the bottom, against the dock: the last message stays next to what answers it. -->
    <div ref="scroller" class="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4" @scroll.passive="onScroll">
      <div class="mx-auto mt-auto w-full max-w-3xl">
        <AssistantRequestBlock :key="request.id" :request="request" :interactive-terminal="interactiveTerminal" />
      </div>
    </div>
    <ConversationTerminal v-if="terminalAvailable" :key="request.id" :request-id="request.id" :available-height="terminalSpace" @interactive="interactiveTerminal = $event" />
    <div ref="composerBar" class="shrink-0 border-t border-line-soft">
      <!-- Keyed by the conversation: a draft, a file or a slow send never moves to another one. -->
      <div class="mx-auto w-full max-w-3xl">
        <DecisionDock v-if="deciding" :key="`dock-${request.id}`" :request="request" />
        <ConversationComposer v-else :key="request.id" ref="composer" :request="request" />
      </div>
    </div>
  </div>
</template>
