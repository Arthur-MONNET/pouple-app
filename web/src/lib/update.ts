import type { UpdateInfo } from '../api/types';

/** What the Updates section shows. One kind at a time, decided here so the template only draws. */
export type UpdateView = 'unknown' | 'unsupported' | 'checking' | 'available' | 'upToDate';

/** An update is offered only when the install can be updated and the core found something newer. */
export const isUpdateAvailable = (info: UpdateInfo | null): boolean => Boolean(info?.supported && info.available);

export function updateView(info: UpdateInfo | null, checking: boolean): UpdateView {
  if (!info) return checking ? 'checking' : 'unknown';
  if (!info.supported) return 'unsupported';
  if (checking || info.checking) return 'checking';
  return info.available ? 'available' : 'upToDate';
}

/** The first commits are listed; the rest is summarised, so a long history stays readable. */
export function splitChanges(info: UpdateInfo, shown = 6): { shown: string[]; hidden: number } {
  const total = Math.max(info.behind, info.changes.length);
  const visible = info.changes.slice(0, shown);
  return { shown: visible, hidden: Math.max(0, total - visible.length) };
}

/** Seven characters are enough to recognise a commit. */
export const shortCommit = (commit: string): string => commit.slice(0, 7);

/** The two commands of the manual update, for a browser that is not inside the macOS shell. */
export const MANUAL_COMMANDS = ['cd pouple-app && git pull --ff-only', 'bash scripts/install-local.sh'] as const;

// ---------------------------------------------------------------------------------------------------------------
// Native bridge
// ---------------------------------------------------------------------------------------------------------------

export interface UpdateBridge {
  postMessage(message: unknown): void;
}

/** The handler the macOS shell exposes in its web view, or `null` in a plain browser. */
export function findUpdateBridge(target: { webkit?: Window['webkit'] } | undefined = typeof window === 'undefined' ? undefined : window): UpdateBridge | null {
  const handler = target?.webkit?.messageHandlers?.pouple;
  return handler && typeof handler.postMessage === 'function' ? handler : null;
}

/**
 * Asks the shell to run the update. The shell quits the app and gives no answer, so the message goes out at most once:
 * a second click (or a second dialog confirmation) does nothing.
 */
export function createUpdateLauncher(getBridge: () => UpdateBridge | null = findUpdateBridge): { launch(): boolean; readonly launched: boolean } {
  let launched = false;
  return {
    launch() {
      if (launched) return false;
      const bridge = getBridge();
      if (!bridge) return false;
      launched = true;
      bridge.postMessage({ type: 'update' });
      return true;
    },
    get launched() {
      return launched;
    },
  };
}

// ---------------------------------------------------------------------------------------------------------------
// Polling
// ---------------------------------------------------------------------------------------------------------------

export const UPDATE_POLL_MS = 30 * 60 * 1000;

export interface Scheduler {
  setInterval(handler: () => void, ms: number): unknown;
  clearInterval(handle: unknown): void;
}

/** Runs `task` now and then every `intervalMs`, whatever it returns or throws. Returns the function that stops it. */
export function startPolling(task: () => Promise<unknown> | unknown, intervalMs = UPDATE_POLL_MS, scheduler: Scheduler = { setInterval: (h, ms) => setInterval(h, ms), clearInterval: (h) => clearInterval(h as ReturnType<typeof setInterval>) }): () => void {
  const run = (): void => {
    try {
      void Promise.resolve(task()).catch(() => undefined);
    } catch {
      // A failed tick is ignored: the next one tries again.
    }
  };
  run();
  const handle = scheduler.setInterval(run, intervalMs);
  return () => scheduler.clearInterval(handle);
}
