package slackinbox

import (
	"context"
	"errors"
	"testing"
)

func TestAMessageReferenceIsReadFromItsPartsOrItsLink(t *testing.T) {
	want := MessageRef{Channel: "C024BE91L", TS: "1700000000.123456"}
	for _, text := range []string{
		"C024BE91L 1700000000.123456",
		"C024BE91L:1700000000.123456",
		"  C024BE91L , 1700000000.123456 ",
		"https://acme.slack.com/archives/C024BE91L/p1700000000123456",
		"https://acme.slack.com/archives/C024BE91L/p1700000000123456?thread_ts=1699999999.000100&cid=C024BE91L",
		"<https://acme.slack.com/archives/C024BE91L/p1700000000123456|this message>",
	} {
		if got, ok := ParseMessageRef(text); !ok || got != want {
			t.Errorf("%q = %+v, %v", text, got, ok)
		}
	}
}

func TestOnlyWhatLooksLikeAMessageIsAReference(t *testing.T) {
	for _, text := range []string{
		"", "preprod", "C024BE91L", "c024be91l 1700000000.123456", "C024BE91L 17000.1",
		"C024BE91L 1700000000.123456 extra", "../users.list 1700000000.123456",
		"https://evil.example.com/archives/C024BE91L/p1700000000123456",
		"http://acme.slack.com/archives/C024BE91L/p1700000000123456",
		"https://acme.slack.com/archives/C024BE91L",
	} {
		if got, ok := ParseMessageRef(text); ok {
			t.Errorf("%q must not be a reference, got %+v", text, got)
		}
	}
}

func TestASearchNamesWhoSaidWhatWhereAndHowPrivateItIs(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("search.messages", ok(object{"messages": object{"matches": []any{
		object{"ts": "1762790400.000100", "text": "je prends preprod 2 avec <@U2>", "user": "U1", "permalink": "https://acme.slack.com/archives/C0PREPROD1/p1762790400000100",
			"channel": object{"id": "C0PREPROD1", "name": "preprod"}},
		object{"ts": "1762790500.000100", "text": "ok", "user": "U2", "permalink": "https://acme.slack.com/archives/C0SECRET01/p1762790500000100?thread_ts=1762790000.000100",
			"channel": object{"id": "C0SECRET01", "name": "leads", "is_private": true}},
		object{"ts": "1762790600.000100", "text": "psst", "user": "U2",
			"channel": object{"id": "D0DIRECT01", "is_im": true, "user": "U1"}},
		object{"ts": "1762790700.000100", "text": "deployed", "bot_id": "B1", "channel": object{"id": "C0PREPROD1", "name": "preprod"}},
	}}}))
	fake.on("users.info", func(c call) any {
		names := map[string]string{"U1": "Jocelyn", "U2": "Arthur"}
		return ok(object{"user": object{"profile": object{"display_name": names[c.form.Get("user")]}}})
	})

	got, err := fake.client().FindMessages(context.Background(), "in:#preprod preprod", 15)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 4 {
		t.Fatalf("%d messages: %+v", len(got), got)
	}
	first := got[0]
	if first.Author != "Jocelyn" || first.Channel != "preprod" || first.Visibility != Public || first.Text != "je prends preprod 2 avec @Arthur" || first.Time.Unix() != 1762790400 || first.ChannelID != "C0PREPROD1" {
		t.Errorf("first = %+v", first)
	}
	if got[1].Visibility != Private || got[1].ThreadTS != "1762790000.000100" {
		t.Errorf("a private channel's reply in a thread: %+v", got[1])
	}
	if got[2].Visibility != Direct || got[2].Channel != "Jocelyn" {
		t.Errorf("a direct message names the other person: %+v", got[2])
	}
	if got[3].Author != "bot" {
		t.Errorf("a message without a user is a bot's: %+v", got[3])
	}
	if form := fake.callsTo("search.messages")[0].form; form.Get("query") != "in:#preprod preprod" || form.Get("count") != "15" || form.Get("sort") != "timestamp" {
		t.Errorf("form = %v", form)
	}
}

func TestAThreadIsReadOldestFirstAndNamedWhenSlackLetsIt(t *testing.T) {
	fake := newFakeSlack(t)
	fake.on("conversations.replies", func(c call) any {
		if c.form.Get("channel") != "C0PREPROD1" || c.form.Get("ts") != "1762790400.000100" {
			t.Errorf("form = %v", c.form)
		}
		return ok(object{"messages": []any{
			object{"ts": "1762790400.000100", "text": "who has preprod 2?", "user": "U1"},
			object{"ts": "1762790460.000100", "text": "me, commit 9f3a1c2", "user": "U2"},
		}})
	})
	fake.respond("conversations.info", ok(object{"channel": object{"name": "preprod"}}))
	fake.on("users.info", func(c call) any {
		return ok(object{"user": object{"real_name": map[string]string{"U1": "Jocelyn", "U2": "Arthur"}[c.form.Get("user")]}})
	})

	got, err := fake.client().ReadThread(context.Background(), MessageRef{Channel: "C0PREPROD1", TS: "1762790400.000100"}, 40)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 || got[0].Text != "who has preprod 2?" || got[1].Author != "Arthur" || got[1].Channel != "preprod" || got[1].Visibility != Public {
		t.Fatalf("%+v", got)
	}
}

func TestAConversationSlackWillNotDescribeIsTreatedAsPrivate(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("conversations.replies", ok(object{"messages": []any{object{"ts": "1762790400.000100", "text": "hi", "user": "U1"}}}))
	fake.respond("conversations.info", slackError("missing_scope", "channels:read"))
	fake.respond("users.info", ok(object{"user": object{"real_name": "Jocelyn"}}))

	got, err := fake.client().ReadThread(context.Background(), MessageRef{Channel: "C0PREPROD1", TS: "1762790400.000100"}, 40)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Visibility != Unknown || got[0].Channel != "" {
		t.Errorf("%+v", got[0])
	}
	direct, err := fake.client().ReadThread(context.Background(), MessageRef{Channel: "D0DIRECT01", TS: "1762790400.000100"}, 40)
	if err != nil || direct[0].Visibility != Direct {
		t.Errorf("a direct message is known by its id: %+v, %v", direct, err)
	}
}

func TestTheConversationAroundAMessageHoldsTheOnesJustBeforeAndJustAfter(t *testing.T) {
	fake := newFakeSlack(t)
	// Slack lists history newest first.
	fake.on("conversations.history", func(c call) any {
		if c.form.Get("latest") == "1762790400.000100" {
			return ok(object{"messages": []any{
				object{"ts": "1762790400.000100", "text": "m3 (the message)", "user": "U1"},
				object{"ts": "1762790300.000100", "text": "m2", "user": "U1"},
				object{"ts": "1762790200.000100", "text": "m1", "user": "U1"},
			}})
		}
		if c.form.Get("oldest") != "1762790400.000100" || c.form.Get("inclusive") != "false" || c.form.Get("latest") != "1762876800" {
			t.Errorf("after: %v", c.form)
		}
		return ok(object{"messages": []any{
			object{"ts": "1762790900.000100", "text": "m7 (too far)", "user": "U1"},
			object{"ts": "1762790600.000100", "text": "m6", "user": "U1"},
			object{"ts": "1762790500.000100", "text": "m5", "user": "U1"},
			object{"ts": "1762790450.000100", "text": "m4", "user": "U1"},
		}})
	})
	fake.respond("conversations.info", ok(object{"channel": object{"name": "preprod"}}))
	fake.respond("users.info", ok(object{"user": object{"real_name": "Jocelyn"}}))

	got, err := fake.client().ReadAround(context.Background(), MessageRef{Channel: "C0PREPROD1", TS: "1762790400.000100"}, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	var texts []string
	for _, message := range got {
		texts = append(texts, message.Text)
	}
	want := []string{"m1", "m2", "m3 (the message)", "m4", "m5", "m6"}
	if len(texts) != len(want) {
		t.Fatalf("%q", texts)
	}
	for i := range want {
		if texts[i] != want[i] {
			t.Fatalf("got %q, want %q", texts, want)
		}
	}
	if form := fake.callsTo("conversations.history")[0].form; form.Get("limit") != "3" {
		t.Errorf("two before and the message itself: %v", form)
	}
}

func TestASlackErrorReachesTheCaller(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("search.messages", slackError("ratelimited"))
	_, err := fake.client().FindMessages(context.Background(), "preprod", 15)
	var api *APIError
	if !errors.As(err, &api) || api.Code != "ratelimited" {
		t.Fatalf("err = %v", err)
	}
}
