<script setup lang="ts">
import { ref, useId, watch } from 'vue';
import { errorMessage } from '../api/client';
import { useSettings } from '../composables/useSettings';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';

const props = withDefaults(defineProps<{ skippable?: boolean; saveLabel?: string }>(), { skippable: false, saveLabel: 'Save' });
const emit = defineEmits<{ saved: []; skip: [] }>();

const { settings, loadError, load, save } = useSettings();
const id = useId();
const path = ref('');
const error = ref<string | null>(null);
const saving = ref(false);

void load();
watch(settings, (value) => value && path.value === '' && (path.value = value.projectsRoot), { once: true });

async function submit(): Promise<void> {
  error.value = null;
  saving.value = true;
  try {
    await save({ projectsRoot: path.value.trim() });
    emit('saved');
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <form class="flex flex-col gap-4" novalidate @submit.prevent="submit">
    <div class="flex flex-col gap-1.5">
      <label :for="id" class="text-sm font-medium text-ink">Projects folder</label>
      <input
        :id="id"
        v-model="path"
        type="text"
        placeholder="/Users/you/Workspace"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="`${id}-help`"
        class="h-10 rounded-lg border bg-surface px-3 font-mono text-sm text-ink outline-none focus:border-accent"
        :class="error ? 'border-fail/70' : 'border-line-strong'"
      />
      <p :id="`${id}-help`" class="text-xs leading-relaxed text-ink-dim">
        The folder that holds one folder per project (for example <span class="font-mono">Workspace/front</span> and <span class="font-mono">Workspace/api</span>).
        Codex looks into them to answer, and applies a change there only after you approve it. Without a folder, Codex can still analyse requests but never proposes code changes.
        Use the full path: the app cannot browse your disk.
      </p>
      <p v-if="error" role="alert" class="flex items-start gap-1.5 text-xs text-fail" data-testid="field-error">
        <AppIcon name="alert" size="sm" /><span>{{ error }}</span>
      </p>
      <InlineError v-if="loadError">The current value could not be read: {{ loadError }}</InlineError>
    </div>
    <div class="flex flex-wrap gap-2">
      <AppButton type="submit" variant="primary" size="lg" :disabled="saving || path.trim() === ''" data-testid="save-projects">
        <AppSpinner v-if="saving" />
        {{ props.saveLabel }}
      </AppButton>
      <AppButton v-if="skippable" variant="ghost" size="lg" :disabled="saving" data-testid="skip-projects" @click="emit('skip')">Skip for now</AppButton>
    </div>
  </form>
</template>
