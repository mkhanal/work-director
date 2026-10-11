package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

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
	StatusLandedPR          Status = "landed-pr"
	StatusLandedCommit      Status = "landed-commit"
	StatusShipped           Status = "shipped"
)

type Attention string

const AttentionReadyToClose Attention = "ready-to-close"

// State is where a goal's work reached. Error says what could not be read;
// Status then says only what could.
type State struct {
	Status     Status      `json:"status"`
	Detail     string      `json:"detail"`
	Attention  []Attention `json:"attention"`
	BaseBranch string      `json:"base_branch,omitempty"`
	Branch     string      `json:"branch,omitempty"`
	LandedAt   *string     `json:"landed_at,omitempty"`
	Error      string      `json:"error,omitempty"`
}

var (
	shaRe       = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	commitURLRe = regexp.MustCompile(`/commit/([0-9a-f]{7,40})/?$`)
	githubRe    = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/(pull|commit)/([0-9a-f]+)/?$`)
)

// landing is the last landing the events record: a commit's sha, or a pull
// request's link.
type landing struct {
	commit string
	pr     string
	at     string
}

func lastLanding(evs []core.Event) (landing, bool) {
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Kind != core.EventPr {
			continue
		}
		kind, target, _ := strings.Cut(strings.TrimSpace(e.Body), " ")
		if target == "" {
			kind, target = "", kind
		}
		if kind == string(core.LandingCommit) || shaRe.MatchString(target) || commitURLRe.MatchString(target) {
			sha := target
			if m := commitURLRe.FindStringSubmatch(target); m != nil {
				sha = m[1]
			}
			return landing{commit: sha, at: e.At}, true
		}
		return landing{pr: target, at: e.At}, true
	}
	return landing{}, false
}

// git runs git in dir and returns its trimmed output, or an error carrying
// what git said.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("git %s: %s", strings.Join(args, " "), firstLine(text, err))
	}
	return text, nil
}

func firstLine(text string, err error) string {
	if line, _, _ := strings.Cut(text, "\n"); line != "" {
		return line
	}
	return err.Error()
}

// baseBranch is the branch origin's HEAD names, else main or master where
// origin has one.
func baseBranch(dir string) (string, error) {
	if out, err := git(dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(out, "origin/"), nil
	}
	for _, b := range []string{"main", "master"} {
		if _, err := git(dir, "rev-parse", "--verify", "--quiet", "origin/"+b); err == nil {
			return b, nil
		}
	}
	return "", errors.New("origin names no base branch: no origin/HEAD, origin/main or origin/master")
}

// contains reports whether ref has commit in its history.
func contains(dir, ref, commit string) (bool, error) {
	cmd := exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", commit, ref)
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	default:
		return false, fmt.Errorf("git merge-base %s %s: %s", commit, ref, firstLine(strings.TrimSpace(string(out)), err))
	}
}

// prState asks gh for a pull request's state.
func prState(url string) (Status, error) {
	out, err := exec.Command("gh", "pr", "view", url, "--json", "state,isDraft").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr view %s: %s", url, firstLine(strings.TrimSpace(string(out)), err))
	}
	var pr struct {
		State   string `json:"state"`
		IsDraft bool   `json:"isDraft"`
	}
	if err := json.Unmarshal(out, &pr); err != nil {
		return "", fmt.Errorf("gh pr view %s: %w", url, err)
	}
	switch {
	case pr.State == "MERGED":
		return StatusLandedPR, nil
	case pr.State == "CLOSED":
		return StatusPrClosedUnmerged, nil
	case pr.IsDraft:
		return StatusPrDraft, nil
	default:
		return StatusPrOpen, nil
	}
}

// StatusFor is where the work of a goal working in dir reached.
func StatusFor(dir string, evs []core.Event, work core.Work, tasks []core.Work) State {
	st := State{Status: StatusNone, Attention: []Attention{}}
	fail := func(err error) State {
		st.Error = err.Error()
		return st
	}
	if br, err := git(dir, "branch", "--show-current"); err == nil {
		st.Branch = br
	} else {
		return fail(err)
	}
	base, err := baseBranch(dir)
	if err != nil {
		return fail(err)
	}
	st.BaseBranch = base
	if land, ok := lastLanding(evs); ok {
		st.LandedAt = &land.at
		if land.pr != "" {
			status, err := prState(land.pr)
			if err != nil {
				st.Detail = "pull request " + land.pr
				return fail(err)
			}
			st.Status, st.Detail = status, "pull request "+land.pr
		} else {
			on, err := contains(dir, "origin/"+base, land.commit)
			if err != nil {
				st.Detail = "commit " + land.commit
				return fail(err)
			}
			st.Status = StatusCommittedUnpushed
			st.Detail = "commit " + land.commit + " is not on origin/" + base
			if on {
				st.Status, st.Detail = StatusLandedCommit, "commit "+land.commit+" is on origin/"+base
			}
		}
	} else if st.Branch != "" {
		ahead, err := git(dir, "rev-list", "--count", "origin/"+base+".."+st.Branch)
		if err != nil {
			return fail(err)
		}
		n, err := strconv.Atoi(ahead)
		if err != nil {
			return fail(fmt.Errorf("git rev-list --count: %q is not a count", ahead))
		}
		if n > 0 {
			st.Status = StatusCommittedUnpushed
			st.Detail = fmt.Sprintf("%d commit(s) not on origin/%s", n, base)
			if upstream, err := git(dir, "rev-parse", "--abbrev-ref", st.Branch+"@{upstream}"); err == nil {
				if pushed, err := contains(dir, upstream, st.Branch); err != nil {
					return fail(err)
				} else if pushed {
					st.Status = StatusPushedUnpr
					st.Detail = fmt.Sprintf("%d commit(s) pushed to %s, not on origin/%s", n, upstream, base)
				}
			}
		}
	}
	if st.Status == StatusLandedCommit || st.Status == StatusLandedPR {
		if work.State == core.StateDone {
			st.Status = StatusShipped
		} else {
			st.Attention = append(st.Attention, AttentionReadyToClose)
		}
	}
	return st
}

// Resolve checks that a landing link points at something that exists: a
// GitHub pull request or commit gh can read, or any other https link that
// answers below 400.
func Resolve(url string) error {
	if m := githubRe.FindStringSubmatch(url); m != nil {
		var cmd *exec.Cmd
		if m[3] == "pull" {
			cmd = exec.Command("gh", "pr", "view", url, "--json", "number")
		} else {
			cmd = exec.Command("gh", "api", fmt.Sprintf("repos/%s/%s/commits/%s", m[1], m[2], m[4]), "--jq", ".sha")
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s does not resolve: %s", url, firstLine(strings.TrimSpace(string(out)), err))
		}
		return nil
	}
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("%s is not an https link", url)
	}
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("%s does not resolve: %v", url, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("%s does not resolve: it answers %s", url, resp.Status)
	}
	return nil
}
