import { ref } from 'vue';
import { errorMessage } from '../api/client';

/**
 * One user action at a time, with its error: the buttons that start one are disabled while it runs, and the core's
 * own message is shown when it is refused. Resolves with whether it succeeded.
 */
export function useAsyncAction() {
  const busy = ref(false);
  const error = ref<string | null>(null);

  async function run(action: () => Promise<unknown>): Promise<boolean> {
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

  const clear = (): void => void (error.value = null);

  return { busy, error, run, clear };
}
