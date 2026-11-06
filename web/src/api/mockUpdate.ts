import { ApiError } from './client';
import type { Lang } from '../i18n';
import type { UpdateInfo } from './types';

export type UpdateScenario = 'uptodate' | 'available' | 'unsupported' | 'error' | 'checking';

export const UPDATE_SCENARIOS: readonly UpdateScenario[] = ['uptodate', 'available', 'unsupported', 'error', 'checking'];

export const isUpdateScenario = (value: unknown): value is UpdateScenario => UPDATE_SCENARIOS.includes(value as UpdateScenario);

const TEXT: Record<Lang, { unsupported: string; blocker: string; error: string; changes: string[] }> = {
  fr: {
    unsupported: "Pouple n'a pas été installé depuis le code source : la mise à jour se fait par la même voie que l'installation.",
    blocker: 'Le dossier du code contient des modifications locales : la mise à jour peut échouer tant que tu ne les as pas rangées (commit ou stash).',
    error: "Impossible de joindre le dépôt pour l'instant. Vérifie ta connexion et réessaie.",
    changes: [
      'feat: réponses plus courtes dans les résumés',
      'fix: la pastille de statut Slack se met à jour après un redémarrage',
      'feat: bouton de mise à jour dans les Réglages',
      'fix: les pièces jointes sans type MIME sont acceptées',
      'chore: nettoyage des journaux du lanceur',
    ],
  },
  en: {
    unsupported: 'Pouple was not installed from source: it is updated the same way it was installed.',
    blocker: 'The code folder has local changes: the update may fail until you set them aside (commit or stash).',
    error: 'Cannot reach the repository right now. Check your connection and try again.',
    changes: [
      'feat: shorter answers in summaries',
      'fix: the Slack status dot refreshes after a restart',
      'feat: update button in Settings',
      'fix: attachments without a MIME type are accepted',
      'chore: tidy the launcher logs',
    ],
  },
};

const INSTALLED = { version: '0.1.0', commit: 'abc1234', installedAt: '2026-10-28T09:12:00Z' };

/** The update answer of each scenario; `checkedAt` is the moment the mock "looked". */
export function mockUpdateInfo(scenario: UpdateScenario, lang: Lang, now: Date = new Date()): UpdateInfo {
  const text = TEXT[lang];
  const base: UpdateInfo = {
    supported: true,
    reason: '',
    message: '',
    installed: { ...INSTALLED },
    latest: { commit: INSTALLED.commit },
    available: false,
    behind: 0,
    changes: [],
    blockers: [],
    checkedAt: new Date(now.getTime() - 4 * 60_000).toISOString(),
    checking: false,
    error: null,
  };
  switch (scenario) {
    case 'available':
      return { ...base, latest: { commit: 'def5678' }, available: true, behind: text.changes.length, changes: [...text.changes], blockers: [{ code: 'local-changes', message: text.blocker }] };
    case 'unsupported':
      return { ...base, supported: false, reason: 'not-installed-from-source', message: text.unsupported, checkedAt: null };
    case 'error':
      return { ...base, error: text.error };
    case 'checking':
      return { ...base, checking: true };
    default:
      return base;
  }
}

/** Where a forced check ends up: "checking" resolves to the available state, "error" refuses. */
export function mockCheckResult(scenario: UpdateScenario, lang: Lang): UpdateInfo {
  if (scenario === 'error') throw new ApiError(502, TEXT[lang].error);
  return mockUpdateInfo(scenario === 'checking' ? 'available' : scenario, lang);
}

/** Stands in for the macOS shell: it records the message and, like the shell, gives no answer. */
export function installMockBridge(target: Window = window, onMessage: (message: unknown) => void = (message) => console.info('[mock bridge] pouple.postMessage', message)): void {
  if (target.webkit?.messageHandlers?.pouple) return;
  target.webkit = { messageHandlers: { pouple: { postMessage: onMessage } } };
}
