/**
 * Keyboard shortcuts of the window, kept as text: `Mod+K`, `Mod+Shift+K`, `Alt+J`. `Mod` is Cmd on a Mac and Ctrl
 * elsewhere, so the same saved value works on both.
 */
export type ShortcutId = 'palette' | 'new-conversation';

export const SHORTCUT_IDS: readonly ShortcutId[] = ['palette', 'new-conversation'];

export const DEFAULT_SHORTCUTS: Readonly<Record<ShortcutId, string>> = { palette: 'Mod+K', 'new-conversation': 'Mod+N' };

export interface Chord {
  mod: boolean;
  shift: boolean;
  alt: boolean;
  /** One upper-case letter or digit. */
  key: string;
}

type KeyPress = Pick<KeyboardEvent, 'key' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>;

/** Combinations the system or the browser already use, and that typing in a field relies on: never taken. */
const RESERVED = new Set(['Mod+A', 'Mod+C', 'Mod+V', 'Mod+X', 'Mod+Z', 'Mod+Y', 'Mod+W', 'Mod+Q', 'Mod+R', 'Mod+T', 'Mod+F', 'Mod+L', 'Mod+S', 'Mod+P', 'Mod+H', 'Mod+M']);

export function parseChord(text: string): Chord | null {
  const parts = text.split('+');
  const key = parts.pop() ?? '';
  const flags = new Set(parts);
  if (!/^[A-Z0-9]$/.test(key) || flags.size !== parts.length) return null;
  if ([...flags].some((flag) => flag !== 'Mod' && flag !== 'Shift' && flag !== 'Alt')) return null;
  const chord = { mod: flags.has('Mod'), shift: flags.has('Shift'), alt: flags.has('Alt'), key };
  return isUsable(chord) ? chord : null;
}

/** A shortcut needs Cmd/Ctrl or Alt, or it would fire while typing; the reserved ones stay with the system. */
export const isUsable = (chord: Chord): boolean => (chord.mod || chord.alt) && !RESERVED.has(chordText(chord));

export function chordText(chord: Chord): string {
  return [chord.mod ? 'Mod' : '', chord.shift ? 'Shift' : '', chord.alt ? 'Alt' : '', chord.key].filter(Boolean).join('+');
}

/** The chord a key press makes, or null for a modifier alone or a key that is not a letter or a digit. */
export function chordOf(event: KeyPress): Chord | null {
  // With Alt held a Mac types another character (Alt+K is a symbol), and so does Shift on a digit of some layouts:
  // then the physical key names the letter or digit. A letter or digit the layout gives is always taken as it is.
  const code = (event as Partial<KeyboardEvent>).code ?? '';
  const fromCode = /^Key([A-Z])$/.exec(code)?.[1] ?? /^Digit([0-9])$/.exec(code)?.[1];
  const key = (/^[a-z0-9]$/i.test(event.key) ? event.key : (fromCode ?? event.key)).toUpperCase();
  if (!/^[A-Z0-9]$/.test(key)) return null;
  return { mod: event.metaKey || event.ctrlKey, shift: event.shiftKey, alt: event.altKey, key };
}

export function matches(event: KeyPress, shortcut: string): boolean {
  const wanted = parseChord(shortcut);
  const pressed = chordOf(event);
  return wanted !== null && pressed !== null && chordText(wanted) === chordText(pressed);
}

/** How a shortcut reads on screen: `⌘K` on a Mac, `Ctrl+K` elsewhere. */
export function displayShortcut(shortcut: string, mac: boolean): string {
  const chord = parseChord(shortcut);
  if (!chord) return shortcut;
  if (mac) return `${chord.alt ? '⌥' : ''}${chord.shift ? '⇧' : ''}${chord.mod ? '⌘' : ''}${chord.key}`;
  return [chord.mod ? 'Ctrl' : '', chord.alt ? 'Alt' : '', chord.shift ? 'Shift' : '', chord.key].filter(Boolean).join('+');
}

export const isMac = (): boolean => typeof navigator !== 'undefined' && /Mac/i.test(navigator.platform);

export type Shortcuts = Record<ShortcutId, string>;

export const SHORTCUTS_KEY = 'pouple.shortcuts';

type ShortcutStorage = Pick<Storage, 'getItem' | 'setItem'>;

/** The saved shortcuts over the defaults. Storage can be missing or hold garbage, and two actions never share a key. */
export function readShortcuts(storage: ShortcutStorage | null): Shortcuts {
  const shortcuts: Shortcuts = { ...DEFAULT_SHORTCUTS };
  try {
    const parsed: unknown = JSON.parse(storage?.getItem(SHORTCUTS_KEY) ?? '{}');
    if (typeof parsed !== 'object' || parsed === null) return shortcuts;
    for (const id of SHORTCUT_IDS) {
      const value = (parsed as Record<string, unknown>)[id];
      if (typeof value !== 'string') continue;
      const chord = parseChord(value);
      if (chord && !SHORTCUT_IDS.some((other) => other !== id && shortcuts[other] === chordText(chord))) shortcuts[id] = chordText(chord);
    }
  } catch {
    // The defaults then apply.
  }
  return shortcuts;
}

export function writeShortcuts(storage: ShortcutStorage | null, shortcuts: Shortcuts): void {
  try {
    storage?.setItem(SHORTCUTS_KEY, JSON.stringify(shortcuts));
  } catch {
    // The shortcuts then last until the page closes, which is fine.
  }
}

/** Why a chord cannot be given to an action, or null when it can. */
export function shortcutProblem(shortcuts: Shortcuts, id: ShortcutId, chord: Chord): 'unusable' | 'taken' | null {
  if (!isUsable(chord)) return 'unusable';
  const text = chordText(chord);
  return SHORTCUT_IDS.some((other) => other !== id && shortcuts[other] === text) ? 'taken' : null;
}
