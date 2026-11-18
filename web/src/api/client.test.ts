import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, createHttpApi, type Fetch } from './client';
import type { ActionEvent } from './types';

const json = (status: number, body: unknown): Response =>
  new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

const stream = (...frames: string[]): Response => {
  const encoder = new TextEncoder();
  return new Response(
    new ReadableStream<Uint8Array>({
      start(controller) {
        for (const frame of frames) controller.enqueue(encoder.encode(frame));
        controller.close();
      },
    }),
    { status: 200, headers: { 'Content-Type': 'text/event-stream' } },
  );
};

function recorder(reply: (path: string, init: RequestInit) => Response): { fetch: Fetch; calls: { path: string; init: RequestInit }[] } {
  const calls: { path: string; init: RequestInit }[] = [];
  return {
    calls,
    fetch: async (path, init = {}) => {
      calls.push({ path, init });
      return reply(path, init);
    },
  };
}

afterEach(() => vi.unstubAllGlobals());

describe('http api', () => {
  it('sends the bearer token injected in the page, never in the URL', async () => {
    vi.stubGlobal('window', { __POUPLE__: { token: 'secret-token' } });
    const { fetch, calls } = recorder(() => json(200, { setupComplete: false, steps: [] }));
    await createHttpApi(fetch).getState();
    const call = calls[0];
    expect(call?.path).toBe('/api/state');
    expect((call?.init.headers as Record<string, string>)['Authorization']).toBe('Bearer secret-token');
    expect(call?.path).not.toContain('secret-token');
  });

  it('turns a 422 into an ApiError carrying the field and the message', async () => {
    const { fetch } = recorder(() => json(422, { field: 'token', message: 'User OAuth Token is required' }));
    const error = await createHttpApi(fetch).validateConnector('slack', { token: '' }).catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ status: 422, field: 'token', message: 'User OAuth Token is required' });
  });

  it('reads a 422 without a field as a general problem', async () => {
    const { fetch } = recorder(() => json(422, { message: 'The Codex CLI is not installed yet.' }));
    const error = await createHttpApi(fetch).validateConnector('codex', {}).catch((cause: unknown) => cause);
    expect(error).toMatchObject({ status: 422, message: 'The Codex CLI is not installed yet.' });
    expect((error as ApiError).field).toBeUndefined();
  });

  it('prefers the message of a 502 and falls back to the error key of a 404', async () => {
    const bad = recorder(() => json(502, { message: 'Slack is unreachable' }));
    await expect(createHttpApi(bad.fetch).saveConnector('slack', {})).rejects.toMatchObject({ status: 502, message: 'Slack is unreachable' });
    const missing = recorder(() => json(404, { error: 'unknown connector: x' }));
    await expect(createHttpApi(missing.fetch).saveConnector('x', {})).rejects.toMatchObject({ status: 404, message: 'unknown connector: x' });
  });

  it('validates with the values in the JSON body and unwraps the profile', async () => {
    const { fetch, calls } = recorder(() => json(200, { profile: { name: 'Arthur', facts: [], warnings: [] } }));
    const profile = await createHttpApi(fetch).validateConnector('slack', { token: 'xoxp-1' });
    expect(profile.name).toBe('Arthur');
    expect(calls[0]?.path).toBe('/api/connectors/slack/validate');
    expect(calls[0]?.init.method).toBe('POST');
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ values: { token: 'xoxp-1' } });
  });

  it('saves with PUT and removes with DELETE (204 has no body)', async () => {
    const { fetch, calls } = recorder((_path, init) => (init.method === 'DELETE' ? new Response(null, { status: 204 }) : json(200, { profile: { name: 'A', facts: [], warnings: [] } })));
    const api = createHttpApi(fetch);
    await api.saveConnector('slack', { token: 'x' });
    await api.removeConnector('slack');
    expect(calls.map((c) => `${c.init.method} ${c.path}`)).toEqual(['PUT /api/connectors/slack', 'DELETE /api/connectors/slack']);
  });

  it('streams connector action events from a POST', async () => {
    const { fetch, calls } = recorder(() =>
      stream('data: {"type":"log","text":"Installing"}\n\n', 'data: {"type":"open","url":"https://x.test/"}\n\ndata: {"type":"done"}\n\n'),
    );
    const events: ActionEvent[] = [];
    await createHttpApi(fetch).runAction('codex', 'login', (event) => events.push(event));
    expect(calls[0]).toMatchObject({ path: '/api/connectors/codex/actions/login', init: { method: 'POST' } });
    expect(events).toEqual([{ type: 'log', text: 'Installing' }, { type: 'open', url: 'https://x.test/' }, { type: 'done' }]);
  });

  it('rejects when the action endpoint answers an error instead of a stream', async () => {
    const { fetch } = recorder(() => json(404, { error: 'not found' }));
    await expect(createHttpApi(fetch).runAction('codex', 'nope', () => undefined)).rejects.toBeInstanceOf(ApiError);
  });

  it('delivers each snapshot of the conversation stream and skips a broken frame', async () => {
    const snapshot = { status: { active: true, problem: null, lastPollAt: null, notices: [], usage: { today: 1, limit: null } }, requests: [] };
    const { fetch, calls } = recorder(() => stream(`data: ${JSON.stringify(snapshot)}\n\n`, 'data: not json\n\n', `data: ${JSON.stringify(snapshot)}\n\n`));
    const seen: unknown[] = [];
    await createHttpApi(fetch).streamSnapshots((s) => seen.push(s), new AbortController().signal);
    expect(calls[0]?.path).toBe('/api/assistant/stream');
    expect(seen).toHaveLength(2);
  });

  it('uploads raw bytes with the name and type in the query', async () => {
    const { fetch, calls } = recorder(() => json(200, { id: 'a1', name: 'shot.png', mime: 'image/png', size: 3, kind: 'image' }));
    const ref = await createHttpApi(fetch).uploadAttachment('r/1', new File(['abc'], 'shot.png', { type: 'image/png' }));
    expect(ref.id).toBe('a1');
    expect(calls[0]?.path).toBe('/api/assistant/requests/r%2F1/attachments?name=shot.png&type=image%2Fpng');
    expect((calls[0]?.init.headers as Record<string, string>)['Content-Type']).toBe('application/octet-stream');
  });

  it('names the ticked actions of a plan, and only when some were ticked', async () => {
    const { fetch, calls } = recorder(() => new Response(null, { status: 202 }));
    const api = createHttpApi(fetch);
    await api.decidePlan('r1', 'approve', 'p1', [0, 2]);
    await api.decidePlan('r1', 'approve', 'p1');
    expect(calls.map((c) => c.path)).toEqual(['/api/assistant/requests/r1/plan', '/api/assistant/requests/r1/plan']);
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ decision: 'approve', planId: 'p1', actions: [0, 2] });
    expect(JSON.parse(String(calls[1]?.init.body))).toEqual({ decision: 'approve', planId: 'p1' });
  });

  it('names the suggestion that was read, and the ticked reminders with their moment, when deciding on reminders', async () => {
    const { fetch, calls } = recorder(() => new Response(null, { status: 202 }));
    const api = createHttpApi(fetch);
    await api.decideReminders('r1', 'approve', 'o1', [{ id: 'r1', at: '2026-11-20T09:00' }, { id: 'r2' }]);
    await api.decideReminders('r1', 'decline', 'o1');
    expect(calls.map((c) => c.path)).toEqual(['/api/assistant/requests/r1/reminders', '/api/assistant/requests/r1/reminders']);
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ decision: 'approve', offerId: 'o1', items: [{ id: 'r1', at: '2026-11-20T09:00' }, { id: 'r2' }] });
    expect(JSON.parse(String(calls[1]?.init.body))).toEqual({ decision: 'decline', offerId: 'o1' });
  });

  it('names the suggestion that was read, and the ticked facts with their sharing, when deciding on memories', async () => {
    const { fetch, calls } = recorder(() => new Response(null, { status: 202 }));
    const api = createHttpApi(fetch);
    await api.decideMemories('r1', 'approve', 'o1', [{ id: 'm1', shared: true }, { id: 'm2' }]);
    await api.decideMemories('r1', 'decline', 'o1');
    expect(calls.map((c) => c.path)).toEqual(['/api/assistant/requests/r1/memories', '/api/assistant/requests/r1/memories']);
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ decision: 'approve', offerId: 'o1', items: [{ id: 'm1', shared: true }, { id: 'm2' }] });
    expect(JSON.parse(String(calls[1]?.init.body))).toEqual({ decision: 'decline', offerId: 'o1' });
  });

  it('makes an alert with POST, moves it with PATCH and deletes it with DELETE (204 has no body)', async () => {
    const alert = { id: 'a1', title: 'Call', at: '2026-11-20T09:00:00+01:00', createdAt: '2026-11-16T10:00:00+01:00' };
    const { fetch, calls } = recorder((_path, init) => (init.method === 'DELETE' ? new Response(null, { status: 204 }) : json(init.method === 'POST' ? 201 : 200, alert)));
    const api = createHttpApi(fetch);
    expect(await api.createAlert({ title: 'Call', at: '2026-11-20T09:00' })).toEqual(alert);
    expect(await api.updateAlert('a/1', { at: '2026-11-21T09:00' })).toEqual(alert);
    await api.deleteAlert('a1');
    expect(calls.map((c) => `${c.init.method} ${c.path}`)).toEqual(['POST /api/alerts', 'PATCH /api/alerts/a%2F1', 'DELETE /api/alerts/a1']);
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ title: 'Call', at: '2026-11-20T09:00' });
    expect(JSON.parse(String(calls[1]?.init.body))).toEqual({ at: '2026-11-21T09:00' });
  });

  it('posts conversation actions to the assistant routes', async () => {
    const { fetch, calls } = recorder(() => new Response(null, { status: 204 }));
    const api = createHttpApi(fetch);
    await api.answer('r1', [{ questionId: 'q', optionId: 'o' }]);
    await api.decideProposal('r1', 'approve');
    await api.sendMessage('r1', 'hi', ['a']);
    await api.ignore('r1');
    await api.retry('r1');
    await api.stop('r1');
    await api.remove('r1');
    expect(calls.map((c) => `${c.init.method} ${c.path}`)).toEqual([
      'POST /api/assistant/requests/r1/answers',
      'POST /api/assistant/requests/r1/proposal',
      'POST /api/assistant/requests/r1/messages',
      'POST /api/assistant/requests/r1/ignore',
      'POST /api/assistant/requests/r1/retry',
      'POST /api/assistant/requests/r1/stop',
      'DELETE /api/assistant/requests/r1',
    ]);
    expect(JSON.parse(String(calls[2]?.init.body))).toEqual({ text: 'hi', attachmentIds: ['a'] });
  });
});

describe('http api, update', () => {
  it('reads the state with GET and forces a check with POST', async () => {
    const { fetch, calls } = recorder(() => json(200, { supported: true, available: false }));
    const api = createHttpApi(fetch);
    await api.getUpdate();
    await api.checkUpdate();
    expect(calls.map((c) => [c.path, c.init.method ?? 'GET'])).toEqual([['/api/update', 'GET'], ['/api/update/check', 'POST']]);
  });
});

describe('http api, memory', () => {
  const view = { enabled: true, folder: '/n', exists: true, notes: [], review: [], clis: [] };

  it('reads the memory and posts the review decision with the exact line', async () => {
    const { fetch, calls } = recorder(() => json(200, view));
    const api = createHttpApi(fetch);
    expect(await api.getMemory()).toEqual(view);
    await api.reviewMemory('people/a.md', '- a fact (2026-01-02)', 'keep');
    expect(calls.map((c) => [c.path, c.init.method ?? 'GET'])).toEqual([['/api/memory', 'GET'], ['/api/memory/review', 'POST']]);
    expect(JSON.parse(String(calls[1]?.init.body))).toEqual({ note: 'people/a.md', line: '- a fact (2026-01-02)', decision: 'keep' });
  });

  it('connects a CLI by id, and reveals and tidies without reading a body', async () => {
    const { fetch, calls } = recorder((path) => (path.includes('/clis/') ? json(200, view) : new Response(null, { status: path.endsWith('tidy') ? 202 : 204 })));
    const api = createHttpApi(fetch);
    await api.connectCli('codex', true);
    await api.revealMemory();
    await api.tidyMemory();
    expect(calls.map((c) => [c.path, c.init.method])).toEqual([['/api/memory/clis/codex', 'POST'], ['/api/memory/reveal', 'POST'], ['/api/memory/tidy', 'POST']]);
    expect(JSON.parse(String(calls[0]?.init.body))).toEqual({ connected: true });
  });

  it('keeps the message of a 409', async () => {
    const { fetch } = recorder(() => json(409, { message: 'the memory is off' }));
    const error = await createHttpApi(fetch).connectCli('claude', true).catch((cause: unknown) => cause);
    expect(error).toMatchObject({ status: 409, message: 'the memory is off' });
  });
});
