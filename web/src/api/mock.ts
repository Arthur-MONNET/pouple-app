import { ApiError, type PoupleApi } from './client';
import { CODEX_CONNECTOR, createFixtures, SLACK_CONNECTOR } from './mockFixtures';
import type {
  ActionEvent,
  AppState,
  AssistantRequest,
  AssistantSnapshot,
  AttachmentRef,
  ConnectorStatus,
  ConnectorValues,
  Profile,
  QuestionAnswer,
  Settings,
} from './types';

export interface MockOptions {
  /** Artificial latency of every call, in ms. */
  latency?: number;
  /** Mock the connectors too (the default). `false` serves only conversations. */
  connectors?: boolean;
  /** How long the mock assistant "works" before answering, in ms. */
  workMs?: number;
  /** Start with Slack and Codex already connected (to look at the main window without going through setup). */
  ready?: boolean;
  /** `empty`: no conversation at all; `paused`: the assistant is off and says why. */
  view?: 'empty' | 'paused';
}

const sleep = (ms: number, signal?: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener('abort', () => (clearTimeout(timer), resolve()), { once: true });
  });

const nowIso = (): string => new Date().toISOString();

const SLACK_PROFILE: Profile = {
  name: 'Arthur Monnet',
  subtitle: 'Acme workspace',
  facts: [
    { label: 'Workspace', value: 'Acme (acme.slack.com)' },
    { label: 'E-mail', value: 'arthur@acme.example' },
    { label: 'Time zone', value: 'Europe/Paris' },
  ],
  warnings: [
    {
      title: 'Reactions are not watched',
      detail: 'The reactions:read permission is missing, so Pouple cannot see when a message gets your emoji. Everything else works.',
    },
  ],
};

const CODEX_PROFILE: Profile = {
  name: 'arthur@acme.example',
  subtitle: 'Signed in with ChatGPT',
  facts: [
    { label: 'Plan', value: 'Plus' },
    { label: 'Codex', value: '0.52.0' },
    { label: 'Organization', value: 'Personal' },
    { label: 'Subscription until', value: '14 Jan 2027' },
  ],
  warnings: [],
};

/**
 * An in-memory backend with realistic data, so the whole interface can be developed and looked at without the core
 * (`VITE_MOCK=1`). Slack accepts any well-formed token that does not contain "bad"; Codex starts "not installed".
 */
export function createMockApi(options: MockOptions = {}): PoupleApi {
  const { latency = 250, connectors: mockConnectors = true, workMs = 2200 } = options;
  const connectors: ConnectorStatus[] = [structuredClone(SLACK_CONNECTOR), structuredClone(CODEX_CONNECTOR)];
  const codex = { installed: false, signedIn: false };
  if (options.ready) {
    codex.installed = codex.signedIn = true;
    for (const status of connectors) Object.assign(status, { connected: true, profile: status.descriptor.id === 'slack' ? SLACK_PROFILE : CODEX_PROFILE, connectedAt: nowIso() });
  }
  let requests: AssistantRequest[] = options.view === 'empty' ? [] : createFixtures();
  const listeners = new Set<(snapshot: AssistantSnapshot) => void>();
  let uploads = 0;
  const settings: Settings = { pollSeconds: 60, dailyTurnLimit: 150, projectsRoot: '', language: '', autoApprove: 'off', keywords: [], ignoreChannels: [] };

  const wait = (): Promise<void> => sleep(latency);
  /** With no latency (tests) nothing waits; otherwise the pauses of slow operations keep their real feel. */
  const slow = (ms: number): number => (latency === 0 ? 0 : ms);

  const snapshot = (): AssistantSnapshot => ({
    status: {
      active: options.view !== 'paused',
      problem: options.view === 'paused' ? 'Codex needs a breather. Back at 14:30.' : null,
      lastPollAt: nowIso(),
      notices: [],
      usage: { today: 7, limit: 40 },
    },
    requests: structuredClone(requests),
  });

  const publish = (): void => {
    const current = snapshot();
    for (const listener of listeners) listener(current);
  };

  const find = (id: string): AssistantRequest => {
    const request = requests.find((item) => item.id === id);
    if (!request) throw new ApiError(404, 'unknown request');
    return request;
  };

  const touch = (request: AssistantRequest, patch: Partial<AssistantRequest>): void => {
    Object.assign(request, patch, { updatedAt: nowIso() });
    publish();
  };

  /** The mock assistant "thinks" for a moment, then answers. */
  function work(request: AssistantRequest, reply: string): void {
    touch(request, { status: 'analyzing', questions: [], proposal: null, error: null });
    setTimeout(() => {
      if (request.status !== 'analyzing') return;
      request.entries.push({ id: `m${Date.now()}`, at: nowIso(), role: 'assistant', kind: 'text', text: reply });
      touch(request, { status: 'ready', continuable: true });
    }, workMs);
  }

  function checkSlackValues(values: ConnectorValues): void {
    const token = values['token'] ?? '';
    const field = SLACK_CONNECTOR.descriptor.fields?.[0];
    if (token === '') throw new ApiError(422, 'User OAuth Token is required', 'token');
    if (field?.pattern && !new RegExp(field.pattern).test(token)) throw new ApiError(422, `User OAuth Token ${field.patternHint ?? 'does not look right'}`, 'token');
    if (token.startsWith('xoxb')) throw new ApiError(422, 'This is a bot token (xoxb-). Pouple needs the User OAuth Token, which starts with xoxp-.', 'token');
    if (token.includes('bad')) throw new ApiError(422, 'Slack does not accept this token (invalid_auth). Copy it again from OAuth & Permissions.', 'token');
  }

  function codexProfile(): Profile {
    if (!codex.installed) throw new ApiError(422, 'The Codex CLI is not installed yet. Click “Install Codex”.');
    if (!codex.signedIn) throw new ApiError(422, 'Not signed in yet. Click “Sign in with ChatGPT”.');
    return CODEX_PROFILE;
  }

  const profileFor = (id: string, values: ConnectorValues): Profile => {
    if (id === 'slack') return (checkSlackValues(values), SLACK_PROFILE);
    if (id === 'codex') return codexProfile();
    throw new ApiError(404, `unknown connector: ${id}`);
  };

  const statusOf = (id: string): ConnectorStatus => {
    const status = connectors.find((item) => item.descriptor.id === id);
    if (!status) throw new ApiError(404, `unknown connector: ${id}`);
    return status;
  };

  return {
    async getState(): Promise<AppState> {
      await wait();
      if (!mockConnectors) return { setupComplete: true, steps: [] };
      const steps = connectors.map((c) => ({ id: c.descriptor.id, name: c.descriptor.name, required: c.descriptor.required, connected: c.connected }));
      return { setupComplete: steps.every((s) => !s.required || s.connected), steps };
    },
    async listConnectors() {
      await wait();
      return structuredClone(connectors);
    },
    async validateConnector(id, values) {
      await sleep(latency + slow(600));
      return structuredClone(profileFor(id, values));
    },
    async saveConnector(id, values) {
      await sleep(latency + slow(300));
      const profile = profileFor(id, values);
      Object.assign(statusOf(id), { connected: true, profile, connectedAt: nowIso() });
      return structuredClone(profile);
    },
    async removeConnector(id) {
      await wait();
      const status = statusOf(id);
      status.connected = false;
      delete status.profile;
      delete status.connectedAt;
      if (id === 'codex') codex.signedIn = false;
    },
    async runAction(id, action, onEvent, signal) {
      const emit = (event: ActionEvent): void => onEvent(event);
      const lines = async (texts: string[], gap = slow(450)): Promise<void> => {
        for (const text of texts) {
          await sleep(gap, signal);
          if (signal?.aborted) return;
          emit({ type: 'log', text });
        }
      };
      if (id !== 'codex') throw new ApiError(404, 'not found');
      if (action === 'install') {
        await lines(['Installing with npm…', 'added 1 package in 6s']);
        codex.installed = true;
        emit({ type: 'done' });
      } else if (action === 'login') {
        await lines(['Starting local login server on http://localhost:1455.', 'If your browser did not open, visit:']);
        emit({ type: 'open', url: 'https://auth.openai.com/oauth/authorize?mock=1' });
        await lines(['Waiting for the sign-in to finish…'], slow(1500));
        codex.signedIn = true;
        emit({ type: 'done' });
      } else {
        throw new ApiError(404, 'not found');
      }
    },

    async getSettings() {
      await wait();
      return structuredClone(settings);
    },
    async updateSettings(patch) {
      await wait();
      const root = patch.projectsRoot?.trim();
      if (root && (!root.startsWith('/') || root.includes('missing'))) throw new ApiError(422, `\u201c${root}\u201d is not a folder`);
      if (patch.dailyTurnLimit !== undefined && patch.dailyTurnLimit < 0) throw new ApiError(422, 'The daily limit cannot be negative (0 means no limit)');
      Object.assign(settings, patch, root !== undefined && { projectsRoot: root });
      return structuredClone(settings);
    },

    async getSnapshot() {
      await wait();
      return snapshot();
    },
    async streamSnapshots(onSnapshot, signal) {
      listeners.add(onSnapshot);
      onSnapshot(snapshot());
      await new Promise<void>((resolve) => signal.addEventListener('abort', () => resolve(), { once: true }));
      listeners.delete(onSnapshot);
    },
    async answer(requestId: string, answers: QuestionAnswer[]) {
      await wait();
      const request = find(requestId);
      request.entries.push({
        id: `u${Date.now()}`,
        at: nowIso(),
        role: 'user',
        kind: 'text',
        text: answers.map((a) => a.text ?? request.questions.find((q) => q.id === a.questionId)?.options.find((o) => o.id === a.optionId)?.label ?? '').join(' / '),
      });
      work(request, 'Thanks, that settles it. The variable exists in production but was never added to the staging secrets: add `STRIPE_WEBHOOK_SECRET` there and redeploy.');
    },
    async decideProposal(requestId, decision) {
      await wait();
      const request = find(requestId);
      if (decision === 'decline') {
        touch(request, { status: 'ready', proposal: null });
        return;
      }
      touch(request, { status: 'applying', proposal: null });
      setTimeout(() => {
        request.entries.push({ id: `m${Date.now()}`, at: nowIso(), role: 'assistant', kind: 'text', text: 'Done: the footer now uses the current year.' });
        touch(request, { status: 'done' });
      }, workMs);
    },
    async sendMessage(requestId, text, attachmentIds) {
      await wait();
      const request = find(requestId);
      const attachments: AttachmentRef[] = attachmentIds.map((id) => ({ id, name: `file-${id}`, mime: 'text/plain', size: 1024, kind: 'text' }));
      request.entries.push({ id: `u${Date.now()}`, at: nowIso(), role: 'user', kind: 'text', text, ...(attachments.length > 0 && { attachments }) });
      work(request, `Here is a bit more detail on that.\n\n- I looked at the files again.\n- Nothing else depends on it.\n\nAsked: ${text || '(files only)'}`);
    },
    async ignore(requestId) {
      await wait();
      touch(find(requestId), { status: 'ignored' });
    },
    async retry(requestId) {
      await wait();
      work(find(requestId), 'I looked again and the answer is the same: nothing changed on my side.');
    },
    async stop(requestId) {
      await wait();
      touch(find(requestId), { status: 'ready' });
    },
    async remove(requestId) {
      await wait();
      requests = requests.filter((item) => item.id !== requestId);
      publish();
    },
    async uploadAttachment(_requestId, file) {
      await sleep(latency + slow(300));
      uploads += 1;
      return { id: `att-${uploads}`, name: file.name || 'file', mime: file.type, size: file.size, kind: file.type.startsWith('image/') ? 'image' : 'text' };
    },
    async getAttachment() {
      await wait();
      const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180"><rect width="320" height="180" fill="#262a31"/><text x="160" y="95" fill="#a4a9b3" font-size="16" text-anchor="middle">mock image</text></svg>';
      return new Blob([svg], { type: 'image/svg+xml' });
    },
  };
}
