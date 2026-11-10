<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { ProviderTestResult, ProvidersInfo, SettingsPatch, SuggestedModel } from '../api/types';
import { useApi } from '../composables/useApi';
import { useAutoSave } from '../composables/useAutoSave';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';
import SaveStatus from './SaveStatus.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const api = useApi();
const info = ref<ProvidersInfo | null>(null);
const provider = ref('');
const model = ref('');
const webAccess = ref(true);
const loadError = ref<string | null>(null);
const error = ref<string | null>(null);
const autosave = useAutoSave();
const testing = ref(false);
const result = ref<ProviderTestResult | null>(null);

const connected = computed(() => info.value?.providers.filter((item) => item.connected) ?? []);
const selected = computed(() => info.value?.providers.find((item) => item.id === provider.value) ?? null);

// The models the selected AI offers, read apart from the providers because the CLI may need a few seconds. When it
// cannot list them, the name can still be typed.
const models = ref<SuggestedModel[]>([]);
const modelsLoading = ref(false);
const modelsMessage = ref('');
let modelsRequest = 0;

/** A model saved earlier that the list does not hold (typed by hand, or retired) stays selectable, so nothing is lost silently. */
const modelOptions = computed(() => (model.value !== '' && !models.value.some((item) => item.id === model.value) ? [{ id: model.value, label: model.value }, ...models.value] : models.value));

async function loadModels(id: string): Promise<void> {
  const request = ++modelsRequest;
  modelsLoading.value = true;
  modelsMessage.value = '';
  try {
    const answer = await api.getProviderModels(id);
    if (request !== modelsRequest) return;
    models.value = answer.models;
    modelsMessage.value = answer.message;
  } catch (cause) {
    if (request !== modelsRequest) return;
    models.value = [];
    modelsMessage.value = errorMessage(cause);
  } finally {
    if (request === modelsRequest) modelsLoading.value = false;
  }
}

onMounted(async () => {
  try {
    info.value = await api.getProviders();
    provider.value = info.value.active;
    model.value = selected.value?.model ?? '';
    webAccess.value = (await api.getSettings()).webAccess !== false;
    void loadModels(provider.value);
  } catch (cause) {
    loadError.value = errorMessage(cause);
  }
});

/** Every change applies at once, and the next analysis already uses it. */
function persist(patch: SettingsPatch): Promise<boolean> {
  return autosave.run(async () => {
    await api.updateSettings(patch);
    info.value = await api.getProviders();
  });
}

function onProvider(): void {
  model.value = selected.value?.model ?? '';
  result.value = null;
  void loadModels(provider.value);
  void persist({ provider: provider.value });
}

function onModel(): void {
  result.value = null;
  void persist({ provider: provider.value, models: { [provider.value]: model.value.trim() } });
}

function onWeb(on: boolean): void {
  webAccess.value = on;
  void persist({ webAccess: on });
}

async function test(): Promise<void> {
  error.value = null;
  result.value = null;
  testing.value = true;
  try {
    result.value = await api.testProvider(provider.value, model.value.trim());
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    testing.value = false;
  }
}
</script>

<template>
  <SettingsSection v-if="info || loadError" :title="$t('settings.ai.title')" :description="$t('settings.ai.help')" data-testid="ai-section">
    <div v-if="loadError" class="py-4"><InlineError>{{ loadError }}</InlineError></div>
    <template v-else-if="info">
      <SettingsRow :label="$t('settings.ai.provider')" label-for="ai-provider">
        <select v-if="connected.length > 1" id="ai-provider" v-model="provider" class="h-10 w-48 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" @change="onProvider">
          <option v-for="item in connected" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select>
        <p v-else class="py-2 text-sm text-ink">{{ selected?.name }}</p>
      </SettingsRow>

      <SettingsRow :label="$t('settings.ai.model')" :description="[$t('settings.ai.modelHelp'), selected?.note, modelsLoading ? '' : modelsMessage].filter(Boolean).join(' ')" label-for="ai-model">
        <select v-if="modelsLoading || modelOptions.length > 0" id="ai-model" v-model="model" :disabled="modelsLoading" class="h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent disabled:opacity-60 min-[560px]:w-72" data-testid="ai-model-select" @change="onModel">
          <option v-if="modelsLoading" value="">{{ $t('settings.ai.modelsLoading') }}</option>
          <template v-else>
            <option value="">{{ $t('settings.ai.modelPlaceholder') }}</option>
            <option v-for="item in modelOptions" :key="item.id" :value="item.id">{{ item.label }}</option>
          </template>
        </select>
        <input v-else id="ai-model" v-model="model" type="text" :placeholder="$t('settings.ai.modelPlaceholder')" autocomplete="off" spellcheck="false" class="h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent min-[560px]:w-72" @change="onModel" />
      </SettingsRow>

      <div class="py-4">
        <ToggleSwitch :model-value="webAccess" :label="$t('settings.ai.web')" :description="$t('settings.ai.webHelp')" data-testid="ai-web-access" @update:model-value="onWeb" />
      </div>

      <div class="flex flex-col gap-3 py-4">
        <InlineError v-if="error">{{ error }}</InlineError>
        <p v-if="result" role="status" class="text-sm" :class="result.ok ? 'text-ok' : 'text-ink-muted'" data-testid="ai-test-result">
          {{ result.ok ? $t('settings.ai.testOk', { model: result.model, seconds: (result.latencyMs / 1000).toFixed(1) }) : result.message }}
        </p>
        <AppButton variant="outline" class="w-fit" :disabled="autosave.saving.value || testing" @click="test">
          <AppSpinner v-if="testing" />
          {{ $t('settings.ai.test') }}
        </AppButton>
      </div>
    </template>
    <template #footer><SaveStatus v-if="info" :saved="autosave.saved.value" :error="autosave.error.value" /></template>
  </SettingsSection>
</template>
