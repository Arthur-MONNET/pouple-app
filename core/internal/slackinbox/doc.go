// Package slackinbox reads Slack the way Slack itself decides what to notify: mentions, direct messages, notes to
// yourself, group chats, replies in threads you took part in, highlight words, and broadcasts or every message in the
// channels you list. It is read-only and uses a user token, which only ever travels in the Authorization header.
//
// Client is the thin Slack Web API client, Inbox turns what it reads into Incoming requests.
package slackinbox
