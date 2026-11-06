import { computed, ref } from 'vue';
import { createI18n } from 'vue-i18n';
import { en } from './en';
import { fr } from './fr';
import { frenchTypography, mapMessages } from './typography';

export type Lang = 'fr' | 'en';
export const LANGS: readonly Lang[] = ['fr', 'en'];
export const DEFAULT_LANG: Lang = 'fr';

export const isLang = (value: unknown): value is Lang => value === 'fr' || value === 'en';

/** French is the source of truth and the default; the language the core reports replaces it at startup. */
export const i18n = createI18n({
  legacy: false,
  locale: DEFAULT_LANG,
  fallbackLocale: DEFAULT_LANG,
  messages: { fr: mapMessages(fr, frenchTypography), en },
  // In French, zero is singular ("0 ligne").
  pluralRules: { fr: (choice: number, choicesLength: number) => (choicesLength === 2 ? (choice < 2 ? 0 : 1) : Math.min(choice, choicesLength - 1)) },
  missingWarn: false,
  fallbackWarn: false,
});

const locale = i18n.global.locale;

/** The active language, reactive. */
export const lang = computed<Lang>(() => (isLang(locale.value) ? locale.value : DEFAULT_LANG));

/** Bumped once the core has switched language, so what came from the API (already translated there) is read again. */
export const languageEpoch = ref(0);

export function setLanguage(next: Lang): void {
  locale.value = next;
  if (typeof document !== 'undefined') document.documentElement.lang = next;
}

/** For code outside components (libs, stores): the same `t` the templates use. */
export const t = i18n.global.t;
