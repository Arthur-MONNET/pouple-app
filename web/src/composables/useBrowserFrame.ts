import { onScopeDispose, ref, watch, type Ref } from 'vue';
import { useApi } from './useApi';

/** How often the picture of the browser is asked for while it works: often enough to follow it, rarely enough to stay light. */
export const FRAME_INTERVAL_MS = 700;

/**
 * The picture the browser shows right now, as an object URL that follows it. Only asked for while `active` and `open`
 * (a collapsed block costs nothing); the last one is released when it is replaced or when the block goes away.
 */
export function useBrowserFrame(requestId: Ref<string>, active: Ref<boolean>) {
  const api = useApi();
  const url = ref<string | null>(null);
  let timer: ReturnType<typeof setTimeout> | undefined;
  let stopped = false;

  const release = (): void => {
    if (url.value) URL.revokeObjectURL(url.value);
    url.value = null;
  };

  async function tick(): Promise<void> {
    try {
      const blob = await api.getBrowserFrame(requestId.value);
      if (stopped) return;
      const next = URL.createObjectURL(blob);
      release();
      url.value = next;
    } catch {
      // No picture yet (the browser is starting) or already gone: the next look will tell.
    }
    if (!stopped) timer = setTimeout(() => void tick(), FRAME_INTERVAL_MS);
  }

  function stop(): void {
    stopped = true;
    if (timer) clearTimeout(timer);
    timer = undefined;
  }

  watch(
    active,
    (on) => {
      stop();
      if (!on) return;
      stopped = false;
      void tick();
    },
    { immediate: true },
  );
  onScopeDispose(() => {
    stop();
    release();
  });

  return { url };
}
