import { ApiError, type PoupleApi } from './client';
import { lang as activeLang, type Lang } from '../i18n';
import { codexConnector, codexProfile, createFixtures, slackConnector, slackProfile } from './mockFixtures';
import { mockText } from './mockText';
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
  /** Starting interface language (default: the active one). */
  lang?: Lang;
  /** `empty`: no conversation at all; `paused`: the assistant is off and says why. */
  view?: 'empty' | 'paused';
}

const sleep = (ms: number, signal?: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener('abort', () => (clearTimeout(timer), resolve()), { once: true });
  });

const nowIso = (): string => new Date().toISOString();

/**
 * An in-memory backend with realistic data, so the whole interface can be developed and looked at without the core
 * (`VITE_MOCK=1`). Slack accepts any well-formed token that does not contain "bad"; Codex starts "not installed".
 */
export function createMockApi(options: MockOptions = {}): PoupleApi {
  const { latency = 250, connectors: mockConnectors = true, workMs = 2200 } = options;
  let lang: Lang = options.lang ?? activeLang.value;
  const text = (): ReturnType<typeof mockText> => mockText(lang);
  const connectors: ConnectorStatus[] = [slackConnector(lang), codexConnector(lang)];
  const codex = { installed: false, signedIn: false };
  if (options.ready) {
    codex.installed = codex.signedIn = true;
    for (const status of connectors) Object.assign(status, { connected: true, profile: status.descriptor.id === 'slack' ? slackProfile(lang) : codexProfile(lang), connectedAt: nowIso() });
  }
  let requests: AssistantRequest[] = options.view === 'empty' ? [] : createFixtures(lang);
  const listeners = new Set<(snapshot: AssistantSnapshot) => void>();
  let uploads = 0;
  const settings: Settings = { pollSeconds: 60, dailyTurnLimit: 150, projectsRoot: '', language: '', uiLanguage: lang, autoApprove: 'off', keywords: [], ignoreChannels: [] };

  /** The core speaks the interface language: descriptors, profiles and the sample conversations follow it. */
  function switchLanguage(next: Lang): void {
    lang = next;
    const fresh = [slackConnector(lang), codexConnector(lang)];
    for (const [index, status] of connectors.entries()) {
      const replacement = fresh[index];
      if (!replacement) continue;
      status.descriptor = replacement.descriptor;
      if (status.connected) status.profile = status.descriptor.id === 'slack' ? slackProfile(lang) : codexProfile(lang);
    }
    if (options.view !== 'empty') requests = createFixtures(lang);
    publish();
  }

  const wait = (): Promise<void> => sleep(latency);
  /** With no latency (tests) nothing waits; otherwise the pauses of slow operations keep their real feel. */
  const slow = (ms: number): number => (latency === 0 ? 0 : ms);

  const snapshot = (): AssistantSnapshot => ({
    status: {
      active: options.view !== 'paused',
      problem: options.view === 'paused' ? text().paused : null,
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
    const field = connectors[0]?.descriptor.fields?.[0];
    const label = field?.label ?? '';
    if (token === '') throw new ApiError(422, text().errors.required, 'token');
    if (field?.pattern && !new RegExp(field.pattern).test(token)) throw new ApiError(422, `${label} ${field.patternHint ?? ''}`.trim(), 'token');
    if (token.startsWith('xoxb')) throw new ApiError(422, text().errors.botToken, 'token');
    if (token.includes('bad')) throw new ApiError(422, text().errors.invalidAuth, 'token');
  }

  function currentCodexProfile(): Profile {
    if (!codex.installed) throw new ApiError(422, text().errors.notInstalled);
    if (!codex.signedIn) throw new ApiError(422, text().errors.notSignedIn);
    return codexProfile(lang);
  }

  const profileFor = (id: string, values: ConnectorValues): Profile => {
    if (id === 'slack') return (checkSlackValues(values), slackProfile(lang));
    if (id === 'codex') return currentCodexProfile();
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
      if (!mockConnectors) return { setupComplete: true, steps: [], language: lang };
      const steps = connectors.map((c) => ({ id: c.descriptor.id, name: c.descriptor.name, required: c.descriptor.required, connected: c.connected }));
      return { setupComplete: steps.every((s) => !s.required || s.connected), steps, language: lang };
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
        await lines(text().logs.install);
        codex.installed = true;
        emit({ type: 'done' });
      } else if (action === 'login') {
        await lines(text().logs.login1);
        emit({ type: 'open', url: 'https://auth.openai.com/oauth/authorize?mock=1' });
        await lines([text().logs.login2], slow(1500));
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
      if (root && (!root.startsWith('/') || root.includes('missing'))) throw new ApiError(422, text().errors.notFolder(root));
      if (patch.dailyTurnLimit !== undefined && patch.dailyTurnLimit < 0) throw new ApiError(422, text().errors.negativeLimit);
      Object.assign(settings, patch, root !== undefined && { projectsRoot: root });
      if (patch.uiLanguage && patch.uiLanguage !== lang) switchLanguage(patch.uiLanguage);
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
      work(request, text().replies.answered);
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
        request.entries.push({ id: `m${Date.now()}`, at: nowIso(), role: 'assistant', kind: 'text', text: text().replies.applied });
        touch(request, { status: 'done' });
      }, workMs);
    },
    async sendMessage(requestId, asked, attachmentIds) {
      await wait();
      const request = find(requestId);
      const attachments: AttachmentRef[] = attachmentIds.map((id) => ({ id, name: `file-${id}`, mime: 'text/plain', size: 1024, kind: 'text' }));
      request.entries.push({ id: `u${Date.now()}`, at: nowIso(), role: 'user', kind: 'text', text: asked, ...(attachments.length > 0 && { attachments }) });
      work(request, text().replies.more(asked || text().replies.filesOnly));
    },
    async ignore(requestId) {
      await wait();
      touch(find(requestId), { status: 'ignored' });
    },
    async retry(requestId) {
      await wait();
      work(find(requestId), text().replies.again);
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
