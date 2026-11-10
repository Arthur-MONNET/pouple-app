<script setup lang="ts">
import { computed, reactive, watch } from 'vue';
import { useRoute } from 'vue-router';
import type { AutoApprove, SettingsPatch } from '../api/types';
import AiSection from '../components/AiSection.vue';
import InlineError from '../components/InlineError.vue';
import SettingsRow from '../components/SettingsRow.vue';
import SettingsSection from '../components/SettingsSection.vue';
import ToggleSwitch from '../components/ToggleSwitch.vue';
import ProjectsFolderForm from '../components/ProjectsFolderForm.vue';
import UpdateSection from '../components/UpdateSection.vue';
import SaveStatus from '../components/SaveStatus.vue';
import { useAutoSave } from '../composables/useAutoSave';
import { useLanguage } from '../composables/useLanguage';
import { useSettings } from '../composables/useSettings';
import { LANGS, type Lang } from '../i18n';

const { settings, loadError, load, save } = useSettings();
const language = useLanguage();
const route = useRoute();

/** The categories of the menu, in order. The one shown is the address's hash, so the badge of the top bar can open Updates. */
const CATEGORIES = ['general', 'projects', 'ai', 'assistant', 'updates'] as const;
type Category = (typeof CATEGORIES)[number];
const category = computed<Category>(() => CATEGORIES.find((item) => `#${item}` === route.hash) ?? 'general');

const form = reactive({ language: '', dailyTurnLimit: 0, autoApprove: 'off' as AutoApprove, keywords: '' });
const autosave = useAutoSave();
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

/** Every change applies at once: the field that changed is the only one sent. */
const apply = (patch: SettingsPatch): Promise<boolean> => autosave.run(() => save(patch));

const ownNotes = computed(() => form.autoApprove === 'own-notes');
function setOwnNotes(on: boolean): void {
  form.autoApprove = on ? 'own-notes' : 'off';
  void apply({ autoApprove: form.autoApprove });
}

function saveDailyLimit(): void {
  form.dailyTurnLimit = Math.max(0, Math.floor(Number(form.dailyTurnLimit) || 0));
  void apply({ dailyTurnLimit: form.dailyTurnLimit });
}

function onInterfaceLanguage(event: Event): void {
  void language.switchTo((event.target as HTMLSelectElement).value as Lang);
}
</script>

<template>
  <main class="flex min-h-0 flex-1 flex-col min-[760px]:flex-row">
    <nav :aria-label="$t('settings.nav.label')" class="flex shrink-0 gap-1 overflow-x-auto border-b border-line-soft px-4 py-3 min-[760px]:w-56 min-[760px]:flex-col min-[760px]:overflow-y-auto min-[760px]:border-b-0 min-[760px]:border-r min-[760px]:py-5">
      <RouterLink
        v-for="item in CATEGORIES"
        :key="item"
        :to="{ path: '/settings', hash: `#${item}` }"
        replace
        class="whitespace-nowrap rounded-lg px-3 py-2 text-sm transition hover:bg-raised hover:text-ink motion-reduce:transition-none"
        :class="{ 'bg-selected font-medium text-ink': category === item, 'text-ink-muted': category !== item }"
        :aria-current="category === item ? 'page' : undefined"
        :data-testid="`settings-nav-${item}`"
      >
        {{ $t(`settings.nav.${item}`) }}
      </RouterLink>
    </nav>

    <div class="min-h-0 flex-1 overflow-y-auto">
      <div class="mx-auto flex max-w-2xl flex-col gap-6 px-5 py-6 min-[760px]:px-8">
        <header>
          <h1 class="text-title font-semibold text-ink">{{ $t('settings.title') }}</h1>
          <p class="text-sm text-ink-muted">{{ $t('settings.intro') }}</p>
        </header>
        <InlineError v-if="loadError" box>{{ $t('settings.loadError', { error: loadError }) }}</InlineError>

        <SettingsSection v-show="category === 'general'" :title="$t('settings.general')">
          <SettingsRow :label="$t('settings.interfaceLanguage')" :description="$t('settings.interfaceLanguageHelp')" label-for="ui-language">
            <select
              id="ui-language"
              :value="language.lang.value"
              class="h-10 w-48 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent"
              data-testid="ui-language"
              @change="onInterfaceLanguage"
            >
              <option v-for="code in LANGS" :key="code" :value="code">{{ $t(`language.${code}`) }}</option>
            </select>
            <InlineError v-if="language.error.value">{{ language.error.value }}</InlineError>
          </SettingsRow>
        </SettingsSection>

        <SettingsSection v-show="category === 'projects'" :title="$t('settings.projects')">
          <div class="py-4">
            <ProjectsFolderForm autosave />
          </div>
        </SettingsSection>

        <div v-show="category === 'ai'"><AiSection /></div>

        <SettingsSection v-show="category === 'assistant'" :title="$t('settings.assistant')">
          <SettingsRow :label="$t('settings.answerLanguage')" :description="$t('settings.answerLanguageHelp')" label-for="language">
            <input id="language" v-model="form.language" list="languages" type="text" :placeholder="$t('settings.answerLanguagePlaceholder')" autocomplete="off" class="h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent min-[560px]:w-56" @change="apply({ language: form.language.trim() })" />
            <datalist id="languages"><option v-for="name in ANSWER_LANGUAGES" :key="name" :value="name" /></datalist>
          </SettingsRow>

          <SettingsRow :label="$t('settings.dailyLimit')" :description="$t('settings.dailyLimitHelp')" label-for="limit">
            <input id="limit" v-model.number="form.dailyTurnLimit" type="number" min="0" step="1" inputmode="numeric" class="h-10 w-28 rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" @change="saveDailyLimit" />
          </SettingsRow>

          <div class="py-4">
            <ToggleSwitch :model-value="ownNotes" :label="$t('settings.ownNotes')" :description="$t('settings.ownNotesHelp')" data-testid="auto-approve-own-notes" @update:model-value="setOwnNotes" />
          </div>

          <SettingsRow :label="$t('settings.keywords')" :description="$t('settings.keywordsHelp')" label-for="keywords" stacked>
            <input id="keywords" v-model="form.keywords" type="text" :placeholder="$t('settings.keywordsPlaceholder')" autocomplete="off" spellcheck="false" class="h-10 w-full rounded-lg border border-line-strong bg-canvas px-3 text-sm text-ink outline-none focus:border-accent" @change="apply({ keywords: keywordList })" />
          </SettingsRow>

          <template #footer><SaveStatus :saved="autosave.saved.value" :error="autosave.error.value" /></template>
        </SettingsSection>

        <div v-show="category === 'updates'"><UpdateSection /></div>
      </div>
    </div>
  </main>
</template>
