import { describe, expect, it } from 'vitest';
import { createMockApi } from '../api/mock';
import { useMemory } from './useMemory';

/** The memory as a user who turned it on sees it: it is off by default. */
const make = async () => {
  const api = createMockApi({ latency: 0 });
  await api.updateSettings({ memory: true });
  return useMemory(api);
};

describe('useMemory', () => {
  it('loads the view, then drops a fact to review', async () => {
    const memory = await make();
    await memory.load();
    const first = memory.view.value?.review[0];
    expect(memory.view.value?.review).toHaveLength(2);
    expect(await memory.review(first!.note, first!.line, 'drop')).toBe(true);
    expect(memory.view.value?.review).toHaveLength(1);
  });

  it('keeps the core message when the line changed meanwhile', async () => {
    const memory = await make();
    await memory.load();
    expect(await memory.review('projects/billing.md', 'a line that is not there', 'keep')).toBe(false);
    expect(memory.error.value).toBeTruthy();
    expect(memory.view.value?.review).toHaveLength(2);
  });

  it('connects and disconnects a CLI', async () => {
    const memory = await make();
    await memory.load();
    await memory.setCli('claude', true);
    expect(memory.view.value?.clis.find((cli) => cli.id === 'claude')?.connected).toBe(true);
    await memory.setCli('claude', false);
    expect(memory.view.value?.clis.find((cli) => cli.id === 'claude')?.connected).toBe(false);
  });

  it('refuses to connect a CLI while the memory is off', async () => {
    const memory = useMemory(createMockApi({ latency: 0 }));
    await memory.load();
    expect(memory.view.value?.enabled).toBe(false);
    await memory.setCli('claude', true);
    expect(memory.error.value).toBeTruthy();
    expect(memory.view.value?.clis.find((cli) => cli.id === 'claude')?.connected).toBe(false);
  });

  it('says a tidy-up was started and shows when it ran', async () => {
    const memory = await make();
    await memory.load();
    expect(memory.view.value?.tidiedAt).toBeUndefined();
    await memory.tidy();
    expect(memory.tidyStarted.value).toBe(true);
    await memory.load();
    expect(memory.view.value?.tidiedAt).toBeTruthy();
  });
});
