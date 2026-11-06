import { ref } from 'vue';
import { errorMessage } from '../api/client';
import type { Settings, SettingsPatch } from '../api/types';
import { useApi } from './useApi';

/** Loads the settings and saves changes; the core's own message is kept when a value is refused. */
export function useSettings() {
  const api = useApi();
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
