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
  const good = { gitlab: 'glpat-abcdefghijklmnopqrstu', asana: '1/1234567890:abcdefghijklmnopqrstuvwx', slite: 'abcdefghijklmnopqrstuvwxyz' };

  it('lists GitLab, Asana and Slite as optional, and setup does not wait for them', async () => {
    const api = createMockApi({ latency: 0, ready: true });
    const optional = (await api.listConnectors()).filter((c) => !c.descriptor.required);
    expect(optional.map((c) => c.descriptor.id)).toEqual(['gitlab', 'asana', 'slite']);
    expect(optional.every((c) => !c.connected && (c.descriptor.fields ?? []).length > 0 && (c.descriptor.guide ?? []).length > 0)).toBe(true);
    expect((await api.getState()).setupComplete).toBe(true);
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

describe('mock settings', () => {
  it('refuses a projects folder that does not exist and keeps the rest', async () => {
    const api = createMockApi({ latency: 0 });
    await expect(api.updateSettings({ projectsRoot: '/missing/dir' })).rejects.toMatchObject({ status: 422 });
    expect((await api.updateSettings({ projectsRoot: '/Users/me/Workspace' })).projectsRoot).toBe('/Users/me/Workspace');
    expect((await api.getSettings()).dailyTurnLimit).toBe(150);
  });
});
