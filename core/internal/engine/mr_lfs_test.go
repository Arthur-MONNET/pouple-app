package engine

import (
	"context"
	"errors"
	"github.com/Arthur-MONNET/pouple/core/internal/i18n"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mrAction() Action {
	return Action{Kind: MergeRequest, Scope: gitlabScope("acme/app"), Summary: "Open MR", Branch: "fix/logo", Base: "master"}
}

func TestMRGrantIsLimitedToProjectSourceTargetAndKind(t *testing.T) {
	a := mrAction()
	grants := grantsOf(t, Plan{Goal: "Open MR", Actions: []Action{a}})
	if !grants.Covers(a) {
		t.Fatal("approved pair not covered")
	}
	for _, change := range []func(*Action){
		func(a *Action) { a.Branch = "fix/other" },
		func(a *Action) { a.Base = "develop" },
		func(a *Action) { a.Scope.ID = "other/app" },
		func(a *Action) { a.Kind = PushBranch },
		func(a *Action) { a.Kind = "gitlab.merge" },
	} {
		other := a
		change(&other)
		if grants.Covers(other) {
			t.Fatalf("widened approval: %+v", other)
		}
	}
	legacy := Grants{{Scope: a.Scope, Kinds: []ActionKind{MergeRequest}}}
	if legacy.Covers(a) {
		t.Fatal("legacy project-wide grant authorized MR")
	}
}

func TestMRRequiresBranchesAndFollowsPush(t *testing.T) {
	a := mrAction()
	for _, pair := range [][2]string{{"", "master"}, {"fix/logo", ""}, {"master", "master"}, {"--bad", "master"}} {
		a.Branch, a.Base = pair[0], pair[1]
		if _, err := (Plan{Goal: "MR", Actions: []Action{a}}).Normalize(); err == nil {
			t.Fatal("invalid branches accepted")
		}
	}
	a = mrAction()
	edit := Action{Kind: EditCode, Scope: codeScope("app"), Summary: "Fix logo", Branch: a.Branch, Base: "master"}
	if _, err := (Plan{Goal: "Fix", Actions: []Action{a, edit}}).Normalize(); err == nil {
		t.Fatal("MR before push accepted")
	}
}

func TestLFSTransferPrecedesPushAndFailuresStopPush(t *testing.T) {
	for _, fail := range []string{"", "version", "transfer", "attributes"} {
		t.Run(fail, func(t *testing.T) {
			dir := t.TempDir()
			pointer := "gitdir: /trusted/git\n"
			if err := os.WriteFile(filepath.Join(dir, ".git"), []byte(pointer), 0600); err != nil {
				t.Fatal(err)
			}
			w := Worktree{Path: dir, gitDir: "/trusted/git", gitFile: pointer}
			var calls []string
			s := &Service{deps: Deps{Language: func() i18n.Lang { return i18n.EN }, Git: func(ctx context.Context, dir string, args ...string) (string, error) {
				command := strings.Join(args, " ")
				label := "push"
				switch {
				case strings.Contains(command, " log "):
					label = "attributes"
				case strings.Contains(command, "lfs version"):
					label = "version"
				case strings.Contains(command, "lfs push"):
					label = "transfer"
				}
				calls = append(calls, label)
				if label == fail {
					return "missing local object", errors.New("failed")
				}
				if label == "attributes" {
					return "commit", nil
				}
				return "", nil
			}}}
			out, err := s.pushWorktree(context.Background(), w, origin, "HEAD:refs/heads/fix/logo")
			want := []string{"attributes", "version", "transfer", "push"}
			if fail != "" {
				for i, v := range want {
					if v == fail {
						want = want[:i+1]
						break
					}
				}
				if err == nil || out == "" {
					t.Fatal("failure not reported")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, want) {
				t.Fatalf("calls %v, want %v", calls, want)
			}
		})
	}
}

func TestLFSCommandsRetainRemoteTimeoutAndCredentials(t *testing.T) {
	if !talksToRemote([]string{"--git-dir=/trusted", "-c", "lfs.allowincompletepush=false", "lfs", "push", "--all", "origin", "HEAD"}) {
		t.Fatal("LFS not recognized as remote command")
	}
}

func TestApprovedCodePlanTransfersLFSPushesThenOpensMR(t *testing.T) {
	for _, failure := range []string{"", "transfer", "push"} {
		t.Run(failure, func(t *testing.T) {
			proposed := samplePlan()
			actions := proposed["plan"].(map[string]any)["actions"].([]any)
			proposed["plan"].(map[string]any)["actions"] = append(actions, map[string]any{"kind": "gitlab.merge-request", "scope": map[string]any{"service": "gitlab", "id": "acme/login-app", "name": "App"}, "summary": "Open MR", "branch": "fix/login-redirect", "base": "main"})
			h, project, _ := planHarness(t, reply(proposed), writes("redirect.txt", "fixed\n"))
			remote := filepath.Join(t.TempDir(), "origin.git")
			gitIn(t, project, "init", "--bare", remote)
			gitIn(t, project, "remote", "add", "origin", remote)
			gitIn(t, project, "push", "origin", "main")
			var order []string
			h.svc.deps.Git = func(ctx context.Context, dir string, args ...string) (string, error) {
				command := strings.Join(args, " ")
				switch {
				case strings.Contains(command, " log -1 --format=%H -G "):
					return "lfs-attributes-commit", nil
				case strings.Contains(command, "lfs version"):
					return "git-lfs", nil
				case strings.Contains(command, "lfs push"):
					order = append(order, "transfer")
					if failure == "transfer" {
						return "missing local LFS object", errors.New("failed")
					}
					return "", nil
				case strings.Contains(command, " push --set-upstream "):
					order = append(order, "push")
					if failure == "push" {
						return "rejected", errors.New("failed")
					}
				}
				return runGit(ctx, dir, args...)
			}
			h.svc.deps.OpenMergeRequest = func(ctx context.Context, a Action, title string) (string, error) {
				order = append(order, "mr")
				if out, err := runGit(ctx, remote, "rev-parse", "--verify", "refs/heads/"+a.Branch); err != nil || out == "" {
					t.Error("MR before successful push")
				}
				return "https://gitlab.example.com/acme/login-app/-/merge_requests/42", nil
			}
			h.receive(message("fix the redirect and open a MR"))
			got := waitingPlan(t, h)
			if err := h.svc.DecidePlan(got.ID, true, got.Plan.ID); err != nil {
				t.Fatal(err)
			}
			h.svc.Idle()
			want := []string{"transfer", "push", "mr"}
			if failure == "transfer" {
				want = want[:1]
			} else if failure == "push" {
				want = want[:2]
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("order %v, want %v", order, want)
			}
			final := h.svc.Snapshot().Requests[0]
			if failure == "" && !strings.Contains(journalOf(final), "/merge_requests/42") {
				t.Fatal("missing MR link")
			}
		})
	}
}

func TestMRCannotBeAuthorizedByAutomaticCodeApprovalOrDeclinedPlan(t *testing.T) {
	a := mrAction()
	proposal := map[string]any{"outcome": ProposesPlan, "summary": "Open MR", "plan": Plan{Goal: "Open MR", Actions: []Action{a}}}
	h := newHarness(t, reply(proposal))
	h.svc.deps.AutoApproveAll = func() bool { return true }
	calls := 0
	h.svc.deps.OpenMergeRequest = func(context.Context, Action, string) (string, error) { calls++; return "url", nil }
	h.receive(message("service data suggests a MR"))
	got := waitingPlan(t, h)
	if calls != 0 {
		t.Fatal("automatic setting authorized GitLab write")
	}
	if err := h.svc.DecidePlan(got.ID, false, got.Plan.ID); err != nil {
		t.Fatal(err)
	}
	h.svc.Idle()
	if calls != 0 {
		t.Fatal("declined MR executed")
	}
}

func TestRealLFSPushTransfersObjectsWithRepositoryHooksDisabled(t *testing.T) {
	if _, err := runGit(context.Background(), t.TempDir(), "lfs", "version"); err != nil {
		t.Skip("git-lfs is not installed")
	}
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "transfer", true: "missing-object"}[missing], func(t *testing.T) {
			h, project, _ := planHarness(t)
			gitIn(t, project, "lfs", "install", "--local")
			if err := os.WriteFile(filepath.Join(project, ".gitattributes"), []byte("*.bin filter=lfs diff=lfs merge=lfs -text\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(project, "asset.bin"), []byte("binary content for the LFS fixture\n"), 0600); err != nil {
				t.Fatal(err)
			}
			gitIn(t, project, "add", ".")
			gitIn(t, project, "commit", "-qm", "Add LFS asset")
			remote := filepath.Join(t.TempDir(), "origin.git")
			gitIn(t, project, "init", "--bare", remote)
			gitIn(t, project, "remote", "add", "origin", remote)
			marker := filepath.Join(t.TempDir(), "hook-ran")
			// A repository hook must remain disabled, even though LFS objects are sent.
			if err := os.WriteFile(filepath.Join(project, ".git", "hooks", "pre-push"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0700); err != nil {
				t.Fatal(err)
			}
			w, err := h.svc.addWorktree(context.Background(), project, "login-app", "fix/lfs", filepath.Join(t.TempDir(), "work"), "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			if missing {
				if err := os.RemoveAll(filepath.Join(project, ".git", "lfs", "objects")); err != nil {
					t.Fatal(err)
				}
				out, err := h.svc.pushWorktree(context.Background(), w, origin, "HEAD:refs/heads/fix/lfs")
				if err == nil || !strings.Contains(out, "missing") {
					t.Fatalf("missing object not reported: %s %v", out, err)
				}
				if _, err := runGit(context.Background(), remote, "rev-parse", "--verify", "refs/heads/fix/lfs"); err == nil {
					t.Fatal("git ref pushed without LFS object")
				}
				return
			}
			if out, err := h.svc.pushWorktree(context.Background(), w, origin, "HEAD:refs/heads/fix/lfs"); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("repository hook ran")
			}
			files, err := filepath.Glob(filepath.Join(remote, "lfs", "objects", "*", "*", "*"))
			if err != nil || len(files) == 0 {
				t.Fatalf("LFS objects were not transferred: %v %v", files, err)
			}
		})
	}
}

func TestAutomaticPlanCannotGrantMRAndForeignDataInvalidatesReuse(t *testing.T) {
	a := mrAction()
	p := approved(t, Plan{Goal: "Open MR", Actions: []Action{a}})
	p.Automatic = true
	if _, err := (Grants{}).With(p); err == nil {
		t.Fatal("automatic plan granted writes")
	}
	p.Automatic = false
	h := newHarness(t, reply(map[string]any{"summary": "Ready"}))
	requests := h.receive(message("hello"))
	id := requests[0].ID
	grants := grantsOf(t, p)
	h.svc.update(id, func(r *Stored) { r.Grants = grants; r.ForeignSinceGrant = true })
	h.svc.proposePlan(id, Outcome{Plan: &p, Summary: "Open MR"}, h.svc.entry("assistant", "text", "Open MR"))
	waitingPlan(t, h)
}
