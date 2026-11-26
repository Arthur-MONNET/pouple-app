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
  it('lists the upcoming alerts, and sets the ones the user ticked in a suggestion', async () => {
    const api = createMockApi({ latency: 0 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const before = store.alerts.value.length;
    const stamps = store.alerts.value.map((alert) => alert.at);
    expect(stamps).toEqual([...stamps].sort());

    const suggesting = store.requests.value.find((r) => r.reminders?.status === 'proposed');
    const offer = suggesting?.reminders;
    expect(offer?.items.length).toBeGreaterThan(1);
    await store.decideReminders(suggesting?.id ?? '', 'approve', offer?.id ?? '', [{ id: offer?.items[0]?.id ?? '' }, { id: offer?.items[2]?.id ?? '' }]);
    await tick();
    expect(store.alerts.value).toHaveLength(before + 2);
    expect(store.alerts.value.filter((alert) => alert.requestId === suggesting?.id).map((alert) => alert.title)).toEqual([offer?.items[0]?.label, offer?.items[2]?.label]);
    expect(store.byId(suggesting?.id)?.reminders?.status).toBe('accepted');
    store.stop();
  });

  it('refuses to set a reminder twice, or one the user never read', async () => {
    const api = createMockApi({ latency: 0 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const suggesting = store.requests.value.find((r) => r.reminders?.status === 'proposed');
    const id = suggesting?.id ?? '';
    const pick = [{ id: suggesting?.reminders?.items[0]?.id ?? '' }];
    await expect(store.decideReminders(id, 'approve', 'another-offer', pick)).rejects.toBeInstanceOf(ApiError);
    await store.decideReminders(id, 'decline', suggesting?.reminders?.id ?? '');
    await tick();
    expect(store.byId(id)?.reminders?.status).toBe('declined');
    await expect(store.decideReminders(id, 'approve', suggesting?.reminders?.id ?? '', pick)).rejects.toBeInstanceOf(ApiError);
    store.stop();
  });

  it('shares a fact of a memory change and undoes it once, refusing unknown changes, a change already undone or a memory that is off', async () => {
    const api = createMockApi({ latency: 0 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const changed = store.requests.value.find((r) => (r.memoryChanges?.length ?? 0) > 0);
    const id = changed?.id ?? '';
    const change = changed?.memoryChanges?.[0];
    expect(change?.items.some((item) => item.kind === 'update' && item.previous)).toBe(true);
    expect(change?.items.some((item) => item.kind === 'add' && item.unconfirmed && item.review)).toBe(true);
    const itemId = change?.items[0]?.id ?? '';
    // The memory is off by default in the mock settings.
    await expect(store.undoMemoryChange(id, change?.id ?? '')).rejects.toMatchObject({ status: 409 });
    await api.updateSettings({ memory: true });
    await expect(store.undoMemoryChange(id, 'unknown')).rejects.toMatchObject({ status: 404 });
    await expect(store.shareMemoryFact(id, change?.id ?? '', 'unknown', true)).rejects.toMatchObject({ status: 404 });
    await store.shareMemoryFact(id, change?.id ?? '', itemId, true);
    await tick();
    expect(store.byId(id)?.memoryChanges?.[0]?.items[0]?.shared).toBe(true);
    await store.undoMemoryChange(id, change?.id ?? '');
    await tick();
    expect(store.byId(id)?.memoryChanges?.[0]?.undone).toBe(true);
    await expect(store.undoMemoryChange(id, change?.id ?? '')).rejects.toMatchObject({ status: 409 });
    await expect(store.shareMemoryFact(id, change?.id ?? '', itemId, false)).rejects.toMatchObject({ status: 409 });
    store.stop();
  });

  it('makes, moves and deletes an alert by hand, refusing a past moment or an empty title', async () => {
    const api = createMockApi({ latency: 0 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const before = store.alerts.value.length;
    const day = new Date(Date.now() + 2 * 86400_000);
    const pad = (n: number): string => String(n).padStart(2, '0');
    const at = (hour: number): string => `${day.getFullYear()}-${pad(day.getMonth() + 1)}-${pad(day.getDate())}T${pad(hour)}:00`;

    await expect(store.createAlert({ title: '  ', at: at(9) })).rejects.toBeInstanceOf(ApiError);
    await expect(store.createAlert({ title: 'Call back', at: '2020-01-01T09:00' })).rejects.toBeInstanceOf(ApiError);
    await store.createAlert({ title: 'Call back', at: at(9) });
    await tick();
    expect(store.alerts.value).toHaveLength(before + 1);
    const made = store.alerts.value.find((alert) => alert.title === 'Call back');
    expect(made).toBeDefined();

    await store.updateAlert(made?.id ?? '', { at: at(18) });
    await tick();
    expect(new Date(store.alerts.value.find((alert) => alert.id === made?.id)?.at ?? '').getHours()).toBe(18);
    await expect(store.updateAlert(made?.id ?? '', { at: '2020-01-01T09:00' })).rejects.toBeInstanceOf(ApiError);

    await store.updateAlert(made?.id ?? '', { title: 'Call back Jocelyn', note: 'About the leave' });
    await tick();
    const edited = store.alerts.value.find((alert) => alert.id === made?.id);
    expect([edited?.title, edited?.note, new Date(edited?.at ?? '').getHours()]).toEqual(['Call back Jocelyn', 'About the leave', 18]);
    await store.updateAlert(made?.id ?? '', { note: '' });
    await tick();
    expect(store.alerts.value.find((alert) => alert.id === made?.id)?.note).toBeUndefined();
    await expect(store.updateAlert(made?.id ?? '', { title: ' ' })).rejects.toBeInstanceOf(ApiError);

    await store.deleteAlert(made?.id ?? '');
    await tick();
    expect(store.alerts.value).toHaveLength(before);
    await expect(store.deleteAlert(made?.id ?? '')).rejects.toBeInstanceOf(ApiError);
    store.stop();
  });

  it('archives a conversation out of the attention count and undoes it', async () => {
    const api = createMockApi({ latency: 0 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const waiting = store.requests.value.find((r) => r.status === 'awaiting-user');
    const id = waiting?.id ?? '';
    const updatedAt = waiting?.updatedAt;
    await store.arrange(id, { archived: true }, 'Archived');
    await tick();
    expect(store.byId(id)?.archivedAt).toBeTruthy();
    expect(store.byId(id)?.updatedAt).toBe(updatedAt);
    expect(store.attention.value).toBe(2);
    expect(store.lastArrange.value?.message).toBe('Archived');

    await store.undoArrange();
    await tick();
    expect(store.byId(id)?.archivedAt).toBeUndefined();
    expect(store.attention.value).toBe(3);
    expect(store.lastArrange.value).toBeNull();
    store.stop();
  });

  it('restores the pin that archiving removed when the archive is undone', async () => {
    const api = createMockApi({ latency: 0 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const id = store.requests.value.find((r) => r.status === 'ready')?.id ?? '';
    await store.arrange(id, { pinned: true }, 'Pinned');
    await tick();
    await store.arrange(id, { archived: true }, 'Archived');
    await tick();
    expect(store.byId(id)?.pinnedAt).toBeUndefined();
    await store.undoArrange();
    await tick();
    expect(store.byId(id)?.pinnedAt).toBeTruthy();
    expect(store.byId(id)?.archivedAt).toBeUndefined();
    store.stop();
  });

  it('refuses to archive a conversation the assistant is working on', async () => {
    const api = createMockApi({ latency: 0, workMs: 1000 });
    const store = createAssistantStore(api);
    store.start();
    await tick();
    const waiting = store.requests.value.find((r) => r.questions.length > 0);
    await store.answer(waiting?.id ?? '', waiting?.questions.map((q) => ({ questionId: q.id, optionId: q.options[0]?.id ?? '' })) ?? []);
    await tick();
    await expect(store.arrange(waiting?.id ?? '', { archived: true }, 'Archived')).rejects.toBeInstanceOf(ApiError);
    expect(store.lastArrange.value).toBeNull();
    store.stop();
  });
});
