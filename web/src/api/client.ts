import { t } from '../i18n';
import { readSse } from './sse';
import type {
  ActionEvent,
  AlertInput,
  MemoryCliId,
  MemoryDecision,
  MemoryView,
  AlertPatch,
  AppState,
  AssistantAlert,
  AssistantSnapshot,
  AttachmentRef,
  ConnectorStatus,
  ConnectorValues,
  GitLabAccess,
  GitLabAccessUpdate,
  NewConversation,
  Profile,
  ProjectInfo,
  PlanDecision,
  ProposalDecision,
  ReminderPick,
  QuestionAnswer,
  ProviderModels,
  ProviderTestResult,
  ProvidersInfo,
  PairingOffer,
  RemoteDevice,
  RemoteSettings,
  RemoteSettingsPatch,
  Settings,
  SettingsPatch,
  StyleInfo,
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

  getGitLabAccess(): Promise<GitLabAccess>;
  updateGitLabAccess(update: GitLabAccessUpdate): Promise<GitLabAccess>;
  getSettings(): Promise<Settings>;
  /** Changes only the given fields; rejects with a 422 ApiError when a value is refused. */
  updateSettings(patch: SettingsPatch): Promise<Settings>;

  getProviders(): Promise<ProvidersInfo>;
  /** The models an AI offers; may take a few seconds the first time (the CLI renders its catalog). */
  getProviderModels(id: string): Promise<ProviderModels>;
  /** Runs one minimal turn with the AI (and the model, when given): it counts as one turn of the daily limit. */
  testProvider(id: string, model?: string): Promise<ProviderTestResult>;

  getRemoteSettings(): Promise<RemoteSettings>;
  /** Changes only the given fields and restarts the listener; rejects with a 422 ApiError when a value is refused. */
  updateRemoteSettings(patch: RemoteSettingsPatch): Promise<RemoteSettings>;
  /** Opens a pairing window (2 minutes). Rejects with 409 when the access is off or not listening. */
  openPairing(): Promise<PairingOffer>;
  cancelPairing(): Promise<void>;
  /** The paired phones, newest first. */
  listRemoteDevices(): Promise<RemoteDevice[]>;
  /** Ends a phone's access for good; rejects with 404 for an unknown id. */
  revokeRemoteDevice(id: string): Promise<void>;

  /** The last known update state; never waits for a check. */
  getUpdate(): Promise<UpdateInfo>;
  /** Forces a check against the source checkout; may take up to 30 s. */
  checkUpdate(): Promise<UpdateInfo>;

  getSnapshot(): Promise<AssistantSnapshot>;
  /** Calls onSnapshot with the full state on connect and after every change; resolves when the stream ends. */
  streamSnapshots(onSnapshot: (snapshot: AssistantSnapshot) => void, signal: AbortSignal): Promise<void>;
  answer(requestId: string, answers: QuestionAnswer[]): Promise<void>;
  decideProposal(requestId: string, decision: ProposalDecision): Promise<void>;
  /** Approves or declines the plan that waits; planId names the plan the user read, actions the positions of those ticked (all when absent). */
  decidePlan(requestId: string, decision: PlanDecision, planId: string, actions?: number[]): Promise<void>;
  /** Sets the alerts the user ticked in the suggestion they read (offerId), or declines it (no items). */
  decideReminders(requestId: string, decision: ProposalDecision, offerId: string, items?: ReminderPick[]): Promise<void>;
  /** Undoes a whole memory change of the conversation. Rejects with 409 when it was undone, when the facts changed since, or when the memory is off. */
  undoMemoryChange(requestId: string, changeId: string): Promise<void>;
  /** Shares a fact of a memory change, or makes it private again. */
  shareMemoryFact(requestId: string, changeId: string, itemId: string, shared: boolean): Promise<void>;
  /** Makes an alert by hand. Rejects with an ApiError (400) when the title is empty or the moment is not in the future. */
  createAlert(input: AlertInput): Promise<AssistantAlert>;
  updateAlert(id: string, patch: AlertPatch): Promise<AssistantAlert>;
  deleteAlert(id: string): Promise<void>;
  sendMessage(requestId: string, text: string, attachmentIds: string[]): Promise<void>;
  /**
   * Starts a conversation with the user's own words. Resolves with its id; the request itself arrives with the next
   * snapshot. Rejects with 400 (empty, too long, unknown file or project) or 409 (no AI connected, too many waiting).
   */
  createConversation(body: NewConversation): Promise<{ id: string }>;
  /** Uploads a file for a conversation not started yet; its id then goes with createConversation. */
  uploadDraftAttachment(file: File): Promise<AttachmentRef>;
  /** The folders of the projects folder, by name; empty when no projects folder is set. */
  listProjects(): Promise<ProjectInfo[]>;
  ignore(requestId: string): Promise<void>;
  retry(requestId: string): Promise<void>;
  /** Makes another AI the one Pouple uses, and investigates the request again with it. */
  continueWith(requestId: string, provider: string): Promise<void>;
  stop(requestId: string): Promise<void>;
  remove(requestId: string): Promise<void>;
  uploadAttachment(requestId: string, file: File): Promise<AttachmentRef>;
  /** The bytes of an attachment (the endpoint needs the bearer token, so an <img src> cannot load it directly). */
  getAttachment(requestId: string, attachmentId: string): Promise<Blob>;
  /** The last picture (JPEG) the browser drew for a conversation; rejects when no browser is open. */
  getBrowserFrame(requestId: string): Promise<Blob>;
  /** Whether the user's writing style was learned, and since when. */
  getStyle(): Promise<StyleInfo>;
  /** Makes Pouple forget the learned style. */
  forgetStyle(): Promise<StyleInfo>;
  /** The memory folder: its notes, the facts to review and the CLIs that can be connected to it. */
  getMemory(): Promise<MemoryView>;
  /** Keeps (marks confirmed today) or drops a fact to review. Rejects with 409 when the line changed meanwhile or the memory is off. */
  reviewMemory(note: string, line: string, decision: MemoryDecision): Promise<MemoryView>;
  /** Writes (or removes) the Pouple block in the CLI's own file. Rejects with 409 when connecting while the memory is off. */
  connectCli(id: MemoryCliId, connected: boolean): Promise<MemoryView>;
  /** Opens the folder in Finder; rejects with 404 when it does not exist yet. */
  revealMemory(): Promise<void>;
  /** Starts a tidy-up in the background; rejects with 409 when the memory is off. */
  tidyMemory(): Promise<void>;
}

export type Fetch = (input: string, init?: RequestInit) => Promise<Response>;

interface ErrorBody {
  error?: string;
  message?: string;
  field?: string;
}

export async function toApiError(response: Response): Promise<ApiError> {
  const body = (await response.json().catch(() => null)) as ErrorBody | null;
  const message = body?.message ?? body?.error ?? (response.statusText || t('errors.requestFailed', { status: response.status }));
  return new ApiError(response.status, message, body?.field || undefined);
}

/** The bearer token the core injected in the page. It is read on each call and never kept anywhere else. */
export function bearer(): Record<string, string> {
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

  function uploadFile(path: string, file: File): Promise<AttachmentRef> {
    const query = new URLSearchParams({ name: file.name || 'file', type: file.type || 'application/octet-stream' });
    // Raw bytes: the type travels in the query so a JSON file is not parsed on the way.
    return read<AttachmentRef>(`${path}?${query}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: file,
    });
  }

  return {
    getGitLabAccess: () => read<GitLabAccess>('/api/gitlab/access'),
    updateGitLabAccess: (update) => read<GitLabAccess>('/api/gitlab/access', { method: 'PUT' }, update),
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

    getRemoteSettings: () => read<RemoteSettings>('/api/remote/settings'),
    updateRemoteSettings: (patch) => read<RemoteSettings>('/api/remote/settings', { method: 'PUT' }, patch),
    openPairing: () => read<PairingOffer>('/api/remote/pairing', { method: 'POST' }, {}),
    cancelPairing: async () => void (await send('/api/remote/pairing', { method: 'DELETE' })),
    listRemoteDevices: async () => (await read<{ devices: RemoteDevice[] | null }>('/api/remote/devices')).devices ?? [],
    revokeRemoteDevice: async (id) => void (await send(`/api/remote/devices/${enc(id)}`, { method: 'DELETE' })),

    getProviders: () => read<ProvidersInfo>('/api/providers'),
    getProviderModels: (id) => read<ProviderModels>(`/api/providers/${enc(id)}/models`),
    testProvider: (id, model) => read<ProviderTestResult>(`/api/providers/${enc(id)}/test`, { method: 'POST' }, model === undefined ? {} : { model }),

    getUpdate: () => read<UpdateInfo>('/api/update'),
    checkUpdate: () => read<UpdateInfo>('/api/update/check', { method: 'POST' }, {}),

    getMemory: () => read<MemoryView>('/api/memory'),
    reviewMemory: (note, line, decision) => read<MemoryView>('/api/memory/review', { method: 'POST' }, { note, line, decision }),
    connectCli: (id, connected) => read<MemoryView>(`/api/memory/clis/${enc(id)}`, { method: 'POST' }, { connected }),
    revealMemory: () => post('/api/memory/reveal'),
    tidyMemory: () => post('/api/memory/tidy'),

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
    decidePlan: (requestId, decision, planId, actions) => post(assistant(requestId, '/plan'), { decision, planId, ...(actions && { actions }) }),
    decideReminders: (requestId, decision, offerId, items) => post(assistant(requestId, '/reminders'), { decision, offerId, ...(items && { items }) }),
    undoMemoryChange: (requestId, changeId) => post(assistant(requestId, '/memories'), { action: 'undo', changeId }),
    shareMemoryFact: (requestId, changeId, item, shared) => post(assistant(requestId, '/memories'), { action: 'share', changeId, item, shared }),
    createAlert: (input) => read<AssistantAlert>('/api/alerts', { method: 'POST' }, input),
    updateAlert: (id, patch) => read<AssistantAlert>(`/api/alerts/${enc(id)}`, { method: 'PATCH' }, patch),
    deleteAlert: async (id) => void (await send(`/api/alerts/${enc(id)}`, { method: 'DELETE' })),
    sendMessage: (requestId, text, attachmentIds) =>
      post(assistant(requestId, '/messages'), { text, ...(attachmentIds.length > 0 && { attachmentIds }) }),
    ignore: (requestId) => post(assistant(requestId, '/ignore')),
    retry: (requestId) => post(assistant(requestId, '/retry')),
    continueWith: (requestId, provider) => post(assistant(requestId, '/continue-with'), { provider }),
    stop: (requestId) => post(assistant(requestId, '/stop')),
    remove: async (requestId) => void (await send(assistant(requestId), { method: 'DELETE' })),

    createConversation: (body) => read<{ id: string }>('/api/assistant/requests', { method: 'POST' }, body),
    uploadDraftAttachment: (file) => uploadFile('/api/assistant/attachments', file),
    listProjects: async () => (await read<{ projects: ProjectInfo[] | null }>('/api/assistant/projects')).projects ?? [],

    uploadAttachment: (requestId, file) => uploadFile(assistant(requestId, '/attachments'), file),
    getAttachment: async (requestId, attachmentId) =>
      (await send(assistant(requestId, `/attachments/${enc(attachmentId)}`))).blob(),
    getBrowserFrame: async (requestId) => (await send(assistant(requestId, '/browser'))).blob(),
    getStyle: () => read<StyleInfo>('/api/assistant/style'),
    forgetStyle: () => read<StyleInfo>('/api/assistant/style', { method: 'DELETE' }),
  };
}
