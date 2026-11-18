import { ref } from 'vue';
import { errorMessage, type PoupleApi } from '../api/client';
import type { MemoryCliId, MemoryDecision, MemoryView } from '../api/types';

/**
 * The memory folder as the core describes it, and the few things the user can do with it. Every action keeps the
 * core's own message when it is refused (a line that changed meanwhile, a memory that is off) and is never run twice at once.
 */
export function useMemory(api: PoupleApi) {
  const view = ref<MemoryView | null>(null);
  const error = ref<string | null>(null);
  const busy = ref(false);
  /** Set once a tidy-up was started from this page, since the core runs it in the background. */
  const tidyStarted = ref(false);

  async function run(action: () => Promise<void>): Promise<boolean> {
    if (busy.value) return false;
    busy.value = true;
    error.value = null;
    try {
      await action();
      return true;
    } catch (cause) {
      error.value = errorMessage(cause);
      return false;
    } finally {
      busy.value = false;
    }
  }

  const load = (): Promise<boolean> => run(async () => void (view.value = await api.getMemory()));
  const review = (note: string, line: string, decision: MemoryDecision): Promise<boolean> => run(async () => void (view.value = await api.reviewMemory(note, line, decision)));
  const setCli = (id: MemoryCliId, connected: boolean): Promise<boolean> => run(async () => void (view.value = await api.connectCli(id, connected)));
  const reveal = (): Promise<boolean> => run(() => api.revealMemory());
  const tidy = (): Promise<boolean> =>
    run(async () => {
      await api.tidyMemory();
      tidyStarted.value = true;
    });

  return { view, error, busy, tidyStarted, load, review, setCli, reveal, tidy };
}
