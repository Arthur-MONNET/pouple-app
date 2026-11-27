import { ref } from 'vue';
import { browserStorage } from '../lib/topicFolds';
import { chordText, DEFAULT_SHORTCUTS, readShortcuts, shortcutProblem, writeShortcuts, type Chord, type ShortcutId, type Shortcuts } from '../lib/shortcuts';

// One set for the whole window: the settings change it and the shell reads it, with no reload.
const shortcuts = ref<Shortcuts>(readShortcuts(browserStorage()));

/** The shortcuts of the window and the way to change them. A change that is refused says why and keeps the old one. */
export function useShortcuts() {
  function set(id: ShortcutId, chord: Chord): 'unusable' | 'taken' | null {
    const problem = shortcutProblem(shortcuts.value, id, chord);
    if (problem) return problem;
    shortcuts.value = { ...shortcuts.value, [id]: chordText(chord) };
    writeShortcuts(browserStorage(), shortcuts.value);
    return null;
  }

  function reset(id: ShortcutId): 'taken' | null {
    const taken = Object.entries(shortcuts.value).some(([other, text]) => other !== id && text === DEFAULT_SHORTCUTS[id]);
    if (taken) return 'taken';
    shortcuts.value = { ...shortcuts.value, [id]: DEFAULT_SHORTCUTS[id] };
    writeShortcuts(browserStorage(), shortcuts.value);
    return null;
  }

  return { shortcuts, set, reset };
}
