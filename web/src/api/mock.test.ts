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

describe('mock settings', () => {
  it('refuses a projects folder that does not exist and keeps the rest', async () => {
    const api = createMockApi({ latency: 0 });
    await expect(api.updateSettings({ projectsRoot: '/missing/dir' })).rejects.toMatchObject({ status: 422 });
    expect((await api.updateSettings({ projectsRoot: '/Users/me/Workspace' })).projectsRoot).toBe('/Users/me/Workspace');
    expect((await api.getSettings()).dailyTurnLimit).toBe(150);
  });
});
