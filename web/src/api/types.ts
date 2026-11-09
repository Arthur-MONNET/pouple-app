// Every JSON shape exchanged with the Pouple core. Connector shapes mirror core/internal/connector; the conversation
// shapes mirror the assistant contract served under /api/assistant. Keep this the only file that describes the wire.

// ---------------------------------------------------------------------------------------------------------------
// Connectors
// ---------------------------------------------------------------------------------------------------------------

export interface Field {
  id: string;
  label: string;
  help?: string;
  placeholder?: string;
  /** Secret fields are write-only: the core never sends their value back. */
  secret: boolean;
  /** An optional field may stay empty. */
  optional?: boolean;
  /** Prefilled in the form, and used by the core when the field is left empty. */
  default?: string;
  /** A regular expression the value must match. */
  pattern?: string;
  patternHint?: string;
}

export interface Link {
  label: string;
  url: string;
  /** Id of a field holding the user's own server address (a self-hosted service): the link then opens on that host. */
  hostField?: string;
}

export interface GuideStep {
  title: string;
  body: string;
  link?: Link;
}

export interface ActionInfo {
  id: string;
  label: string;
}

export interface Descriptor {
  /** Connectors sharing a choice group are alternatives: one connected is enough (Codex or Claude). */
  choice?: string;
  id: string;
  name: string;
  tagline: string;
  required: boolean;
  order: number;
  /** `null` when the Go side has no field (a nil slice is encoded as null). */
  fields: Field[] | null;
  guide: GuideStep[] | null;
  actions?: ActionInfo[];
}

export interface Fact {
  label: string;
  value: string;
}

export interface Warning {
  title: string;
  detail: string;
}

export interface Profile {
  name: string;
  subtitle?: string;
  avatarUrl?: string;
  facts: Fact[] | null;
  warnings: Warning[] | null;
}

export interface ConnectorStatus {
  descriptor: Descriptor;
  connected: boolean;
  profile?: Profile;
  connectedAt?: string;
}

export interface SetupStepState {
  id: string;
  name: string;
  required: boolean;
  connected: boolean;
}

/** GET /api/state */
export interface AppState {
  setupComplete: boolean;
  /** Interface language the core is set to. */
  language?: UiLanguage;
  steps: SetupStepState[];
}

/** Values typed in a connector form, by field id. */
export type ConnectorValues = Record<string, string>;

/** Body of a 422 answer: something the user can fix, with the field when there is one. */
export interface ValidationFailure {
  field?: string;
  message: string;
}

/** One message of a streamed connector action (`data: {json}` lines of a POST SSE answer). */
export type ActionEvent =
  | { type: 'log'; text: string }
  | { type: 'open'; url: string }
  | { type: 'done' }
  | { type: 'error'; text: string };

// ---------------------------------------------------------------------------------------------------------------
// Conversations (/api/assistant)
// ---------------------------------------------------------------------------------------------------------------

/** Why a Slack message reached the assistant: the same situations in which Slack itself would notify. */
export type RequestOrigin =
  | 'mention'
  | 'direct-message'
  | 'note-to-self'
  | 'group-message'
  | 'broadcast'
  | 'keyword'
  | 'thread-reply'
  | 'channel-message';

export interface RequestSource {
  origin: RequestOrigin;
  channelId: string;
  /** Channel name for channels, null for direct messages. */
  channelName: string | null;
  senderId: string;
  senderName: string;
  /** Slack message timestamp, e.g. "1730812345.000100". */
  ts: string;
  permalink: string | null;
}

export type AssistantStatus =
  | 'queued'
  | 'analyzing'
  /** The assistant needs a decision from the user (questions or a change to approve). */
  | 'awaiting-user'
  /** The analysis is finished; nothing is pending. */
  | 'ready'
  | 'applying'
  | 'done'
  | 'ignored'
  | 'failed';

export interface AssistantOption {
  id: string;
  label: string;
  description: string | null;
}

export interface AssistantQuestion {
  id: string;
  text: string;
  options: AssistantOption[];
}

/** A change the assistant wants to make; nothing is written before the user approves it. */
export interface AssistantProposal {
  title: string;
  description: string;
  /** Local project the change applies to. */
  projectId: string;
}

/** A file that came with a message. The bytes stay on the core. */
export interface AttachmentRef {
  id: string;
  name: string;
  mime: string;
  size: number;
  /** `image` is shown to the assistant as an image, `text` is read as text, `other` is only listed. */
  kind: 'image' | 'text' | 'other';
}

export interface ChatEntry {
  id: string;
  at: string;
  role: 'system' | 'assistant' | 'user';
  /** `step` entries are the assistant's work log; `text` entries are messages. */
  kind: 'text' | 'step';
  text: string;
  attachments?: AttachmentRef[];
}

export interface AssistantRequest {
  id: string;
  source: RequestSource;
  /** The Slack message, as received. Untrusted content. */
  text: string;
  /** The few messages posted just before it. Untrusted content. */
  context: string;
  attachments?: AttachmentRef[];
  receivedAt: string;
  updatedAt: string;
  status: AssistantStatus;
  /** What the assistant understood and found. */
  summary: string | null;
  /** A suggested reply to the sender; Pouple never posts it. */
  draftReply: string | null;
  questions: AssistantQuestion[];
  proposal: AssistantProposal | null;
  entries: ChatEntry[];
  error: string | null;
  /** A follow-up message can continue the Codex conversation of this request. */
  continuable?: boolean;
}

export interface AssistantStatusInfo {
  /** The assistant runs: enabled, Slack connected and Codex found. */
  active: boolean;
  /** Why it is not running, or the last problem met while polling. */
  problem: string | null;
  lastPollAt: string | null;
  /** Parts of Slack that could not be watched; the rest keeps working. */
  notices: string[];
  /** Codex turns started today, and the daily limit (null when there is none). */
  usage: { today: number; limit: number | null };
}

/** GET /api/assistant, and the payload of every `data:` frame of GET /api/assistant/stream. */
export interface AssistantSnapshot {
  status: AssistantStatusInfo;
  requests: AssistantRequest[];
}

export interface QuestionAnswer {
  questionId: string;
  /** Id of a proposed option; omit when answering with free text. */
  optionId?: string;
  text?: string;
}

export type ProposalDecision = 'approve' | 'decline';

// ---------------------------------------------------------------------------------------------------------------
// Settings (/api/settings)
// ---------------------------------------------------------------------------------------------------------------

export type UiLanguage = 'fr' | 'en';

export type AutoApprove = 'off' | 'own-notes';

/** GET and PUT /api/settings. A PUT carries only the fields to change; a 422 answers `{message}`. */
export interface Settings {
  pollSeconds: number;
  /** Codex turns per day; 0 means no limit. */
  dailyTurnLimit: number;
  /** A folder with one folder per project. Empty: Codex can analyse but never proposes code changes. */
  projectsRoot: string;
  /** Language of summaries and questions ("French"); empty uses the system language. */
  language: string;
  /** Language of the whole interface, and of every string the core sends. */
  uiLanguage: UiLanguage;
  autoApprove: AutoApprove;
  keywords: string[];
  ignoreChannels: string[];
  /** The AI that runs the analyses (`codex` or `claude`); the effective one on read. */
  provider?: string;
  /** One model name per AI; empty is the CLI's own default. */
  models?: Record<string, string>;
  /** Lets the AI search the web and read public pages while it investigates. Off by default. */
  webAccess?: boolean;
}

export interface SuggestedModel {
  id: string;
  label: string;
}

export interface ProviderInfo {
  id: string;
  name: string;
  connected: boolean;
  active: boolean;
  model: string;
  /** `null` when the core has none to propose (a nil slice is encoded as null). */
  suggestedModels: SuggestedModel[] | null;
  note?: string;
}

/** GET /api/providers */
export interface ProvidersInfo {
  active: string;
  providers: ProviderInfo[];
}

/** POST /api/providers/{id}/test: a failed test is still an answer, with `ok: false` and a message. */
export interface ProviderTestResult {
  ok: boolean;
  latencyMs: number;
  model: string;
  message: string;
  code?: string;
}

export type SettingsPatch = Partial<Settings>;

// ---------------------------------------------------------------------------------------------------------------
// In-app update (/api/update)
// ---------------------------------------------------------------------------------------------------------------

export type UpdateUnsupportedReason = '' | 'not-installed-from-source' | 'checkout-missing' | 'checkout-invalid';

export type UpdateBlockerCode = 'local-changes' | 'not-on-main' | 'diverged';

export interface UpdateBlocker {
  code: UpdateBlockerCode;
  /** Already localized by the core. */
  message: string;
}

/** GET /api/update and POST /api/update/check (the check may take up to 30 s). Every string is already localized. */
export interface UpdateInfo {
  supported: boolean;
  reason: UpdateUnsupportedReason;
  /** Why updating is not possible here; only meaningful when `supported` is false. */
  message: string;
  installed: { version: string; commit: string; installedAt: string };
  latest: { commit: string };
  available: boolean;
  /** How many commits the checkout is behind. */
  behind: number;
  /** Subjects of the new commits, at most 15. */
  changes: string[];
  blockers: UpdateBlocker[];
  checkedAt: string | null;
  checking: boolean;
  error: string | null;
}
