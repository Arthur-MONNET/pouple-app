import { createHttpApi, type PoupleApi } from './client';

/**
 * The API the interface talks to. `VITE_MOCK=1` swaps in the in-memory backend, `VITE_MOCK=assistant` only the
 * conversations; the mock is a dynamic import so a production build does not carry it.
 */
export async function createApi(): Promise<PoupleApi> {
  const mode = import.meta.env.VITE_MOCK;
  const view = import.meta.env.VITE_MOCK_VIEW;
  if (!mode) return createHttpApi();
  const { createMockApi } = await import('./mock');
  if (mode === 'assistant') {
    const real = createHttpApi();
    const mock = createMockApi({ connectors: false });
    return {
      ...real,
      getSnapshot: mock.getSnapshot,
      streamSnapshots: mock.streamSnapshots,
      answer: mock.answer,
      decideProposal: mock.decideProposal,
      sendMessage: mock.sendMessage,
      ignore: mock.ignore,
      retry: mock.retry,
      stop: mock.stop,
      remove: mock.remove,
      uploadAttachment: mock.uploadAttachment,
      getAttachment: mock.getAttachment,
    };
  }
  return createMockApi({ ready: import.meta.env.VITE_MOCK_READY === '1', ...(view === 'empty' || view === 'paused' ? { view } : {}) });
}
