import { onBeforeUnmount, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { ActionEvent } from '../api/types';
import { useApi } from './useApi';

const MAX_LINES = 400;

/** Runs one connector action at a time and keeps what it says: output lines, a page to open, how it ended. */
export function useActionRun() {
  const api = useApi();
  const running = ref<string | null>(null);
  const lines = ref<string[]>([]);
  const openUrl = ref<string | null>(null);
  const error = ref<string | null>(null);
  let abort: AbortController | null = null;

  function handle(event: ActionEvent): void {
    if (event.type === 'log') lines.value = [...lines.value, event.text].slice(-MAX_LINES);
    else if (event.type === 'open') openUrl.value = event.url;
    else if (event.type === 'error') error.value = event.text;
  }

  /** Resolves true when the action ended with `done`. */
  async function run(connectorId: string, actionId: string): Promise<boolean> {
    if (running.value) return false;
    running.value = actionId;
    lines.value = [];
    openUrl.value = null;
    error.value = null;
    abort = new AbortController();
    let finished = false;
    try {
      await api.runAction(
        connectorId,
        actionId,
        (event) => {
          handle(event);
          if (event.type === 'done') finished = true;
        },
        abort.signal,
      );
      if (!finished && !error.value) error.value = 'The action ended without saying it was done.';
    } catch (cause) {
      if (!abort.signal.aborted) error.value = errorMessage(cause);
    } finally {
      running.value = null;
      abort = null;
    }
    return finished;
  }

  const cancel = (): void => abort?.abort();
  onBeforeUnmount(cancel);

  return { running, lines, openUrl, error, run, cancel };
}
