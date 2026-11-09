<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { errorMessage } from '../api/client';
import type { ProviderTestResult, ProvidersInfo } from '../api/types';
import { useApi } from '../composables/useApi';
import AppButton from './AppButton.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';

const api = useApi();
const info = ref<ProvidersInfo | null>(null);
const provider = ref('');
const model = ref('');
const webAccess = ref(false);
const loadError = ref<string | null>(null);
const error = ref<string | null>(null);
const saved = ref(false);
const saving = ref(false);
const testing = ref(false);
const result = ref<ProviderTestResult | null>(null);

const connected = computed(() => info.value?.providers.filter((item) => item.connected) ?? []);
const selected = computed(() => info.value?.providers.find((item) => item.id === provider.value) ?? null);
const suggestions = computed(() => selected.value?.suggestedModels ?? []);

onMounted(async () => {
  try {
    info.value = await api.getProviders();
    provider.value = info.value.active;
    model.value = selected.value?.model ?? '';
    webAccess.value = (await api.getSettings()).webAccess === true;
  } catch (cause) {
    loadError.value = errorMessage(cause);
  }
});

function onProvider(): void {
  model.value = selected.value?.model ?? '';
  result.value = null;
  saved.value = false;
}

async function save(): Promise<void> {
  error.value = null;
  saved.value = false;
  saving.value = true;
  try {
    await api.updateSettings({ provider: provider.value, models: { [provider.value]: model.value.trim() }, webAccess: webAccess.value });
    info.value = await api.getProviders();
    saved.value = true;
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    saving.value = false;
  }
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
  <section v-if="info || loadError" aria-labelledby="ai-title" class="flex flex-col gap-3 rounded-xl border border-line bg-surface p-5" data-testid="ai-section">
    <h2 id="ai-title" class="text-base font-semibold text-ink">{{ $t('settings.ai.title') }}</h2>
    <InlineError v-if="loadError">{{ loadError }}</InlineError>
    <template v-else-if="info">
      <p class="text-xs text-ink-muted">{{ $t('settings.ai.help') }}</p>
      <div v-if="connected.length > 1" class="flex flex-col gap-1.5">
        <label for="ai-provider" class="text-sm font-medium text-ink">{{ $t('settings.ai.provider') }}</label>
        <select id="ai-provider" v-model="provider" class="h-10 w-48 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" @change="onProvider">
          <option v-for="item in connected" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select>
      </div>
      <p v-else class="text-sm text-ink">{{ selected?.name }}</p>
      <div class="flex flex-col gap-1.5">
        <label for="ai-model" class="text-sm font-medium text-ink">{{ $t('settings.ai.model') }}</label>
        <input id="ai-model" v-model="model" list="ai-models" type="text" :placeholder="$t('settings.ai.modelPlaceholder')" autocomplete="off" spellcheck="false" class="h-10 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" @input="saved = false" />
        <datalist id="ai-models"><option v-for="item in suggestions" :key="item.id" :value="item.id">{{ item.label }}</option></datalist>
        <p class="text-xs text-ink-muted">{{ $t('settings.ai.modelHelp') }}</p>
        <p v-if="selected?.note" class="text-xs text-ink-dim">{{ selected.note }}</p>
      </div>
      <label class="flex items-start gap-2.5 text-sm text-ink" for="ai-web">
        <input id="ai-web" v-model="webAccess" type="checkbox" class="mt-1 accent-[var(--color-accent)]" data-testid="ai-web-access" @change="saved = false" />
        <span>
          <span class="font-medium">{{ $t('settings.ai.web') }}</span>
          <span class="mt-0.5 block text-xs text-ink-muted">{{ $t('settings.ai.webHelp') }}</span>
        </span>
      </label>
      <InlineError v-if="error">{{ error }}</InlineError>
      <p v-if="result" role="status" class="text-sm" :class="result.ok ? 'text-ok' : 'text-ink-muted'" data-testid="ai-test-result">
        {{ result.ok ? $t('settings.ai.testOk', { model: result.model, seconds: (result.latencyMs / 1000).toFixed(1) }) : result.message }}
      </p>
      <p v-if="saved" role="status" class="text-sm text-ok">{{ $t('settings.saved') }}</p>
      <div class="flex flex-wrap gap-3">
        <AppButton variant="primary" :disabled="saving || testing" @click="save">
          <AppSpinner v-if="saving" />
          {{ $t('settings.save') }}
        </AppButton>
        <AppButton variant="ghost" :disabled="saving || testing" @click="test">
          <AppSpinner v-if="testing" />
          {{ $t('settings.ai.test') }}
        </AppButton>
      </div>
    </template>
  </section>
</template>
