import { inject, ref, type App, type InjectionKey } from 'vue';
import { errorMessage, type PoupleApi } from '../api/client';
import type { Settings, SettingsPatch } from '../api/types';

/**
 * The settings, shared by every screen: what one saves (the memory turned on, for example) is what the sidebar shows at
 * once. The core's own message is kept when a value is refused.
 */
export function createSettingsStore(api: PoupleApi) {
  const settings = ref<Settings | null>(null);
  const loadError = ref<string | null>(null);

  async function load(): Promise<void> {
    try {
      settings.value = await api.getSettings();
      loadError.value = null;
    } catch (cause) {
      loadError.value = errorMessage(cause);
    }
  }

  /** Resolves with the saved settings; rejects with the core's message. */
  async function save(patch: SettingsPatch): Promise<Settings> {
    const saved = await api.updateSettings(patch);
    settings.value = saved;
    return saved;
  }

  return { settings, loadError, load, save };
}

export type SettingsStore = ReturnType<typeof createSettingsStore>;
const KEY: InjectionKey<SettingsStore> = Symbol('settings');

export function provideSettings(app: App, store: SettingsStore): void {
  app.provide(KEY, store);
}

export function useSettings(): SettingsStore {
  const store = inject(KEY);
  if (!store) throw new Error('useSettings() called outside of an app providing the settings');
  return store;
}
