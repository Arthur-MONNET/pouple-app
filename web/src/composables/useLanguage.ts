import { ref } from 'vue';
import { errorMessage } from '../api/client';
import { lang, languageEpoch, setLanguage, type Lang } from '../i18n';
import { useApi } from './useApi';
import { useConnectorsStore } from './useConnectorsStore';

/** Switches the interface language: the screen follows at once, the core is told, and what it sends is read again. */
export function useLanguage() {
  const api = useApi();
  const connectors = useConnectorsStore();
  const error = ref<string | null>(null);

  async function switchTo(next: Lang): Promise<void> {
    if (next === lang.value) return;
    const previous = lang.value;
    error.value = null;
    setLanguage(next);
    try {
      await api.updateSettings({ uiLanguage: next });
      await connectors.refresh();
      languageEpoch.value += 1;
    } catch (cause) {
      setLanguage(previous);
      error.value = errorMessage(cause);
    }
  }

  return { lang, error, switchTo };
}
