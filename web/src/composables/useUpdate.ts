import { computed, inject, ref, type App, type InjectionKey } from 'vue';
import type { PoupleApi } from '../api/client';
import { errorMessage } from '../api/client';
import type { UpdateInfo } from '../api/types';
import { createUpdateLauncher, findUpdateBridge, isUpdateAvailable, startPolling, updateView, type UpdateBridge } from '../lib/update';

export interface UpdateStoreOptions {
  /** Looked up on each use, so a bridge injected late is still found. */
  getBridge?: () => UpdateBridge | null;
}

/** The update state shared by the top bar badge and the Settings section. */
export function createUpdateStore(api: PoupleApi, options: UpdateStoreOptions = {}) {
  const getBridge = options.getBridge ?? findUpdateBridge;
  const info = ref<UpdateInfo | null>(null);
  const checking = ref(false);
  /** The message of a failed check, shown calmly in the section. */
  const checkError = ref<string | null>(null);
  const closing = ref(false);
  const launcher = createUpdateLauncher(getBridge);

  const view = computed(() => updateView(info.value, checking.value));
  const available = computed(() => isUpdateAvailable(info.value));
  const canLaunch = computed(() => getBridge() !== null);
  /** The core's own error, or the one of a request that failed. */
  const error = computed(() => checkError.value ?? info.value?.error ?? null);

  /** Reads the last known state. Never throws: it runs in the background and a failure must not bother anyone. */
  async function refresh(): Promise<void> {
    try {
      info.value = await api.getUpdate();
    } catch {
      // Silent on purpose.
    }
  }

  /** Forces a check; the loading state lasts as long as the core takes. */
  async function check(): Promise<void> {
    if (checking.value) return;
    checking.value = true;
    checkError.value = null;
    try {
      info.value = await api.checkUpdate();
    } catch (cause) {
      checkError.value = errorMessage(cause);
    } finally {
      checking.value = false;
    }
  }

  /** Posts the message to the shell once; the app quits right after, so the interface only says it is closing. */
  function launch(): boolean {
    const sent = launcher.launch();
    if (sent) closing.value = true;
    return sent;
  }

  /** One read now, then one every 30 minutes while the window is open. Returns the stop function. */
  const startPolling_ = (intervalMs?: number): (() => void) => startPolling(refresh, intervalMs);

  return { info, checking, error, closing, view, available, canLaunch, refresh, check, launch, startPolling: startPolling_ };
}

export type UpdateStore = ReturnType<typeof createUpdateStore>;

const KEY: InjectionKey<UpdateStore> = Symbol('pouple-update');

export const provideUpdate = (app: App, store: UpdateStore): void => void app.provide(KEY, store);

export function useUpdate(): UpdateStore {
  const store = inject(KEY);
  if (!store) throw new Error('useUpdate() called outside of an app that provides the update store');
  return store;
}
