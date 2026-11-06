import { t } from '../i18n';
import { readSse } from './sse';
import type {
  ActionEvent,
  AppState,
  AssistantSnapshot,
  AttachmentRef,
  ConnectorStatus,
  ConnectorValues,
  Profile,
  ProposalDecision,
  QuestionAnswer,
  ProviderTestResult,
  ProvidersInfo,
  Settings,
  SettingsPatch,
  UpdateInfo,
} from './types';

/** An answer the core refused. `field` names the form field of a 422 when there is one. */
export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    readonly field?: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export const errorMessage = (cause: unknown): string => (cause instanceof Error ? cause.message : String(cause));

/** Everything the interface asks of the core. The HTTP client and the in-memory mock both implement it. */
export interface PoupleApi {
  getState(): Promise<AppState>;
  listConnectors(): Promise<ConnectorStatus[]>;
  /** Asks the service who the values belong to. Saves nothing. Rejects with an ApiError (422) the user can fix. */
  validateConnector(id: string, values: ConnectorValues): Promise<Profile>;
  saveConnector(id: string, values: ConnectorValues): Promise<Profile>;
  removeConnector(id: string): Promise<void>;
  /** Runs a connector action; resolves when its stream ends. */
  runAction(id: string, action: string, onEvent: (event: ActionEvent) => void, signal?: AbortSignal): Promise<void>;

  getSettings(): Promise<Settings>;
  /** Changes only the given fields; rejects with a 422 ApiError when a value is refused. */
  updateSettings(patch: SettingsPatch): Promise<Settings>;

  getProviders(): Promise<ProvidersInfo>;
  /** Runs one minimal turn with the AI (and the model, when given): it counts as one turn of the daily limit. */
  testProvider(id: string, model?: string): Promise<ProviderTestResult>;

  /** The last known update state; never waits for a check. */
  getUpdate(): Promise<UpdateInfo>;
  /** Forces a check against the source checkout; may take up to 30 s. */
  checkUpdate(): Promise<UpdateInfo>;

  getSnapshot(): Promise<AssistantSnapshot>;
  /** Calls onSnapshot with the full state on connect and after every change; resolves when the stream ends. */
  streamSnapshots(onSnapshot: (snapshot: AssistantSnapshot) => void, signal: AbortSignal): Promise<void>;
  answer(requestId: string, answers: QuestionAnswer[]): Promise<void>;
  decideProposal(requestId: string, decision: ProposalDecision): Promise<void>;
  sendMessage(requestId: string, text: string, attachmentIds: string[]): Promise<void>;
  ignore(requestId: string): Promise<void>;
  retry(requestId: string): Promise<void>;
  stop(requestId: string): Promise<void>;
  remove(requestId: string): Promise<void>;
  uploadAttachment(requestId: string, file: File): Promise<AttachmentRef>;
  /** The bytes of an attachment (the endpoint needs the bearer token, so an <img src> cannot load it directly). */
  getAttachment(requestId: string, attachmentId: string): Promise<Blob>;
}

export type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

interface ErrorBody {
  error?: string;
  message?: string;
  field?: string;
}

async function toApiError(response: Response): Promise<ApiError> {
  const body = (await response.json().catch(() => null)) as ErrorBody | null;
  const message = body?.message ?? body?.error ?? (response.statusText || t('errors.requestFailed', { status: response.status }));
  return new ApiError(response.status, message, body?.field || undefined);
}

/** The bearer token the core injected in the page. It is read on each call and never kept anywhere else. */
function bearer(): Record<string, string> {
  const token = typeof window === 'undefined' ? undefined : window.__POUPLE__?.token;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const enc = encodeURIComponent;

export function createHttpApi(fetchImpl: Fetch = (input, init) => fetch(input, init)): PoupleApi {
  async function send(path: string, init: RequestInit = {}, json?: unknown): Promise<Response> {
    const headers: Record<string, string> = { ...bearer(), ...(init.headers as Record<string, string> | undefined) };
    let body = init.body;
    if (json !== undefined) {
      headers['Content-Type'] = 'application/json';
      body = JSON.stringify(json);
    }
    const response = await fetchImpl(path, { ...init, headers, ...(body !== undefined && { body }) });
    if (!response.ok) throw await toApiError(response);
    return response;
  }

  async function read<T>(path: string, init?: RequestInit, json?: unknown): Promise<T> {
    const response = await send(path, init, json);
    return (await response.json()) as T;
  }

  const post = async (path: string, json?: unknown): Promise<void> => void (await send(path, { method: 'POST' }, json ?? {}));

  const assistant = (requestId: string, rest = ''): string => `/api/assistant/requests/${enc(requestId)}${rest}`;

  return {
    getState: () => read<AppState>('/api/state'),
    listConnectors: async () => (await read<{ connectors: ConnectorStatus[] }>('/api/connectors')).connectors,
    validateConnector: async (id, values) =>
      (await read<{ profile: Profile }>(`/api/connectors/${enc(id)}/validate`, { method: 'POST' }, { values })).profile,
    saveConnector: async (id, values) =>
      (await read<{ profile: Profile }>(`/api/connectors/${enc(id)}`, { method: 'PUT' }, { values })).profile,
    removeConnector: async (id) => void (await send(`/api/connectors/${enc(id)}`, { method: 'DELETE' })),

    async runAction(id, action, onEvent, signal) {
      const response = await send(`/api/connectors/${enc(id)}/actions/${enc(action)}`, {
        method: 'POST',
        ...(signal && { signal }),
      });
      await readSse(response, (data) => {
        try {
          onEvent(JSON.parse(data) as ActionEvent);
        } catch {
          // A frame that is not JSON is not an event: ignore it rather than stop the whole stream.
        }
      });
    },

    getSettings: () => read<Settings>('/api/settings'),
    updateSettings: (patch) => read<Settings>('/api/settings', { method: 'PUT' }, patch),

    getProviders: () => read<ProvidersInfo>('/api/providers'),
    testProvider: (id, model) => read<ProviderTestResult>(`/api/providers/${enc(id)}/test`, { method: 'POST' }, model === undefined ? {} : { model }),

    getUpdate: () => read<UpdateInfo>('/api/update'),
    checkUpdate: () => read<UpdateInfo>('/api/update/check', { method: 'POST' }, {}),

    getSnapshot: () => read<AssistantSnapshot>('/api/assistant'),
    async streamSnapshots(onSnapshot, signal) {
      const response = await send('/api/assistant/stream', { signal, headers: { Accept: 'text/event-stream' } });
      await readSse(response, (data) => {
        try {
          onSnapshot(JSON.parse(data) as AssistantSnapshot);
        } catch {
          // Ignore a malformed frame; the next one carries the full state anyway.
        }
      });
    },
    answer: (requestId, answers) => post(assistant(requestId, '/answers'), { answers }),
    decideProposal: (requestId, decision) => post(assistant(requestId, '/proposal'), { decision }),
    sendMessage: (requestId, text, attachmentIds) =>
      post(assistant(requestId, '/messages'), { text, ...(attachmentIds.length > 0 && { attachmentIds }) }),
    ignore: (requestId) => post(assistant(requestId, '/ignore')),
    retry: (requestId) => post(assistant(requestId, '/retry')),
    stop: (requestId) => post(assistant(requestId, '/stop')),
    remove: async (requestId) => void (await send(assistant(requestId), { method: 'DELETE' })),

    async uploadAttachment(requestId, file) {
      const query = new URLSearchParams({ name: file.name || 'file', type: file.type || 'application/octet-stream' });
      // Raw bytes: the type travels in the query so a JSON file is not parsed on the way.
      return read<AttachmentRef>(assistant(requestId, `/attachments?${query}`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/octet-stream' },
        body: file,
      });
    },
    getAttachment: async (requestId, attachmentId) =>
      (await send(assistant(requestId, `/attachments/${enc(attachmentId)}`))).blob(),
  };
}
