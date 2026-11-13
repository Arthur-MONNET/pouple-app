import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BLINK_PHASE_MS, HOLD_MS, useSpringLoad } from './useSpringLoad';

const dragOf = (types: string[]): DragEvent => ({ dataTransfer: { types } }) as unknown as DragEvent;
const files = dragOf(['Files']);
const total = HOLD_MS + BLINK_PHASE_MS * 4;

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe('a file held over a target', () => {
  it('blinks twice, then opens it', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => true);
    spring.enter('a', files);

    vi.advanceTimersByTime(HOLD_MS - 1);
    expect(spring.lit.value).toBe(false);
    const seen: boolean[] = [];
    for (let phase = 0; phase < 4; phase += 1) {
      vi.advanceTimersByTime(phase === 0 ? 1 : BLINK_PHASE_MS);
      seen.push(spring.lit.value);
    }
    expect(seen).toEqual([true, false, true, false]);
    expect(open).not.toHaveBeenCalled();

    vi.advanceTimersByTime(BLINK_PHASE_MS);
    expect(open).toHaveBeenCalledExactlyOnceWith('a');
    expect(spring.target.value).toBeNull();
  });

  it('does not open it when it leaves first', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => true);
    spring.enter('a', files);
    vi.advanceTimersByTime(HOLD_MS + 50);
    spring.leave('a', files);
    vi.advanceTimersByTime(total);
    expect(open).not.toHaveBeenCalled();
    expect(spring.lit.value).toBe(false);
  });

  it('keeps counting while it moves over the children of the target', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => true);
    spring.enter('a', files); // the target
    vi.advanceTimersByTime(HOLD_MS - 100);
    spring.enter('a', files); // a child of it
    spring.leave('a', files); // the target's own dragleave, fired after the child's dragenter
    vi.advanceTimersByTime(total);
    expect(open).toHaveBeenCalledOnce();
  });

  it('moves on to another target without opening the first', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => true);
    spring.enter('a', files);
    vi.advanceTimersByTime(HOLD_MS - 100);
    spring.leave('a', files);
    spring.enter('b', files);
    vi.advanceTimersByTime(total);
    expect(open.mock.calls).toEqual([['b']]);
  });

  it('is left alone when it is already open', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => false);
    spring.enter('a', files);
    expect(spring.target.value).toBeNull();
    vi.advanceTimersByTime(total);
    expect(open).not.toHaveBeenCalled();
  });

  it('only counts when the drag carries files', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => true);
    spring.enter('a', dragOf(['text/plain']));
    vi.advanceTimersByTime(total);
    expect(open).not.toHaveBeenCalled();
  });

  it('forgets everything when the drag ends elsewhere', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => true);
    spring.enter('a', files);
    vi.advanceTimersByTime(HOLD_MS + 10);
    spring.reset();
    vi.advanceTimersByTime(total);
    expect(open).not.toHaveBeenCalled();
    // A fresh entry works again: the count of the first one is gone.
    spring.enter('a', files);
    vi.advanceTimersByTime(total);
    expect(open).toHaveBeenCalledOnce();
  });

  it('highlights steadily, without blinking, when motion is reduced', () => {
    const open = vi.fn();
    const spring = useSpringLoad(open, () => true, () => true);
    spring.enter('a', files);
    expect(spring.lit.value).toBe(true);
    vi.advanceTimersByTime(HOLD_MS);
    expect(spring.lit.value).toBe(true);
    vi.advanceTimersByTime(BLINK_PHASE_MS * 4);
    expect(open).toHaveBeenCalledOnce();
  });
});
