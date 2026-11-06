import { computed, ref } from 'vue';
import { lang } from '../i18n';
import { optionalConnectors, readHintDismissed, shouldShowHint, writeHintDismissed } from '../lib/optionalConnectors';
import { useConnectorsStore } from './useConnectorsStore';

/** The small suggestion to connect the optional connectors, with its dismissal remembered in the browser. */
export function useExtrasHint() {
  const store = useConnectorsStore();
  const dismissed = ref(readHintDismissed());

  const visible = computed(() => shouldShowHint(store.connectors.value, store.state.value?.setupComplete === true, dismissed.value));
  /** "GitLab, Asana ou Slite", in the language of the interface. */
  const names = computed(() => new Intl.ListFormat(lang.value, { type: 'disjunction' }).format(optionalConnectors(store.connectors.value).map((c) => c.descriptor.name)));

  function dismiss(): void {
    dismissed.value = true;
    writeHintDismissed();
  }

  return { visible, names, dismiss };
}
