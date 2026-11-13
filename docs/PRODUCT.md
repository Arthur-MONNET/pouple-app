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
- A ready answer with nothing left to decide stays until you press **Done**. A change made is confirmed for a few
  seconds, then the bubble goes away.
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

The window lists the tokens of the day and each AI's windows; the menu bar menu repeats them, and each conversation shows
the tokens it used. A window that started again since the AI reported it is no longer shown.

The usage follows the AIs as they report it, not the next time something else changes: the meter tells the clients at the
start of a turn, when Claude reports a window (during the turn), and when Codex reports its tokens. Only what Pouple's own
turns report is shown: Pouple has no way to read what you use of an AI outside of it.

## Changes to your code

The setting "Changes to your code" has three levels: ask each time (default), apply at once the changes proposed from a note
you wrote to yourself, or apply every proposed change at once. The last one is a conscious choice to trust the assistant:
whoever asked for the change, it is made by a turn that sees only the proposal, in the project folder, without network, and
nothing is committed, pushed or published.

## Screenshots

The AI cannot run a browser or a local server from its sandbox. It names a page (a lookup `screenshot` / `capture`, with a
screen size, desktop or mobile), and Pouple opens it in a Chromium-based browser found on the Mac (Chrome, Chromium, Edge
or Brave; nothing is downloaded) and takes the picture. The picture appears in the conversation and goes back to the AI,
which can look at it: before a visual change, and after it was applied.

- **What may be opened.** Only `localhost` (a dev server the user already runs) and the sites the user lists in Settings,
  never Pouple's own port. The AI chooses the address and the screen size, never a command.
- **The server.** The assistant cannot start a dev server (its sandbox forbids it) and never has to ask you to. Through a lookup
  `screenshot` / `serve` it names a project and what to start (nothing for the project's usual script, a script name, or a
  command), and Pouple starts it outside the sandbox, waits until it listens and answers, however long that takes, and gives
  the assistant its address. The usual ways run at once: a script named dev, develop, start, serve, preview, preprod or
  storybook, `vite`, `next`, `nuxt`… (directly or through the package manager), `python3 -m http.server`. Anything else
  (another script, a shell line, a variable before the command, a server open to the network, or a start configuration
  such as package.json or vite.config changed since the last commit) is shown to you as a question, and runs only if you
  allow that exact command, in that conversation. A server must listen on this computer only. At most two servers run; one
  stops after 30 minutes without use and all stop when Pouple quits. Settings can turn this off.
- **The browser.** It starts empty and is thrown away afterwards: no account, no cookie, no extension, no download and no
  keychain access. Pouple talks to it over a private pipe, so it listens on no port. Every request the page makes, redirects
  and frames included, is checked against the same rule: pages, frames and calls to a service must go to localhost or a
  listed site, while images, styles, fonts and scripts may come from anywhere (and then count as the web). A page behind a
  login shows its login.
- **Live view.** While the browser works, the conversation shows a collapsible "Chromium" block with what it draws and what
  it is doing. The picture is asked for several times a second only while the block is open.
- **Trust.** A picture of a site other than localhost counts like the web: a change proposed after it is never applied
  without the user.

## Privacy

Everything that notifies you is sent to the AI you chose, direct messages included. There is no exclusion list.

## What differs from today

| Topic | Today | Target |
|---|---|---|
| Writing code | One proposal, approved, then an isolated writing turn that never sees the Slack text. It writes in your project folder. | A frozen plan approved by you, a scope per project, free work inside it, a worktree by default, then branch, push and merge request. |
| Writing in services | Read-only. A comment is possible only on a request you made yourself. | Writes in Asana, Slite and GitLab inside the approved scope. |
| Sources | Slack, finished GitLab pipelines, and Asana (a task assigned to you, a comment that mentions you on one of your tasks). | Plus feedback on your merge requests. |
| Reading | A fixed number of lookup rounds. | No limit. |
| Replies | A neutral, short suggested text. | Written in your learned style. |
| Thread updates | Not followed. | Taken into account silently. |

## Open points

These are not decided or not verified. None of them changes the decisions above, but each needs an answer before it is built.

- **Claude's windows.** Codex and Claude Code both report their windows on a real turn and the app shows them. Claude only
  reports them during a turn, so they are as fresh as the last turn.
- **Asana limits.** Only the first 100 open tasks are read, and a mention is found only on a task assigned to you (finding
  it elsewhere takes the search of a paid Asana plan).
- **Credentials for writing.** The GitLab token needs a write scope, and pushing needs git credentials on your computer. Asana
  and Slite tokens need write rights. Today they are read-only tokens.
- **What a plan covers.** The plan fixes scopes and kinds of action, and the writing AI chooses the steps. The exact boundary
  (for example, whether a new file, a dependency change or a CI change needs its own line in the plan) is to be defined.
- **Sensitive changes.** The list of changes that always ask you, even inside an approved scope, is to be defined (for
  example: deleting files, rewriting history, changing CI, changing dependencies).
- **Learning your style.** Which past messages are read, how many, where the result is stored, and how you reset it.
- **Silent update of a thread.** What counts as "changes what it proposes", and how Pouple tells you when it does.
- **Screenshots of localhost.** Opening a page on localhost runs that page's scripts and makes GET requests to your own
  dev servers, which a few tools answer with an action (opening a file in the editor, for example). The AI picks the
  address, so a message from someone else could try one. To decide: ask before a screenshot of a request that came from
  someone else, or list the ports that may be opened.
- **No exclusion list.** Everything is sent to the AI you chose. This is a conscious choice and is worth stating in the first
  launch screen.
