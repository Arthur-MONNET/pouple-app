import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';

const ROOT = join(__dirname, '..');

function vueFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? vueFiles(path) : path.endsWith('.vue') ? [path] : [];
  });
}

/** The template of a single-file component, without its script and style. */
const templateOf = (source: string): string => /<template>([\s\S]*)<\/template>\s*(?:<script[\s\S]*)?$/.exec(source.replace(/<style[\s\S]*?<\/style>/g, ''))?.[1] ?? '';

const WORD = /[\p{L}]{3,}/u;
/** Words that are the same in every language: the product name and technical tokens. */
const ALLOWED = new Set(['Pouple', 'Slack', 'Codex', 'Cmd']);

/** Static text a reader would see: text nodes and plain (not bound) placeholder, title, aria-label, alt, label attributes. */
export function hardcodedText(template: string): string[] {
  const found: string[] = [];
  const withoutMustaches = template.replace(/\{\{[\s\S]*?\}\}/g, ' ').replace(/&[a-z]+;|&#\d+;/gi, ' ');
  for (const match of withoutMustaches.matchAll(/>([^<>]+)</g)) {
    const text = (match[1] ?? '').trim();
    if (WORD.test(text) && ![...text.split(/\s+/)].every((word) => ALLOWED.has(word) || !WORD.test(word))) found.push(text);
  }
  for (const match of template.matchAll(/\s(placeholder|title|aria-label|alt|label)="([^"]+)"/g)) {
    const value = match[2] ?? '';
    if (WORD.test(value) && !ALLOWED.has(value)) found.push(`${match[1] ?? ''}="${value}"`);
  }
  return found;
}

describe('no hard-coded user-visible text in templates', () => {
  for (const file of vueFiles(ROOT)) {
    const name = file.slice(ROOT.length + 1);
    it(name, () => {
      expect(hardcodedText(templateOf(readFileSync(file, 'utf8')))).toEqual([]);
    });
  }

  it('catches a sentence written straight in a template (the heuristic itself)', () => {
    expect(hardcodedText('<div><p>Bienvenue sur Pouple</p><input placeholder="Ton jeton" /></div>')).toEqual(['Bienvenue sur Pouple', 'placeholder="Ton jeton"']);
    expect(hardcodedText("<p>{{ $t('a.b') }}</p><button :aria-label=\"$t('x')\">{{ n }}</button>")).toEqual([]);
  });
});
