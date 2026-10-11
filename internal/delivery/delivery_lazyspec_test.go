package delivery

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"wd/internal/core"
)

// repo is a clone of a bare origin with one commit on main, checked out on
// branch; it returns the clone and runs git in it.
func repo(t *testing.T, branch string) (string, func(args ...string) string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	clone := filepath.Join(root, "clone")
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run(root, "init", "--bare", "-b", "main", origin)
	run(root, "clone", origin, clone)
	run(clone, "commit", "--allow-empty", "-m", "first")
	run(clone, "push", "origin", "HEAD:main")
	run(clone, "remote", "set-head", "origin", "main")
	if branch != "main" {
		run(clone, "checkout", "-b", branch)
	}
	return clone, func(args ...string) string { return run(clone, args...) }
}

// fakeGH puts a gh on PATH that answers from the given replies, keyed by the
// link it is asked about; a link with no reply fails as gh does.
func fakeGH(t *testing.T, replies map[string]string) {
	t.Helper()
	bin := t.TempDir()
	var cases strings.Builder
	for link, reply := range replies {
		cases.WriteString("    *'" + link + "'*) echo '" + reply + "'; exit 0;;\n")
	}
	script := "#!/bin/sh\ncase \"$*\" in\n" + cases.String() + "    *) echo 'GraphQL: Could not resolve to a PullRequest' >&2; exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func landed(body string) []core.Event {
	return []core.Event{{ID: 1, Kind: core.EventPr, Body: body, At: "2026-10-11T10:00:00Z"}}
}

var running = core.Work{ID: "g1", State: core.StateRunning}

func TestDelivery(t *testing.T) {
	t.Run("A Commit Landing Is Landed Once Origin's Base Branch Has It", func(t *testing.T) {
		dir, git := repo(t, "work")
		git("commit", "--allow-empty", "-m", "the change")
		sha := git("rev-parse", "HEAD")
		for _, body := range []string{"commit " + sha, "commit https://github.com/x/y/commit/" + sha, sha} {
			if st := StatusFor(dir, landed(body), running, nil); st.Status != StatusCommittedUnpushed || st.Error != "" {
				t.Fatalf("%q before it reaches origin/main = %+v, want committed-unpushed", body, st)
			}
		}
		git("push", "origin", "HEAD:main")
		if st := StatusFor(dir, landed("commit https://github.com/x/y/commit/"+sha), running, nil); st.Status != StatusLandedCommit {
			t.Fatalf("commit link on origin/main = %+v, want landed-commit", st)
		}
	})

	t.Run("A Pull Request Landing Reads Its State From GitHub", func(t *testing.T) {
		dir, _ := repo(t, "work")
		fakeGH(t, map[string]string{
			"pull/1": `{"state":"OPEN","isDraft":true}`,
			"pull/2": `{"state":"OPEN","isDraft":false}`,
			"pull/3": `{"state":"CLOSED","isDraft":false}`,
			"pull/4": `{"state":"MERGED","isDraft":false}`,
		})
		for n, want := range map[string]Status{"1": StatusPrDraft, "2": StatusPrOpen, "3": StatusPrClosedUnmerged, "4": StatusLandedPR} {
			if st := StatusFor(dir, landed("pull-request https://github.com/x/y/pull/"+n), running, nil); st.Status != want {
				t.Errorf("pull %s = %+v, want %s", n, st, want)
			}
		}
	})

	t.Run("Landed Work Is Shipped Once It Is Done", func(t *testing.T) {
		dir, git := repo(t, "main")
		sha := git("rev-parse", "HEAD")
		st := StatusFor(dir, landed("commit "+sha), running, nil)
		if st.Status != StatusLandedCommit || len(st.Attention) != 1 || st.Attention[0] != AttentionReadyToClose {
			t.Fatalf("landed, not done = %+v, want ready-to-close", st)
		}
		done := core.Work{ID: "g1", State: core.StateDone}
		if st := StatusFor(dir, landed("commit "+sha), done, nil); st.Status != StatusShipped || len(st.Attention) != 0 {
			t.Fatalf("landed and done = %+v, want shipped", st)
		}
	})

	t.Run("Unlanded Work Is Read From Its Branch", func(t *testing.T) {
		dir, git := repo(t, "work")
		if st := StatusFor(dir, nil, running, nil); st.Status != StatusNone || st.Error != "" {
			t.Fatalf("nothing beyond origin/main = %+v, want none", st)
		}
		git("commit", "--allow-empty", "-m", "local")
		if st := StatusFor(dir, nil, running, nil); st.Status != StatusCommittedUnpushed {
			t.Fatalf("unpushed = %+v, want committed-unpushed", st)
		}
		git("push", "-u", "origin", "work")
		if st := StatusFor(dir, nil, running, nil); st.Status != StatusPushedUnpr {
			t.Fatalf("pushed = %+v, want pushed-unpr", st)
		}
		git("commit", "--allow-empty", "-m", "more")
		if st := StatusFor(dir, nil, running, nil); st.Status != StatusCommittedUnpushed {
			t.Fatalf("partly pushed = %+v, want committed-unpushed", st)
		}
	})

	t.Run("What Cannot Be Read Is Said, Not Guessed", func(t *testing.T) {
		dir, _ := repo(t, "work")
		fakeGH(t, map[string]string{})
		st := StatusFor(dir, landed("pull-request https://github.com/x/y/pull/9"), running, nil)
		if st.Status != StatusNone || !strings.Contains(st.Error, "Could not resolve") {
			t.Fatalf("unreadable pull request = %+v, want the gh error and no guessed status", st)
		}
		if st := StatusFor(t.TempDir(), nil, running, nil); st.Error == "" || st.Status != StatusNone {
			t.Fatalf("not a repository = %+v, want git's error", st)
		}
	})

	t.Run("A Landing Link Is Checked Before It Is Recorded", func(t *testing.T) {
		fakeGH(t, map[string]string{"pull/5": `{"number":5}`, "commits/abc1234": "abc1234"})
		for _, ok := range []string{"https://github.com/x/y/pull/5", "https://github.com/x/y/commit/abc1234"} {
			if err := Resolve(ok); err != nil {
				t.Errorf("Resolve(%s) = %v, want it accepted", ok, err)
			}
		}
		if err := Resolve("https://github.com/x/y/pull/6"); err == nil || !strings.Contains(err.Error(), "pull/6") {
			t.Errorf("a pull request gh cannot read = %v, want it refused naming it", err)
		}
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/gone" {
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()
		was := http.DefaultTransport
		http.DefaultTransport = srv.Client().Transport
		t.Cleanup(func() { http.DefaultTransport = was })
		if err := Resolve(srv.URL + "/mr/1"); err != nil {
			t.Errorf("a link that answers = %v, want it accepted", err)
		}
		if err := Resolve(srv.URL + "/gone"); err == nil || !strings.Contains(err.Error(), "404") {
			t.Errorf("a link that answers 404 = %v, want it refused saying so", err)
		}
		for _, bad := range []string{"https://example.test/pr/1", "http://plain.example/x", "not a link"} {
			if err := Resolve(bad); err == nil || !strings.Contains(err.Error(), bad) {
				t.Errorf("Resolve(%q) = %v, want it refused naming it", bad, err)
			}
		}
	})
}
