package delivery

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"wd/internal/core"
)

type Status string

const (
	StatusNone              Status = "none"
	StatusCommittedUnpushed Status = "committed-unpushed"
	StatusPushedUnpr        Status = "pushed-unpr"
	StatusPrDraft           Status = "pr-draft"
	StatusPrOpen            Status = "pr-open"
	StatusPrClosedUnmerged  Status = "pr-closed-unmerged"
	StatusPrMerged          Status = "pr-merged"
	StatusLandedPR          Status = "landed-pr"
	StatusLandedCommit      Status = "landed-commit"
	StatusShipped           Status = "shipped"
)

type Attention string

const (
	AttentionReadyToClose   Attention = "ready-to-close"
	AttentionStalePR        Attention = "stale-pr"
	AttentionCIBlocked      Attention = "ci-blocked"
	AttentionForgottenAfter Attention = "forgotten-after-merge"
)

type State struct {
	Status     Status      `json:"status"`
	Detail     string      `json:"detail"`
	Attention  []Attention `json:"attention"`
	PRState    string      `json:"pr_state,omitempty"`
	PRMerged   *bool       `json:"pr_merged,omitempty"`
	PRDraft    *bool       `json:"pr_draft,omitempty"`
	BaseBranch string      `json:"base_branch,omitempty"`
	Branch     string      `json:"branch,omitempty"`
	LandedAt   *string     `json:"landed_at,omitempty"`
}

var (
	shaRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

type Landing struct {
	Kind    string
	Target  string // url or sha
	Time    string
	EventID int
}

func findLatestLanding(evs []core.Event) *Landing {
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Kind == core.EventPr {
			body := strings.TrimSpace(e.Body)
			if strings.HasPrefix(body, "pull-request ") {
				return &Landing{Kind: "pull-request", Target: strings.TrimSpace(strings.TrimPrefix(body, "pull-request ")), Time: e.At, EventID: e.ID}
			}
			if strings.HasPrefix(body, "commit ") {
				return &Landing{Kind: "commit", Target: strings.TrimSpace(strings.TrimPrefix(body, "commit ")), Time: e.At, EventID: e.ID}
			}
			if strings.HasPrefix(body, "pr ") {
				return &Landing{Kind: "pull-request", Target: strings.TrimSpace(strings.TrimPrefix(body, "pr ")), Time: e.At, EventID: e.ID}
			}
			// fallback
			if strings.Contains(body, "http") {
				return &Landing{Kind: "pull-request", Target: body, Time: e.At, EventID: e.ID}
			}
			if shaRe.MatchString(body) {
				return &Landing{Kind: "commit", Target: body, Time: e.At, EventID: e.ID}
			}
		}
	}
	return nil
}

func isCommitOnBase(cwd, base, sha string) (bool, error) {
	if sha == "" {
		return false, nil
	}
	// Try to see if sha is reachable from base
	_, err := run(cwd, "git", "merge-base", "--is-ancestor", sha, base)
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// exit code 1 means not ancestor
		if exitErr.ExitCode() == 1 {
			return false, nil
		}
	}
	// try contains
	out, err2 := run(cwd, "git", "branch", "-r", "--contains", sha)
	if err2 != nil {
		return false, err
	}
	baseRef := "origin/" + base
	if strings.Contains(out, baseRef) || strings.Contains(out, base) {
		return true, nil
	}
	return false, nil
}

func run(cwd string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	var b bytes.Buffer
	cmd.Stdout = &b
	cmd.Stderr = &b
	err := cmd.Run()
	return strings.TrimSpace(b.String()), err
}

func getBranch(cwd string) (string, error) {
	out, err := run(cwd, "git", "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func baseBranch(cwd string) string {
	// try origin/HEAD
	out, err := run(cwd, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil && strings.Contains(out, "refs/remotes/origin/") {
		return strings.TrimPrefix(strings.TrimSpace(out), "refs/remotes/origin/")
	}
	for _, c := range []string{"main", "master"} {
		if _, err := run(cwd, "git", "rev-parse", "--verify", "origin/"+c); err == nil {
			return c
		}
	}
	return "main"
}

func commitsAheadBehind(cwd, branch, base string) (ahead, behind int, err error) {
	out, err := run(cwd, "git", "rev-list", "--left-right", "--count", "origin/"+base+"..."+branch)
	if err != nil {
		out, err = run(cwd, "git", "rev-list", "--left-right", "--count", base+"..."+branch)
		if err != nil {
			return 0, 0, err
		}
	}
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		parts := strings.Fields(line)
		if len(parts) == 2 {
			fmt.Sscan(parts[0], &behind)
			fmt.Sscan(parts[1], &ahead)
			return
		}
	}
	return
}

func StatusFor(cwd string, evs []core.Event, work core.Work, tasks []core.Work) State {
	st := State{Status: StatusNone, BaseBranch: baseBranch(cwd)}
	br, err := getBranch(cwd)
	if err == nil {
		st.Branch = br
	}

	land := findLatestLanding(evs)
	if land != nil {
		st.LandedAt = &land.Time
		if land.Kind == "commit" {
			onBase, err := isCommitOnBase(cwd, st.BaseBranch, land.Target)
			if err == nil && onBase {
				st.Status = StatusLandedCommit
				st.Detail = "commit on " + st.BaseBranch + ": " + land.Target
			} else {
				st.Status = StatusCommittedUnpushed
				st.Detail = "commit recorded: " + land.Target
			}
		} else if land.Kind == "pull-request" {
			// Without API, we can't know draft/open/merged reliably; record as PR recorded
			st.Status = StatusPrOpen // best guess unknown -> open/recorded, but be conservative
			st.Detail = "PR recorded: " + land.Target
			// if merged commit on base, prefer landed-pr
			if strings.Contains(land.Target, "commit") || shaRe.MatchString(land.Target) {
				onBase, err := isCommitOnBase(cwd, st.BaseBranch, land.Target)
				if err == nil && onBase {
					st.Status = StatusLandedPR
					st.Detail = "PR merged/commit on base: " + land.Target
				}
			}
		}
	} else {
		if st.Branch != "" && st.BaseBranch != "" {
			ahead, behind, err := commitsAheadBehind(cwd, st.Branch, st.BaseBranch)
			if err == nil {
				if ahead > 0 && behind == 0 {
					st.Status = StatusCommittedUnpushed
					st.Detail = fmt.Sprintf("%d commits ahead of origin/%s", ahead, st.BaseBranch)
				} else if ahead > 0 {
					st.Status = StatusPushedUnpr
					st.Detail = fmt.Sprintf("%d ahead, %d behind origin/%s", ahead, behind, st.BaseBranch)
				}
			}
		}
	}

	// shipped if landed and work accounted for
	landed := st.Status == StatusLandedCommit || st.Status == StatusLandedPR || st.Status == StatusPrMerged
	if landed {
		if work.State == core.StateDone {
			st.Status = StatusShipped
		}
		// keep landed variants if not done yet (ready-to-close)
	}

	// attention
	att := []Attention{}
	if landed && work.State != core.StateDone {
		att = append(att, AttentionReadyToClose)
	}
	sort.Slice(att, func(i, j int) bool { return string(att[i]) < string(att[j]) })
	st.Attention = att
	return st
}

func ProjectRoot(start string) (string, error) {
	cur, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, ".git")); err == nil {
			return cur, nil
		}
		p := filepath.Dir(cur)
		if p == cur {
			return "", errors.New("no git repo found")
		}
		cur = p
	}
}
