import type { Tokens, Usage, UsageWindow } from '../api/types';

/** The names of the AIs, kept as they are in every language. */
const NAMES: Record<string, string> = { codex: 'Codex', claude: 'Claude' };

export type WindowKey = 'fiveHour' | 'week';

/** One AI's usage of the day: its own tokens and its windows. */
export interface UsageRow {
  provider: string;
  name: string;
  /** Absent when the AI used none today. */
  tokens?: Tokens;
  windows: { key: WindowKey; window: UsageWindow }[];
}

/** A window that started again since it was reported: what it said no longer holds. */
const expired = (window: UsageWindow, now: Date): boolean => window.resetsAt !== undefined && new Date(window.resetsAt).getTime() <= now.getTime();

/** All the tokens of a count, read and written. */
export const totalTokens = (tokens: Tokens): number => tokens.input + tokens.output;

/**
 * Each AI's usage, by name: its tokens of the day and its windows that still hold (five hours, then week). Tokens are
 * never added up across AIs, since each has its own plan. An AI with neither is left out.
 */
export function usageRows(usage: Usage | null, now: Date = new Date()): UsageRow[] {
  const providers = new Set([...Object.keys(usage?.limits ?? {}), ...Object.keys(usage?.tokensByAI ?? {})]);
  return [...providers]
    .map((provider) => {
      const limits = usage?.limits?.[provider];
      const tokens = usage?.tokensByAI?.[provider];
      return {
        provider,
        name: NAMES[provider] ?? provider,
        ...(tokens && totalTokens(tokens) > 0 ? { tokens } : {}),
        windows: (['fiveHour', 'week'] as const).flatMap((key) => {
          const window = limits?.[key];
          return window && !expired(window, now) ? [{ key, window }] : [];
        }),
      };
    })
    .filter((row) => row.tokens !== undefined || row.windows.length > 0)
    .sort((a, b) => a.name.localeCompare(b.name));
}

/** How full a window is: high from 75 %, critical from 90 % (10 % or less left). */
export type UsageLevel = 'normal' | 'high' | 'critical';

export function usageLevel(window: UsageWindow): UsageLevel {
  if (window.usedPercent >= 90) return 'critical';
  if (window.usedPercent >= 75) return 'high';
  return 'normal';
}
