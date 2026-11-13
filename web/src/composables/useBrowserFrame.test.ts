import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createApp, effectScope, nextTick, ref } from 'vue';
import type { PoupleApi } from '../api/client';
import { FRAME_INTERVAL_MS, useBrowserFrame } from './useBrowserFrame';
import { provideApi } from './useApi';

function mount(getBrowserFrame: () => Promise<Blob>, active = ref(true)) {
  const app = createApp({});
  provideApi(app, { getBrowserFrame } as unknown as PoupleApi);
  const scope = effectScope();
  const frame = app.runWithContext(() => scope.run(() => useBrowserFrame(ref('r1'), active))!);
  return { scope, frame, active };
}

describe('the live picture of the browser', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    let count = 0;
    vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: () => `blob:frame-${++count}`, revokeObjectURL: vi.fn() }));
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('asks for a new picture as it goes, releases the old one, and stops when the block goes away', async () => {
    const get = vi.fn(() => Promise.resolve(new Blob(['x'])));
    const { scope, frame } = mount(get);
    await vi.advanceTimersByTimeAsync(0);
    expect(frame.url.value).toBe('blob:frame-1');
    await vi.advanceTimersByTimeAsync(FRAME_INTERVAL_MS);
    expect(frame.url.value).toBe('blob:frame-2');
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:frame-1');

    scope.stop();
    const calls = get.mock.calls.length;
    await vi.advanceTimersByTimeAsync(FRAME_INTERVAL_MS * 3);
    expect(get.mock.calls.length).toBe(calls);
  });

  it('keeps trying while the browser has not drawn anything yet, and costs nothing when folded', async () => {
    const get = vi.fn().mockRejectedValueOnce(new Error('404')).mockResolvedValue(new Blob(['x']));
    const { frame, active } = mount(get);
    await vi.advanceTimersByTimeAsync(0);
    expect(frame.url.value).toBeNull();
    await vi.advanceTimersByTimeAsync(FRAME_INTERVAL_MS);
    expect(frame.url.value).toBe('blob:frame-1');

    active.value = false;
    await nextTick();
    const calls = get.mock.calls.length;
    await vi.advanceTimersByTimeAsync(FRAME_INTERVAL_MS * 3);
    expect(get.mock.calls.length).toBe(calls);
  });
});
