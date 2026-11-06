package slackinbox

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func ptr(s string) *string { return &s }

func TestIdentityIsResolvedOnce(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("auth.test", ok(object{"user_id": "U1", "url": "https://acme.slack.com"}))
	client := fake.client()

	for range 2 {
		userID, teamURL, err := client.Identity(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if userID != "U1" || teamURL != "https://acme.slack.com/" {
			t.Fatalf("identity = %q %q", userID, teamURL)
		}
	}
	if n := len(fake.callsTo("auth.test")); n != 1 {
		t.Fatalf("auth.test called %d times, want 1", n)
	}
}

func TestIdentityDoesNotCacheFailures(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("auth.test", slackError("ratelimited"))
	client := fake.client()
	if _, _, err := client.Identity(context.Background()); err == nil {
		t.Fatal("want an error")
	}
	fake.respond("auth.test", ok(object{"user_id": "U1", "url": "https://acme.slack.com/"}))
	if _, teamURL, err := client.Identity(context.Background()); err != nil || teamURL != "https://acme.slack.com/" {
		t.Fatalf("second attempt = %q, %v", teamURL, err)
	}
}

func TestIdentityRejectsUnexpectedPayload(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("auth.test", ok(object{"user_id": "U1"}))
	_, _, err := fake.client().Identity(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unexpected payload") {
		t.Fatalf("err = %v", err)
	}
}

func TestSlackErrorsBecomeActionableMessages(t *testing.T) {
	tests := []struct {
		name    string
		payload object
		code    string
		want    string
	}{
		{"bad token", slackError("invalid_auth"), "invalid_auth", "rejected the token"},
		{"missing scope", slackError("missing_scope", "channels:history"), "missing_scope", `"channels:history" scope`},
		{"missing scope without detail", slackError("missing_scope"), "missing_scope", `"required" scope`},
		{"not a member", slackError("channel_not_found"), "channel_not_found", "must be a member"},
		{"wrong token type", slackError("not_allowed_token_type"), "not_allowed_token_type", "needs a user token"},
		{"rate limit", slackError("ratelimited"), "ratelimited", "rate limit"},
		{"unknown code", slackError("weird"), "weird", "Slack error: weird"},
		{"no code", object{"ok": false}, "unknown_error", "Slack error: unknown_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeSlack(t)
			fake.respond("conversations.history", tt.payload)
			_, err := fake.client().messagesSince(context.Background(), "C1", 0, 50)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.Code != tt.code || !strings.Contains(apiErr.Error(), tt.want) {
				t.Fatalf("got code %q message %q", apiErr.Code, apiErr.Error())
			}
		})
	}
}

func TestTransportFailuresAreReadableAndNeverCarryTheToken(t *testing.T) {
	t.Run("unknown method", func(t *testing.T) {
		fake := newFakeSlack(t)
		_, err := fake.client().messagesSince(context.Background(), "C1", 0, 50)
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("err = %v", err)
		}
		if strings.Contains(err.Error(), testToken) {
			t.Fatalf("token in error: %v", err)
		}
	})
	t.Run("unreachable", func(t *testing.T) {
		fake := newFakeSlack(t)
		client := fake.client()
		fake.server.Close()
		_, err := client.messagesSince(context.Background(), "C1", 0, 50)
		if err == nil || !strings.Contains(err.Error(), "unreachable") || strings.Contains(err.Error(), testToken) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestHTTPStatusesAreExplained(t *testing.T) {
	tests := []struct {
		name  string
		reply reply
		want  []string
	}{
		{"unauthorized", reply{status: 401, body: "denied"}, []string{"401", "token rejected", "denied"}},
		{"forbidden", reply{status: 403}, []string{"403", "access denied"}},
		{"rate limited", reply{status: 429, header: map[string]string{"Retry-After": "30"}}, []string{"429", "retry after 30s"}},
		{"server error", reply{status: 500, body: strings.Repeat("x", 1000)}, []string{"500"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeSlack(t)
			fake.respond("users.info", tt.reply)
			_, err := fake.client().displayName(context.Background(), "U1")
			if err == nil {
				t.Fatal("want an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
			if len(err.Error()) > 500 {
				t.Errorf("error excerpt is not capped: %d chars", len(err.Error()))
			}
		})
	}
}

func TestMalformedPayloads(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"not json", "{not json", "not valid JSON"},
		{"empty body", "", "not valid JSON"},
		{"truncated", `{"ok":true,"messages":[`, "not valid JSON"},
		{"json but not an object", `[1,2]`, "unexpected payload"},
		{"messages is not a list", `{"ok":true,"messages":"nope"}`, "unexpected payload"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeSlack(t)
			fake.respond("conversations.history", reply{body: tt.body})
			_, err := fake.client().messagesSince(context.Background(), "C1", 0, 50)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestEveryMethodRejectsAnUnexpectedShape(t *testing.T) {
	fake := newFakeSlack(t)
	for _, method := range []string{"search.messages", "conversations.history", "conversations.replies", "users.conversations"} {
		fake.respond(method, ok(object{"messages": "nope", "channels": "nope"}))
	}
	ctx := context.Background()
	client := fake.client()
	checks := map[string]error{}
	_, checks["search"] = client.search(ctx, "q", 5)
	_, checks["since"] = client.messagesSince(ctx, "C1", 0, 5)
	_, checks["replies"] = client.replies(ctx, "C1", "1.1", 0)
	_, checks["groups"] = client.groupConversations(ctx)
	_, checks["before"] = client.messagesBefore(ctx, "C1", "1.1", 5)
	for name, err := range checks {
		if err == nil || !strings.Contains(err.Error(), "unexpected payload") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestRequestsAreFormPostsWithTheTokenInTheHeaderOnly(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("search.messages", ok(object{"messages": object{"matches": []any{}}}))
	if _, err := fake.client().search(context.Background(), "is:dm after:2026-11-04", 50); err != nil {
		t.Fatal(err)
	}
	calls := fake.callsTo("search.messages")
	if len(calls) != 1 {
		t.Fatalf("calls = %d", len(calls))
	}
	want := url.Values{"query": {"is:dm after:2026-11-04"}, "count": {"50"}, "sort": {"timestamp"}, "sort_dir": {"desc"}}
	if !reflect.DeepEqual(calls[0].form, want) {
		t.Fatalf("form = %v, want %v", calls[0].form, want)
	}
	// fakeSlack.serve already failed the test if the token or a query string had reached the address.
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDefaultBaseURLIsSlack(t *testing.T) {
	var got string
	client := NewClient(testToken, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r.URL.String()
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true,"user_id":"U1","url":"https://a.slack.com/"}`)), Header: http.Header{}}, nil
	})}, "")
	if _, _, err := client.Identity(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got != "https://slack.com/api/auth.test" {
		t.Fatalf("url = %q", got)
	}
}

func TestCancelledContextStopsTheCall(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("auth.test", ok(object{"user_id": "U1", "url": "https://a.slack.com/"}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := fake.client().Identity(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestDisplayNamePrefersTheDisplayNameAndCaches(t *testing.T) {
	tests := []struct {
		name string
		user object
		want string
	}{
		{"display name", object{"name": "bob", "real_name": "Bob Martin", "profile": object{"display_name": "Bobby"}}, "Bobby"},
		{"real name", object{"name": "bob", "real_name": "Bob Martin", "profile": object{"display_name": ""}}, "Bob Martin"},
		{"handle", object{"name": "bob"}, "bob"},
		{"nothing", object{}, "U2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakeSlack(t)
			fake.respond("users.info", ok(object{"user": tt.user}))
			client := fake.client()
			for range 2 {
				if got, err := client.displayName(context.Background(), "U2"); err != nil || got != tt.want {
					t.Fatalf("displayName = %q, %v", got, err)
				}
			}
			if n := len(fake.callsTo("users.info")); n != 1 {
				t.Fatalf("users.info called %d times, want 1", n)
			}
		})
	}
}

func searchMatch(overrides object) object {
	match := object{"ts": "5.1", "text": "hi", "user": "U2", "permalink": "https://x/p", "channel": object{"id": "C1", "name": "front", "is_im": false}}
	for key, value := range overrides {
		match[key] = value
	}
	return match
}

func TestSearchMapsMatchesAndTellsDirectMessagesApart(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("search.messages", ok(object{"messages": object{"matches": []any{
		searchMatch(nil),
		searchMatch(object{"ts": "4.1", "channel": object{"id": "D1", "name": "U2", "is_im": true}}),
		searchMatch(object{"ts": "3.1", "channel": object{"id": "D2", "user": "W7", "name": "someone", "is_im": true}}),
		object{"ts": "1"},
		searchMatch(object{"ts": 2}),
		searchMatch(object{"user": nil}),
		"junk",
	}}}))
	got, err := fake.client().search(context.Background(), "is:dm", 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []searchHit{
		{ts: "5.1", text: "hi", userID: "U2", channelID: "C1", channelName: "front", permalink: ptr("https://x/p")},
		{ts: "4.1", text: "hi", userID: "U2", channelID: "D1", isDM: true, directPeerID: "U2", permalink: ptr("https://x/p")},
		{ts: "3.1", text: "hi", userID: "U2", channelID: "D2", isDM: true, directPeerID: "W7", permalink: ptr("https://x/p")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %+v\nwant %+v", got, want)
	}
}

func TestSearchReadsTheThreadParentFromThePermalink(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("search.messages", ok(object{"messages": object{"matches": []any{
		searchMatch(object{"permalink": "https://x/archives/C1/p51?thread_ts=4.000100&cid=C1"}),
		searchMatch(object{"ts": "6.1", "permalink": "://not a url"}),
		searchMatch(object{"ts": "7.1", "permalink": nil}),
	}}}))
	hits, err := fake.client().search(context.Background(), "q", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].threadTS != "4.000100" || hits[1].threadTS != "" || hits[2].threadTS != "" || hits[2].permalink != nil {
		t.Fatalf("hits = %+v", hits)
	}
}

func TestSearchExplainsAMissingScope(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("search.messages", slackError("missing_scope", "search:read"))
	_, err := fake.client().search(context.Background(), "q", 50)
	if err == nil || !strings.Contains(err.Error(), `"search:read" scope`) {
		t.Fatalf("err = %v", err)
	}
}

func TestMessagesSinceAreOldestFirstAndSkipMalformedEntries(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("conversations.history", ok(object{"messages": []any{
		object{"ts": "3", "text": "c", "user": "U1"}, object{"ts": "2", "text": "b"}, object{"ts": 9}, "junk",
	}}))
	got, err := fake.client().messagesSince(context.Background(), "C1", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []conversationMessage{{ts: "2", text: "b", files: []FileRef{}}, {ts: "3", text: "c", userID: "U1", files: []FileRef{}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %+v", got)
	}
	form := fake.callsTo("conversations.history")[0].form
	if form.Get("channel") != "C1" || form.Get("oldest") != "1" || form.Get("limit") != "50" {
		t.Fatalf("form = %v", form)
	}
}

func TestRepliesExcludeTheParentMessage(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("conversations.replies", ok(object{"messages": []any{
		object{"ts": "4.1", "text": "parent", "user": "U1"}, object{"ts": "5.1", "text": "reply", "user": "U2"},
	}}))
	got, err := fake.client().replies(context.Background(), "C1", "4.1", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []conversationMessage{{ts: "5.1", text: "reply", userID: "U2", files: []FileRef{}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replies = %+v", got)
	}
}

func TestMessagesBeforeAreOldestFirst(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("conversations.history", ok(object{"messages": []any{object{"user": "U1", "text": "newer"}, object{"text": "older"}, object{"user": "U3"}}}))
	got, err := fake.client().messagesBefore(context.Background(), "C1", "5.1", 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []contextMessage{{text: "older"}, {userID: "U1", text: "newer"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %+v", got)
	}
	if form := fake.callsTo("conversations.history")[0].form; form.Get("latest") != "5.1" || form.Get("inclusive") != "false" {
		t.Fatalf("form = %v", form)
	}
}

func TestGroupConversations(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("users.conversations", ok(object{"channels": []any{object{"id": "G1", "name": "mpdm-a--b"}, object{"id": "G2"}, object{"name": "x"}}}))
	got, err := fake.client().groupConversations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []conversation{{"G1", "mpdm-a--b"}, {"G2", "G2"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("groups = %+v", got)
	}
	if form := fake.callsTo("users.conversations")[0].form; form.Get("types") != "mpim" {
		t.Fatalf("form = %v", form)
	}

	fake.respond("users.conversations", slackError("missing_scope", "mpim:read"))
	if _, err := fake.client().groupConversations(context.Background()); err == nil || !strings.Contains(err.Error(), `"mpim:read" scope`) {
		t.Fatalf("err = %v", err)
	}
}

var slackFile = object{"id": "F1", "name": "error.png", "mimetype": "image/png", "size": 1234.0, "url_private": "https://files.slack.com/files-pri/T1-F1/error.png"}

func with(base object, overrides object) object {
	merged := object{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range overrides {
		merged[key] = value
	}
	return merged
}

func TestParseFiles(t *testing.T) {
	got := parseFiles([]any{
		slackFile,
		with(slackFile, object{"id": "F2", "is_external": true}),
		with(slackFile, object{"id": "F3", "mode": "tombstone"}),
		object{"id": "F4"},
		object{"url_private": "https://files.slack.com/x"},
		with(slackFile, object{"id": "F5", "name": "", "title": "Screenshot"}),
		object{"id": "F6", "url_private": "https://files.slack.com/y"},
		"junk",
	})
	want := []FileRef{
		{ID: "F1", Name: "error.png", Mime: "image/png", URL: "https://files.slack.com/files-pri/T1-F1/error.png", Size: 1234},
		{ID: "F5", Name: "Screenshot", Mime: "image/png", URL: "https://files.slack.com/files-pri/T1-F1/error.png", Size: 1234},
		{ID: "F6", Name: "file", Mime: "application/octet-stream", URL: "https://files.slack.com/y"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %+v", got)
	}
	if parseFiles(nil) == nil || len(parseFiles("nope")) != 0 {
		t.Fatal("a payload without files must give an empty, non-nil list")
	}
}

func TestSearchFilesAreOnlyKnownWhenSlackSaysSo(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("search.messages", ok(object{"messages": object{"matches": []any{
		searchMatch(object{"files": []any{slackFile, with(slackFile, object{"id": "F2", "is_external": true})}}),
		searchMatch(object{"ts": "4.1"}),
		searchMatch(object{"ts": "3.1", "files": []any{}}),
	}}}))
	hits, err := fake.client().search(context.Background(), "q", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits[0].files) != 1 || hits[0].files[0].ID != "F1" {
		t.Fatalf("files = %+v", hits[0].files)
	}
	if hits[1].files != nil {
		t.Fatalf("search did not list files, want nil, got %+v", hits[1].files)
	}
	if hits[2].files == nil || len(hits[2].files) != 0 {
		t.Fatalf("search said there are no files, want empty non-nil, got %+v", hits[2].files)
	}
}

func TestMessageFilesAsksForOneMessage(t *testing.T) {
	fake := newFakeSlack(t)
	fake.respond("conversations.history", ok(object{"messages": []any{object{"ts": "5.1", "text": "x", "files": []any{slackFile}}}}))
	files, err := fake.client().messageFiles(context.Background(), "C1", "5.1")
	if err != nil || len(files) != 1 {
		t.Fatalf("files = %+v, %v", files, err)
	}
	form := fake.callsTo("conversations.history")[0].form
	if form.Get("latest") != "5.1" || form.Get("oldest") != "5.1" || form.Get("inclusive") != "true" || form.Get("limit") != "1" {
		t.Fatalf("form = %v", form)
	}

	fake.respond("conversations.history", ok(object{"messages": []any{}}))
	if files, err := fake.client().messageFiles(context.Background(), "C1", "5.1"); err != nil || files == nil || len(files) != 0 {
		t.Fatalf("files = %+v, %v", files, err)
	}
}

// slackFilesServer serves file downloads on a local server while the client believes it talks to files.slack.com:
// the transport keeps the original Host and sends the request to the test server over plain HTTP.
type slackFilesServer struct {
	server *httptest.Server
	hosts  []string
	tokens []string
	client *http.Client
}

func newSlackFilesServer(t *testing.T, handler http.HandlerFunc) *slackFilesServer {
	t.Helper()
	s := &slackFilesServer{}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hosts = append(s.hosts, r.Host)
		s.tokens = append(s.tokens, r.Header.Get("Authorization"))
		handler(w, r)
	}))
	t.Cleanup(s.server.Close)
	target, _ := url.Parse(s.server.URL)
	s.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rewritten := r.Clone(r.Context())
		rewritten.URL.Scheme = "http"
		rewritten.URL.Host = target.Host
		return http.DefaultTransport.RoundTrip(rewritten)
	})}
	return s
}

func TestDownloadFileSendsTheTokenToSlackAndReturnsTheBytes(t *testing.T) {
	files := newSlackFilesServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
	})
	client := NewClient(testToken, files.client, "")
	data, err := client.DownloadFile(context.Background(), FileRef{ID: "F1", URL: "https://files.slack.com/files-pri/T1-F1/error.png", Size: 4})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(data, []byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("data = %v", data)
	}
	if files.tokens[0] != "Bearer "+testToken || files.hosts[0] != "files.slack.com" {
		t.Fatalf("token %q sent to %q", files.tokens[0], files.hosts[0])
	}
	// A workspace host is Slack's too.
	if _, err := client.DownloadFile(context.Background(), FileRef{ID: "F2", URL: "https://acme.slack.com/f", Size: 1}); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadFileNeverContactsAHostThatIsNotSlack(t *testing.T) {
	client := NewClient(testToken, &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		t.Errorf("a request left for %s", r.URL)
		return nil, errors.New("must not be called")
	})}, "")
	for _, link := range []string{
		"https://evil.example.com/a.png",
		"http://files.slack.com/a.png",
		"https://slack.com.evil.example.com/a.png",
		"https://evilslack.com/a.png",
		"https://files.slack.com@evil.example.com/a.png",
		"ftp://files.slack.com/a.png",
		"not a url",
		"",
	} {
		_, err := client.DownloadFile(context.Background(), FileRef{ID: "F1", URL: link, Size: 1})
		if !errors.Is(err, ErrNotSlackFile) {
			t.Errorf("%q: err = %v, want ErrNotSlackFile", link, err)
		}
	}
}

func TestDownloadFileRefusesFilesThatAreTooLarge(t *testing.T) {
	client := NewClient(testToken, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("a file announced as too large must not be fetched")
		return nil, errors.New("must not be called")
	})}, "")
	_, err := client.DownloadFile(context.Background(), FileRef{URL: "https://files.slack.com/a.png", Size: 9 << 20})
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("err = %v", err)
	}

	// The announced size can lie: the cap is enforced on what actually arrives.
	files := newSlackFilesServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 8<<20+1))
	})
	_, err = NewClient(testToken, files.client, "").DownloadFile(context.Background(), FileRef{URL: "https://files.slack.com/a.bin", Size: 10})
	if !errors.Is(err, ErrFileTooLarge) {
		t.Fatalf("err = %v", err)
	}

	exact := newSlackFilesServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 8<<20))
	})
	data, err := NewClient(testToken, exact.client, "").DownloadFile(context.Background(), FileRef{URL: "https://files.slack.com/a.bin", Size: 8 << 20})
	if err != nil || len(data) != 8<<20 {
		t.Fatalf("a file of exactly 8 MB is allowed: %d bytes, %v", len(data), err)
	}
}

func TestDownloadFileReportsAMissingFilesScope(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		status  int
	}{
		{"sign-in page", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html>Sign in</html>"))
		}, 0},
		{"forbidden", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusForbidden) }, 403},
		{"unauthorized", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusUnauthorized) }, 401},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := newSlackFilesServer(t, tt.handler)
			_, err := NewClient(testToken, files.client, "").DownloadFile(context.Background(), FileRef{URL: "https://files.slack.com/a.png", Size: 10})
			var access *FilesAccessError
			if !errors.As(err, &access) || access.Status != tt.status || !strings.Contains(err.Error(), "files:read") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestDownloadFileOtherFailuresAreNotScopeErrors(t *testing.T) {
	files := newSlackFilesServer(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "boom", http.StatusInternalServerError) })
	_, err := NewClient(testToken, files.client, "").DownloadFile(context.Background(), FileRef{URL: "https://files.slack.com/a.png", Size: 10})
	var access *FilesAccessError
	if err == nil || errors.As(err, &access) || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadFileDoesNotFollowARedirectOutOfSlack(t *testing.T) {
	files := newSlackFilesServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example.com/steal", http.StatusFound)
	})
	_, err := NewClient(testToken, files.client, "").DownloadFile(context.Background(), FileRef{URL: "https://files.slack.com/a.png", Size: 10})
	if !errors.Is(err, ErrNotSlackFile) {
		t.Fatalf("err = %v", err)
	}
	if len(files.hosts) != 1 {
		t.Fatalf("the redirect was followed: hosts %v", files.hosts)
	}
}

func TestDownloadFileHonoursTheContext(t *testing.T) {
	files := newSlackFilesServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient(testToken, files.client, "").DownloadFile(ctx, FileRef{URL: "https://files.slack.com/a.png", Size: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadFileLeavesTheCallersClientUntouched(t *testing.T) {
	files := newSlackFilesServer(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("x")) })
	if _, err := NewClient(testToken, files.client, "").DownloadFile(context.Background(), FileRef{URL: "https://files.slack.com/a.png", Size: 1}); err != nil {
		t.Fatal(err)
	}
	if files.client.CheckRedirect != nil {
		t.Fatal("the caller's http.Client was modified")
	}
}
