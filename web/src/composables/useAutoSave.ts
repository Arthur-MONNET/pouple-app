import { onScopeDispose, ref } from 'vue';
import { errorMessage } from '../api/client';

/** How long "saved" stays on screen. */
const SAVED_MS = 2500;

/**
 * For a setting that applies as soon as it changes: runs the save, says it worked for a moment, and keeps the
 * core's own message when a value is refused.
 */
export function useAutoSave() {
  const saved = ref(false);
  const saving = ref(false);
  const error = ref<string | null>(null);
  let timer: ReturnType<typeof setTimeout> | undefined;

  /** Resolves true when the change was saved. */
  async function run(action: () => Promise<unknown>): Promise<boolean> {
    clearTimeout(timer);
    saved.value = false;
    error.value = null;
    saving.value = true;
    try {
      await action();
      saved.value = true;
      timer = setTimeout(() => (saved.value = false), SAVED_MS);
      return true;
    } catch (cause) {
      error.value = errorMessage(cause);
      return false;
    } finally {
      saving.value = false;
    }
  }

  onScopeDispose(() => clearTimeout(timer));
  return { saved, saving, error, run };
}
