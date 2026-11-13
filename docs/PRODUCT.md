# Pouple: product scope

This document is the target behavior of Pouple, as decided with its owner. It describes what Pouple should do and how a
request goes through it, end to end. It is not a description of the current code: [ARCHITECTURE.md](ARCHITECTURE.md)
covers that, and the last section lists what differs.

## What Pouple is

Pouple is an assistant that **accompanies you until a request is really resolved**. A request arrives from Slack, Asana or
GitLab. Pouple looks for what there is to do, suggests how to answer, asks you for what it does not know, and keeps going
until the conversation ends with a real action: a reply to the person, a task or a comment in Asana, or code in a merge
request.

It is not a notifier and not an autonomous agent. You stay in charge: it asks before it writes anywhere, and it asks again
when it wants to write somewhere else.

## Inputs

Every signal is its own request. Pouple does not merge related signals (a Slack message, an Asana task and a merge request on
the same subject stay three conversations).

| Source | What triggers a request |
|---|---|
| Slack | Everything that would notify you: mentions, direct messages, group chats, threads you follow, notes to yourself, `@here` and `@channel`, your watched words. No filter: everything is read. |
| Asana | A task assigned to you, or a comment that mentions you. |
| GitLab | One of your pipelines finishing, and feedback on one of your merge requests (a comment, a review, a requested change). |

When someone answers in the Slack thread (or comments on the task) while a conversation is open, Pouple takes the new message
into account silently, and tells you only if it changes what it proposes.

## Life of a request

1. **Silent thinking.** The request arrives. Pouple reads as much as it needs, with no limit per request: your connectors
   (Slack, Asana, GitLab, Slite) and the projects on your computer. **No bubble is shown during this phase.**
2. **First contact.** If at least one action is worth doing, the bubble appears with what Pouple found and what it suggests.
   Suggestions are a choice among proposals, and the choice can be multiple when Pouple offers it. When it needs more context,
   it asks one or several multiple-choice questions in a row, as in a plan in Claude. No questions when none are needed.
3. **Plan.** Before any write, Pouple shows a plan and you approve it (see [Writing](#writing)).
4. **Execution.** Pouple does the work inside what you approved. It asks only about preferences and sensitive changes that
   follow from the work. You can follow it in the bubble.
5. **End.** The conversation ends when no question is waiting for you.

At any time, **Ignore** closes the request in one click from the bubble, on a question included.

### The bubble

- It shows up at first contact and stays as long as a question waits for you.
- After you answer, it stays while Pouple works, and you watch the work unfold in it.
- It goes away when the conversation is over, when you ignored the request, and it is not shown during silent thinking.
- Ignoring only closes this request. A new message in the same thread opens a new request.
- The window gives the full conversation and the history, and can be used at any time.

## Writing

Reading is free. **Every write needs your approval first**, and the approval is for a **scope**.

| Service | A scope is |
|---|---|
| Code | One project on your computer. |
| GitLab | One GitLab project. |
| Asana | One Asana project. |
| Slite | One Slite space. |

- Pouple asks before the first write in a scope: "may I change this project, in this way".
- The approval lasts for **this conversation** only. A new conversation asks again, even for the same project.
- Inside an approved scope, Pouple works freely and asks you only about preferences and sensitive changes that result from the
  work.
- A write outside the approved scopes (another project, another repository, another Asana project, another Slite space) needs
  a new approval.
- Inside an approved code project, Pouple may create a branch and commits, push the branch, open the merge request, and push
  fixes after a failed pipeline or a reviewer's comments. It does not merge.
- Replies to people are never sent by Pouple: you copy the text and paste it in the conversation yourself.

### Approved by a frozen plan

The AI that investigates reads the Slack, Asana or GitLab text. The AI that writes does not receive that text: it receives a
**plan** that you approved.

1. The investigating AI writes the plan: the goal, the exact scopes it will write in, the kinds of action it will take there,
   and the information it needs from the request, restated in its own words.
2. You approve the plan, or you change it. Approving it is the approval of its scopes.
3. The writing AI receives the plan and nothing else from the request. It decides the steps inside the plan's scopes.
4. If the work needs a scope or an action the plan does not cover, the writing stops and a new plan, or an addition to the
   plan, comes back to you.

This keeps what a person can write in a message (instructions hidden in a Slack message, an Asana comment, a merge request
description) from reaching the AI that has write access.

### Code

Pouple decides where to work on the code: in a dedicated git worktree by default, so your branch and your unsaved files are
never touched, and in your project folder only when the project is not a clean git repository.

## Answering people

The text of a reply is written in **your style**, learned from your past Slack messages (tone, length, tutoiement), in the
language of the request. It is stored on your computer only. You copy it and send it yourself, and Pouple never posts it.

## Usage

There is no limit on how much Pouple reads per request. Instead, usage is shown: tokens used, and the share used of the
current 5-hour window and of the current week.

What the Codex and Claude Code CLIs expose for this is not verified yet (see [Open points](#open-points)).

## Privacy

Everything that notifies you is sent to the AI you chose, direct messages included. There is no exclusion list.

## What differs from today

| Topic | Today | Target |
|---|---|---|
| Writing code | One proposal, approved, then an isolated writing turn that never sees the Slack text. It writes in your project folder. | A frozen plan approved by you, a scope per project, free work inside it, a worktree by default, then branch, push and merge request. |
| Writing in services | Read-only. A comment is possible only on a request you made yourself. | Writes in Asana, Slite and GitLab inside the approved scope. |
| Sources | Slack and finished GitLab pipelines. | Plus Asana (assigned or mentioned) and feedback on your merge requests. |
| Reading | A fixed number of lookup rounds. | No limit. |
| Bubble | None while Pouple works; a ready answer fades after about 25 seconds. | Hidden during silent thinking, then persistent until the conversation ends or is ignored, with the work visible in it. |
| Ignore | No one-click ignore on a question. | One click in the bubble, this request only. |
| Questions | A series of multiple-choice questions, one choice each. | Also multiple choice when Pouple offers it, and suggested actions that can be ticked. |
| End of a request | Nothing ends it. | It ends when no question is waiting for you. |
| Usage | A daily count of turns. | Tokens, and the share of the 5-hour window and of the week. |
| Replies | A neutral, short suggested text. | Written in your learned style. |
| Thread updates | Not followed. | Taken into account silently. |

## Open points

These are not decided or not verified. None of them changes the decisions above, but each needs an answer before it is built.

- **Quotas.** Whether Codex and Claude Code expose the 5-hour window, the weekly window and token counts in a form Pouple can
  read, and how. This is to be checked against the installed CLIs, not assumed.
- **Credentials for writing.** The GitLab token needs a write scope, and pushing needs git credentials on your computer. Asana
  and Slite tokens need write rights. Today they are read-only tokens.
- **What a plan covers.** The plan fixes scopes and kinds of action, and the writing AI chooses the steps. The exact boundary
  (for example, whether a new file, a dependency change or a CI change needs its own line in the plan) is to be defined.
- **Sensitive changes.** The list of changes that always ask you, even inside an approved scope, is to be defined (for
  example: deleting files, rewriting history, changing CI, changing dependencies).
- **Learning your style.** Which past messages are read, how many, where the result is stored, and how you reset it.
- **Silent update of a thread.** What counts as "changes what it proposes", and how Pouple tells you when it does.
- **No exclusion list.** Everything is sent to the AI you chose. This is a conscious choice and is worth stating in the first
  launch screen.
