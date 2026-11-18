<script setup lang="ts">
import { onMounted, ref, useId } from 'vue';
import { errorMessage } from '../api/client';
import { useApi } from '../composables/useApi';
import { useAutoSave } from '../composables/useAutoSave';
import { useMemory } from '../composables/useMemory';
import MemoryCliList from './MemoryCliList.vue';
import InlineError from './InlineError.vue';
import SaveStatus from './SaveStatus.vue';
import SettingsRow from './SettingsRow.vue';
import SettingsSection from './SettingsSection.vue';
import ToggleSwitch from './ToggleSwitch.vue';

const api = useApi();
const folderId = useId();
const cliTitleId = useId();
const on = ref(false);
/** What the field shows, and the last folder the core accepted (to put back a refused one). */
const folder = ref('');
const accepted = ref('');
const loadError = ref<string | null>(null);
const autosave = useAutoSave();
const memory = useMemory(api);

onMounted(async () => {
  try {
    const settings = await api.getSettings();
    on.value = settings.memory === true;
    folder.value = accepted.value = settings.memoryFolder ?? '';
    await memory.load();
  } catch (cause) {
    loadError.value = errorMessage(cause);
  }
});

async function onToggle(next: boolean): Promise<void> {
  const previous = on.value;
  on.value = next;
  if (!(await autosave.run(async () => void (await api.updateSettings({ memory: next }))))) on.value = previous;
  else await memory.load();
}

/** Saved when the field is left or Enter is pressed. Empty resets to the default folder, which the core then names. */
async function saveFolder(): Promise<void> {
  const next = folder.value.trim();
  if (autosave.saving.value || next === accepted.value) return;
  const saved = await autosave.run(async () => {
    const settings = await api.updateSettings({ memoryFolder: next });
    folder.value = accepted.value = settings.memoryFolder ?? next;
  });
  // A refused folder stays in the field, next to the core's message, so it can be corrected.
  if (!saved) folder.value = next;
}
</script>

<template>
  <SettingsSection :title="$t('settings.memory.title')" data-testid="memory-settings">
    <div class="py-4">
      <ToggleSwitch :model-value="on" :label="$t('settings.memory.label')" :description="$t('settings.memory.help')" data-testid="memory-toggle" @update:model-value="onToggle" />
    </div>
    <SettingsRow :label="$t('settings.memory.folderLabel')" :description="$t('settings.memory.folderHelp')" :label-for="folderId" stacked>
      <input
        :id="folderId"
        v-model="folder"
        type="text"
        :placeholder="$t('settings.memory.folderPlaceholder')"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :aria-invalid="autosave.error.value ? 'true' : undefined"
        class="h-10 w-full rounded-lg border bg-canvas px-3 font-mono text-sm text-ink outline-none focus:border-accent"
        :class="autosave.error.value ? 'border-fail/70' : 'border-line-strong'"
        data-testid="memory-folder"
        @change="saveFolder"
        @keydown.enter.prevent="saveFolder"
      />
    </SettingsRow>
    <div v-if="loadError" class="pb-4"><InlineError>{{ loadError }}</InlineError></div>
    <div v-if="memory.view.value" class="py-4" data-testid="memory-cli-section">
      <h3 :id="cliTitleId" class="text-sm font-medium text-ink">{{ $t('settings.memory.cli.title') }}</h3>
      <p class="mt-0.5 text-xs leading-relaxed text-ink-muted">{{ $t('settings.memory.cli.intro') }}</p>
      <MemoryCliList :clis="memory.view.value.clis" :enabled="memory.view.value.enabled" :busy="memory.busy.value" @toggle="(id, connected) => memory.setCli(id, connected)" />
    </div>
    <div v-if="memory.error.value" class="pb-4"><InlineError box>{{ memory.error.value }}</InlineError></div>
    <template #footer><SaveStatus :saved="autosave.saved.value" :error="autosave.error.value" /></template>
  </SettingsSection>
</template>
