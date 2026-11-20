<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, useId } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import AppButton from './AppButton.vue';
import InlineError from './InlineError.vue';
import { ApiError, errorMessage } from '../api/client';
import { terminalRequest, type TerminalState } from '../api/terminal';

const props = defineProps<{ requestId: string; availableHeight: number }>();
const state = ref<TerminalState>();
const error = ref('');
const opening = ref(false);
const terminalHost = ref<HTMLElement>();
const commandHost = ref<HTMLElement>();
const commands = ref('');
const showCommands = ref(false);
const chrome = ref<HTMLElement>();
const viewportId = useId();
const preferredHeight = ref(192);
const chromeHeight = ref(0);
const maxHeight = computed(() => Math.max(0, props.availableHeight - chromeHeight.value));
const minHeight = computed(() => Math.min(72, maxHeight.value));
const viewportHeight = computed(() => clampHeight(preferredHeight.value));
let chromeObserver: ResizeObserver | undefined;
let drag: { pointer: number; y: number; height: number } | undefined;
const lifetime = new AbortController();
let terminal: Terminal | undefined;
let observer: ResizeObserver | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;
let queue = Promise.resolve();

function clampHeight(height: number): number {
  return Math.min(maxHeight.value, Math.max(minHeight.value, Math.round(height)));
}

function startResize(event: PointerEvent): void {
  if (!event.isPrimary || event.button !== 0 || minHeight.value === maxHeight.value) return;
  const handle = event.currentTarget as HTMLElement;
  handle.focus();
  handle.setPointerCapture(event.pointerId);
  drag = { pointer: event.pointerId, y: event.clientY, height: viewportHeight.value };
  event.preventDefault();
}

function moveResize(event: PointerEvent): void {
  if (drag?.pointer === event.pointerId) {
    preferredHeight.value = clampHeight(drag.height + drag.y - event.clientY);
  }
}

function endResize(event: PointerEvent): void {
  if (drag?.pointer === event.pointerId) drag = undefined;
}

function resizeWithKeyboard(event: KeyboardEvent): void {
  let height: number;
  switch (event.key) {
    case 'ArrowUp': height = viewportHeight.value + 16; break;
    case 'ArrowDown': height = viewportHeight.value - 16; break;
    case 'Home': height = minHeight.value; break;
    case 'End': height = maxHeight.value; break;
    default: return;
  }
  event.preventDefault();
  if (minHeight.value !== maxHeight.value) preferredHeight.value = clampHeight(height);
}

function receive(next: TerminalState): void {
  // Track cursors and ownership independently of the output bytes.
  const { output, commands: delta, ...metadata } = next;
  state.value = { ...metadata, output: null, commands: '' };
  if (output) terminal?.write(Uint8Array.from(atob(output), (byte) => byte.charCodeAt(0)));
  if (delta) {
    const host = commandHost.value;
    const follow = !host || host.scrollHeight - host.scrollTop - host.clientHeight < 40;
    commands.value = (commands.value + delta).slice(-256 * 1024);
    if (follow) void nextTick(() => commandHost.value?.scrollTo({ top: commandHost.value.scrollHeight }));
  }
  if (terminal) terminal.options.disableStdin = !next.user || next.shellExited;
}

function reset(): void {
  drag = undefined;
  clearTimeout(timer);
  observer?.disconnect();
  terminal?.dispose();
  terminal = undefined;
  state.value = undefined;
  commands.value = '';
  showCommands.value = false;
}

function send(action: string, extra: object = {}): Promise<void> {
  const session = state.value?.id;
  const revision = state.value?.revision;
  queue = queue.then(async () => {
    const current = state.value;
    if (lifetime.signal.aborted || !current || session !== current.id) return;
    try {
      const next = await terminalRequest(props.requestId, {
        action, session, revision, offset: current.offset, commandOffset: current.commandOffset, ...extra,
      }, lifetime.signal);
      if (lifetime.signal.aborted) return;
      if (action !== 'read') error.value = '';
      if (action === 'close') reset();
      else receive(next);
      if (action === 'release') {
        showCommands.value = true;
        void nextTick(() => commandHost.value?.scrollTo({ top: commandHost.value.scrollHeight }));
      }
      if (action === 'take') {
        showCommands.value = false;
        void nextTick(() => terminal?.focus());
      }
    } catch (cause) {
      if (lifetime.signal.aborted) return;
      if (cause instanceof ApiError && cause.status === 404) reset();
      error.value = errorMessage(cause);
    }
  });
  return queue;
}

async function poll(): Promise<void> {
  await send('read');
  if (state.value && !lifetime.signal.aborted) timer = setTimeout(() => void poll(), 200);
}

async function open(): Promise<void> {
  if (opening.value) return;
  opening.value = true;
  error.value = '';
  try {
    const first = await terminalRequest(props.requestId, { action: 'open' }, lifetime.signal);
    if (lifetime.signal.aborted) return;
    state.value = first;
    showCommands.value = !first.user;
    await nextTick();
    if (!terminalHost.value || lifetime.signal.aborted) return;
    terminal = new Terminal({
      cursorBlink: true,
      fontSize: 12,
      fontFamily: 'ui-monospace, monospace',
      theme: { background: '#101014', foreground: '#dedee3' },
      scrollback: 2000,
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(terminalHost.value);
    terminal.onData((data) => {
      // Split large pastes on Unicode character boundaries.
      const characters = Array.from(data);
      for (let offset = 0; offset < characters.length; offset += 4096) {
        void send('input', { data: characters.slice(offset, offset + 4096).join('') });
      }
    });
    terminal.onResize(({ rows, cols }) => void send('resize', { rows, cols }));
    observer = new ResizeObserver(() => {
      if (terminalHost.value?.clientHeight) fit.fit();
    });
    observer.observe(terminalHost.value);
    receive(first);
    if (terminalHost.value.clientHeight) fit.fit();
    void send('resize', { rows: terminal.rows, cols: terminal.cols });
    if (first.user) terminal.focus();
    void poll();
  } catch (cause) {
    if (!lifetime.signal.aborted) {
      reset();
      error.value = errorMessage(cause);
    }
  } finally {
    opening.value = false;
  }
}

function switchView(): void {
  showCommands.value = !showCommands.value;
  if (showCommands.value) void nextTick(() => commandHost.value?.scrollTo({ top: commandHost.value.scrollHeight }));
  else if (state.value?.user) void nextTick(() => terminal?.focus());
}

onMounted(() => {
  const controls = chrome.value;
  if (!controls) return;
  const measure = () => { chromeHeight.value = controls.offsetHeight; };
  measure();
  chromeObserver = new ResizeObserver(measure);
  chromeObserver.observe(controls);
});

onUnmounted(() => {
  chromeObserver?.disconnect();
  lifetime.abort();
  reset();
});
</script>

<template>
  <section class="shrink-0 bg-surface" :aria-label="$t('terminal.title')">
    <div ref="chrome" class="border-t border-line-soft">
      <div
        v-if="state"
        role="separator"
        tabindex="0"
        class="flex h-4 cursor-row-resize touch-none select-none items-center justify-center hover:bg-canvas focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-accent"
        :aria-label="$t('terminal.resize')"
        aria-orientation="horizontal"
        :aria-controls="viewportId"
        :aria-valuemin="minHeight"
        :aria-valuemax="maxHeight"
        :aria-valuenow="viewportHeight"
        :aria-disabled="minHeight === maxHeight"
        @pointerdown="startResize"
        @pointermove="moveResize"
        @pointerup="endResize"
        @pointercancel="endResize"
        @lostpointercapture="endResize"
        @keydown="resizeWithKeyboard"
      >
        <span class="h-1 w-10 rounded-full bg-ink-muted" />
      </div>
      <div class="px-3 py-1.5">
        <div class="flex flex-wrap items-center gap-2">
          <AppButton v-if="!state" variant="outline" size="sm" :disabled="opening" @click="open">{{ $t('terminal.open') }}</AppButton>
          <template v-else>
            <span class="text-xs text-ink-muted" role="status">{{ $t(state.waiting ? 'terminal.waiting' : state.busy ? 'terminal.busy' : state.shellExited ? 'terminal.exited' : state.user ? 'terminal.user' : 'terminal.assistant') }}</span>
            <AppButton v-if="state.user" variant="outline" size="sm" @click="send('release')">{{ $t('terminal.release') }}</AppButton>
            <AppButton v-else-if="!state.shellExited" variant="outline" size="sm" :disabled="state.busy" @click="send('take')">{{ $t('terminal.take') }}</AppButton>
            <AppButton v-if="state.user && !state.shellExited" variant="outline" size="sm" @click="send('interrupt')">{{ $t('terminal.interrupt') }}</AppButton>
            <AppButton v-if="state.busy || state.waiting" variant="outline" size="sm" @click="send('stop')">{{ $t('terminal.stop') }}</AppButton>
            <AppButton variant="outline" size="sm" @click="switchView">{{ $t(showCommands ? 'terminal.shell' : 'terminal.commands') }}</AppButton>
            <AppButton variant="outline" size="sm" @click="send('close')">{{ $t('terminal.close') }}</AppButton>
          </template>
        </div>
        <InlineError v-if="error">{{ error }}</InlineError>
      </div>
    </div>
    <div v-if="state" :id="viewportId" class="relative mx-3 overflow-hidden rounded bg-[#101014]" :style="{ height: `${viewportHeight}px` }">
      <div class="h-full p-1">
        <div ref="terminalHost" class="h-full" :class="{ invisible: showCommands }" />
      </div>
      <pre v-if="showCommands" ref="commandHost" class="absolute inset-0 overflow-auto whitespace-pre-wrap p-2 font-mono text-xs text-ink">{{ commands || $t('terminal.noCommands') }}</pre>
    </div>
  </section>
</template>
