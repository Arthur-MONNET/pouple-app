import { ApiError, type PoupleApi } from './client';
import { lang as activeLang, type Lang } from '../i18n';
import { codexConnector, codexProfile, createAlerts, createFixtures, createMemory, createTopics, slackConnector, slackProfile } from './mockFixtures';
import { checkExtraValues, extraConnectors, extraProfile, isExtraId } from './mockExtras';
import { mockText } from './mockText';
import { mockProviderModels, mockProviderTest, mockProviders } from './mockProviders';
import { mockCheckResult, mockUpdateInfo, type UpdateScenario } from './mockUpdate';
import type {
  ActionEvent,
  AppState,
  AssistantAlert,
  AssistantRequest,
  AssistantSnapshot,
  AssistantTopic,
  AttachmentRef,
  ConnectorStatus,
  ConnectorValues,
  GitLabAccess,
  Profile,
  QuestionAnswer,
  MemoryView,
  Settings,
  UpdateChannel,
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
  /** What the Updates section starts with (default `uptodate`). */
  update?: UpdateScenario;
}

const sleep = (ms: number, signal?: AbortSignal): Promise<void> =>
  new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    signal?.addEventListener('abort', () => (clearTimeout(timer), resolve()), { once: true });
  });

const nowIso = (): string => new Date().toISOString();

/** The folders of the mock projects folder, by name, as GET /api/assistant/projects lists them. */
const MOCK_PROJECTS: readonly string[] = ['api', 'front', 'mobile'];

/**
 * An in-memory backend with realistic data, so the whole interface can be developed and looked at without the core
 * (`VITE_MOCK=1`). Slack accepts any well-formed token that does not contain "bad"; Codex starts "not installed".
 */
export function createMockApi(options: MockOptions = {}): PoupleApi {
  const { latency = 250, connectors: mockConnectors = true, workMs = 2200 } = options;
  let lang: Lang = options.lang ?? activeLang.value;
  const text = (): ReturnType<typeof mockText> => mockText(lang);
  const connectors: ConnectorStatus[] = [slackConnector(lang), codexConnector(lang), ...extraConnectors(lang)];
  const codex = { installed: false, signedIn: false };
  // The main window as most people see it: Slack (now optional) and Codex connected, and a projects folder chosen.
  if (options.ready) {
    codex.installed = codex.signedIn = true;
    for (const status of connectors.filter((c) => c.descriptor.id === 'slack' || c.descriptor.id === 'codex')) Object.assign(status, { connected: true, profile: status.descriptor.id === 'slack' ? slackProfile(lang) : codexProfile(lang), connectedAt: nowIso() });
  }
  let requests: AssistantRequest[] = options.view === 'empty' ? [] : createFixtures(lang);
  let topics: AssistantTopic[] = options.view === 'empty' ? [] : createTopics(lang);
  let alerts: AssistantAlert[] = options.view === 'empty' ? [] : createAlerts(lang);
  let alertCounter = 0;
  let memory: MemoryView = createMemory();
  /** The memory as the core shows it: on exactly when the setting says so. */
  const view = (): MemoryView => structuredClone({ ...memory, enabled: settings.memory === true });
  const listeners = new Set<(snapshot: AssistantSnapshot) => void>();
  let uploads = 0;
  /** Files uploaded for a conversation not started yet, by id: creating the conversation takes them. */
  const drafts = new Map<string, AttachmentRef>();
  let created = 0;
  const style = { learned: true };
  const settings: Settings = { pollSeconds: 60, dailyTurnLimit: 150, projectsRoot: options.ready ? '/Users/you/Workspace' : '',language: '', uiLanguage: lang, autoApprove: 'off', keywords: [], ignoreChannels: [], screenshotHosts: [], provider: 'codex', models: { codex: '', claude: '' }, webAccess: true, gitlabPipelines: 'all', betaUpdates: false, memory: false, memoryFolder: '/Users/you/SecondBrain' };

  const profileOf = (id: string): Profile => (id === 'slack' ? slackProfile(lang) : isExtraId(id) ? extraProfile(id, lang) : codexProfile(lang));

  /** The core speaks the interface language: descriptors, profiles and the sample conversations follow it. */
  function switchLanguage(next: Lang): void {
    lang = next;
    const fresh = [slackConnector(lang), codexConnector(lang), ...extraConnectors(lang)];
    for (const [index, status] of connectors.entries()) {
      const replacement = fresh[index];
      if (!replacement) continue;
      status.descriptor = replacement.descriptor;
      if (status.connected) status.profile = profileOf(status.descriptor.id);
    }
    if (options.view !== 'empty') {
      requests = createFixtures(lang);
      topics = createTopics(lang);
      alerts = createAlerts(lang);
    }
    publish();
  }

  let updateScenario: UpdateScenario = options.update ?? 'uptodate';
  const channel = (): UpdateChannel => (settings.betaUpdates ? 'beta' : 'stable');

  const wait = (): Promise<void> => sleep(latency);
  /** With no latency (tests) nothing waits; otherwise the pauses of slow operations keep their real feel. */
  const slow = (ms: number): number => (latency === 0 ? 0 : ms);

  const snapshot = (): AssistantSnapshot => ({
    status: {
      active: options.view !== 'paused',
      problem: options.view === 'paused' ? text().paused : null,
      lastPollAt: nowIso(),
      notices: [],
      usage: {
        today: 7,
        limit: 40,
        tokensByAI: { codex: { input: 412_000, cached: 351_000, output: 9_800 }, claude: { input: 61_000, cached: 40_000, output: 2_100 } },
        limits: {
          codex: { fiveHour: { usedPercent: 9, resetsAt: new Date(Date.now() + 3 * 3600_000).toISOString() }, week: { usedPercent: 78, resetsAt: new Date(Date.now() + 2 * 86400_000).toISOString() }, seenAt: nowIso() },
          claude: { fiveHour: { usedPercent: 92, resetsAt: new Date(Date.now() + 1 * 3600_000).toISOString() }, week: { usedPercent: 64, resetsAt: new Date(Date.now() + 4 * 86400_000).toISOString() }, seenAt: nowIso() },
        },
        machine: {
          disk: { freeBytes: 1_847_410_688, totalBytes: 494_384_795_648, level: 'critical' },
          memory: { usedPercent: 61, totalBytes: 25_769_803_776, level: 'high' },
        },
      },
    },
    requests: structuredClone(requests),
    topics: structuredClone(topics.filter((topic) => requests.some((item) => item.topicId === topic.id))),
    alerts: structuredClone(alerts),
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

  function uploaded(file: File): AttachmentRef {
    uploads += 1;
    return { id: `att-${uploads}`, name: file.name || 'file', mime: file.type, size: file.size, kind: file.type.startsWith('image/') ? 'image' : 'text' };
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
    if (isExtraId(id)) return (checkExtraValues(id, values, lang), extraProfile(id, lang));
    throw new ApiError(404, `unknown connector: ${id}`);
  };

  const statusOf = (id: string): ConnectorStatus => {
    const status = connectors.find((item) => item.descriptor.id === id);
    if (!status) throw new ApiError(404, `unknown connector: ${id}`);
    return status;
  };

  const gitLabAccess: GitLabAccess = {
    mode: 'read-only', selected: 'primary',
    tokens: [{ id: 'primary', name: 'Pouple', scopes: ['read_api'] }],
  };

  return {
    async getGitLabAccess() {
      return structuredClone(gitLabAccess);
    },
    async updateGitLabAccess(update) {
      if (update.primaryName) gitLabAccess.tokens[0]!.name = update.primaryName;
      if (update.token && update.name) {
        gitLabAccess.tokens[1] = { id: 'additional', name: update.name, scopes: ['api'] };
        if (gitLabAccess.selected === 'additional') gitLabAccess.mode = 'read-only';
      }
      if (update.selected && update.selected !== gitLabAccess.selected) {
        gitLabAccess.selected = update.selected;
        gitLabAccess.mode = 'read-only';
      }
      if (update.mode) gitLabAccess.mode = update.mode;
      return structuredClone(gitLabAccess);
    },
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
      const folder = patch.memoryFolder?.trim();
      if (folder && (!folder.startsWith('/') || folder === '/')) throw new ApiError(422, text().errors.notFolder(folder));
      Object.assign(settings, patch, root !== undefined && { projectsRoot: root }, folder !== undefined && { memoryFolder: folder === '' ? '/Users/you/SecondBrain' : folder });
      if (patch.uiLanguage && patch.uiLanguage !== lang) switchLanguage(patch.uiLanguage);
      return structuredClone(settings);
    },

    async getProviders() {
      await wait();
      return mockProviders(settings);
    },
    async getProviderModels(id) {
      await wait();
      return mockProviderModels(id);
    },
    async testProvider(id, model) {
      await wait();
      return mockProviderTest(id, model);
    },
    async getUpdate() {
      await wait();
      return mockUpdateInfo(updateScenario, lang, new Date(), channel());
    },
    async checkUpdate() {
      await sleep(latency + slow(1500));
      const result = mockCheckResult(updateScenario, lang, channel());
      // A check that was running ends on the available state; the next read agrees.
      if (updateScenario === 'checking') updateScenario = 'available';
      return result;
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
        text: answers
          .map((a) => {
            const options = request.questions.find((q) => q.id === a.questionId)?.options ?? [];
            const picked = [a.optionId, ...(a.optionIds ?? [])].filter(Boolean).map((id) => options.find((o) => o.id === id)?.label ?? '');
            return [...picked, a.text ?? ''].filter(Boolean).join(', ');
          })
          .join(' / '),
      });
      work(request, text().replies.answered);
    },
    async decidePlan(requestId, decision, _planId, actions) {
      await wait();
      const request = find(requestId);
      const kept = request.plan && actions ? request.plan.actions.filter((_, index) => actions.includes(index)) : request.plan?.actions;
      touch(request, { status: decision === 'approve' ? 'done' : 'ready', ...(request.plan && { plan: { ...request.plan, ...(kept && { actions: kept }), status: decision === 'approve' ? 'approved' : 'declined' } }) });
    },
    async decideReminders(requestId, decision, offerId, items = []) {
      await wait();
      const request = find(requestId);
      const offer = request.reminders;
      if (!offer || offer.status !== 'proposed' || offer.id !== offerId) throw new ApiError(409, 'no reminder is waiting for a choice');
      if (decision === 'decline') {
        touch(request, { reminders: { ...offer, status: 'declined' } });
        return;
      }
      const made = items.map((pick) => {
        const option = offer.items.find((item) => item.id === pick.id);
        if (!option) throw new ApiError(400, 'unknown reminder');
        const at = new Date(pick.at ?? option.at);
        if (Number.isNaN(at.getTime()) || at.getTime() <= Date.now()) throw new ApiError(400, text().errors.alertPast);
        return { id: `alert-${++alertCounter + 100}`, title: option.label, ...(option.note && { note: option.note }), at: at.toISOString(), requestId, createdAt: nowIso() };
      });
      if (made.length === 0) throw new ApiError(400, 'choose at least one reminder');
      alerts = [...alerts, ...made].sort((a, b) => a.at.localeCompare(b.at));
      request.entries.push({ id: `u${Date.now()}`, at: nowIso(), role: 'user', kind: 'text', text: made.map((alert) => `- ${alert.title}`).join('\n') });
      touch(request, { reminders: { ...offer, status: 'accepted', chosen: items.map((pick) => pick.id) } });
    },
    async undoMemoryChange(requestId, changeId) {
      await wait();
      const request = find(requestId);
      const changes = request.memoryChanges ?? [];
      const change = changes.find((entry) => entry.id === changeId);
      if (changeId === '' || !change) throw new ApiError(404, 'unknown memory change');
      if (!settings.memory) throw new ApiError(409, 'the memory is off');
      if (change.undone) throw new ApiError(409, 'this change was undone already');
      touch(request, { memoryChanges: changes.map((entry) => (entry === change ? { ...entry, undone: true } : entry)) });
    },
    async shareMemoryFact(requestId, changeId, itemId, shared) {
      await wait();
      const request = find(requestId);
      const changes = request.memoryChanges ?? [];
      const change = changes.find((entry) => entry.id === changeId);
      if (!change || !change.items.some((item) => item.id === itemId)) throw new ApiError(404, 'unknown memory change or fact');
      if (!settings.memory) throw new ApiError(409, 'the memory is off');
      if (change.undone) throw new ApiError(409, 'this change was undone already');
      const items = change.items.map((item) => (item.id === itemId ? { ...item, shared } : item));
      touch(request, { memoryChanges: changes.map((entry) => (entry === change ? { ...entry, items } : entry)) });
    },
    async createAlert(input) {
      await wait();
      if (input.title.trim() === '') throw new ApiError(400, text().errors.alertTitle);
      const at = new Date(input.at);
      if (Number.isNaN(at.getTime()) || at.getTime() <= Date.now()) throw new ApiError(400, text().errors.alertPast);
      const alert: AssistantAlert = { id: `alert-${++alertCounter + 100}`, title: input.title.trim(), ...(input.note && { note: input.note }), at: at.toISOString(), createdAt: nowIso() };
      alerts = [...alerts, alert].sort((a, b) => a.at.localeCompare(b.at));
      publish();
      return alert;
    },
    async updateAlert(id, patch) {
      await wait();
      const alert = alerts.find((item) => item.id === id);
      if (!alert) throw new ApiError(404, 'unknown alert');
      if (patch.title !== undefined && patch.title.trim() === '') throw new ApiError(400, text().errors.alertTitle);
      if (patch.at !== undefined) {
        const at = new Date(patch.at);
        if (Number.isNaN(at.getTime()) || at.getTime() <= Date.now()) throw new ApiError(400, text().errors.alertPast);
        alert.at = at.toISOString();
      }
      if (patch.title !== undefined) alert.title = patch.title.trim();
      if (patch.note !== undefined) {
        if (patch.note.trim() === '') delete alert.note;
        else alert.note = patch.note.trim();
      }
      alerts.sort((a, b) => a.at.localeCompare(b.at));
      publish();
      return structuredClone(alert);
    },
    async deleteAlert(id) {
      await wait();
      if (!alerts.some((item) => item.id === id)) throw new ApiError(404, 'unknown alert');
      alerts = alerts.filter((item) => item.id !== id);
      publish();
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
    async continueWith(requestId) {
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
      return uploaded(file);
    },
    async uploadDraftAttachment(file) {
      await sleep(latency + slow(300));
      const ref = uploaded(file);
      drafts.set(ref.id, ref);
      return ref;
    },
    async listProjects() {
      await wait();
      return settings.projectsRoot === '' ? [] : MOCK_PROJECTS.map((name) => ({ id: name, name }));
    },
    async createConversation(body) {
      await wait();
      const asked = body.text.trim();
      if (asked === '' && body.attachmentIds.length === 0) throw new ApiError(400, text().create.empty);
      if (new TextEncoder().encode(asked).length > 4000) throw new ApiError(400, text().create.tooLong);
      const attachments: AttachmentRef[] = [];
      for (const fileId of body.attachmentIds) {
        const ref = drafts.get(fileId);
        if (!ref) throw new ApiError(400, text().create.unknownFile);
        attachments.push(ref);
      }
      if (body.projectId !== undefined && (settings.projectsRoot === '' || !MOCK_PROJECTS.includes(body.projectId))) throw new ApiError(400, text().create.unknownProject);
      const aiConnected = !mockConnectors || connectors.some((c) => c.descriptor.id === 'codex' && c.connected);
      if (options.view === 'paused' || !aiConnected) throw new ApiError(409, text().create.inactive);
      for (const id of body.attachmentIds) drafts.delete(id);
      const id = `new-${++created}`;
      const at = nowIso();
      const request: AssistantRequest = {
        id,
        source: { origin: 'in-app', channelId: `in-app:${id}`, channelName: null, senderId: 'self', senderName: 'Arthur', ts: `${Math.floor(Date.now() / 1000)}.000000`, permalink: null, label: (asked.split('\n', 1)[0] ?? '').slice(0, 80) },
        text: asked,
        context: '',
        ...(attachments.length > 0 && { attachments }),
        ...(body.projectId !== undefined && { projectId: body.projectId }),
        receivedAt: at,
        updatedAt: at,
        status: 'queued',
        summary: null,
        draftReply: null,
        questions: [],
        proposal: null,
        entries: [],
        error: null,
        engaged: true,
      };
      requests = [request, ...requests];
      publish();
      work(request, text().replies.started(body.projectId ?? null));
      return { id };
    },
    async getStyle() {
      await wait();
      return { learned: style.learned, ...(style.learned && { learnedAt: new Date().toISOString(), from: 42 }) };
    },
    async forgetStyle() {
      await wait();
      style.learned = false;
      return { learned: false };
    },
    async getMemory() {
      await wait();
      return view();
    },
    async reviewMemory(note, line, decision) {
      await wait();
      if (!settings.memory) throw new ApiError(409, 'the memory is off');
      const item = memory.review.find((entry) => entry.note === note && entry.line === line);
      if (!item) throw new ApiError(409, 'the line changed meanwhile');
      memory.review = memory.review.filter((entry) => entry !== item);
      const target = memory.notes.find((entry) => entry.path === note);
      if (target && decision === 'drop') target.facts = Math.max(0, target.facts - 1);
      return view();
    },
    async connectCli(id, connected) {
      await wait();
      const cli = memory.clis.find((entry) => entry.id === id);
      if (!cli) throw new ApiError(404, 'unknown CLI');
      if (connected && !settings.memory) throw new ApiError(409, 'the memory is off');
      cli.connected = connected;
      return view();
    },
    async revealMemory() {
      await wait();
      if (!memory.exists) throw new ApiError(404, 'the folder does not exist yet');
    },
    async tidyMemory() {
      await wait();
      if (!settings.memory) throw new ApiError(409, 'the memory is off');
      memory = { ...memory, tidiedAt: nowIso() };
    },
    async getBrowserFrame() {
      await wait();
      const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="200"><rect width="320" height="200" fill="#262a31"/><text x="160" y="105" fill="#a4a9b3" font-size="16" text-anchor="middle">mock browser</text></svg>';
      return new Blob([svg], { type: 'image/svg+xml' });
    },
    async getAttachment() {
      await wait();
      const svg = '<svg xmlns="http://www.w3.org/2000/svg" width="320" height="180"><rect width="320" height="180" fill="#262a31"/><text x="160" y="95" fill="#a4a9b3" font-size="16" text-anchor="middle">mock image</text></svg>';
      return new Blob([svg], { type: 'image/svg+xml' });
    },
  };
}
