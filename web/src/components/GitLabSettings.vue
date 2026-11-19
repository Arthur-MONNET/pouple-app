<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { errorMessage } from '../api/client';
import type { GitLabAccess, GitLabAccessUpdate, GitLabMode, GitLabTokenID, PipelineMode } from '../api/types';
import { useApi } from '../composables/useApi';
import { useAutoSave } from '../composables/useAutoSave';
import InlineError from './InlineError.vue';
import SaveStatus from './SaveStatus.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';

/** The connector's own name, as the connectors list says it. */
const props = defineProps<{ name: string; connected: boolean }>();

const MODES: readonly PipelineMode[] = ['all', 'failures', 'off'];

const api = useApi();
const mode = ref<PipelineMode>('all');
const loaded = ref(false);
const loadError = ref<string | null>(null);
const autosave = useAutoSave();

const access = ref<GitLabAccess | null>(null);
const accessError = ref<string | null>(null);
const savingAccess = ref(false);
const primaryName = ref('');
const additionalName = ref('');
const additionalToken = ref('');
const canWrite = computed(() => access.value?.tokens.find((token) => token.id === access.value?.selected)?.scopes?.includes('api'));
const canAdd = computed(() => access.value?.tokens[0]?.scopes?.includes('read_api') && !access.value?.tokens[0]?.scopes?.includes('api'));
const inputClass = 'h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent disabled:opacity-60';
const buttonClass = 'rounded-lg border border-line-strong px-3 py-2 text-sm text-ink hover:bg-canvas disabled:opacity-60';

watch(() => props.connected, async (connected, _, onCleanup) => {
  let current = true;
  onCleanup(() => { current = false; });
  access.value = null;
  accessError.value = null;
  if (!connected) return;
  try {
    const settings = await api.getGitLabAccess();
    if (current) showAccess(settings);
  } catch (cause) {
    if (current) accessError.value = errorMessage(cause);
  }
}, { immediate: true });

function showAccess(settings: GitLabAccess): void {
  access.value = settings;
  primaryName.value = settings.tokens[0]?.name ?? '';
  additionalName.value = settings.tokens[1]?.name ?? '';
}

async function updateAccess(update: GitLabAccessUpdate): Promise<void> {
  savingAccess.value = true;
  accessError.value = null;
  try {
    const settings = await api.updateGitLabAccess(update);
    if (props.connected) showAccess(settings);
    if (update.token) additionalToken.value = '';
  } catch (cause) {
    accessError.value = errorMessage(cause);
  } finally {
    savingAccess.value = false;
  }
}

async function chooseAccess(event: Event, field: 'mode' | 'selected'): Promise<void> {
  const select = event.target as HTMLSelectElement;
  const value = select.value;
  select.value = access.value?.[field] ?? '';
  await updateAccess(field === 'mode' ? { mode: value as GitLabMode } : { selected: value as GitLabTokenID });
}
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
    <template v-if="access">
      <SettingsRow :label="$t('connectors.gitlabAccess.primaryName')" label-for="gitlab-primary-name">
        <form class="flex gap-2" @submit.prevent="updateAccess({ primaryName })">
          <input id="gitlab-primary-name" v-model="primaryName" :class="inputClass" :disabled="savingAccess" maxlength="80" required />
          <button :class="buttonClass" :disabled="savingAccess || !primaryName.trim() || primaryName === access.tokens[0]?.name">{{ $t('connectors.gitlabAccess.save') }}</button>
        </form>
      </SettingsRow>
      <SettingsRow :label="$t('connectors.gitlabAccess.scopes')">
        <div class="flex flex-col items-end gap-2 text-sm text-ink">
          <div v-for="token in access.tokens" :key="token.id">
            {{ token.name }} · {{ token.scopes?.join(', ') || $t('connectors.gitlabAccess.unverified') }}
          </div>
          <button :class="buttonClass" :disabled="savingAccess" @click="updateAccess({ verify: true })">
            {{ $t('connectors.gitlabAccess.verify') }}
          </button>
        </div>
      </SettingsRow>
      <SettingsRow v-if="access.tokens.length > 1" :label="$t('connectors.gitlabAccess.selected')" label-for="gitlab-token">
        <select id="gitlab-token" :class="inputClass" :value="access.selected" :disabled="savingAccess" @change="chooseAccess($event, 'selected')">
          <option v-for="token in access.tokens" :key="token.id" :value="token.id">{{ token.name }}</option>
        </select>
      </SettingsRow>
      <SettingsRow :label="$t('connectors.gitlabAccess.mode')" :description="$t('connectors.gitlabAccess.help')" label-for="gitlab-mode">
        <select id="gitlab-mode" :class="inputClass" :value="access.mode" :disabled="savingAccess" @change="chooseAccess($event, 'mode')">
          <option value="read-only">{{ $t('connectors.gitlabAccess.readOnly') }}</option>
          <option value="write" :disabled="!canWrite">{{ $t('connectors.gitlabAccess.write') }}</option>
        </select>
      </SettingsRow>
      <SettingsRow v-if="canAdd || access.tokens.length > 1" :label="$t('connectors.gitlabAccess.additional')" :description="$t('connectors.gitlabAccess.additionalHelp')" label-for="gitlab-additional-name">
        <form class="flex flex-col items-start gap-2" @submit.prevent="updateAccess({ name: additionalName, token: additionalToken })">
          <input id="gitlab-additional-name" v-model="additionalName" :class="inputClass" :placeholder="$t('connectors.gitlabAccess.tokenName')" :disabled="savingAccess" maxlength="80" required />
          <input v-model="additionalToken" :class="inputClass" type="password" autocomplete="off" :aria-label="$t('connectors.gitlabAccess.additional')" :disabled="savingAccess" required />
          <button :class="buttonClass" :disabled="savingAccess || !additionalName.trim() || !additionalToken.trim()">{{ $t('connectors.gitlabAccess.save') }}</button>
        </form>
      </SettingsRow>
    </template>
    <div v-if="accessError" class="py-4"><InlineError>{{ accessError }}</InlineError></div>
    <template #footer><SaveStatus :saved="autosave.saved.value" :error="autosave.error.value" /></template>
  </SettingsSection>
</template>
