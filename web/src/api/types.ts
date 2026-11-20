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
  | 'channel-message'
  /** A GitLab pipeline of the user finished: nobody wrote, the text is what GitLab reports. */
  | 'pipeline'
  /** Something happened on a task of the user in Asana (assigned, mentioned): the text is what Asana reports. */
  | 'asana'
  /** A reviewer wrote on a merge request of the user: the text is what GitLab reports. */
  | 'merge-request'
  /** An alert the user set came due: the text is its title, which the user wrote. */
  | 'reminder';

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
  /** For a reminder: the conversation it was set from, when it came from one. */
  relatedRequestId?: string;
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
  /** Several options can be picked together (several actions to take, for example); otherwise one. */
  multiple?: boolean;
}

/** A change the assistant wants to make; nothing is written before the user approves it. */
export interface AssistantProposal {
  title: string;
  description: string;
  /** Local project the change applies to. */
  projectId: string;
}

/** A place a write approval covers: a project folder, a GitLab project, an Asana project or a Slite space. */
export interface PlanScope {
  service: 'code' | 'gitlab' | 'asana' | 'slite';
  id: string;
  name?: string;
}

/** One write a plan asks for, in the words of the assistant that investigated; the user approves it as written. */
export interface PlanAction {
  kind: string;
  scope: PlanScope;
  summary: string;
  /** Source and base branches for code; source and target branches for gitlab.merge-request. */
  branch?: string;
  base?: string;
}

/** What the assistant wants to write, for the user to approve before anything is written. */
export interface AssistantPlan {
  /** Names this exact plan: an approval carries it, so it can only approve the plan that was read. */
  id?: string;
  goal: string;
  /** What the AI that writes will know of the request, restated by the assistant that read it. */
  brief: string;
  actions: PlanAction[];
  status: 'proposed' | 'approved' | 'declined';
}

/** One alert the assistant suggests. Nothing is set until the user ticks it. */
export interface ReminderOption {
  id: string;
  label: string;
  note?: string;
  /** The suggested moment, RFC 3339 with the zone of the user's computer. */
  at: string;
}

/** What the assistant suggests to be reminded of, waiting for the user's pick or already decided. */
export interface ReminderOffer {
  /** Names this exact suggestion: a decision carries it, so it can only answer the suggestion that was read. */
  id: string;
  status: 'proposed' | 'accepted' | 'declined';
  items: ReminderOption[];
  /** Once accepted: the ids of the options that became alerts. */
  chosen?: string[];
}

/** An option the user ticked, with the local moment ("2026-11-20T09:00") they want for it; absent keeps the suggested one. */
export interface ReminderPick {
  id: string;
  at?: string;
}

/** One fact the assistant changed in the memory with its answer. */
export interface MemoryChangeItem {
  id: string;
  kind: 'add' | 'update' | 'remove';
  /** Path of the note. */
  note: string;
  noteTitle: string;
  /** What the fact says now (what it said, for a removal). */
  fact: string;
  /** For an update: what it said before. */
  previous?: string;
  /** The exact line of the note. */
  line: string;
  /** Private until the user shares it. */
  shared: boolean;
  /** Written from other people's words and not confirmed yet. */
  unconfirmed?: boolean;
  /** YYYY-MM-DD: when the fact should be looked at again. */
  review?: string;
}

/** What the assistant changed in the memory at once, with its answer. It can be undone as a whole. */
export interface MemoryChange {
  /** The git commit of the change; empty when git failed: then it cannot be undone. */
  id: string;
  /** RFC 3339. */
  at: string;
  undone?: boolean;
  items: MemoryChangeItem[];
}

/** An upcoming alert. When it comes due it becomes a conversation with a bubble. */
export interface AssistantAlert {
  id: string;
  title: string;
  note?: string;
  /** RFC 3339 with the zone offset of the computer the core runs on. */
  at: string;
  /** The conversation the alert was set from; absent for an alert made by hand. */
  requestId?: string;
  createdAt: string;
}

/** What the user types to make an alert; `at` is a local date and time, "2026-11-20T09:00". */
export interface AlertInput {
  title: string;
  note?: string;
  at: string;
}

/** What the user changes of an alert; a field left out stays as it is. */
export type AlertPatch = Partial<AlertInput>;

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

/** What is known of the user's learned writing style (the style itself stays in the core). */
export interface StyleInfo {
  learned: boolean;
  learnedAt?: string;
  /** How many of their messages it was learned from. */
  from?: number;
}

/** The browser at work for a conversation (a screenshot being taken): what it opens and what it is doing. */
export interface BrowserState {
  url: string;
  step: string;
}

export interface AssistantRequest {
  stopCheckpoint?: string;
  id: string;
  /** Set only while the browser is at work for this conversation. */
  browser?: BrowserState;
  /** The writes the assistant asks to make, waiting for approval or approved. */
  plan?: AssistantPlan;
  /** The alerts the assistant suggests, waiting for the user's pick or already decided. */
  reminders?: ReminderOffer;
  /** What the assistant changed in the memory with its answers, oldest first (at most 10). */
  memoryChanges?: MemoryChange[];
  source: RequestSource;
  /** The topic the core sorted this conversation into. Absent or empty: not sorted yet (small talk, or not looked at yet). */
  topicId?: string;
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
  /** The request has reached the user; its work then shows in the bubble. */
  engaged?: boolean;
  /** The user closed its bubble. */
  closed?: boolean;
  /** What the AI used on this conversation; absent until it used any. */
  tokens?: Tokens;
  /** The other AI ("codex", "claude") that can take over a request stopped by a usage limit. */
  switchTo?: string;
}

export interface AssistantStatusInfo {
  /** The assistant runs: enabled, Slack connected and Codex found. */
  active: boolean;
  /** Why it is not running, or the last problem met while polling. */
  problem: string | null;
  lastPollAt: string | null;
  /** Parts of Slack that could not be watched; the rest keeps working. */
  notices: string[];
  /** AI turns started today, the daily limit (null when there is none), tokens and each AI's own usage windows. */
  usage: Usage;
}

/** Tokens an AI used: input (cached ones included), cached, output. */
export interface Tokens {
  input: number;
  cached: number;
  output: number;
}

/** One usage window of an AI subscription, as the AI last reported it. */
export interface UsageWindow {
  /** From 0 to 100. */
  usedPercent: number;
  resetsAt?: string;
}

/** An AI's own usage windows: the current five hours and the current week. A window it did not report is absent. */
export interface UsageLimits {
  fiveHour?: UsageWindow;
  week?: UsageWindow;
  seenAt: string;
}

/** How tight a resource of the Mac is, with the same steps as the AIs' windows. */
export type MachineLevel = 'normal' | 'high' | 'critical';

/** The volume holding Pouple's data, where the AIs keep their conversations. */
export interface MachineDisk {
  freeBytes: number;
  totalBytes: number;
  level: MachineLevel;
}

/** The Mac's memory; its level is macOS's own memory pressure. */
export interface MachineMemory {
  /** From 0 to 100. */
  usedPercent: number;
  totalBytes: number;
  level: MachineLevel;
}

/** The Mac's disk and memory; a part the core could not read is absent. */
export interface MachineState {
  disk?: MachineDisk;
  memory?: MachineMemory;
}

export interface Usage {
  today: number;
  limit: number | null;
  /** What each AI used today, by AI; older cores do not send it. Never added up: each AI has its own plan. */
  tokensByAI?: Record<string, Tokens>;
  /** By AI ("codex", "claude"); an AI that never reported its windows, or whose windows started again, is absent. */
  limits?: Record<string, UsageLimits>;
  /** The Mac's disk and memory: when either runs out, the AIs fail with errors that never say so. Older cores do not send it. */
  machine?: MachineState;
}

/** GET /api/assistant, and the payload of every `data:` frame of GET /api/assistant/stream. */
/** A subject the core sorts conversations into. Never empty: the core deletes a topic with no conversation. */
export interface AssistantTopic {
  id: string;
  /** A short plain text written by the AI, and renamed as the topic evolves. Never HTML. */
  title: string;
  createdAt: string;
  updatedAt: string;
}

export interface AssistantSnapshot {
  status: AssistantStatusInfo;
  requests: AssistantRequest[];
  /** Missing from a core that does not sort conversations yet: treat as none. */
  topics?: AssistantTopic[];
  /** The upcoming alerts, soonest first. */
  alerts: AssistantAlert[];
}

export interface QuestionAnswer {
  questionId: string;
  /** Id of a proposed option; omit when answering with free text. */
  optionId?: string;
  /** Ids of the options picked on a question that allows several. */
  optionIds?: string[];
  text?: string;
}

export type ProposalDecision = 'approve' | 'decline';
export type PlanDecision = ProposalDecision;

// ---------------------------------------------------------------------------------------------------------------
// Settings (/api/settings)
// ---------------------------------------------------------------------------------------------------------------

export type UiLanguage = 'fr' | 'en';

export type AutoApprove = 'off' | 'own-notes' | 'all';

/** Which finished GitLab pipelines of the user become conversations. */
export type PipelineMode = 'all' | 'failures' | 'off';

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
  /** Sites, besides this computer, whose pages the assistant may ask Pouple to open to take a picture. */
  screenshotHosts: string[];
  /** The AI that runs the analyses (`codex` or `claude`); the effective one on read. */
  provider?: string;
  /** One model name per AI; empty is the CLI's own default. */
  models?: Record<string, string>;
  /** Lets the AI search the web and read public pages while it investigates. On unless turned off. */
  webAccess?: boolean;
  /** Whether Pouple may start a project's development server for a screenshot; on unless turned off. */
  devServers?: boolean;
  /** Whether Pouple learns how the user writes from their Slack messages, to draft replies in that style; on unless turned off. */
  learnStyle?: boolean;
  /** Follows the develop branch (beta channel) instead of main; off by default. */
  betaUpdates?: boolean;
  /** Which finished GitLab pipelines become conversations; set on the GitLab connector's card. */
  gitlabPipelines?: PipelineMode;
  /** Whether the assistant keeps confirmed facts in notes; off until the user turns it on. */
  memory?: boolean;
  /** The folder of those notes; always the resolved folder on read, empty on write resets it to the default. */
  memoryFolder?: string;
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

/** GET /api/providers/{id}/models: the models to pick from; empty, with a message, when the AI cannot list them. */
export interface ProviderModels {
  models: SuggestedModel[];
  message: string;
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

export type UpdateBlockerCode = 'local-changes' | 'other-branch' | 'diverged';

export interface UpdateBlocker {
  code: UpdateBlockerCode;
  /** Already localized by the core. */
  message: string;
}

/** GET /api/update and POST /api/update/check (the check may take up to 30 s). Every string is already localized. */
export type UpdateChannel = 'stable' | 'beta';
export type UpdateBranch = 'main' | 'develop';

export interface UpdateInfo {
  /** The channel the core follows, and the git branch it maps to. */
  channel: UpdateChannel;
  branch: UpdateBranch;
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

/** One Markdown note of the memory folder. */
export interface MemoryNote {
  path: string;
  title: string;
  facts: number;
}

/** A fact to look at: its own review date came, or it was written from other people's words and nobody confirmed it. */
export interface MemoryReviewItem {
  note: string;
  noteTitle: string;
  /** The exact line of the note: it identifies the fact when deciding. */
  line: string;
  fact: string;
  /** YYYY-MM-DD. */
  date: string;
  /** Written by the AI from other people's text: keeping it confirms it. */
  unconfirmed: boolean;
}

export type MemoryCliId = 'claude' | 'codex';

/** A CLI the user can connect to the memory: Pouple writes `lines` into `file`, and only when asked. */
export interface MemoryCli {
  id: MemoryCliId;
  name: string;
  file: string;
  connected: boolean;
  /** The exact block that is (or would be) in the file. */
  lines: string;
}

export interface MemoryView {
  enabled: boolean;
  folder: string;
  exists: boolean;
  notes: MemoryNote[];
  review: MemoryReviewItem[];
  clis: MemoryCli[];
  /** RFC 3339, the last tidy-up; absent if never. */
  tidiedAt?: string;
}

export type MemoryDecision = 'keep' | 'drop';

export type GitLabMode = 'read-only' | 'write';
export type GitLabTokenID = 'primary' | 'additional';
export interface GitLabAccess {
  mode: GitLabMode;
  selected: GitLabTokenID;
  tokens: { id: GitLabTokenID; name: string; scopes: string[] | null }[];
}
export interface GitLabAccessUpdate {
  mode?: GitLabMode;
  selected?: GitLabTokenID;
  primaryName?: string;
  name?: string;
  token?: string;
  verify?: boolean;
}
