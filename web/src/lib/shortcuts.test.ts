import { describe, expect, it } from 'vitest';
import { chordOf, DEFAULT_SHORTCUTS, displayShortcut, matches, parseChord, readShortcuts, shortcutProblem, SHORTCUTS_KEY } from './shortcuts';

const press = (overrides: Partial<KeyboardEvent>): Pick<KeyboardEvent, 'key' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'> =>
  ({ key: 'k', metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...overrides }) as KeyboardEvent;

describe('chords', () => {
  it('reads a saved chord and refuses what is not one', () => {
    expect(parseChord('Mod+Shift+K')).toEqual({ mod: true, shift: true, alt: false, key: 'K' });
    for (const bad of ['', 'K', 'Mod+', 'Mod+KK', 'Mod+Mod+K', 'Ctrl+K', 'Mod+Enter', 'Shift+K']) expect(parseChord(bad), bad).toBeNull();
  });

  it('never gives away the keys of the system and of typing', () => {
    for (const reserved of ['Mod+C', 'Mod+V', 'Mod+Z', 'Mod+W', 'Mod+Q']) expect(parseChord(reserved), reserved).toBeNull();
    expect(parseChord('Alt+K')).not.toBeNull();
  });
});

describe('matching a key press', () => {
  it('accepts Cmd or Ctrl for Mod, case ignored', () => {
    expect(matches(press({ metaKey: true }), 'Mod+K')).toBe(true);
    expect(matches(press({ ctrlKey: true, key: 'K' }), 'Mod+K')).toBe(true);
  });

  it('wants exactly the modifiers of the chord', () => {
    expect(matches(press({}), 'Mod+K')).toBe(false);
    expect(matches(press({ metaKey: true, shiftKey: true }), 'Mod+K')).toBe(false);
    expect(matches(press({ metaKey: true, key: 'j' }), 'Mod+K')).toBe(false);
    expect(matches(press({ metaKey: true, shiftKey: true }), 'Mod+Shift+K')).toBe(true);
  });

  it('reads the physical key when Alt changes the character', () => {
    expect(chordOf({ ...press({ altKey: true, key: '˚' }), code: 'KeyK' } as KeyboardEvent)).toEqual({ mod: false, shift: false, alt: true, key: 'K' });
  });

  it('is not a chord for a modifier alone or a key that is not a letter or a digit', () => {
    expect(chordOf(press({ key: 'Meta', metaKey: true }))).toBeNull();
    expect(chordOf(press({ key: 'Enter', metaKey: true }))).toBeNull();
  });
});

describe('display', () => {
  it('writes ⌘K on a Mac and Ctrl+K elsewhere', () => {
    expect(displayShortcut('Mod+K', true)).toBe('⌘K');
    expect(displayShortcut('Mod+Shift+K', true)).toBe('⇧⌘K');
    expect(displayShortcut('Mod+Shift+K', false)).toBe('Ctrl+Shift+K');
  });
});

describe('saved shortcuts', () => {
  const store = (value: string | null) => ({ getItem: () => value, setItem: () => undefined });

  it('uses the defaults with nothing saved, with garbage, or with no storage', () => {
    expect(readShortcuts(null)).toEqual(DEFAULT_SHORTCUTS);
    expect(readShortcuts(store('not json'))).toEqual(DEFAULT_SHORTCUTS);
    expect(readShortcuts(store('{"palette":42,"new-conversation":"Mod+C"}'))).toEqual(DEFAULT_SHORTCUTS);
  });

  it('keeps a valid saved shortcut and refuses one that two actions would share', () => {
    expect(readShortcuts(store(JSON.stringify({ palette: 'Mod+J' })))).toEqual({ palette: 'Mod+J', 'new-conversation': 'Mod+N' });
    expect(readShortcuts(store(JSON.stringify({ palette: 'Mod+N' }))).palette).toBe('Mod+K');
    expect(SHORTCUTS_KEY).toBe('pouple.shortcuts');
  });

  it('says why a chord is refused', () => {
    expect(shortcutProblem(DEFAULT_SHORTCUTS, 'palette', parseChord('Mod+N')!)).toBe('taken');
    expect(shortcutProblem(DEFAULT_SHORTCUTS, 'palette', parseChord('Mod+K')!)).toBeNull();
    expect(shortcutProblem(DEFAULT_SHORTCUTS, 'palette', { mod: false, shift: true, alt: false, key: 'K' })).toBe('unusable');
  });
});
