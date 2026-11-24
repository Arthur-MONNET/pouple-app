package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Arthur-MONNET/pouple/core/internal/connectors/asana"
)

func TestAsanaTaskFromChatRequiresApprovalAndSurvivesReplay(t *testing.T) {
	const link = "https://app.asana.com/0/12345/67890"
	action := Action{
		Kind: CreateTask, Scope: Scope{Service: ScopeAsana, ID: "12345", Name: "Spiice"},
		Summary:     "Créer la tâche Asana",
		Title:       "Spiice — conserver les filtres au retour",
		Description: "Depuis la liste des offres, ouvrir une offre puis revenir.\n\nAttendu : conserver les filtres sélectionnés.\n",
	}
	for _, tc := range []struct {
		name         string
		status       int
		firstResult  string
		replayResult string
		replayPosts  int32
	}{
		{"confirmed", http.StatusCreated, "[Spiice — conserver les filtres au retour](" + link + ")", "already created", 1},
		{"uncertain", http.StatusBadGateway, "is unconfirmed", "is unconfirmed", 1},
		{"rejected", http.StatusForbidden, "could not create", "created [", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fake-token" {
					t.Error("missing Asana authorization")
				}
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/users/me":
					fmt.Fprint(w, `{"data":{"gid":"11","workspaces":[{"gid":"12","name":"Produit"}]}}`)
				case r.Method == http.MethodGet && r.URL.Path == "/workspaces/12/projects":
					fmt.Fprint(w, `{"data":[{"gid":"12345","name":"Spiice"}]}`)
				case r.Method == http.MethodGet && r.URL.Path == "/projects/12345":
					fmt.Fprint(w, `{"data":{"gid":"12345","name":"Spiice"}}`)
				case r.Method == http.MethodPost && r.URL.Path == "/tasks":
					var payload struct {
						Data struct {
							Name     string
							Notes    string
							Projects []string
						}
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil ||
						payload.Data.Name != action.Title || payload.Data.Notes != action.Description ||
						!slices.Equal(payload.Data.Projects, []string{action.Scope.ID}) {
						t.Errorf("incorrect task payload: %+v (%v)", payload, err)
					}
					if posts.Add(1) == 1 {
						w.WriteHeader(tc.status)
					}
					fmt.Fprintf(w, `{"data":{"gid":"67890","permalink_url":%q}}`, link)
				default:
					t.Errorf("unexpected API request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			client := (&asana.Connector{Base: server.URL, HTTP: server.Client()}).Client("fake-token")
			answer := func(summary string) func(context.Context, Turn) (TurnResult, error) {
				a := action
				a.Summary = summary
				return reply(map[string]any{
					"outcome": ProposesPlan, "summary": "**Créer la tâche Spiice**",
					"plan": Plan{Goal: "Créer la tâche Spiice", Actions: []Action{a}},
				})
			}
			draft := action.Title + "\n\n" + action.Description
			h := newHarness(t, reply(map[string]any{"outcome": Answered, "summary": draft}),
				lookupReply(Lookup{Service: "asana", Action: "project", Target: "Spiice"}),
				answer(action.Summary), answer("Reprendre la création"))
			h.svc.deps.Lookups = func(ctx context.Context, lookups []Lookup) ServiceContext {
				if !slices.Equal(lookups, []Lookup{{Service: "asana", Action: "project", Target: "Spiice"}}) {
					t.Errorf("unexpected project lookup: %+v", lookups)
					return ServiceContext{}
				}
				projects, err := client.FindProjects(ctx, lookups[0].Target)
				if err != nil || len(projects) != 1 {
					t.Errorf("project lookup: %+v, %v", projects, err)
					return ServiceContext{}
				}
				return ServiceContext{Text: fmt.Sprintf("Asana project %s (gid %s)", projects[0].Name, projects[0].GID)}
			}
			h.svc.deps.AutoApproveAll = func() bool { return true }
			h.svc.deps.AsanaProjectName = func(ctx context.Context, gid string) (string, error) {
				p, err := client.Project(ctx, gid)
				return p.Name, err
			}
			h.svc.deps.CreateAsanaTask = client.CreateTask
			h.receive(message("Prépare un brouillon à partir des retours Spiice", func(in *Incoming) { in.Source.Origin = "note-to-self" }))
			id := only(t, h).ID
			if err := h.svc.Message(id, "Crée cette tâche dans le projet Asana Spiice", nil); err != nil {
				t.Fatal(err)
			}
			h.svc.Idle()
			proposed := waitingPlan(t, h)
			if posts.Load() != 0 || !slices.Equal(proposed.Plan.Actions, []Action{action}) ||
				!strings.Contains(h.codex.turns[2].Prompt, "Asana project Spiice (gid 12345)") {
				t.Fatalf("before approval: posts=%d plan=%+v", posts.Load(), proposed.Plan)
			}
			changed := *proposed.Plan
			changed.Actions = slices.Clone(changed.Actions)
			changed.Actions[0].Description += "Changed"
			if err := h.svc.DecidePlan(id, true, PlanFingerprint(changed)); err == nil {
				t.Fatal("approval allowed changed task contents")
			}
			approve := func() {
				t.Helper()
				p := waitingPlan(t, h)
				if err := h.svc.DecidePlan(id, true, p.Plan.ID); err != nil {
					t.Fatal(err)
				}
				h.svc.Idle()
			}
			approve()
			if final := only(t, h); !strings.Contains(journalOf(final), tc.firstResult) || len(final.Grants) != 0 || posts.Load() != 1 {
				t.Fatalf("first result: posts=%d journal=%s", posts.Load(), journalOf(final))
			}
			if err := h.svc.DecidePlan(id, true, proposed.Plan.ID); err == nil {
				t.Fatal("a second click replayed approval")
			}
			store := h.svc.deps.Store
			store.Flush()
			reopened, err := OpenStore(store.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			h.svc.deps.Store = reopened
			t.Cleanup(reopened.Flush)
			if err := h.svc.Message(id, "Relance la création de ce brouillon", nil); err != nil {
				t.Fatal(err)
			}
			h.svc.Idle()
			approve()
			got := only(t, h)
			if posts.Load() != tc.replayPosts || !strings.Contains(got.Entries[len(got.Entries)-1].Text, tc.replayResult) {
				t.Fatalf("replay: posts=%d journal=%s", posts.Load(), journalOf(got))
			}
		})
	}
}
