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
  const { isUpdateScenario, installMockBridge } = await import('./mockUpdate');
  const update = import.meta.env.VITE_MOCK_UPDATE;
  if (mode === 'assistant') {
    const real = createHttpApi();
    const mock = createMockApi({ connectors: false });
    return {
      ...real,
      getSnapshot: mock.getSnapshot,
      streamSnapshots: mock.streamSnapshots,
      answer: mock.answer,
      decideProposal: mock.decideProposal,
      decidePlan: mock.decidePlan,
      decideReminders: mock.decideReminders,
      undoMemoryChange: mock.undoMemoryChange,
      shareMemoryFact: mock.shareMemoryFact,
      createAlert: mock.createAlert,
      updateAlert: mock.updateAlert,
      deleteAlert: mock.deleteAlert,
      sendMessage: mock.sendMessage,
      ignore: mock.ignore,
      retry: mock.retry,
      stop: mock.stop,
      remove: mock.remove,
      arrange: mock.arrange,
      uploadAttachment: mock.uploadAttachment,
      createConversation: mock.createConversation,
      uploadDraftAttachment: mock.uploadDraftAttachment,
      listProjects: mock.listProjects,
      getAttachment: mock.getAttachment,
      getBrowserFrame: mock.getBrowserFrame,
      getStyle: mock.getStyle,
      forgetStyle: mock.forgetStyle,
      getMemory: mock.getMemory,
      reviewMemory: mock.reviewMemory,
      connectCli: mock.connectCli,
      revealMemory: mock.revealMemory,
      tidyMemory: mock.tidyMemory,
    };
  }
  if (import.meta.env.VITE_MOCK_BRIDGE !== '0') installMockBridge();
  return createMockApi({ ready: import.meta.env.VITE_MOCK_READY === '1', ...(view === 'empty' || view === 'paused' ? { view } : {}), ...(isUpdateScenario(update) ? { update } : {}) });
}
