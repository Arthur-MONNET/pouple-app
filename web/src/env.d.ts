/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** "1" mocks the whole backend, "assistant" mocks only the conversations (the connectors stay real). */
  readonly VITE_MOCK?: string;
  /** With VITE_MOCK=1: "1" starts with both required connectors already connected. */
  readonly VITE_MOCK_READY?: string;
  /** With VITE_MOCK=1: "empty" (no conversation) or "paused" (the assistant is off). */
  readonly VITE_MOCK_VIEW?: string;
}

interface Window {
  /** Injected by the core in index.html. */
  __POUPLE__?: { token: string };
}
