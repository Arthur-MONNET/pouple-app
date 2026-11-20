import { describe, expect, it } from 'vitest';
import { createMockApi } from './mock';

describe('mock connectors', () => {
  const api = createMockApi({ latency: 0 });

  it('refuses a bad Slack token the way the core does, with the field', async () => {
    await expect(api.validateConnector('slack', { token: '' })).rejects.toMatchObject({ status: 422, field: 'token' });
    await expect(api.validateConnector('slack', { token: 'nope' })).rejects.toMatchObject({ field: 'token', message: expect.stringContaining('Slack token') });
    await expect(api.validateConnector('slack', { token: 'xoxb-' + '1234567890-abcdefghijklmnop' })).rejects.toMatchObject({ message: expect.stringContaining('bot token') });
  });

  it('saves nothing on validate and connects on save', async () => {
    const token = 'xoxp-' + '1234567890-abcdefghijklmnop';
    const profile = await api.validateConnector('slack', { token });
    expect(profile.warnings?.length).toBeGreaterThan(0);
    expect((await api.getState()).setupComplete).toBe(false);
    expect((await api.listConnectors())[0]?.connected).toBe(false);
    await api.saveConnector('slack', { token });
    expect((await api.listConnectors())[0]?.connected).toBe(true);
  });

  it('walks Codex from not installed to signed in through its actions', async () => {
    await expect(api.validateConnector('codex', {})).rejects.toMatchObject({ message: expect.stringContaining('not installed') });
    const events: string[] = [];
    await api.runAction('codex', 'install', (e) => events.push(e.type));
    expect(events.at(-1)).toBe('done');
    await expect(api.validateConnector('codex', {})).rejects.toMatchObject({ message: expect.stringContaining('Not signed in') });
    await api.runAction('codex', 'login', (e) => events.push(e.type));
    expect(events).toContain('open');
    expect((await api.validateConnector('codex', {})).name).toContain('@');
  });
});

describe('mock optional connectors', () => {
  const good = { gitlab: 'glpat-abcdefghijklmnopqrstu', github: 'ghp_abcdefghijklmnopqrstuvwx', asana: '1/1234567890:abcdefghijklmnopqrstuvwx', slite: 'abcdefghijklmnopqrstuvwxyz' };

  it('lists Slack, GitLab, GitHub, Asana and Slite as optional, and setup does not wait for them', async () => {
    const api = createMockApi({ latency: 0, ready: true });
    const optional = (await api.listConnectors()).filter((c) => !c.descriptor.required);
    expect(optional.map((c) => c.descriptor.id)).toEqual(['slack', 'gitlab', 'github', 'asana', 'slite']);
    expect(optional.filter((c) => c.descriptor.id !== 'slack').every((c) => !c.connected && (c.descriptor.fields ?? []).length > 0 && (c.descriptor.guide ?? []).length > 0)).toBe(true);
    expect((await api.getState()).setupComplete).toBe(true);
  });

  it('still starts ready with Slack and Codex connected', async () => {
    const connected = (await createMockApi({ latency: 0, ready: true }).listConnectors()).filter((c) => c.connected);
    expect(connected.map((c) => c.descriptor.id)).toEqual(['slack', 'codex']);
  });

  it('completes setup with the AI alone, Slack left aside', async () => {
    const api = createMockApi({ latency: 0 });
    await api.runAction('codex', 'install', () => undefined);
    await api.runAction('codex', 'login', () => undefined);
    await api.saveConnector('codex', {});
    const state = await api.getState();
    expect(state.setupComplete).toBe(true);
    expect(state.steps.find((step) => step.id === 'slack')).toMatchObject({ required: false, connected: false });
  });

  it('describes GitHub with its API address and token, as the core does', async () => {
    const github = (await createMockApi({ latency: 0 }).listConnectors()).find((c) => c.descriptor.id === 'github');
    expect(github?.descriptor).toMatchObject({ name: 'GitHub', required: false, order: 11 });
    expect(github?.descriptor.fields?.map((field) => [field.id, field.secret, field.optional ?? false])).toEqual([['token', true, false], ['url', false, true]]);
    expect(github?.descriptor.fields?.[1]?.default).toBe('https://api.github.com');
  });

  it('answers 422 with the field for a bad token, and a profile for a good one', async () => {
    const api = createMockApi({ latency: 0 });
    for (const [id, token] of Object.entries(good)) {
      await expect(api.validateConnector(id, { token: '' })).rejects.toMatchObject({ status: 422, field: 'token' });
      await expect(api.validateConnector(id, { token: token + 'bad' })).rejects.toMatchObject({ status: 422, field: 'token' });
      expect((await api.validateConnector(id, { token })).name).toBe('Arthur Monnet');
    }
  });

  it('saves only on save and disconnects', async () => {
    const api = createMockApi({ latency: 0 });
    await api.validateConnector('gitlab', { token: good.gitlab });
    expect((await api.listConnectors()).find((c) => c.descriptor.id === 'gitlab')?.connected).toBe(false);
    await api.saveConnector('gitlab', { token: good.gitlab });
    expect((await api.listConnectors()).find((c) => c.descriptor.id === 'gitlab')?.profile?.name).toBe('Arthur Monnet');
    await api.removeConnector('gitlab');
    expect((await api.listConnectors()).find((c) => c.descriptor.id === 'gitlab')?.connected).toBe(false);
  });

  it('speaks the interface language', async () => {
    const api = createMockApi({ latency: 0, lang: 'fr' });
    const gitlab = (await api.listConnectors()).find((c) => c.descriptor.id === 'gitlab');
    expect(gitlab?.descriptor.tagline).toContain('merge requests');
    await expect(api.validateConnector('gitlab', { token: 'glpat-badbadbadbadbadbadbad' })).rejects.toMatchObject({ message: expect.stringContaining('refuse') });
  });
});

describe('mock new conversation', () => {
  it('lists the projects only once a projects folder is chosen', async () => {
    expect(await createMockApi({ latency: 0 }).listProjects()).toEqual([]);
    expect((await createMockApi({ latency: 0, ready: true }).listProjects()).map((project) => project.id)).toEqual(['api', 'front', 'mobile']);
  });

  it('starts an in-app conversation with its files and project, then answers it', async () => {
    const api = createMockApi({ latency: 0, ready: true, workMs: 5 });
    const file = await api.uploadDraftAttachment(new File(['x'], 'trace.log', { type: 'text/plain' }));
    const { id } = await api.createConversation({ text: '  Why is the build red?\nSince 9:00.  ', attachmentIds: [file.id], projectId: 'front' });
    const created = (await api.getSnapshot()).requests.find((request) => request.id === id);
    expect(created?.source).toMatchObject({ origin: 'in-app', senderId: 'self', channelName: null, permalink: null, label: 'Why is the build red?' });
    expect(created).toMatchObject({ text: 'Why is the build red?\nSince 9:00.', projectId: 'front', engaged: true, attachments: [{ id: file.id, name: 'trace.log' }] });
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect((await api.getSnapshot()).requests.find((request) => request.id === id)?.status).toBe('ready');
  });

  it('refuses an empty draft, an unknown file or project with 400, and 409 when the assistant is off', async () => {
    const api = createMockApi({ latency: 0, ready: true });
    await expect(api.createConversation({ text: '   ', attachmentIds: [] })).rejects.toMatchObject({ status: 400 });
    await expect(api.createConversation({ text: 'a'.repeat(4001), attachmentIds: [] })).rejects.toMatchObject({ status: 400 });
    await expect(api.createConversation({ text: 'hi', attachmentIds: ['nope'] })).rejects.toMatchObject({ status: 400 });
    await expect(api.createConversation({ text: 'hi', attachmentIds: [], projectId: 'elsewhere' })).rejects.toMatchObject({ status: 400 });
    await expect(createMockApi({ latency: 0, ready: true, view: 'paused' }).createConversation({ text: 'hi', attachmentIds: [] })).rejects.toMatchObject({ status: 409 });
  });

  it('shows a sample in-app conversation and a pull request event', async () => {
    const origins = (await createMockApi({ latency: 0, ready: true }).getSnapshot()).requests.map((request) => request.source.origin);
    expect(origins).toContain('in-app');
    expect(origins).toContain('pull-request');
  });
});

describe('mock settings', () => {
  it('refuses a projects folder that does not exist and keeps the rest', async () => {
    const api = createMockApi({ latency: 0 });
    await expect(api.updateSettings({ projectsRoot: '/missing/dir' })).rejects.toMatchObject({ status: 422 });
    expect((await api.updateSettings({ projectsRoot: '/Users/me/Workspace' })).projectsRoot).toBe('/Users/me/Workspace');
    expect((await api.getSettings()).dailyTurnLimit).toBe(150);
  });
});
