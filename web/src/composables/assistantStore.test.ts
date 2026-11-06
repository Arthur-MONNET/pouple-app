import { describe, expect, it } from 'vitest';
import { ApiError, type PoupleApi } from '../api/client';
import { createMockApi } from '../api/mock';
import type { AssistantSnapshot } from '../api/types';
import { createAssistantStore } from './assistantStore';

const tick = (ms = 5): Promise<void> => new Promise((resolve) => setTimeout(resolve, ms));

describe('assistant store', () => {
  it('shows the pushed snapshots, newest activity first', async () => {
    const api = createMockApi({ latency: 0 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    expect(store.loaded.value).toBe(true);
    expect(store.live.value).toBe(true);
    expect(store.requests.value.length).toBeGreaterThan(5);
    const stamps = store.requests.value.map((r) => r.updatedAt);
    expect(stamps).toEqual([...stamps].sort().reverse());
    expect(store.attention.value).toBe(3);
    store.stop();
  });

  it('applies an action through the stream: ignoring a request updates the list', async () => {
    const api = createMockApi({ latency: 0, workMs: 5 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const ready = store.requests.value.find((r) => r.status === 'ready');
    expect(ready).toBeDefined();
    await store.ignore(ready?.id ?? '');
    await tick();
    expect(store.byId(ready?.id)?.status).toBe('ignored');
    await store.remove(ready?.id ?? '');
    await tick();
    expect(store.byId(ready?.id)).toBeNull();
    store.stop();
  });

  it('answers a question then waits for the assistant to come back', async () => {
    const api = createMockApi({ latency: 0, workMs: 10 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const waiting = store.requests.value.find((r) => r.questions.length > 0);
    await store.answer(waiting?.id ?? '', waiting?.questions.map((q) => ({ questionId: q.id, optionId: q.options[0]?.id ?? '' })) ?? []);
    await tick();
    expect(store.byId(waiting?.id)?.status).toBe('analyzing');
    await tick(30);
    expect(store.byId(waiting?.id)?.status).toBe('ready');
    store.stop();
  });

  it('falls back to polling when the core has no stream endpoint', async () => {
    const mock = createMockApi({ latency: 0 });
    let snapshots = 0;
    const api: PoupleApi = {
      ...mock,
      streamSnapshots: async () => {
        throw new ApiError(404, 'not found');
      },
      getSnapshot: async (): Promise<AssistantSnapshot> => {
        snapshots += 1;
        return mock.getSnapshot();
      },
    };
    const store = createAssistantStore(api);
    store.start();
    await tick(20);
    expect(store.loaded.value).toBe(true);
    expect(store.live.value).toBe(false);
    expect(snapshots).toBeGreaterThanOrEqual(1);
    store.stop();
  });

  it('reports an unreachable core without losing what it showed', async () => {
    const mock = createMockApi({ latency: 0 });
    let fail = false;
    const api: PoupleApi = {
      ...mock,
      getSnapshot: async () => {
        if (fail) throw new Error('connection refused');
        return mock.getSnapshot();
      },
      streamSnapshots: async () => {
        throw new ApiError(404, 'nope');
      },
    };
    const store = createAssistantStore(api);
    await store.refresh();
    const count = store.requests.value.length;
    fail = true;
    await store.refresh();
    expect(store.error.value).toBe('connection refused');
    expect(store.requests.value).toHaveLength(count);
  });

  it('remembers a stopped request until something else happens to it', async () => {
    const api = createMockApi({ latency: 0, workMs: 5 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const working = store.requests.value.find((r) => r.status === 'analyzing');
    await store.stopRequest(working?.id ?? '');
    expect(store.stopped.value).toContain(working?.id);
    await store.retry(working?.id ?? '');
    expect(store.stopped.value).not.toContain(working?.id);
    store.stop();
  });
});
