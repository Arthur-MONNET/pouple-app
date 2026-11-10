<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { PipelineMode } from '../api/types';
import { useApi } from '../composables/useApi';
import InlineError from './InlineError.vue';

const MODES: readonly PipelineMode[] = ['all', 'failures', 'off'];

const api = useApi();
const mode = ref<PipelineMode>('all');
const loaded = ref(false);
const saving = ref(false);
const saved = ref(false);
const error = ref<string | null>(null);

onMounted(async () => {
  try {
    mode.value = (await api.getSettings()).gitlabPipelines ?? 'all';
    loaded.value = true;
  } catch (cause) {
    error.value = errorMessage(cause);
  }
});

/** The choice applies as soon as it is made: the core reads it at its next look at GitLab. */
async function choose(event: Event): Promise<void> {
  const next = (event.target as HTMLSelectElement).value as PipelineMode;
  const previous = mode.value;
  mode.value = next;
  saved.value = false;
  error.value = null;
  saving.value = true;
  try {
    mode.value = (await api.updateSettings({ gitlabPipelines: next })).gitlabPipelines ?? next;
    saved.value = true;
  } catch (cause) {
    mode.value = previous;
    error.value = errorMessage(cause);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <div class="flex flex-col gap-1.5" data-testid="gitlab-pipelines">
    <label for="gitlab-pipelines-mode" class="text-xs font-medium text-ink">{{ $t('connectors.gitlabPipelines.label') }}</label>
    <select
      id="gitlab-pipelines-mode"
      :value="mode"
      :disabled="!loaded || saving"
      class="h-9 rounded-lg border border-line-strong bg-canvas px-2.5 text-xs text-ink outline-none focus:border-accent disabled:opacity-60"
      @change="choose"
    >
      <option v-for="item in MODES" :key="item" :value="item">{{ $t(`connectors.gitlabPipelines.${item}`) }}</option>
    </select>
    <p class="text-2xs leading-relaxed text-ink-dim">{{ $t('connectors.gitlabPipelines.help') }}</p>
    <InlineError v-if="error">{{ error }}</InlineError>
    <p v-if="saved" role="status" class="text-2xs text-ok">{{ $t('settings.saved') }}</p>
  </div>
</template>
