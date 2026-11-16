<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { StyleInfo } from '../api/types';
import { useApi } from '../composables/useApi';
import { useAutoSave } from '../composables/useAutoSave';
import { t } from '../i18n';
import { formatDay } from '../lib/format';
import AppButton from './AppButton.vue';
import InlineError from './InlineError.vue';
import SaveStatus from './SaveStatus.vue';
import SettingsSection from './SettingsSection.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const api = useApi();
const learn = ref(true);
const info = ref<StyleInfo>({ learned: false });
const loadError = ref<string | null>(null);
const busy = ref(false);
const autosave = useAutoSave();

onMounted(async () => {
  try {
    const [settings, style] = await Promise.all([api.getSettings(), api.getStyle()]);
    learn.value = settings.learnStyle !== false;
    info.value = style;
  } catch (cause) {
    loadError.value = errorMessage(cause);
  }
});

/** What is known, in a line: since when, from how many messages. */
const status = computed(() =>
  info.value.learned && info.value.learnedAt
    ? t('settings.style.learned', { day: formatDay(info.value.learnedAt), count: info.value.from ?? 0 })
    : t('settings.style.notLearned'),
);

async function onLearn(on: boolean): Promise<void> {
  const previous = learn.value;
  learn.value = on;
  if (!(await autosave.run(async () => void (await api.updateSettings({ learnStyle: on }))))) learn.value = previous;
}

async function forget(): Promise<void> {
  busy.value = true;
  loadError.value = null;
  try {
    info.value = await api.forgetStyle();
  } catch (cause) {
    loadError.value = errorMessage(cause);
  } finally {
    busy.value = false;
  }
}
</script>

<template>
  <SettingsSection :title="$t('settings.style.title')" data-testid="style-settings">
    <div class="py-4">
      <ToggleSwitch :model-value="learn" :label="$t('settings.style.label')" :description="$t('settings.style.help')" data-testid="learn-style" @update:model-value="onLearn" />
    </div>
    <div class="flex flex-wrap items-center gap-3 py-4">
      <p class="min-w-0 flex-1 text-sm text-ink-muted" role="status" data-testid="style-status">{{ status }}</p>
      <AppButton variant="outline" size="sm" :disabled="busy || !info.learned" data-testid="forget-style" @click="forget">{{ $t('settings.style.forget') }}</AppButton>
    </div>
    <div v-if="loadError" class="pb-4"><InlineError>{{ loadError }}</InlineError></div>
    <template #footer><SaveStatus :saved="autosave.saved.value" :error="autosave.error.value" /></template>
  </SettingsSection>
</template>
