import type { MachineLevel, MachineState, Tokens, Usage, UsageWindow } from '../api/types';

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

/** The name of an AI, kept as it is in every language. */
export const providerName = (provider: string): string => NAMES[provider] ?? provider;

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

export type GaugeKey = 'disk' | 'memory';

/** One resource of the Mac, as a bar like the AIs' windows. */
export interface MachineGauge {
  key: GaugeKey;
  /** From 0 to 100. */
  usedPercent: number;
  level: MachineLevel;
}

/** The disk, then the memory; a part the core could not read is left out. */
export function machineGauges(machine: MachineState | undefined): MachineGauge[] {
  const gauges: MachineGauge[] = [];
  const disk = machine?.disk;
  if (disk && disk.totalBytes > 0) {
    gauges.push({ key: 'disk', usedPercent: ((disk.totalBytes - disk.freeBytes) / disk.totalBytes) * 100, level: disk.level });
  }
  const memory = machine?.memory;
  if (memory) gauges.push({ key: 'memory', usedPercent: memory.usedPercent, level: memory.level });
  return gauges;
}

/** One figure of the compact usage line: an AI's fullest window, or a resource of the Mac that needs attention. */
export interface UsageHeadline {
  key: string;
  name: string;
  /** From 0 to 100 for an AI's window or the memory; absent for the disk, which shows its free space. */
  usedPercent?: number;
  /** The disk's free bytes. */
  freeBytes?: number;
  level: UsageLevel;
}

const LEVELS: UsageLevel[] = ['normal', 'high', 'critical'];
const worst = (levels: UsageLevel[]): UsageLevel => LEVELS[Math.max(0, ...levels.map((level) => LEVELS.indexOf(level)))] ?? 'normal';

/**
 * What the sidebar shows in one line: each AI by its fullest window that still holds, then the disk, and the memory only
 * when it runs short. The full detail stays one click away.
 */
export function usageHeadline(usage: Usage | null, now: Date = new Date()): UsageHeadline[] {
  const out: UsageHeadline[] = usageRows(usage, now)
    .filter((row) => row.windows.length > 0)
    .map((row) => {
      const fullest = row.windows.reduce((a, b) => (b.window.usedPercent > a.window.usedPercent ? b : a));
      return { key: row.provider, name: row.name, usedPercent: fullest.window.usedPercent, level: usageLevel(fullest.window) };
    });
  const { disk, memory } = usage?.machine ?? {};
  if (disk && disk.totalBytes > 0) out.push({ key: 'disk', name: 'disk', freeBytes: disk.freeBytes, level: disk.level });
  if (memory && memory.level !== 'normal') out.push({ key: 'memory', name: 'memory', usedPercent: memory.usedPercent, level: memory.level });
  return out;
}

/** The most pressing level of the line, so the folded line itself can warn. */
export const headlineLevel = (items: UsageHeadline[]): UsageLevel => worst(items.map((item) => item.level));
