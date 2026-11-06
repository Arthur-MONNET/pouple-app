import type { AssistantRequest, AssistantStatus, ConnectorStatus, RequestSource } from './types';

const minutesAgo = (minutes: number): string => new Date(Date.now() - minutes * 60_000).toISOString();

export const SLACK_CONNECTOR: ConnectorStatus = {
  connected: false,
  descriptor: {
    id: 'slack',
    name: 'Slack',
    tagline: 'Where your requests come from',
    required: true,
    order: 1,
    fields: [
      {
        id: 'token',
        label: 'User OAuth Token',
        help: 'Starts with xoxp-. It stays in your Mac’s keychain and is never shown again.',
        placeholder: 'xoxp-…',
        secret: true,
        pattern: '^xox[a-z]-[A-Za-z0-9-]{20,}$',
        patternHint: 'does not look like a Slack token (it starts with xoxp-)',
      },
    ],
    guide: [
      {
        title: 'Create the Slack app',
        body: 'One click: Slack opens with the name and every permission already filled in. Choose your workspace and confirm.',
        link: { label: 'Create the Slack app', url: 'https://api.slack.com/apps?new_app=1' },
      },
      { title: 'Install it to your workspace', body: 'On the app’s page, open Install App and click Install to Workspace, then Allow.' },
      { title: 'Copy the User OAuth Token', body: 'It is on the same page and starts with xoxp-. Paste it here.' },
    ],
  },
};

export const CODEX_CONNECTOR: ConnectorStatus = {
  connected: false,
  descriptor: {
    id: 'codex',
    name: 'Codex',
    tagline: 'The assistant that looks into your requests',
    required: true,
    order: 2,
    fields: [],
    guide: [
      { title: 'Install the Codex CLI', body: 'Pouple uses OpenAI’s Codex on your Mac. If it is not installed, one click installs it.' },
      { title: 'Sign in with ChatGPT', body: 'A browser page opens: sign in with the account you want Pouple to use. It has its own login and never touches your usual Codex.' },
      { title: 'Check the account', body: 'Pouple shows who is signed in, so you can be sure it is the right one.' },
    ],
    actions: [
      { id: 'install', label: 'Install Codex' },
      { id: 'login', label: 'Sign in with ChatGPT' },
    ],
  },
};

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

const LONG_ANSWER = `The login form posts to \`/api/session\`, but the request is sent before the CSRF cookie is set.

**What I found**
- \`LoginForm.vue\` calls \`submit()\` on mount when the browser autofills the fields.
- The CSRF cookie is created by \`useSession()\`, which resolves after the first paint.
- On a slow connection the first POST therefore goes out without the cookie and the API answers 403.

**Why it is intermittent**
It only happens when the autofill fires before the session bootstrap ends: Safari does it most of the time, Chrome rarely.

1. Await \`useSession().ready\` before the first submit.
2. Disable the button until the cookie exists.
3. Add a regression test with a delayed bootstrap.

\`\`\`ts
await session.ready;
await api.post('/session', credentials);
\`\`\`

I did not change anything. Say the word and I will prepare the fix in the \`front\` project.`;

/** One request in every status, so each state of the interface can be looked at. */
export function createFixtures(): AssistantRequest[] {
  return [
    build({
      status: 'analyzing',
      minutes: 2,
      text: '@arthur do you know why the invoice export is empty for the March period?',
      source: { channelName: 'billing', senderName: 'Camille' },
      patch: {
        entries: [
          step('s1', '$ rg "exportInvoices" -n src', 2),
          step('s2', 'Reading src/billing/export.ts', 1),
        ],
      },
    }),
    build({
      status: 'awaiting-user',
      minutes: 9,
      text: 'Can you check the failing deploy on staging? I think it is the new env var.',
      source: { channelName: 'platform', senderName: 'Nadia' },
      patch: {
        summary: 'Two staging deploys failed with a missing variable.',
        entries: [
          step('s1', '$ gh run list --limit 5', 9),
          { id: 'e1', at: minutesAgo(8), role: 'assistant', kind: 'text', text: 'The last two deploys failed on `STRIPE_WEBHOOK_SECRET`. I need to know which environment to look at.' },
        ],
        questions: [
          {
            id: 'q1',
            text: 'Which environment should I compare it with?',
            options: [
              { id: 'prod', label: 'Production', description: 'Check what the live deploy uses.' },
              { id: 'preview', label: 'Preview', description: null },
            ],
          },
          {
            id: 'q2',
            text: 'Should I look at the CI logs too?',
            options: [
              { id: 'yes', label: 'Yes, read the logs', description: 'Read-only, last 200 lines.' },
              { id: 'no', label: 'No, just the configuration', description: null },
            ],
          },
        ],
      },
    }),
    build({
      status: 'awaiting-user',
      minutes: 22,
      text: 'The footer still shows 2023, can you fix it?',
      source: { origin: 'direct-message', channelName: null, channelId: 'D024BE91L', senderName: 'Marc' },
      patch: {
        summary: 'The year is hardcoded in the footer.',
        continuable: true,
        entries: [
          { id: 'e1', at: minutesAgo(21), role: 'assistant', kind: 'text', text: 'The year is hardcoded in `AppFooter.vue`. A one-line change replaces it with the current year.' },
        ],
        proposal: {
          title: 'Use the current year in AppFooter.vue',
          description: 'Replace the literal 2023 with new Date().getFullYear().',
          projectId: 'front',
        },
      },
    }),
    build({
      status: 'ready',
      minutes: 45,
      text: 'Login is flaky on Safari, the form sometimes answers 403. Any idea?',
      source: { channelName: 'front', senderName: 'Morgan' },
      patch: {
        continuable: true,
        summary: 'The first POST races the CSRF cookie.',
        draftReply: 'Thanks for the report. It is a race between the autofill submit and the CSRF cookie: I will await the session before the first submit and ship it today.',
        entries: [step('s1', '$ rg "csrf" -n src', 46), step('s2', 'Reading LoginForm.vue', 45), { id: 'e1', at: minutesAgo(44), role: 'assistant', kind: 'text', text: LONG_ANSWER }],
      },
    }),
    build({
      status: 'applying',
      minutes: 70,
      text: 'Please rename the "Workspace" label to "Team" everywhere in settings.',
      source: { origin: 'thread-reply', channelName: 'product', senderName: 'Léa' },
      patch: {
        entries: [
          { id: 'e1', at: minutesAgo(65), role: 'assistant', kind: 'text', text: 'Found 6 occurrences in `settings/`. Applying the rename.' },
          step('s1', '$ sed -i "" "s/Workspace/Team/g" settings/GeneralTab.vue', 64),
        ],
      },
    }),
    build({
      status: 'done',
      minutes: 180,
      text: 'Note to self: bump the Node version in the CI image to 22.',
      source: { origin: 'note-to-self', channelName: null, channelId: 'D0SELF', senderName: 'Arthur', permalink: null },
      patch: {
        continuable: true,
        entries: [{ id: 'e1', at: minutesAgo(170), role: 'assistant', kind: 'text', text: 'Done: `.github/workflows/ci.yml` now uses Node 22. The change is committed on a local branch, nothing is pushed.' }],
      },
    }),
    build({
      status: 'ignored',
      minutes: 600,
      text: '@here lunch order closes at noon!',
      source: { origin: 'broadcast', channelName: 'random', senderName: 'Sophie' },
    }),
    build({
      status: 'failed',
      minutes: 1500,
      text: 'Could you look at the memory spike on the worker?',
      source: { origin: 'keyword', channelName: 'ops', senderName: 'Hugo' },
      patch: { error: 'Codex needs a breather. Back at 18:30.' },
    }),
    build({
      status: 'queued',
      minutes: 0,
      text: 'Quick one: which port does the local API use?',
      source: { origin: 'group-message', channelName: null, channelId: 'G0123', senderName: 'Inès' },
    }),
  ];
}
