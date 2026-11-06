<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue';
import { t } from '../i18n';
import { errorMessage } from '../api/client';
import { useSettings } from '../composables/useSettings';
import AppButton from './AppButton.vue';
import AppIcon from './AppIcon.vue';
import AppSpinner from './AppSpinner.vue';
import InlineError from './InlineError.vue';

const props = withDefaults(defineProps<{ skippable?: boolean; saveLabel?: string }>(), { skippable: false, saveLabel: undefined });
const saveText = computed(() => props.saveLabel ?? t('projects.saveFolder'));
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
      <label :for="id" class="text-sm font-medium text-ink">{{ $t('projects.label') }}</label>
      <input
        :id="id"
        v-model="path"
        type="text"
        :placeholder="$t('projects.placeholder')"
        autocomplete="off"
        autocapitalize="off"
        spellcheck="false"
        :aria-invalid="error ? 'true' : undefined"
        :aria-describedby="`${id}-help`"
        class="h-10 rounded-lg border bg-surface px-3 font-mono text-sm text-ink outline-none focus:border-accent"
        :class="error ? 'border-fail/70' : 'border-line-strong'"
      />
      <p :id="`${id}-help`" class="text-xs leading-relaxed text-ink-dim">
        {{ $t('projects.help') }}
      </p>
      <p v-if="error" role="alert" class="flex items-start gap-1.5 text-xs text-fail" data-testid="field-error">
        <AppIcon name="alert" size="sm" /><span>{{ error }}</span>
      </p>
      <InlineError v-if="loadError">{{ $t('projects.loadError', { error: loadError }) }}</InlineError>
    </div>
    <div class="flex flex-wrap gap-2">
      <AppButton type="submit" variant="primary" size="lg" :disabled="saving || path.trim() === ''" data-testid="save-projects">
        <AppSpinner v-if="saving" />
        {{ saveText }}
      </AppButton>
      <AppButton v-if="skippable" variant="ghost" size="lg" :disabled="saving" data-testid="skip-projects" @click="emit('skip')">{{ $t('projects.skip') }}</AppButton>
    </div>
  </form>
</template>
