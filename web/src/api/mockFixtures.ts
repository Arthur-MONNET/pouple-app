import type { Lang } from '../i18n';
import { mockText } from './mockText';
import type { AssistantRequest, AssistantStatus, ConnectorStatus, Profile, RequestSource } from './types';

const minutesAgo = (minutes: number): string => new Date(Date.now() - minutes * 60_000).toISOString();

export function slackConnector(lang: Lang): ConnectorStatus {
  const text = mockText(lang).slack;
  const guide = text.guide;
  return {
    connected: false,
    descriptor: {
      id: 'slack',
      name: 'Slack',
      tagline: text.tagline,
      required: true,
      order: 1,
      fields: [
        {
          id: 'token',
          label: text.label,
          help: text.help,
          placeholder: 'xoxp-…',
          secret: true,
          pattern: '^xox[a-z]-[A-Za-z0-9-]{20,}$',
          patternHint: text.patternHint,
        },
        {
          id: 'appToken',
          label: text.live.label,
          help: text.live.help,
          placeholder: 'xapp-…',
          secret: true,
          optional: true,
          pattern: '^xapp-[A-Za-z0-9-]{20,}$',
          patternHint: text.live.hint,
        },
      ],
      guide: [
        { title: guide[0] ?? '', body: guide[1] ?? '', link: { label: text.linkLabel, url: 'https://api.slack.com/apps?new_app=1' } },
        { title: guide[2] ?? '', body: guide[3] ?? '' },
        { title: guide[4] ?? '', body: guide[5] ?? '' },
        { title: text.live.title, body: text.live.body },
      ],
    },
  };
}

export function codexConnector(lang: Lang): ConnectorStatus {
  const text = mockText(lang).codex;
  const guide = text.guide;
  return {
    connected: false,
    descriptor: {
      id: 'codex',
      name: 'Codex',
      tagline: text.tagline,
      required: true,
      order: 2,
      fields: [],
      guide: [
        { title: guide[0] ?? '', body: guide[1] ?? '' },
        { title: guide[2] ?? '', body: guide[3] ?? '' },
        { title: guide[4] ?? '', body: guide[5] ?? '' },
      ],
      actions: [
        { id: 'install', label: text.install },
        { id: 'login', label: text.login },
      ],
    },
  };
}

export function slackProfile(lang: Lang): Profile {
  const p = mockText(lang).profiles.slack;
  return {
    name: 'Arthur Monnet',
    subtitle: p.subtitle,
    facts: p.facts.map((label, i) => ({ label, value: p.values[i] ?? '' })),
    warnings: [{ title: p.warningTitle, detail: p.warningDetail }],
  };
}

export function codexProfile(lang: Lang): Profile {
  const p = mockText(lang).profiles.codex;
  const [plan, codex, organization, until] = p.facts;
  return {
    name: 'arthur@acme.example',
    subtitle: p.subtitle,
    facts: [
      { label: plan, value: 'Plus' },
      { label: codex, value: '0.52.0' },
      { label: organization, value: lang === 'fr' ? 'Personnel' : 'Personal' },
      { label: until, value: p.until },
    ],
    warnings: [],
  };
}

let counter = 0;
const nextId = (prefix: string): string => `${prefix}-${++counter}`;

function source(overrides: Partial<RequestSource>): RequestSource {
  return {
    origin: 'mention',
    channelId: 'C024BE91L',
    channelName: 'front',
    senderId: 'U0123',
    senderName: 'Morgan',
    ts: '1730812345.000100',
    permalink: 'https://example.slack.com/archives/C024BE91L/p1730812345000100',
    ...overrides,
  };
}

interface Seed {
  status: AssistantStatus;
  minutes: number;
  text: string;
  source?: Partial<RequestSource>;
  patch?: Partial<AssistantRequest>;
}

function build(seed: Seed): AssistantRequest {
  const id = nextId('req');
  const base: AssistantRequest = {
    id,
    source: source(seed.source ?? {}),
    text: seed.text,
    context: '',
    receivedAt: minutesAgo(seed.minutes),
    updatedAt: minutesAgo(Math.max(0, seed.minutes - 1)),
    status: seed.status,
    summary: null,
    draftReply: null,
    questions: [],
    proposal: null,
    entries: [],
    error: null,
    continuable: false,
  };
  return { ...base, ...seed.patch };
}

const step = (id: string, text: string, minutes: number): AssistantRequest['entries'][number] => ({
  id,
  at: minutesAgo(minutes),
  role: 'assistant',
  kind: 'step',
  text,
});

const reply = (id: string, text: string, minutes: number): AssistantRequest['entries'][number] => ({
  id,
  at: minutesAgo(minutes),
  role: 'assistant',
  kind: 'text',
  text,
});

/** One request in every status, so each state of the interface can be looked at, in the interface language. */
export function createFixtures(lang: Lang): AssistantRequest[] {
  const f = mockText(lang).fixtures;
  const q = (index: number): string => f.q[index] ?? '';
  return [
    build({
      status: 'analyzing',
      minutes: 2,
      text: q(0),
      source: { channelName: 'billing', senderName: 'Camille' },
      patch: { entries: [step('s1', '$ rg "exportInvoices" -n src', 2), step('s2', f.steps[0], 1)] },
    }),
    build({
      status: 'awaiting-user',
      minutes: 9,
      text: q(1),
      source: { channelName: 'platform', senderName: 'Nadia' },
      patch: {
        summary: f.stagingSummary,
        entries: [step('s1', '$ gh run list --limit 5', 9), reply('e1', f.stagingEntry, 8)],
        questions: [
          {
            id: 'q1',
            text: f.staging.q1,
            options: [
              { id: 'prod', label: f.staging.prod, description: f.staging.prodDesc },
              { id: 'preview', label: f.staging.preview, description: null },
            ],
          },
          {
            id: 'q2',
            text: f.staging.q2,
            options: [
              { id: 'yes', label: f.staging.yes, description: f.staging.yesDesc },
              { id: 'no', label: f.staging.no, description: null },
            ],
          },
        ],
      },
    }),
    build({
      status: 'awaiting-user',
      minutes: 22,
      text: q(2),
      source: { origin: 'direct-message', channelName: null, channelId: 'D024BE91L', senderName: 'Marc' },
      patch: {
        summary: f.footer.summary,
        continuable: true,
        entries: [reply('e1', f.footer.entry, 21)],
        proposal: { title: f.footer.title, description: f.footer.description, projectId: 'front' },
      },
    }),
    build({
      status: 'ready',
      minutes: 45,
      text: q(3),
      source: { channelName: 'front', senderName: 'Morgan' },
      patch: {
        continuable: true,
        summary: f.loginSummary,
        draftReply: f.loginDraft,
        entries: [step('s1', '$ rg "csrf" -n src', 46), step('s2', f.steps[1], 45), reply('e1', f.login, 44)],
      },
    }),
    build({
      status: 'applying',
      minutes: 70,
      text: q(4),
      source: { origin: 'thread-reply', channelName: 'product', senderName: 'Léa' },
      patch: { entries: [reply('e1', f.rename, 65), step('s1', '$ sed -i "" "s/Workspace/Team/g" settings/GeneralTab.vue', 64)] },
    }),
    build({
      status: 'done',
      minutes: 180,
      text: q(5),
      source: { origin: 'note-to-self', channelName: null, channelId: 'D0SELF', senderName: 'Arthur', permalink: null },
      patch: { continuable: true, entries: [reply('e1', f.noteDone, 170)] },
    }),
    build({
      status: 'ignored',
      minutes: 600,
      text: q(6),
      source: { origin: 'broadcast', channelName: 'random', senderName: 'Sophie' },
    }),
    build({
      status: 'failed',
      minutes: 1500,
      text: q(7),
      source: { origin: 'keyword', channelName: 'ops', senderName: 'Hugo' },
      patch: { error: f.limit },
    }),
    build({
      status: 'queued',
      minutes: 0,
      text: q(8),
      source: { origin: 'group-message', channelName: null, channelId: 'G0123', senderName: 'Inès' },
    }),
  ];
}
