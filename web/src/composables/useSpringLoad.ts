import { getCurrentInstance, onBeforeUnmount, ref, type Ref } from 'vue';
import { hasFiles } from '../lib/dropFiles';

/** How long a dragged file must stay over a target before it blinks. */
export const HOLD_MS = 900;
/** One phase of the blink: lit, dark, lit, dark. */
export const BLINK_PHASE_MS = 130;
const BLINK_PHASES = 4;

export interface SpringLoad {
  /** The target being opened, or null. */
  target: Ref<string | null>;
  /** Whether the target is lit right now: it blinks twice before it opens. */
  lit: Ref<boolean>;
  enter(key: string, event: DragEvent): void;
  leave(key: string, event: DragEvent): void;
  /** The drag ended, wherever it did: forget everything. */
  reset(): void;
}

/**
 * Opens a target when a dragged file stays over it, like the spring-loaded folders of the Finder: the target blinks
 * twice, then `open` runs, so the file can then be dropped inside it. Only a drag that carries files counts.
 *
 * `canOpen` says whether a target still needs opening (a conversation already open does not); `reducedMotion` turns the
 * blink into a steady highlight.
 */
export function useSpringLoad(
  open: (key: string) => void,
  canOpen: (key: string) => boolean,
  reducedMotion: () => boolean = () => false,
): SpringLoad {
  const target = ref<string | null>(null);
  const lit = ref(false);
  /** Entering a child fires dragenter before the parent's dragleave: each target counts how many it is inside. */
  const depth = new Map<string, number>();
  let timers: ReturnType<typeof setTimeout>[] = [];

  function cancel(): void {
    for (const timer of timers) clearTimeout(timer);
    timers = [];
    target.value = null;
    lit.value = false;
  }

  function start(key: string): void {
    cancel();
    target.value = key;
    if (reducedMotion()) {
      lit.value = true;
      timers.push(setTimeout(() => finish(key), HOLD_MS + BLINK_PHASE_MS * BLINK_PHASES));
      return;
    }
    for (let phase = 0; phase < BLINK_PHASES; phase += 1) {
      timers.push(setTimeout(() => (lit.value = phase % 2 === 0), HOLD_MS + BLINK_PHASE_MS * phase));
    }
    timers.push(setTimeout(() => finish(key), HOLD_MS + BLINK_PHASE_MS * BLINK_PHASES));
  }

  function finish(key: string): void {
    cancel();
    open(key);
  }

  function enter(key: string, event: DragEvent): void {
    if (!hasFiles(event)) return;
    const inside = (depth.get(key) ?? 0) + 1;
    depth.set(key, inside);
    if (inside === 1 && canOpen(key)) start(key);
  }

  function leave(key: string, event: DragEvent): void {
    if (!hasFiles(event)) return;
    const inside = (depth.get(key) ?? 1) - 1;
    if (inside > 0) {
      depth.set(key, inside);
      return;
    }
    depth.delete(key);
    if (target.value === key) cancel();
  }

  function reset(): void {
    depth.clear();
    cancel();
  }

  if (getCurrentInstance()) onBeforeUnmount(reset);
  return { target, lit, enter, leave, reset };
}
