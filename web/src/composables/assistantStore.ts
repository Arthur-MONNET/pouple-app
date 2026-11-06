import { computed, inject, onBeforeUnmount, onMounted, provide, ref, type InjectionKey } from 'vue';
import { ApiError, errorMessage, type PoupleApi } from '../api/client';
import type { AssistantRequest, AssistantSnapshot, ProposalDecision, QuestionAnswer } from '../api/types';
import { attentionCount, byRecentActivity } from '../lib/assistantRequests';

const RETRY_MS = [1000, 2000, 5000, 10_000];
const POLL_MS = 3000;

const sleep = (ms: number, signal: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal.addEventListener('abort', () => (clearTimeout(timer), resolve()), { once: true });
  });

/**
 * The conversations. The core pushes the full state over a stream; when the stream is unavailable (the endpoint is
 * missing, or it keeps failing) the store falls back to reading a snapshot every few seconds, so the window never
 * freezes. Actions are optimistic about nothing: they call the core, then trust the next pushed state.
 */
export function createAssistantStore(api: PoupleApi) {
  const snapshot = ref<AssistantSnapshot | null>(null);
  const error = ref<string | null>(null);
  const live = ref(false);
  /** Requests the user stopped: the conversation says so until something else happens to them. */
  const stopped = ref<string[]>([]);
  const forgetStopped = (id: string): void => void (stopped.value = stopped.value.filter((item) => item !== id));
  let abort: AbortController | null = null;

  /** Newest activity first, for the list. */
  const requests = computed<AssistantRequest[]>(() => byRecentActivity(snapshot.value?.requests ?? []));
  const status = computed(() => snapshot.value?.status ?? null);
  const attention = computed(() => attentionCount(requests.value));
  const loaded = computed(() => snapshot.value !== null);

  const byId = (id: string | null | undefined): AssistantRequest | null => (id ? (requests.value.find((r) => r.id === id) ?? null) : null);

  async function refresh(): Promise<void> {
    try {
      snapshot.value = await api.getSnapshot();
      error.value = null;
    } catch (cause) {
      error.value = errorMessage(cause);
    }
  }

  async function loop(signal: AbortSignal): Promise<void> {
    let failures = 0;
    let streamBroken = false;
    while (!signal.aborted) {
      if (!streamBroken) {
        try {
          await api.streamSnapshots((next) => {
            snapshot.value = next;
            error.value = null;
            live.value = true;
            failures = 0;
          }, signal);
        } catch (cause) {
          if (signal.aborted) return;
          error.value = errorMessage(cause);
          // No stream endpoint at all: stop trying and poll instead.
          if (cause instanceof ApiError && (cause.status === 404 || cause.status === 405)) streamBroken = true;
        }
        live.value = false;
        if (signal.aborted) return;
        if (!streamBroken) {
          failures += 1;
          await sleep(RETRY_MS[Math.min(failures - 1, RETRY_MS.length - 1)] ?? 10_000, signal);
          if (failures >= 3) await refresh();
        }
      } else {
        await refresh();
        await sleep(POLL_MS, signal);
      }
    }
  }

  function start(): void {
    if (abort) return;
    abort = new AbortController();
    void refresh();
    void loop(abort.signal);
  }

  function stop(): void {
    abort?.abort();
    abort = null;
    live.value = false;
  }

  /** Runs an action, then reads the state without waiting for the stream (it keeps the list honest if the stream lags). */
  async function act(action: () => Promise<unknown>): Promise<void> {
    try {
      await action();
    } finally {
      if (!live.value) await refresh();
    }
  }

  return {
    requests,
    status,
    attention,
    loaded,
    error,
    live,
    stopped,
    byId,
    refresh,
    start,
    stop,
    answer: (id: string, answers: QuestionAnswer[]) => (forgetStopped(id), act(() => api.answer(id, answers))),
    decide: (id: string, decision: ProposalDecision) => (forgetStopped(id), act(() => api.decideProposal(id, decision))),
    send: (id: string, text: string, attachmentIds: string[] = []) => (forgetStopped(id), act(() => api.sendMessage(id, text, attachmentIds))),
    ignore: (id: string) => act(() => api.ignore(id)),
    retry: (id: string) => (forgetStopped(id), act(() => api.retry(id))),
    stopRequest: async (id: string) => {
      await act(() => api.stop(id));
      if (!stopped.value.includes(id)) stopped.value = [...stopped.value, id];
    },
    remove: (id: string) => act(() => api.remove(id)),
  };
}

export type AssistantStore = ReturnType<typeof createAssistantStore>;
const KEY: InjectionKey<AssistantStore> = Symbol('assistant-store');

/** Provides the store to the components below and keeps it running while they are mounted. */
export function provideAssistantStore(api: PoupleApi): AssistantStore {
  const store = createAssistantStore(api);
  provide(KEY, store);
  onMounted(store.start);
  onBeforeUnmount(store.stop);
  return store;
}

export function useAssistantStore(): AssistantStore {
  const store = inject(KEY);
  if (!store) throw new Error('useAssistantStore() called outside of a component tree providing the assistant store');
  return store;
}
