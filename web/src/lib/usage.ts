import type { Tokens, Usage, UsageWindow } from '../api/types';

/** The names of the AIs, kept as they are in every language. */
const NAMES: Record<string, string> = { codex: 'Codex', claude: 'Claude' };

export type WindowKey = 'fiveHour' | 'week';

/** One AI's usage windows, as a row of the status line. */
export interface LimitRow {
  provider: string;
  name: string;
  windows: { key: WindowKey; window: UsageWindow }[];
}

/** A window that started again since it was reported: what it said no longer holds. */
const expired = (window: UsageWindow, now: Date): boolean => window.resetsAt !== undefined && new Date(window.resetsAt).getTime() <= now.getTime();

/**
 * The AIs that reported their usage windows, by name, each with the windows it reported (five hours, then week) and
 * that still hold: the snapshot may be older than a reset.
 */
export function limitRows(usage: Usage | null, now: Date = new Date()): LimitRow[] {
  return Object.entries(usage?.limits ?? {})
    .map(([provider, limits]) => ({
      provider,
      name: NAMES[provider] ?? provider,
      windows: (['fiveHour', 'week'] as const).flatMap((key) => {
        const window = limits[key];
        return window && !expired(window, now) ? [{ key, window }] : [];
      }),
    }))
    .filter((row) => row.windows.length > 0)
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** All the tokens of a count, read and written. */
export const totalTokens = (tokens: Tokens): number => tokens.input + tokens.output;

/** A window nearly used up: from 80 %, the status line warns. */
export const nearlyUsed = (window: UsageWindow): boolean => window.usedPercent >= 80;
