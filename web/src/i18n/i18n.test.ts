import { describe, expect, it } from 'vitest';
import { en } from './en';
import { fr } from './fr';
import { i18n, setLanguage, t } from './index';
import { frenchTypography, type MessageTree } from './typography';

function flatten(tree: MessageTree, prefix = ''): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [key, value] of Object.entries(tree)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === 'string') out[path] = value;
    else Object.assign(out, flatten(value, path));
  }
  return out;
}

describe('catalogs', () => {
  const french = flatten(fr);
  const english = flatten(en);

  it('have exactly the same keys in French and in English', () => {
    expect(Object.keys(english).sort()).toEqual(Object.keys(french).sort());
  });

  it('have no empty value', () => {
    for (const [key, value] of [...Object.entries(french), ...Object.entries(english)]) expect(value.trim(), key).not.toBe('');
  });

  it('use the same placeholders in both languages', () => {
    const names = (text: string): string[] => [...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1] ?? '').sort();
    for (const key of Object.keys(french)) expect(names(english[key] ?? ''), key).toEqual(names(french[key] ?? ''));
  });

  it('have the same number of plural forms in both languages', () => {
    for (const key of Object.keys(french)) expect((english[key] ?? '').split(' | ').length, key).toBe((french[key] ?? '').split(' | ').length);
  });

  it('keep the French catalog free of straight apostrophes once loaded (curly ones only)', () => {
    setLanguage('fr');
    expect(t('welcome.titleLine1')).toBe('Salut, moi c’est Pouple.');
    setLanguage('en');
  });
});

describe('French typography', () => {
  it('puts a narrow no-break space before ? ! ; and a no-break space before : and inside guillemets', () => {
    expect(frenchTypography('On y va ?')).toBe('On y va ?');
    expect(frenchTypography('Attention : « test » !')).toBe('Attention : « test » !');
  });

  it('turns an apostrophe between letters into a curly one and leaves the rest', () => {
    expect(frenchTypography("l'écran d'accueil")).toBe('l’écran d’accueil');
    expect(frenchTypography('14:30')).toBe('14:30');
  });
});

describe('language switching', () => {
  it('changes every message at once, without a reload', () => {
    setLanguage('fr');
    expect(i18n.global.t('nav.settings')).toBe('Réglages');
    setLanguage('en');
    expect(i18n.global.t('nav.settings')).toBe('Settings');
  });

  it('pluralises with the active language', () => {
    expect(t('actions.lines', 1)).toBe('1 line');
    expect(t('actions.lines', 3)).toBe('3 lines');
    setLanguage('fr');
    expect(t('actions.lines', 0)).toBe('0 ligne');
    expect(t('actions.lines', 2)).toBe('2 lignes');
    setLanguage('en');
  });
});
