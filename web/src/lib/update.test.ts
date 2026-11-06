import { afterEach, describe, expect, it, vi } from 'vitest';
import { mockCheckResult, mockUpdateInfo } from '../api/mockUpdate';
import { createUpdateStore } from '../composables/useUpdate';
import { createMockApi } from '../api/mock';
import { createUpdateLauncher, findUpdateBridge, isUpdateAvailable, shortCommit, splitChanges, startPolling, updateView, UPDATE_POLL_MS } from './update';

const info = (scenario: Parameters<typeof mockUpdateInfo>[0]) => mockUpdateInfo(scenario, 'en');

describe('update presentation', () => {
  it('picks one view per state', () => {
    expect(updateView(null, false)).toBe('unknown');
    expect(updateView(null, true)).toBe('checking');
    expect(updateView(info('unsupported'), false)).toBe('unsupported');
    expect(updateView(info('checking'), false)).toBe('checking');
    expect(updateView(info('uptodate'), true)).toBe('checking');
    expect(updateView(info('available'), false)).toBe('available');
    expect(updateView(info('uptodate'), false)).toBe('upToDate');
    // An error does not change the view: the last known state stays on screen.
    expect(updateView(info('error'), false)).toBe('upToDate');
  });

  it('offers an update only on a supported install', () => {
    expect(isUpdateAvailable(null)).toBe(false);
    expect(isUpdateAvailable(info('available'))).toBe(true);
    expect(isUpdateAvailable({ ...info('available'), supported: false })).toBe(false);
    expect(isUpdateAvailable(info('uptodate'))).toBe(false);
  });

  it('lists the first changes and counts the rest', () => {
    const changes = Array.from({ length: 15 }, (_, i) => `c${i}`);
    expect(splitChanges({ ...info('available'), behind: 20, changes }, 6)).toEqual({ shown: changes.slice(0, 6), hidden: 14 });
    expect(splitChanges({ ...info('available'), behind: 2, changes: ['a', 'b'] })).toEqual({ shown: ['a', 'b'], hidden: 0 });
  });

  it('shortens a commit to seven characters', () => {
    expect(shortCommit('0123456789abcdef')).toBe('0123456');
  });
});

describe('native bridge', () => {
  it('is detected only when the shell exposes a postMessage handler', () => {
    expect(findUpdateBridge({})).toBeNull();
    expect(findUpdateBridge({ webkit: { messageHandlers: {} } })).toBeNull();
    const handler = { postMessage: vi.fn() };
    expect(findUpdateBridge({ webkit: { messageHandlers: { pouple: handler } } })).toBe(handler);
  });

  it('posts the update message once, however many times it is asked', () => {
    const postMessage = vi.fn();
    const launcher = createUpdateLauncher(() => ({ postMessage }));
    expect([launcher.launch(), launcher.launch(), launcher.launch()]).toEqual([true, false, false]);
    expect(postMessage).toHaveBeenCalledTimes(1);
    expect(postMessage).toHaveBeenCalledWith({ type: 'update' });
    expect(launcher.launched).toBe(true);
  });

  it('does nothing, and can still try later, without a bridge', () => {
    let bridge: { postMessage: () => void } | null = null;
    const launcher = createUpdateLauncher(() => bridge);
    expect(launcher.launch()).toBe(false);
    expect(launcher.launched).toBe(false);
    const postMessage = vi.fn();
    bridge = { postMessage };
    expect(launcher.launch()).toBe(true);
    expect(postMessage).toHaveBeenCalledTimes(1);
  });
});

describe('polling', () => {
  afterEach(() => vi.useRealTimers());

  it('reads once at startup then every 30 minutes, and stops', () => {
    vi.useFakeTimers();
    const task = vi.fn().mockResolvedValue(undefined);
    const stop = startPolling(task);
    expect(task).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(UPDATE_POLL_MS - 1);
    expect(task).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1);
    expect(task).toHaveBeenCalledTimes(2);
    vi.advanceTimersByTime(UPDATE_POLL_MS * 2);
    expect(task).toHaveBeenCalledTimes(4);
    stop();
    vi.advanceTimersByTime(UPDATE_POLL_MS * 5);
    expect(task).toHaveBeenCalledTimes(4);
  });

  it('keeps going when a tick fails, with no unhandled rejection', async () => {
    vi.useFakeTimers();
    const task = vi.fn().mockRejectedValueOnce(new Error('offline')).mockImplementationOnce(() => { throw new Error('sync'); }).mockResolvedValue(undefined);
    const stop = startPolling(task, 1000);
    await vi.advanceTimersByTimeAsync(2000);
    expect(task).toHaveBeenCalledTimes(3);
    stop();
  });
});

describe('update store', () => {
  it('reads silently: a failing read leaves the state alone and throws nothing', async () => {
    const api = { ...createMockApi({ latency: 0 }), getUpdate: () => Promise.reject(new Error('offline')) };
    const store = createUpdateStore(api, { getBridge: () => null });
    await expect(store.refresh()).resolves.toBeUndefined();
    expect(store.info.value).toBeNull();
    expect(store.error.value).toBeNull();
  });

  it('shows the checking state while a forced check runs, then the result', async () => {
    const store = createUpdateStore(createMockApi({ latency: 0, update: 'available' }), { getBridge: () => null });
    await store.refresh();
    expect(store.view.value).toBe('available');
    expect(store.available.value).toBe(true);
    const pending = store.check();
    expect(store.checking.value).toBe(true);
    await pending;
    expect(store.checking.value).toBe(false);
  });

  it('keeps a failed check as a calm message and the last state', async () => {
    const store = createUpdateStore(createMockApi({ latency: 0, update: 'error' }), { getBridge: () => null });
    await store.refresh();
    expect(store.error.value).toContain('Cannot reach');
    await store.check();
    expect(store.error.value).toContain('Cannot reach');
    expect(store.view.value).toBe('upToDate');
  });

  it('launches once and then says it is closing', () => {
    const postMessage = vi.fn();
    const store = createUpdateStore(createMockApi({ latency: 0 }), { getBridge: () => ({ postMessage }) });
    expect(store.canLaunch.value).toBe(true);
    store.launch();
    store.launch();
    expect(postMessage).toHaveBeenCalledTimes(1);
    expect(store.closing.value).toBe(true);
  });

  it('cannot launch in a plain browser', () => {
    const store = createUpdateStore(createMockApi({ latency: 0 }), { getBridge: () => null });
    expect(store.canLaunch.value).toBe(false);
    expect(store.launch()).toBe(false);
    expect(store.closing.value).toBe(false);
  });
});

describe('mock update states', () => {
  it('covers up to date, available with a blocker, unsupported, error and checking', async () => {
    expect(info('uptodate')).toMatchObject({ supported: true, available: false, error: null, checking: false });
    expect(info('available')).toMatchObject({ available: true, behind: 5, blockers: [{ code: 'local-changes' }] });
    expect(info('available').changes.length).toBeLessThanOrEqual(15);
    expect(info('unsupported')).toMatchObject({ supported: false, reason: 'not-installed-from-source', checkedAt: null });
    expect(info('unsupported').message).not.toBe('');
    expect(info('error').error).not.toBeNull();
    expect(info('checking').checking).toBe(true);
    expect(() => mockCheckResult('error', 'en')).toThrow();
    expect(mockCheckResult('checking', 'en').available).toBe(true);
  });

  it('answers in the mock language and serves the same shape through the API', async () => {
    expect(mockUpdateInfo('unsupported', 'fr').message).toContain('Pouple');
    const api = createMockApi({ latency: 0, update: 'available' });
    expect((await api.getUpdate()).available).toBe(true);
    expect((await api.checkUpdate()).available).toBe(true);
  });
});
