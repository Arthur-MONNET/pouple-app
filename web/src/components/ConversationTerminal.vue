<script setup lang="ts">
import { nextTick, onUnmounted, ref } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import AppButton from './AppButton.vue';
import InlineError from './InlineError.vue';
import { ApiError, errorMessage } from '../api/client';
import { terminalRequest, type TerminalState } from '../api/terminal';

const props = defineProps<{ requestId: string }>();
const state = ref<TerminalState>();
const error = ref('');
const opening = ref(false);
const terminalHost = ref<HTMLElement>();
const commandHost = ref<HTMLElement>();
const commands = ref('');
const showCommands = ref(false);
const lifetime = new AbortController();
let terminal: Terminal | undefined;
let observer: ResizeObserver | undefined;
let timer: ReturnType<typeof setTimeout> | undefined;
let queue = Promise.resolve();

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
    observer = new ResizeObserver(() => fit.fit());
    observer.observe(terminalHost.value);
    receive(first);
    fit.fit();
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

onUnmounted(() => {
  lifetime.abort();
  reset();
});
</script>

<template>
  <section class="shrink-0 border-t border-line-soft bg-surface px-3 py-1.5" :aria-label="$t('terminal.title')">
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
    <div v-if="state" class="relative mt-1 h-48 overflow-hidden rounded bg-[#101014] p-1">
      <div ref="terminalHost" class="h-full" :class="{ invisible: showCommands }" />
      <pre v-if="showCommands" ref="commandHost" class="absolute inset-0 overflow-auto whitespace-pre-wrap p-2 font-mono text-xs text-ink">{{ commands || $t('terminal.noCommands') }}</pre>
    </div>
  </section>
</template>
