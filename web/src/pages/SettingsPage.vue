<script setup lang="ts">
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { errorMessage } from '../api/client';
import type { AutoApprove } from '../api/types';
import AppButton from '../components/AppButton.vue';
import AppSpinner from '../components/AppSpinner.vue';
import InlineError from '../components/InlineError.vue';
import ProjectsFolderForm from '../components/ProjectsFolderForm.vue';
import UpdateSection from '../components/UpdateSection.vue';
import { useLanguage } from '../composables/useLanguage';
import { useSettings } from '../composables/useSettings';
import { LANGS, type Lang } from '../i18n';

const { settings, loadError, load, save } = useSettings();
const language = useLanguage();
const route = useRoute();

// The page opens on the Updates section when the badge of the top bar sent the reader here.
onMounted(async () => {
  if (route.hash === '#updates') {
    await nextTick();
    document.getElementById('updates')?.scrollIntoView({ block: 'start' });
  }
});

const form = reactive({ language: '', dailyTurnLimit: 0, autoApprove: 'off' as AutoApprove, keywords: '' });
const saving = ref(false);
const error = ref<string | null>(null);
const saved = ref(false);
/** Names written to Codex as they are: it understands them in any language. */
const ANSWER_LANGUAGES = ['Français', 'English', 'Español', 'Deutsch', 'Italiano', 'Português'];

void load();
watch(settings, (value) => {
  if (!value) return;
  form.language = value.language;
  form.dailyTurnLimit = value.dailyTurnLimit;
  form.autoApprove = value.autoApprove;
  form.keywords = value.keywords.join(', ');
}, { once: true });

const keywordList = computed(() => form.keywords.split(/[,\n]/).map((word) => word.trim()).filter(Boolean));

function onInterfaceLanguage(event: Event): void {
  void language.switchTo((event.target as HTMLSelectElement).value as Lang);
}

async function submit(): Promise<void> {
  error.value = null;
  saved.value = false;
  saving.value = true;
  try {
    await save({ language: form.language.trim(), dailyTurnLimit: Math.max(0, Math.floor(Number(form.dailyTurnLimit) || 0)), autoApprove: form.autoApprove, keywords: keywordList.value });
    saved.value = true;
  } catch (cause) {
    error.value = errorMessage(cause);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <main class="min-h-0 flex-1 overflow-y-auto">
    <div class="mx-auto flex max-w-2xl flex-col gap-8 px-5 py-6">
      <header>
        <h1 class="text-title font-semibold text-ink">{{ $t('settings.title') }}</h1>
        <p class="text-sm text-ink-muted">{{ $t('settings.intro') }}</p>
      </header>
      <InlineError v-if="loadError" box>{{ $t('settings.loadError', { error: loadError }) }}</InlineError>

      <section aria-labelledby="interface-title" class="flex flex-col gap-3 rounded-xl border border-line bg-surface p-5">
        <h2 id="interface-title" class="text-base font-semibold text-ink">{{ $t('settings.interfaceLanguage') }}</h2>
        <select
          id="ui-language"
          :value="language.lang.value"
          :aria-label="$t('settings.interfaceLanguage')"
          aria-describedby="ui-language-help"
          class="h-10 w-48 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent"
          data-testid="ui-language"
          @change="onInterfaceLanguage"
        >
          <option v-for="code in LANGS" :key="code" :value="code">{{ $t(`language.${code}`) }}</option>
        </select>
        <p id="ui-language-help" class="text-xs text-ink-muted">{{ $t('settings.interfaceLanguageHelp') }}</p>
        <InlineError v-if="language.error.value">{{ language.error.value }}</InlineError>
      </section>

      <section aria-labelledby="projects-title" class="flex flex-col gap-3 rounded-xl border border-line bg-surface p-5">
        <h2 id="projects-title" class="text-base font-semibold text-ink">{{ $t('settings.projects') }}</h2>
        <ProjectsFolderForm :save-label="$t('projects.saveFolder')" />
      </section>

      <UpdateSection />

      <form class="flex flex-col gap-6 rounded-xl border border-line bg-surface p-5" novalidate @submit.prevent="submit">
        <h2 class="text-base font-semibold text-ink">{{ $t('settings.assistant') }}</h2>

        <div class="flex flex-col gap-1.5">
          <label for="language" class="text-sm font-medium text-ink">{{ $t('settings.answerLanguage') }}</label>
          <input id="language" v-model="form.language" list="languages" type="text" :placeholder="$t('settings.answerLanguagePlaceholder')" autocomplete="off" class="h-10 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" />
          <datalist id="languages"><option v-for="name in ANSWER_LANGUAGES" :key="name" :value="name" /></datalist>
          <p class="text-xs text-ink-muted">{{ $t('settings.answerLanguageHelp') }}</p>
        </div>

        <div class="flex flex-col gap-1.5">
          <label for="limit" class="text-sm font-medium text-ink">{{ $t('settings.dailyLimit') }}</label>
          <input id="limit" v-model.number="form.dailyTurnLimit" type="number" min="0" step="1" inputmode="numeric" class="h-10 w-32 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" />
          <p class="text-xs text-ink-muted">{{ $t('settings.dailyLimitHelp') }}</p>
        </div>

        <fieldset class="flex flex-col gap-2">
          <legend class="mb-1 text-sm font-medium text-ink">{{ $t('settings.changes') }}</legend>
          <label class="flex items-start gap-2.5 text-sm text-ink-muted">
            <input v-model="form.autoApprove" type="radio" name="auto" value="off" class="mt-1 accent-[var(--color-accent)]" />
            <span><span class="text-ink">{{ $t('settings.alwaysAsk') }}</span><br /><span class="text-xs text-ink-dim">{{ $t('settings.alwaysAskHelp') }}</span></span>
          </label>
          <label class="flex items-start gap-2.5 text-sm text-ink-muted">
            <input v-model="form.autoApprove" type="radio" name="auto" value="own-notes" class="mt-1 accent-[var(--color-accent)]" />
            <span><span class="text-ink">{{ $t('settings.ownNotes') }}</span><br /><span class="text-xs text-ink-dim">{{ $t('settings.ownNotesHelp') }}</span></span>
          </label>
        </fieldset>

        <div class="flex flex-col gap-1.5">
          <label for="keywords" class="text-sm font-medium text-ink">{{ $t('settings.keywords') }}</label>
          <input id="keywords" v-model="form.keywords" type="text" :placeholder="$t('settings.keywordsPlaceholder')" autocomplete="off" spellcheck="false" class="h-10 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" />
          <p class="text-xs text-ink-muted">{{ $t('settings.keywordsHelp') }}</p>
        </div>

        <InlineError v-if="error" data-testid="settings-error">{{ error }}</InlineError>
        <p v-if="saved" role="status" class="text-sm text-ok">{{ $t('settings.saved') }}</p>
        <AppButton type="submit" variant="primary" size="lg" class="w-fit" :disabled="saving || !settings">
          <AppSpinner v-if="saving" />
          {{ $t('settings.save') }}
        </AppButton>
      </form>
    </div>
  </main>
</template>
