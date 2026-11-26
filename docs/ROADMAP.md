# Roadmap: closing the gap with T3 Code, step by step

Why: T3 Code (github.com/pingdotgg/t3code) is the best reference for a control plane around coding agents. Pouple keeps
what T3 does not have (conversations started by connectors, the bubble, memory, the trust model of origins and grants)
and borrows what makes T3 pleasant and solid. Written from a read of T3's docs and code on 2026-11-23; behaviour of T3
was not run.

Method: one phase at a time, each one small enough to ship to the beta channel on its own. Before code, a short plan
(problem, boundaries, affected modules, risks); structural changes wait for approval. Each item has tests, goes through
a worktree and develop, and is reviewed independently when it touches trust (origins, grants, remote access).

Effort: S = hours, M = a day or two, L = several days.

## Phase 0: foundations (S)
- [x] `AGENTS.md`: rules for AI contributors (tests at the lowest layer that fails, no sleeps in tests, how docs are kept).
- [x] `SECURITY.md`: public threat model, from docs/REMOTE-API.md and REMOTE-E2E.md (principals, what counts as a finding).
- [x] CI gates: dead-code check on web (knip), `govulncheck`, a changed-files check script.
- [x] Go 1.27.4 (GO-2026-6859), only with Arthur's approval.

## Phase 1: comfort in conversations (S to M)
- [ ] Pin, archive, settle (automatic after 7 days or when the PR merges), snooze; 5-second undo.
- [ ] Command palette (Cmd+K) that also searches message text; customizable shortcuts.
- [ ] OS notifications with sound modes, Dock badge.

## Phase 2: triggers, Pouple's core (M each)
- [ ] Scheduled prompts (interval, daily, once) into a new or existing conversation.
- [ ] User rules on connectors: "when X arrives in Slack, Asana or GitLab, start a conversation with this prompt".
- [ ] Webhook origin with signature check, filters, deduplication; same safeguards as the other origins.
- [ ] Resume a limited turn when the usage limit resets; resume threads after a restart (never archived, stopped or awaiting approval).

## Phase 3: composer (M)
- [ ] Queue and steer while the assistant works, "send now", recall last prompt, stash a draft.
- [ ] Mentions (`@` conversation, `#` PR), slash commands, bigger attachments.
- [ ] Side question that does not change the conversation.

## Phase 4: git and review (L)
- [ ] Diff panel per turn, files marked as viewed, comments.
- [ ] Commit, push and open a PR in one button, generated titles and bodies.
- [ ] Explicit links between a conversation and its PRs or issues.
- [ ] Checkpoints on hidden Git refs and "edit from here" (rewind).

## Phase 5: providers and connectors (M to L)
- [ ] Named permission modes (Supervised, Accept edits, Auto, Full access) mapped on grants.
- [ ] Usage page across providers; ACP agents (Cursor, OpenCode, Kiro...).
- [ ] A common tracker contract, then Linear and Jira; 1Password references for secrets.

## Phase 6: phone and remote (M to L)
- [ ] Device approval before a new phone connects; scoped tokens.
- [ ] Offline drafts, share sheet, dictation; push notifications (needs a relay, so a decision first).

## Phase 7: engineering practices (M)
- [ ] Replay of recorded agent transcripts through the real engine; evals of generated text.
- [ ] Transfer and render budgets measured in CI; updater with snapshot, trial start and rollback; `doctor` command.
