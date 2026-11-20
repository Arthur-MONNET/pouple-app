<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, useId, watch } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import AppButton from './AppButton.vue';
import InlineError from './InlineError.vue';
import { ApiError, errorMessage } from '../api/client';
import { terminalRequest, type TerminalState } from '../api/terminal';

const props = defineProps<{ requestId: string; availableHeight: number }>();
const emit = defineEmits<{ interactive: [active: boolean] }>();
const sessions = ref<TerminalState[]>([]);
watch(() => sessions.value.some((session) => session.interactive), (active) => emit('interactive', active));
const selected = ref('');
const state = ref<TerminalState>();
const error = ref('');
const opening = ref(false);
const terminalHost = ref<HTMLElement>();
const commandHost = ref<HTMLElement>();
const commandInput = ref<HTMLInputElement>();
const commands = ref('');
const drafts = ref<Record<string, string>>({});
const showCommands = ref(false);
const visible = computed(() => sessions.value.length > 0);
const panel = ref<HTMLElement>();
const viewport = ref<HTMLElement>();
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

const folder = (dir: string): string => dir.split('/').filter(Boolean).at(-1) ?? '/';
const status = (session: TerminalState): string =>
  session.running ? 'terminal.running' : session.result ? `terminal.${session.result.status}` : 'terminal.idle';

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
  if (next.interactive && next.revision !== state.value?.revision) showCommands.value = false;
  state.value = { ...next, output: null, commands: '' };
  if (next.output) terminal?.write(Uint8Array.from(atob(next.output), (byte) => byte.charCodeAt(0)));
  if (next.commands) {
    const host = commandHost.value;
    const follow = host && host.scrollHeight - host.scrollTop - host.clientHeight < 40;
    commands.value = (commands.value + next.commands).slice(-256 * 1024);
    if (follow) void nextTick(() => host.scrollTo({ top: host.scrollHeight }));
  }
  if (terminal) terminal.options.disableStdin = !next.running;
}

function switchView(): void {
  showCommands.value = !showCommands.value;
  if (showCommands.value) void nextTick(() => commandHost.value?.scrollTo({ top: commandHost.value.scrollHeight }));
  else if (state.value?.running) void nextTick(() => terminal?.focus());
}

function schedule(job: () => Promise<void>): Promise<void> {
  queue = queue.then(async () => {
    if (lifetime.signal.aborted) return;
    try {
      await job();
    } catch (cause) {
      if (lifetime.signal.aborted) return;
      if (cause instanceof ApiError && cause.status === 404) {
        state.value = undefined;
        return;
      }
      error.value = errorMessage(cause);
    }
  });
  return queue;
}

async function read(session: string, action = 'read', extra: object = {}): Promise<TerminalState> {
  const current = state.value?.id === session ? state.value : undefined;
  return terminalRequest(props.requestId, {
    action, session, revision: current?.revision, offset: current?.offset, commandOffset: current?.commandOffset, ...extra,
  }, lifetime.signal);
}

function send(action: string, extra: object = {}): Promise<void> {
  const session = selected.value;
  const revision = state.value?.revision;
  return schedule(async () => {
    if (!session || state.value?.id !== session || selected.value !== session) return;
    const next = await read(session, action, { revision, ...extra });
    if (!lifetime.signal.aborted) {
      receive(next);
      error.value = '';
    }
  });
}

function disposeTerminal(): void {
  drag = undefined;
  observer?.disconnect();
  terminal?.dispose();
  terminal = undefined;
  state.value = undefined;
  commands.value = '';
  showCommands.value = false;
}

async function select(session: string): Promise<void> {
  if (session === selected.value && state.value?.id === session) return;
  disposeTerminal();
  selected.value = session;
  if (!session) return;
  await nextTick();
  if (!terminalHost.value || lifetime.signal.aborted) return;
  terminal = new Terminal({
    cursorBlink: true,
    disableStdin: true,
    fontSize: 12,
    fontFamily: 'ui-monospace, monospace',
    theme: { background: '#101014', foreground: '#dedee3' },
    scrollback: 2000,
  });
  const fit = new FitAddon();
  terminal.loadAddon(fit);
  terminal.open(terminalHost.value);
  terminal.onData((data) => {
    if (data === '\x03') {
      void send('interrupt');
      return;
    }
    const characters = Array.from(data);
    for (let offset = 0; offset < characters.length; offset += 4096) {
      void send('input', { data: characters.slice(offset, offset + 4096).join('') });
    }
  });
  terminal.onResize(({ rows, cols }) => void send('resize', { rows, cols }));
  const fitViewport = () => {
    if (terminalHost.value?.clientHeight) fit.fit();
  };
  observer = new ResizeObserver(fitViewport);
  observer.observe(terminalHost.value);
  const next = await read(session, 'select');
  if (lifetime.signal.aborted) return;
  receive(next);
  fitViewport();
  void send('resize', { rows: terminal.rows, cols: terminal.cols });
}

async function refresh(): Promise<void> {
  const next = await terminalRequest<TerminalState[]>(props.requestId, { action: 'list' }, lifetime.signal);
  if (lifetime.signal.aborted) return;
  const previous = sessions.value;
  sessions.value = next;
  const active = next.find((session) => session.id === selected.value);
  const interactive = next.find((session) => session.interactive);
  const justStarted = interactive && !previous.some((session) => session.id === interactive.id && session.interactive);
  if (justStarted && !active?.interactive) {
    await select(interactive.id);
  } else if (!active || !state.value) {
    await select(active?.id ?? next.find((session) => session.running)?.id ?? next[0]?.id ?? '');
  } else {
    const view = await read(active.id);
    if (!lifetime.signal.aborted) receive(view);
  }
}

async function poll(): Promise<void> {
  await schedule(refresh);
  if (!lifetime.signal.aborted) timer = setTimeout(() => void poll(), visible.value ? 250 : 600);
}

function open(): void {
  if (opening.value) return;
  opening.value = true;
  void schedule(async () => {
    const session = await terminalRequest(props.requestId, { action: 'open' }, lifetime.signal);
    if (lifetime.signal.aborted) return;
    sessions.value = [...sessions.value, session];
    await select(session.id);
    commandInput.value?.focus();
    error.value = '';
  }).finally(() => { opening.value = false; });
}

function close(session: string): void {
  void schedule(async () => {
    await read(session, 'close');
    if (lifetime.signal.aborted) return;
    delete drafts.value[session];
    await refresh();
  });
}

function submit(): void {
  const session = selected.value;
  const command = drafts.value[session]?.trim();
  if (!command || state.value?.running) return;
  drafts.value[session] = '';
  showCommands.value = false;
  void send('run', { data: command }).then(() => {
    if (selected.value !== session) return;
    if (error.value) drafts.value[session] ||= command;
    if (state.value?.running) terminal?.focus();
    else void nextTick(() => commandInput.value?.focus());
  });
}

onMounted(() => {
  const section = panel.value;
  if (section) {
    const measure = () => {
      chromeHeight.value = section.offsetHeight - (viewport.value?.offsetHeight ?? 0);
    };
    measure();
    chromeObserver = new ResizeObserver(measure);
    chromeObserver.observe(section);
  }
  void poll();
});
onUnmounted(() => {
  chromeObserver?.disconnect();
  lifetime.abort();
  clearTimeout(timer);
  disposeTerminal();
});
</script>

<template>
  <section ref="panel" class="shrink-0 border-t border-line-soft bg-surface px-3 py-1.5" :aria-label="$t('terminal.title')">
    <div
      v-if="visible"
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
    <div class="flex items-center gap-2">
      <div v-if="visible" class="flex min-w-0 gap-1 overflow-x-auto" role="group" :aria-label="$t('terminal.sessions')">
        <div
          v-for="session in sessions"
          :key="session.id"
          class="flex shrink-0 items-center rounded-md border border-line-soft"
          :class="{ 'bg-selected': session.id === selected }"
        >
          <button
            type="button"
            class="flex items-center gap-1.5 px-2 py-1 text-xs text-ink"
            :aria-pressed="session.id === selected"
            :title="`${session.dir} · ${$t(status(session), { code: session.result?.exitCode, signal: session.result?.signal })}`"
            @click="schedule(() => select(session.id))"
          >
            <span
              class="size-1.5 rounded-full"
              :class="session.running ? 'bg-accent motion-safe:animate-pulse' : session.result?.status === 'success' ? 'bg-ok' : session.result ? 'bg-fail' : 'bg-ink-dim'"
              aria-hidden="true"
            />
            {{ folder(session.dir) }}
          </button>
          <button type="button" class="px-1.5 text-xs text-ink-muted hover:text-ink" :aria-label="$t('terminal.closeSession', { folder: folder(session.dir) })" @click="close(session.id)">×</button>
        </div>
      </div>
      <AppButton variant="outline" size="sm" class="shrink-0" :disabled="opening" @click="open">{{ $t(visible ? 'terminal.new' : 'terminal.open') }}</AppButton>
    </div>
    <InlineError v-if="error">{{ error }}</InlineError>
    <div v-if="visible" class="mt-1 overflow-hidden rounded-md bg-[#101014] p-2">
      <div v-if="state" class="flex flex-wrap items-center gap-2 pb-1 text-xs text-[#dedee3]">
        <span class="break-all font-mono text-teal-300">{{ state.dir }}</span>
        <span v-if="state.branch" class="break-all font-mono text-violet-300">{{ state.branch }}</span>
        <span class="ml-auto" role="status">{{ $t(status(state), { code: state.result?.exitCode, signal: state.result?.signal }) }}</span>
        <AppButton v-if="state.running" variant="outline" size="sm" @click="send('interrupt')">{{ $t('terminal.interrupt') }}</AppButton>
        <AppButton variant="outline" size="sm" :aria-pressed="showCommands" @click="switchView">{{ $t(showCommands ? 'terminal.shell' : 'terminal.commands') }}</AppButton>
      </div>
      <div :id="viewportId" ref="viewport" class="relative" :style="{ height: `${viewportHeight}px` }">
        <div ref="terminalHost" class="h-full" :class="{ invisible: showCommands }" />
        <pre v-if="showCommands" ref="commandHost" class="absolute inset-0 overflow-auto whitespace-pre-wrap font-mono text-xs text-[#dedee3]">{{ commands || $t('terminal.noCommands') }}</pre>
      </div>
      <form v-if="state && !state.running" class="mt-1 flex items-center gap-2 border-t border-white/15 pt-1 font-mono text-xs text-[#dedee3]" @submit.prevent="submit">
        <span class="text-teal-300" aria-hidden="true">$</span>
        <input
          ref="commandInput"
          v-model="drafts[state.id]"
          class="min-w-0 flex-1 bg-transparent outline-none focus-visible:ring-1 focus-visible:ring-teal-300"
          :aria-label="$t('terminal.command')"
          :placeholder="$t('terminal.command')"
          autocomplete="off"
          spellcheck="false"
        />
      </form>
    </div>
  </section>
</template>
