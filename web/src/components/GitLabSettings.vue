<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { PipelineMode } from '../api/types';
import { useApi } from '../composables/useApi';
import { useAutoSave } from '../composables/useAutoSave';
import InlineError from './InlineError.vue';
import SaveStatus from './SaveStatus.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';

/** The connector's own name, as the connectors list says it. */
defineProps<{ name: string }>();

const MODES: readonly PipelineMode[] = ['all', 'failures', 'off'];

const api = useApi();
const mode = ref<PipelineMode>('all');
const loaded = ref(false);
const loadError = ref<string | null>(null);
const autosave = useAutoSave();

onMounted(async () => {
  try {
    mode.value = (await api.getSettings()).gitlabPipelines ?? 'all';
    loaded.value = true;
  } catch (cause) {
    loadError.value = errorMessage(cause);
  }
});

/** The choice applies as soon as it is made: the core reads it at its next look at GitLab. */
async function choose(event: Event): Promise<void> {
  const next = (event.target as HTMLSelectElement).value as PipelineMode;
  const previous = mode.value;
  mode.value = next;
  const saved = await autosave.run(async () => {
    mode.value = (await api.updateSettings({ gitlabPipelines: next })).gitlabPipelines ?? next;
  });
  if (!saved) mode.value = previous;
}
</script>

<template>
  <SettingsSection :title="name" data-testid="gitlab-settings">
    <div v-if="loadError" class="py-4"><InlineError>{{ loadError }}</InlineError></div>
    <SettingsRow v-else :label="$t('connectors.gitlabPipelines.label')" :description="$t('connectors.gitlabPipelines.help')" label-for="gitlab-pipelines-mode">
      <select
        id="gitlab-pipelines-mode"
        :value="mode"
        :disabled="!loaded || autosave.saving.value"
        class="h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent disabled:opacity-60 min-[560px]:w-56"
        data-testid="gitlab-pipelines-mode"
        @change="choose"
      >
        <option v-for="item in MODES" :key="item" :value="item">{{ $t(`connectors.gitlabPipelines.${item}`) }}</option>
      </select>
    </SettingsRow>
    <template #footer><SaveStatus :saved="autosave.saved.value" :error="autosave.error.value" /></template>
  </SettingsSection>
</template>
