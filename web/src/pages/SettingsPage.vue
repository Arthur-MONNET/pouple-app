<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { errorMessage } from '../api/client';
import type { AutoApprove } from '../api/types';
import AppButton from '../components/AppButton.vue';
import AppSpinner from '../components/AppSpinner.vue';
import ProjectsFolderForm from '../components/ProjectsFolderForm.vue';
import { useSettings } from '../composables/useSettings';

const { settings, loadError, load, save } = useSettings();

const form = reactive({ language: '', dailyTurnLimit: 0, autoApprove: 'off' as AutoApprove, keywords: '' });
const saving = ref(false);
const error = ref<string | null>(null);
const saved = ref(false);
const LANGUAGES = ['English', 'French', 'Spanish', 'German', 'Italian', 'Portuguese'];

void load();
watch(settings, (value) => {
  if (!value) return;
  form.language = value.language;
  form.dailyTurnLimit = value.dailyTurnLimit;
  form.autoApprove = value.autoApprove;
  form.keywords = value.keywords.join(', ');
}, { once: true });

const keywordList = computed(() => form.keywords.split(/[,\n]/).map((word) => word.trim()).filter(Boolean));

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
        <h1 class="text-title font-semibold text-ink">Settings</h1>
        <p class="text-sm text-ink-muted">How Pouple works with your projects and how much it may do on its own.</p>
      </header>
      <p v-if="loadError" role="alert" class="rounded-lg border border-fail/40 bg-fail/10 px-3 py-2 text-sm text-fail">The settings could not be read: {{ loadError }}</p>

      <section aria-labelledby="projects-title" class="flex flex-col gap-3 rounded-xl border border-line bg-surface p-5">
        <h2 id="projects-title" class="text-base font-semibold text-ink">Projects</h2>
        <ProjectsFolderForm save-label="Save folder" />
      </section>

      <form class="flex flex-col gap-6 rounded-xl border border-line bg-surface p-5" novalidate @submit.prevent="submit">
        <h2 class="text-base font-semibold text-ink">Assistant</h2>

        <div class="flex flex-col gap-1.5">
          <label for="language" class="text-sm font-medium text-ink">Language of answers</label>
          <input id="language" v-model="form.language" list="languages" type="text" placeholder="System language" autocomplete="off" class="h-10 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" />
          <datalist id="languages"><option v-for="language in LANGUAGES" :key="language" :value="language" /></datalist>
          <p class="text-xs text-ink-dim">The language of summaries and questions. Leave empty to use your system language.</p>
        </div>

        <div class="flex flex-col gap-1.5">
          <label for="limit" class="text-sm font-medium text-ink">Daily Codex limit</label>
          <input id="limit" v-model.number="form.dailyTurnLimit" type="number" min="0" step="1" inputmode="numeric" class="h-10 w-32 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" />
          <p class="text-xs text-ink-dim">The most Codex turns Pouple may start in a day. 0 means no limit.</p>
        </div>

        <fieldset class="flex flex-col gap-2">
          <legend class="mb-1 text-sm font-medium text-ink">Changes proposed by Codex</legend>
          <label class="flex items-start gap-2.5 text-sm text-ink-muted">
            <input v-model="form.autoApprove" type="radio" name="auto" value="off" class="mt-1 accent-[var(--color-accent)]" />
            <span><span class="text-ink">Always ask me</span><br /><span class="text-xs text-ink-dim">Nothing is changed in your code before you approve it.</span></span>
          </label>
          <label class="flex items-start gap-2.5 text-sm text-ink-muted">
            <input v-model="form.autoApprove" type="radio" name="auto" value="own-notes" class="mt-1 accent-[var(--color-accent)]" />
            <span><span class="text-ink">Apply changes proposed from my notes to self</span><br /><span class="text-xs text-ink-dim">Only for notes you wrote to yourself in Slack. Requests from other people still need your approval.</span></span>
          </label>
        </fieldset>

        <div class="flex flex-col gap-1.5">
          <label for="keywords" class="text-sm font-medium text-ink">Watched words</label>
          <input id="keywords" v-model="form.keywords" type="text" placeholder="deploy, incident, invoice" autocomplete="off" spellcheck="false" class="h-10 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" />
          <p class="text-xs text-ink-dim">Separate words with commas. A message containing one becomes a request wherever it is posted.</p>
        </div>

        <p v-if="error" role="alert" class="text-sm text-fail" data-testid="settings-error">{{ error }}</p>
        <p v-if="saved" role="status" class="text-sm text-ok">Settings saved.</p>
        <AppButton type="submit" variant="primary" size="lg" class="w-fit" :disabled="saving || !settings">
          <AppSpinner v-if="saving" />
          Save settings
        </AppButton>
      </form>
    </div>
  </main>
</template>
